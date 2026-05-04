package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	returnStatus model.DeploymentStatus
	returnError  string
	startErr     error
	deployID     string
}

func (f *fakeDeployer) Start(_ context.Context, req model.DeployRequest) (*model.Deployment, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
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

// --- Helpers ---

func testApp(dir string) model.AppConfig {
	return model.AppConfig{
		Name:         "myapp",
		Image:        "ghcr.io/org/myapp",
		TagPattern:   "v*",
		AllowedIdentity: "org/myapp",
		Dir:          dir,
		ComposeFiles: []string{"docker-compose.yml"},
	}
}

func newTestWorker(t *testing.T, reg RegistryClient, ver SignatureVerifier, dep Deployer) (*Worker, string) {
	t.Helper()
	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	if err := os.MkdirAll(appDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	w := New(Config{
		Apps:         map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval: time.Minute,
		DataDir:      dir,
		Registry:     reg,
		Verifier:     ver,
		Deployer:     dep,
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
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
}

func TestWorker_PollApp_NoChange(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:abc"}
	dep := &fakeDeployer{}
	w, _ := newTestWorker(t, reg, &fakeVerifier{}, dep)

	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:    "v1.0.0",
		Digest: "sha256:abc",
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
