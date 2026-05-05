package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/deploy"
	"github.com/go-sum/furnace/internal/model"
)

// --- Fakes ---

type fakeRegistry struct {
	tag    string
	digest string
	err    error
}

func (f *fakeRegistry) LatestTag(_ context.Context, _, _ string) (string, string, error) {
	return f.tag, f.digest, f.err
}

type fakeVerifier struct{ err error }

func (f *fakeVerifier) Verify(_ context.Context, _, _ string) error { return f.err }

type fakeDeployer struct {
	returnStatus   model.DeploymentStatus
	returnError    string
	startErr       error
	deployID       string
	lastImage      string
	lastArtifactDigest string
}

func (f *fakeDeployer) Start(_ context.Context, req model.DeployRequest) (*model.Deployment, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.lastImage = req.Image
	f.lastArtifactDigest = req.ArtifactDigest
	id := f.deployID
	if id == "" {
		id = "deploy-" + req.Tag
	}
	status := f.returnStatus
	if status == "" {
		status = model.StatusCompleted
	}
	return &model.Deployment{
		ID:      id,
		AppName: req.AppName,
		Image:   req.Image,
		Tag:     req.Tag,
		Digest:  req.Digest,
		Status:  status,
	}, nil
}

func (f *fakeDeployer) Status(_ context.Context, appName string) (*model.Deployment, error) {
	status := f.returnStatus
	if status == "" {
		status = model.StatusCompleted
	}
	return &model.Deployment{
		ID:      f.deployID,
		AppName: appName,
		Status:  status,
		Error:   f.returnError,
	}, nil
}

type fakeArtifactFetcher struct {
	digest        string
	resolveDigest string
	err           error
	resolveErr    error
}

func (f *fakeArtifactFetcher) FetchAndVerify(_ context.Context, _, _, _ string) (string, error) {
	d := f.digest
	if d == "" {
		d = "sha256:fakedigest"
	}
	return d, f.err
}

func (f *fakeArtifactFetcher) ResolveDigest(_ context.Context, _ string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	d := f.resolveDigest
	if d == "" {
		d = f.digest
	}
	if d == "" {
		d = "sha256:fakedigest"
	}
	return d, nil
}

// --- Helpers ---

func testApp(dir string) model.AppConfig {
	return model.AppConfig{
		Name:            "myapp",
		Image:           "ghcr.io/org/myapp",
		TagPattern:      "v*",
		AllowedIdentity: "org/myapp",
		Dir:             dir,
		Artifact:        "ghcr.io/org/myapp:{tag}-compose",
	}
}

func newTestWorker(t *testing.T, reg RegistryClient, ver SignatureVerifier, dep Deployer) (*Worker, string) {
	t.Helper()
	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	if err := os.MkdirAll(appDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rm := deploy.NewReleaseManager(slog.Default())
	w := New(Config{
		Apps:            map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval:    time.Minute,
		DataDir:         dir,
		Registry:        reg,
		Verifier:        ver,
		Deployer:        dep,
		ArtifactFetcher: &fakeArtifactFetcher{},
		Releases:        rm,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	})
	return w, appDir
}

// --- Tests ---

func TestWorker_PollApp_HappyPath(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{deployID: "deploy-v1.0.0", returnStatus: model.StatusCompleted}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, dep)

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}

	state, err := w.states.Load(context.Background(), "myapp")
	if err != nil || state == nil {
		t.Fatalf("state not saved: %v", err)
	}
	if state.Tag != "v1.0.0" || state.Digest != "sha256:abc" {
		t.Fatalf("wrong state: %+v", state)
	}
	if state.ArtifactDigest != "sha256:fakedigest" {
		t.Fatalf("expected artifact digest in state, got: %q", state.ArtifactDigest)
	}

	if !strings.Contains(dep.lastImage, "@sha256:") {
		t.Fatalf("expected image pinned by digest, got: %q", dep.lastImage)
	}
	if dep.lastArtifactDigest != "sha256:fakedigest" {
		t.Fatalf("expected artifact digest passed to deployer, got: %q", dep.lastArtifactDigest)
	}
}

func TestWorker_PollApp_NoChange(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, dep)

	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:            "v1.0.0",
		Digest:         "sha256:abc",
		ArtifactDigest: "sha256:fakedigest",
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}
	// Deployer Start must not have been called (deployID stays empty in state).
	state, _ := w.states.Load(context.Background(), "myapp")
	if state.Tag != "v1.0.0" && state.Digest != "sha256:abc" {
		t.Fatal("state should be unchanged")
	}
}

func TestWorker_PollApp_SignatureFailure(t *testing.T) {
	reg := &fakeRegistry{tag: "v2.0.0", digest: "sha256:newdigest"}
	dep := &fakeDeployer{}
	ver := &fakeVerifier{err: model.ErrSignatureInvalid}
	w, _ := newTestWorker(t, reg, ver, dep)

	err := w.pollApp(context.Background(), w.apps["myapp"])
	if err == nil {
		t.Fatal("expected error on signature failure")
	}
	if !errors.Is(err, model.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got: %v", err)
	}
}

func TestWorker_PollApp_RegistryError(t *testing.T) {
	reg := &fakeRegistry{err: errors.New("network timeout")}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, &fakeDeployer{})
	if err := w.pollApp(context.Background(), w.apps["myapp"]); err == nil {
		t.Fatal("expected error on registry failure")
	}
}

func TestWorker_PollApp_DeployFailed(t *testing.T) {
	reg := &fakeRegistry{tag: "v2.0.0", digest: "sha256:new"}
	dep := &fakeDeployer{
		deployID:     "fail-deploy",
		returnStatus: model.StatusFailed,
		returnError:  "container crash",
	}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, dep)

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err == nil {
		t.Fatal("expected error when deploy fails")
	}

	// State must not be saved on failed deploy.
	state, _ := w.states.Load(context.Background(), "myapp")
	if state != nil {
		t.Fatalf("state should not be saved on failure, got: %+v", state)
	}
}

func TestWorker_PollApp_ArtifactFetchFailure(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{}
	fetcher := &fakeArtifactFetcher{err: errors.New("registry unavailable")}

	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	os.MkdirAll(appDir, 0750)
	rm := deploy.NewReleaseManager(slog.Default())

	w := New(Config{
		Apps:            map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval:    time.Minute,
		DataDir:         dir,
		Registry:        reg,
		Verifier:        &fakeVerifier{},
		Deployer:        dep,
		ArtifactFetcher: fetcher,
		Releases:        rm,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	})

	err := w.pollApp(context.Background(), w.apps["myapp"])
	if err == nil {
		t.Fatal("expected error on artifact fetch failure")
	}
	if !strings.Contains(err.Error(), "fetch artifact") {
		t.Fatalf("expected 'fetch artifact' in error, got: %v", err)
	}
	// Deploy must not have been triggered.
	if dep.lastImage != "" {
		t.Fatal("deployer should not have been called after fetch failure")
	}
	// No staging dirs should remain.
	relDir := filepath.Join(appDir, ".furnace", "releases")
	if entries, err := os.ReadDir(relDir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".staging-") {
				t.Fatalf("staging dir not cleaned up: %s", e.Name())
			}
		}
	}
}

func TestWorker_DrainHints(t *testing.T) {
	pollCount := 0
	reg := &countingRegistry{
		inner: &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"},
		count: &pollCount,
	}
	dep := &fakeDeployer{deployID: "d1", returnStatus: model.StatusCompleted}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, dep)

	hintDir := filepath.Join(w.dataDir, "hints")
	if err := os.MkdirAll(hintDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hintDir, "myapp"), nil, 0640); err != nil {
		t.Fatal(err)
	}

	w.drainHints(context.Background())

	if pollCount != 1 {
		t.Fatalf("expected 1 poll from hint, got %d", pollCount)
	}
	if _, err := os.Stat(filepath.Join(hintDir, "myapp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("hint file not removed after drain")
	}
}

func TestWorker_DrainHints_UnknownApp(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, &fakeDeployer{})

	hintDir := filepath.Join(w.dataDir, "hints")
	os.MkdirAll(hintDir, 0750)
	os.WriteFile(filepath.Join(hintDir, "unknown-app"), nil, 0640)

	// Must not panic or error; just removes the hint file.
	w.drainHints(context.Background())
	if _, err := os.Stat(filepath.Join(hintDir, "unknown-app")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("hint file for unknown app should still be removed")
	}
}

// countingRegistry wraps a RegistryClient and counts LatestTag calls.
type countingRegistry struct {
	inner RegistryClient
	count *int
}

func (c *countingRegistry) LatestTag(ctx context.Context, repo, pattern string) (string, string, error) {
	*c.count++
	return c.inner.LatestTag(ctx, repo, pattern)
}

func TestWorker_PollApp_CleansStaleStagingDirs(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{deployID: "deploy-v1.0.0", returnStatus: model.StatusCompleted}
	w, appDir := newTestWorker(t, reg, &fakeVerifier{}, dep)

	// Create stale staging dirs manually.
	relDir := filepath.Join(appDir, ".furnace", "releases")
	if err := os.MkdirAll(relDir, 0755); err != nil {
		t.Fatalf("mkdir releases: %v", err)
	}
	stale1 := filepath.Join(relDir, ".staging-aaaa")
	stale2 := filepath.Join(relDir, ".staging-bbbb")
	if err := os.Mkdir(stale1, 0755); err != nil {
		t.Fatalf("mkdir stale1: %v", err)
	}
	if err := os.Mkdir(stale2, 0755); err != nil {
		t.Fatalf("mkdir stale2: %v", err)
	}

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}

	if _, err := os.Stat(stale1); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale1 to be removed")
	}
	if _, err := os.Stat(stale2); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale2 to be removed")
	}
}

func TestWorker_PollApp_ArtifactOnlyChange(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{deployID: "deploy-v1.0.0", returnStatus: model.StatusCompleted}
	fetcher := &fakeArtifactFetcher{
		digest:        "sha256:new-artifact",
		resolveDigest: "sha256:new-artifact",
	}

	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	os.MkdirAll(appDir, 0750)
	rm := deploy.NewReleaseManager(slog.Default())
	w := New(Config{
		Apps:            map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval:    time.Minute,
		DataDir:         dir,
		Registry:        reg,
		Verifier:        &fakeVerifier{},
		Deployer:        dep,
		ArtifactFetcher: fetcher,
		Releases:        rm,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	})

	// Save state with matching image digest but stale artifact digest.
	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:            "v1.0.0",
		Digest:         "sha256:abc",
		ArtifactDigest: "sha256:old-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}

	if dep.lastImage == "" {
		t.Fatal("expected deployer Start to be called on artifact-only change")
	}

	state, _ := w.states.Load(context.Background(), "myapp")
	if state == nil || state.ArtifactDigest != "sha256:new-artifact" {
		t.Fatalf("expected updated artifact digest in state, got: %+v", state)
	}
}

func TestWorker_PollApp_ArtifactUnchanged(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{}
	fetcher := &fakeArtifactFetcher{resolveDigest: "sha256:same-artifact"}

	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	os.MkdirAll(appDir, 0750)
	rm := deploy.NewReleaseManager(slog.Default())
	w := New(Config{
		Apps:            map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval:    time.Minute,
		DataDir:         dir,
		Registry:        reg,
		Verifier:        &fakeVerifier{},
		Deployer:        dep,
		ArtifactFetcher: fetcher,
		Releases:        rm,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	})

	// Save state with both image and artifact digest matching current registry state.
	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:            "v1.0.0",
		Digest:         "sha256:abc",
		ArtifactDigest: "sha256:same-artifact",
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}

	if dep.lastImage != "" {
		t.Fatal("deployer should not be called when image and artifact are both unchanged")
	}
}

func TestWorker_PollApp_ArtifactResolveFailure(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{}
	fetcher := &fakeArtifactFetcher{resolveErr: errors.New("registry unavailable")}

	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	os.MkdirAll(appDir, 0750)
	rm := deploy.NewReleaseManager(slog.Default())
	w := New(Config{
		Apps:            map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval:    time.Minute,
		DataDir:         dir,
		Registry:        reg,
		Verifier:        &fakeVerifier{},
		Deployer:        dep,
		ArtifactFetcher: fetcher,
		Releases:        rm,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	})

	// Save state with matching image digest so we enter the artifact-check branch.
	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:    "v1.0.0",
		Digest: "sha256:abc",
	}); err != nil {
		t.Fatal(err)
	}

	err := w.pollApp(context.Background(), w.apps["myapp"])
	if err == nil {
		t.Fatal("expected error when artifact ResolveDigest fails")
	}
	if !strings.Contains(err.Error(), "resolve artifact digest") {
		t.Fatalf("expected 'resolve artifact digest' in error, got: %v", err)
	}
}
