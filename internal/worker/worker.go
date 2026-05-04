package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-sum/furnace/internal/deploy"
	"github.com/go-sum/furnace/internal/model"
)

// RegistryClient polls a container registry for new image versions.
type RegistryClient interface {
	LatestTag(ctx context.Context, imageRepo, pattern string) (tag, digest string, err error)
}

// SignatureVerifier checks Sigstore signatures on container images.
type SignatureVerifier interface {
	Verify(ctx context.Context, imageRef, allowedIdentity string) error
}

// Deployer starts and monitors deployments.
type Deployer interface {
	Start(ctx context.Context, req model.DeployRequest) (*model.Deployment, error)
	Status(ctx context.Context, appName string) (*model.Deployment, error)
}

// ComposeArtifactFetcher fetches and verifies a compose OCI artifact,
// writing compose files to the app directory.
type ComposeArtifactFetcher interface {
	FetchAndVerify(ctx context.Context, artifactRef, allowedIdentity, destDir string) error
}

// Config holds all Worker dependencies.
type Config struct {
	Apps           map[string]model.AppConfig
	PollInterval   time.Duration
	DataDir        string
	Registry       RegistryClient
	Verifier       SignatureVerifier
	Deployer       Deployer
	ComposeFetcher ComposeArtifactFetcher
	Logger         *slog.Logger
}

// Worker polls GHCR for new image versions, verifies Sigstore signatures,
// and triggers deploys via the deploy service.
type Worker struct {
	apps           map[string]model.AppConfig
	pollInterval   time.Duration
	dataDir        string
	registry       RegistryClient
	verifier       SignatureVerifier
	deployer       Deployer
	composeFetcher ComposeArtifactFetcher
	logger         *slog.Logger
	states         *stateStore
}

// New creates a Worker with the given configuration.
func New(cfg Config) *Worker {
	return &Worker{
		apps:           cfg.Apps,
		pollInterval:   cfg.PollInterval,
		dataDir:        cfg.DataDir,
		registry:       cfg.Registry,
		verifier:       cfg.Verifier,
		deployer:       cfg.Deployer,
		composeFetcher: cfg.ComposeFetcher,
		logger:         cfg.Logger,
		states:         newStateStore(filepath.Join(cfg.DataDir, "state")),
	}
}

// Run starts the poll loop. It polls all apps immediately on startup, then on
// each pollInterval tick. It also checks for hint files every second to
// short-circuit the interval when a deploy hint arrives from furnace-web.
// Run blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	pollTicker := time.NewTicker(w.pollInterval)
	hintTicker := time.NewTicker(time.Second)
	defer pollTicker.Stop()
	defer hintTicker.Stop()

	w.pollAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollTicker.C:
			w.pollAll(ctx)
		case <-hintTicker.C:
			w.drainHints(ctx)
		}
	}
}

func (w *Worker) pollAll(ctx context.Context) {
	// Deterministic order for consistent logging.
	names := make([]string, 0, len(w.apps))
	for n := range w.apps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := w.pollApp(ctx, w.apps[n]); err != nil {
			w.logger.Error("poll failed", "app", n, "error", err)
		}
	}
}

// drainHints reads any hint files written by furnace-web and triggers an
// immediate poll for the affected app. Hint files are deleted after being read.
func (w *Worker) drainHints(ctx context.Context) {
	hintDir := filepath.Join(w.dataDir, "hints")
	entries, err := os.ReadDir(hintDir)
	if err != nil {
		return // hint dir not yet created
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		appName := e.Name()
		os.Remove(filepath.Join(hintDir, appName))
		app, ok := w.apps[appName]
		if !ok {
			continue
		}
		w.logger.Info("hint triggered poll", "app", appName)
		if err := w.pollApp(ctx, app); err != nil {
			w.logger.Error("hint poll failed", "app", appName, "error", err)
		}
	}
}

// pollApp checks whether a new version of app is available and, if so, verifies
// and deploys it. It is safe to call concurrently for different apps; the deploy
// service enforces per-app serialization via its own lock.
func (w *Worker) pollApp(ctx context.Context, app model.AppConfig) error {
	tag, digest, err := w.registry.LatestTag(ctx, app.Image, app.TagPattern)
	if err != nil {
		return fmt.Errorf("get latest tag: %w", err)
	}

	state, err := w.states.Load(ctx, app.Name)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if state != nil && state.Digest == digest {
		return nil // nothing changed
	}

	w.logger.Info("new version detected", "app", app.Name, "tag", tag, "digest", digest)

	imageRef := fmt.Sprintf("%s:%s@%s", app.Image, tag, digest)

	if err := w.verifier.Verify(ctx, imageRef, app.AllowedIdentity); err != nil {
		return fmt.Errorf("signature verification: %w", err)
	}
	w.logger.Info("signature verified", "app", app.Name, "tag", tag)

	if app.ComposeArtifact != "" && w.composeFetcher != nil {
		artifactRef := deploy.ResolveArtifactRef(app.ComposeArtifact, tag)
		if err := w.composeFetcher.FetchAndVerify(ctx, artifactRef, app.AllowedIdentity, app.Dir); err != nil {
			return fmt.Errorf("fetch compose artifact: %w", err)
		}
		w.logger.Info("compose artifact synced", "app", app.Name, "tag", tag)
	}

	req := model.DeployRequest{
		AppName: app.Name,
		Image:   fmt.Sprintf("%s:%s", app.Image, tag),
		Tag:     tag,
		Digest:  digest,
	}

	d, err := w.deployer.Start(ctx, req)
	if err != nil {
		return fmt.Errorf("start deploy: %w", err)
	}

	final, err := w.waitForDeploy(ctx, app.Name, d.ID)
	if err != nil {
		return fmt.Errorf("wait for deploy: %w", err)
	}

	if final.Status != model.StatusCompleted {
		return fmt.Errorf("deploy failed: %s", final.Error)
	}

	w.logger.Info("deploy completed", "app", app.Name, "tag", tag)
	return w.states.Save(ctx, app.Name, &AppState{
		Tag:        tag,
		Digest:     digest,
		DeployedAt: time.Now(),
	})
}

// waitForDeploy polls Status until the deployment with deployID reaches a terminal state.
func (w *Worker) waitForDeploy(ctx context.Context, appName, deployID string) (*model.Deployment, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		d, err := w.deployer.Status(ctx, appName)
		if err != nil {
			return nil, fmt.Errorf("status: %w", err)
		}
		if d != nil && d.ID == deployID && d.Status.IsTerminal() {
			return d, nil
		}
	}
}
