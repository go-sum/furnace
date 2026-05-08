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

	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/release"
)

type fakeRegistry struct {
	tag    string
	digest string
	err    error
	hits   int
}

func (f *fakeRegistry) LatestTag(_ context.Context, _, _ string) (string, string, error) {
	f.hits++
	return f.tag, f.digest, f.err
}

type fakeVerifier struct {
	err  error
	hits int
}

func (f *fakeVerifier) Verify(_ context.Context, _, _ string) error {
	f.hits++
	return f.err
}

type fakeArtifactFetcher struct {
	digest        string
	resolveDigest string
	err           error
	resolveErr    error
	hits          int
	resolveHits   int
}

func (f *fakeArtifactFetcher) FetchAndVerify(_ context.Context, _, _, destDir string) (string, error) {
	f.hits++
	if f.err != nil {
		return "", f.err
	}
	if err := os.WriteFile(filepath.Join(destDir, "docker-compose.yml"), []byte("services: {}\n"), 0644); err != nil {
		return "", err
	}
	if f.digest == "" {
		return "sha256:compose123", nil
	}
	return f.digest, nil
}

func (f *fakeArtifactFetcher) ResolveDigest(_ context.Context, _ string) (string, error) {
	f.resolveHits++
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	if f.resolveDigest != "" {
		return f.resolveDigest, nil
	}
	if f.digest != "" {
		return f.digest, nil
	}
	return "sha256:compose123", nil
}

type fakeDeployer struct {
	startErr           error
	status             model.DeploymentStatus
	returnError        string
	deployID           string
	startHits          int
	lastImage          string
	lastDigest         string
	lastArtifactDigest string
}

func (f *fakeDeployer) Start(_ context.Context, req model.DeployRequest) (*model.Deployment, error) {
	f.startHits++
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.lastImage = req.Image
	f.lastDigest = req.Digest
	f.lastArtifactDigest = req.ArtifactDigest
	id := f.deployID
	if id == "" {
		id = "deploy-" + req.Tag
	}
	status := f.status
	if status == "" {
		status = model.StatusCompleted
	}
	f.deployID = id
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
	status := f.status
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

func newTestWorker(t *testing.T, reg RegistryClient, ver SignatureVerifier, fetcher ArtifactFetcher, dep Deployer) (*Worker, string) {
	t.Helper()
	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "myapp")
	if err := os.MkdirAll(appDir, 0750); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	rm := release.NewReleaseManager(slog.Default())
	w := New(Config{
		Apps:            map[string]model.AppConfig{"myapp": testApp(appDir)},
		PollInterval:    time.Minute,
		DataDir:         dir,
		Registry:        reg,
		Verifier:        ver,
		Deployer:        dep,
		ArtifactFetcher: fetcher,
		Releases:        rm,
		Logger:          slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})),
	})
	return w, appDir
}

func TestWorker_PollApp_HappyPath(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:image123"}
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{digest: "sha256:compose123"}
	dep := &fakeDeployer{}
	w, appDir := newTestWorker(t, reg, ver, fetcher, dep)

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}

	state, err := w.states.Load(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state == nil {
		t.Fatal("expected saved state")
	}
	if state.Tag != "v1.0.0" || state.Digest != "sha256:image123" || state.ArtifactDigest != "sha256:compose123" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if dep.lastImage != "ghcr.io/org/myapp@sha256:image123" {
		t.Fatalf("expected deploy image ghcr.io/org/myapp@sha256:image123, got %q", dep.lastImage)
	}
	if ver.hits != 1 {
		t.Fatalf("expected one signature verify, got %d", ver.hits)
	}
	releaseFile := filepath.Join(appDir, ".furnace", "releases", "compose123", "docker-compose.yml")
	if _, err := os.Stat(releaseFile); err != nil {
		t.Fatalf("expected staged compose file %s: %v", releaseFile, err)
	}
}

func TestWorker_PollApp_NoChange(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:image123"}
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{digest: "sha256:compose123", resolveDigest: "sha256:compose123"}
	dep := &fakeDeployer{}
	w, _ := newTestWorker(t, reg, ver, fetcher, dep)
	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:            "v1.0.0",
		Digest:         "sha256:image123",
		ArtifactDigest: "sha256:compose123",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}
	if ver.hits != 0 {
		t.Fatalf("expected no signature verify, got %d", ver.hits)
	}
	if fetcher.hits != 0 {
		t.Fatalf("expected no fetch, got %d", fetcher.hits)
	}
	if dep.startHits != 0 {
		t.Fatalf("expected no deploy start, got %d", dep.startHits)
	}
}

func TestWorker_PollApp_ArtifactOnlyChange(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.0", digest: "sha256:image123"}
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{digest: "sha256:compose124", resolveDigest: "sha256:compose124"}
	dep := &fakeDeployer{}
	w, _ := newTestWorker(t, reg, ver, fetcher, dep)
	if err := w.states.Save(context.Background(), "myapp", &AppState{
		Tag:            "v1.0.0",
		Digest:         "sha256:image123",
		ArtifactDigest: "sha256:compose123",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}
	if ver.hits != 0 {
		t.Fatalf("expected no image verify on artifact-only change, got %d", ver.hits)
	}
	if dep.startHits != 1 {
		t.Fatalf("expected deploy start on artifact-only change, got %d", dep.startHits)
	}
}

func TestWorker_PollApp_SignatureFailure(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.1", digest: "sha256:image124"}
	ver := &fakeVerifier{err: model.ErrSignatureInvalid}
	fetcher := &fakeArtifactFetcher{}
	w, _ := newTestWorker(t, reg, ver, fetcher, &fakeDeployer{})
	err := w.pollApp(context.Background(), w.apps["myapp"])
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestWorker_PollApp_ArtifactFetchFailure(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.1", digest: "sha256:image124"}
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{err: errors.New("registry unavailable")}
	w, _ := newTestWorker(t, reg, ver, fetcher, &fakeDeployer{})
	err := w.pollApp(context.Background(), w.apps["myapp"])
	if err == nil {
		t.Fatal("expected error")
	}
	want := "fetch artifact: registry unavailable"
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestWorker_PollApp_DeployFailure(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.2", digest: "sha256:image125"}
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{digest: "sha256:compose125"}
	dep := &fakeDeployer{
		status:      model.StatusFailed,
		returnError: "health check failed",
	}
	w, _ := newTestWorker(t, reg, ver, fetcher, dep)
	err := w.pollApp(context.Background(), w.apps["myapp"])
	if err == nil {
		t.Fatal("expected error")
	}
	want := "deploy failed: health check failed"
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
	state, loadErr := w.states.Load(context.Background(), "myapp")
	if loadErr != nil {
		t.Fatalf("load state: %v", loadErr)
	}
	if state != nil {
		t.Fatalf("expected no saved state after failed deploy, got %+v", state)
	}
}

func TestWorker_DrainHints_TriggersImmediatePoll(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.3", digest: "sha256:image126"}
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{digest: "sha256:compose126"}
	dep := &fakeDeployer{}
	w, _ := newTestWorker(t, reg, ver, fetcher, dep)

	hintDir := filepath.Join(w.dataDir, "hints")
	if err := os.MkdirAll(hintDir, 0755); err != nil {
		t.Fatalf("mkdir hints: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hintDir, "myapp"), []byte(""), 0644); err != nil {
		t.Fatalf("write hint: %v", err)
	}

	w.drainHints(context.Background())

	if reg.hits != 1 {
		t.Fatalf("expected one latest tag lookup from hint, got %d", reg.hits)
	}
	if dep.startHits != 1 {
		t.Fatalf("expected one deploy start from hint, got %d", dep.startHits)
	}
	if _, err := os.Stat(filepath.Join(hintDir, "myapp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected hint file removed, got %v", err)
	}
}

func TestWorker_PollApp_PassesDigestPinnedImageToVerifier(t *testing.T) {
	reg := &fakeRegistry{tag: "v1.0.4", digest: "sha256:image127"}
	var gotRef string
	ver := &fakeVerifier{}
	fetcher := &fakeArtifactFetcher{digest: "sha256:compose127"}
	dep := &fakeDeployer{}

	verifier := SignatureVerifierFunc(func(ctx context.Context, imageRef, allowedIdentity string) error {
		gotRef = imageRef
		return ver.Verify(ctx, imageRef, allowedIdentity)
	})

	w, _ := newTestWorker(t, reg, verifier, fetcher, dep)
	if err := w.pollApp(context.Background(), w.apps["myapp"]); err != nil {
		t.Fatalf("pollApp: %v", err)
	}
	if !strings.Contains(gotRef, ":v1.0.4@sha256:image127") {
		t.Fatalf("expected digest-pinned image ref, got %q", gotRef)
	}
}

type SignatureVerifierFunc func(ctx context.Context, imageRef, allowedIdentity string) error

func (f SignatureVerifierFunc) Verify(ctx context.Context, imageRef, allowedIdentity string) error {
	return f(ctx, imageRef, allowedIdentity)
}
