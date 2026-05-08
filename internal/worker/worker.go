package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-sum/furnace/internal/artifact"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/release"
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

// ArtifactFetcher fetches and verifies a compose OCI artifact,
// writing compose files to destDir and returning the manifest digest.
type ArtifactFetcher interface {
	FetchAndVerify(ctx context.Context, artifactRef, allowedIdentity, destDir string) (string, error)
	ResolveDigest(ctx context.Context, artifactRef string) (string, error)
}

// Config holds all Worker dependencies.
type Config struct {
	Apps            map[string]model.AppConfig
	PollInterval    time.Duration
	DataDir         string
	Registry        RegistryClient
	Verifier        SignatureVerifier
	Deployer        Deployer
	ArtifactFetcher ArtifactFetcher
	Releases        *release.ReleaseManager
	Logger          *slog.Logger
}

// Worker polls GHCR for new image versions, verifies Sigstore signatures,
// and triggers deploys via the deploy service.
type Worker struct {
	apps            map[string]model.AppConfig
	pollInterval    time.Duration
	dataDir         string
	registry        RegistryClient
	verifier        SignatureVerifier
	deployer        Deployer
	artifactFetcher ArtifactFetcher
	releases        *release.ReleaseManager
	logger          *slog.Logger
	states          *stateStore
	backoffs        map[string]*backoff
}

// New creates a Worker with the given configuration.
func New(cfg Config) *Worker {
	boffs := make(map[string]*backoff, len(cfg.Apps))
	for name := range cfg.Apps {
		boffs[name] = &backoff{}
	}
	return &Worker{
		apps:            cfg.Apps,
		pollInterval:    cfg.PollInterval,
		dataDir:         cfg.DataDir,
		registry:        cfg.Registry,
		verifier:        cfg.Verifier,
		deployer:        cfg.Deployer,
		artifactFetcher: cfg.ArtifactFetcher,
		releases:        cfg.Releases,
		logger:          cfg.Logger,
		states:          newStateStore(filepath.Join(cfg.DataDir, "state")),
		backoffs:        boffs,
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
		b := w.backoffs[n]
		if !b.ready() {
			w.logger.Debug("poll deferred by backoff", "app", n, "failures", b.failures)
			continue
		}
		if err := w.pollApp(ctx, w.apps[n]); err != nil {
			w.logger.Error("poll failed", "app", n, "error", err)
			b.record(w.pollInterval)
		} else {
			b.reset()
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
		b := w.backoffs[appName]
		b.reset()
		if err := w.pollApp(ctx, app); err != nil {
			w.logger.Error("hint poll failed", "app", appName, "error", err)
			b.record(w.pollInterval)
		}
	}
}

// pollApp checks whether a new version of app is available and, if so, verifies
// and deploys it. It is safe to call concurrently for different apps; the deploy
// service enforces per-app serialization via its own lock.
func (w *Worker) pollApp(ctx context.Context, app model.AppConfig) error {
	if err := w.releases.CleanupStaleStagingDirs(app.Dir); err != nil {
		w.logger.Warn("cleanup stale staging dirs failed", "app", app.Name, "error", err)
	}

	tag, digest, err := w.registry.LatestTag(ctx, app.Image, app.TagPattern)
	if err != nil {
		return fmt.Errorf("get latest tag: %w", err)
	}

	state, err := w.states.Load(ctx, app.Name)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if state != nil && state.Digest == digest {
		artifactRef := artifact.ResolveArtifactRef(app.Artifact, tag)
		currentArtDigest, err := w.artifactFetcher.ResolveDigest(ctx, artifactRef)
		if err != nil {
			return fmt.Errorf("resolve artifact digest: %w", err)
		}
		if currentArtDigest == state.ArtifactDigest {
			return nil
		}
		w.logger.Info("artifact change detected", "app", app.Name, "tag", tag)
	} else {
		w.logger.Info("new version detected", "app", app.Name, "tag", tag, "digest", digest)
		imageRef := fmt.Sprintf("%s:%s@%s", app.Image, tag, digest)
		if err := w.verifier.Verify(ctx, imageRef, app.AllowedIdentity); err != nil {
			return fmt.Errorf("signature verification: %w", err)
		}
		w.logger.Info("signature verified", "app", app.Name, "tag", tag)
	}

	artifactRef := artifact.ResolveArtifactRef(app.Artifact, tag)

	stagingDir, err := w.releases.CreateStagingDir(app.Dir)
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	artifactDigest, err := w.artifactFetcher.FetchAndVerify(ctx, artifactRef, app.AllowedIdentity, stagingDir)
	if err != nil {
		w.releases.CleanupStagingDir(stagingDir)
		return fmt.Errorf("fetch artifact: %w", err)
	}

	if err := w.releases.CommitStaging(app.Dir, stagingDir, artifactDigest); err != nil {
		w.releases.CleanupStagingDir(stagingDir)
		return fmt.Errorf("commit staging: %w", err)
	}

	w.logger.Info("artifact staged", "app", app.Name, "tag", tag, "artifact_digest", artifactDigest)

	req := model.DeployRequest{
		AppName:        app.Name,
		Image:          fmt.Sprintf("%s@%s", app.Image, digest),
		Tag:            tag,
		Digest:         digest,
		ArtifactDigest: artifactDigest,
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
		Tag:            tag,
		Digest:         digest,
		ArtifactDigest: artifactDigest,
		DeployedAt:     time.Now(),
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
