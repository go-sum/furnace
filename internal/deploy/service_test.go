package deploy

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/audit"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

type fakeExecutor struct {
	calls   [][]string
	results []fakeExecResult
	idx     int
}

type fakeExecResult struct {
	output []byte
	err    error
}

func (f *fakeExecutor) Exec(_ context.Context, _ string, args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.idx >= len(f.results) {
		return nil, nil
	}
	r := f.results[f.idx]
	f.idx++
	return r.output, r.err
}

type fakeHealthChecker struct {
	err              error
	calledContainer  string
}

func (f *fakeHealthChecker) Check(_ context.Context, container string, _ time.Duration) error {
	f.calledContainer = container
	return f.err
}

func newTestService(t *testing.T, executor CommandExecutor, health HealthChecker) (*Service, string, *ReleaseManager) {
	t.Helper()
	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "testapp")
	os.MkdirAll(appDir, 0750)

	store := storage.NewFileDeploymentStore(filepath.Join(dir, "deployments"), slog.Default())
	auditLogger, _ := audit.NewFileLogger(filepath.Join(dir, "audit"))
	lock := NewFileLock(filepath.Join(dir, "locks"))
	rm := NewReleaseManager(slog.Default())

	apps := map[string]model.AppConfig{
		"testapp": {
			Name:            "testapp",
			Image:           "ghcr.io/org/myapp",
			TagPattern:      "v*",
			AllowedIdentity: "org/myapp",
			Dir:             appDir,
			Port:            8080,
			Artifact:        "ghcr.io/org/myapp:{tag}-compose",
			EnvFile:         ".deploy.env",
			ImageVar:        "APP_IMAGE",
			Container:       "testapp-web-1",
			HealthTimeout:   5 * time.Second,
			KeepReleases:    5,
		},
	}

	svc := NewService(ServiceConfig{
		Apps:     apps,
		Executor: executor,
		Lock:     lock,
		Health:   health,
		Store:    store,
		Audit:    auditLogger,
		DataDir:  dir,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Releases: rm,
	})

	return svc, appDir, rm
}

func validRequest() model.DeployRequest {
	return model.DeployRequest{
		AppName:        "testapp",
		Image:          "ghcr.io/org/myapp:v1.0.0",
		Tag:            "v1.0.0",
		Digest:         "sha256:abc123",
		ArtifactDigest: "sha256:testart123",
	}
}

// createTestRelease creates a release directory for the given digest in the app dir.
func createTestRelease(t *testing.T, rm *ReleaseManager, appDir, digest string) {
	t.Helper()
	stagingDir, err := rm.CreateStagingDir(appDir)
	if err != nil {
		t.Fatalf("CreateStagingDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "docker-compose.yml"), []byte("services: {}"), 0644); err != nil {
		t.Fatalf("write compose.yml: %v", err)
	}
	if err := rm.CommitStaging(appDir, stagingDir, digest); err != nil {
		t.Fatalf("CommitStaging: %v", err)
	}
}

func waitForTerminal(t *testing.T, svc *Service, appName string, timeout time.Duration) *model.Deployment {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("deployment did not reach terminal state within %v", timeout)
		case <-time.After(50 * time.Millisecond):
		}
		d, err := svc.Status(context.Background(), appName)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if d != nil && d.Status.IsTerminal() {
			return d
		}
	}
}

func TestService_Start_HappyPath(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{output: []byte("started")},
		},
	}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	d, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if d.Status != model.StatusPending {
		t.Fatalf("expected pending on immediate return, got %s", d.Status)
	}

	final := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if final.Status != model.StatusCompleted {
		t.Fatalf("expected completed, got %s (error: %s)", final.Status, final.Error)
	}
	if final.Image != "ghcr.io/org/myapp:v1.0.0" {
		t.Fatalf("expected image ghcr.io/org/myapp:v1.0.0, got %s", final.Image)
	}
	if final.Tag != "v1.0.0" {
		t.Fatalf("expected tag v1.0.0, got %s", final.Tag)
	}
}

func TestService_Start_UnknownApp(t *testing.T) {
	svc, _, _ := newTestService(t, &fakeExecutor{}, &fakeHealthChecker{})
	req := validRequest()
	req.AppName = "nonexistent"

	_, err := svc.Start(context.Background(), req)
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got: %v", err)
	}
}

func TestService_Start_PullFails(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{{err: errors.New("network timeout")}}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed status, got %s", d.Status)
	}
	if d.Error != "compose pull: network timeout" {
		t.Fatalf("expected pull failure error, got %q", d.Error)
	}
}

func TestService_Start_ComposeUpFails(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{err: errors.New("container crash")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed status, got %s", d.Status)
	}
	if d.Error != "compose up: container crash" {
		t.Fatalf("unexpected error: %q", d.Error)
	}
}

func TestService_Start_HealthCheckFails(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{output: []byte("started")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{err: model.ErrHealthCheckFailed})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed status, got %s", d.Status)
	}
	if d.Error != "health check: health check failed" {
		t.Fatalf("unexpected error: %q", d.Error)
	}
}

func TestService_Start_ConcurrentReject(t *testing.T) {
	blockExec := &blockingExecutor{done: make(chan struct{})}
	svc, appDir, rm := newTestService(t, blockExec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	_, err := svc.Start(context.Background(), validRequest())
	if !errors.Is(err, model.ErrDeploymentInProgress) {
		t.Fatalf("expected ErrDeploymentInProgress, got: %v", err)
	}

	close(blockExec.done)
	waitForTerminal(t, svc, "testapp", 5*time.Second)
}

type blockingExecutor struct {
	done chan struct{}
}

func (b *blockingExecutor) Exec(ctx context.Context, _ string, _ []string) ([]byte, error) {
	select {
	case <-b.done:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestService_Status_UnknownApp(t *testing.T) {
	svc, _, _ := newTestService(t, &fakeExecutor{}, &fakeHealthChecker{})
	_, err := svc.Status(context.Background(), "nonexistent")
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got: %v", err)
	}
}

func TestService_Start_WritesEnvFile(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{output: []byte("started")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	data, err := os.ReadFile(filepath.Join(appDir, ".deploy.env"))
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	want := "APP_IMAGE=ghcr.io/org/myapp:v1.0.0\n"
	if string(data) != want {
		t.Fatalf("env file:\ngot  %q\nwant %q", string(data), want)
	}
}

func TestService_Start_RestoresEnvAfterPullFailure(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{err: errors.New("network timeout")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	envPath := filepath.Join(appDir, ".deploy.env")
	previous := "APP_IMAGE=ghcr.io/org/myapp:v0.9.0\n"
	os.WriteFile(envPath, []byte(previous), 0640)

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env after failure: %v", err)
	}
	if string(data) != previous {
		t.Fatalf("env not restored:\ngot  %q\nwant %q", string(data), previous)
	}
}

func TestService_Start_RestoresEnvAfterHealthFailure(t *testing.T) {
	// Set up previous release and activate it.
	// The new deploy will have a different digest, with its own release.
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{output: []byte("started")},
		{output: []byte("rolled back")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{err: model.ErrHealthCheckFailed})

	// Create and activate a previous release so prevComposeFiles is populated.
	prevDigest := "sha256:prevart"
	createTestRelease(t, rm, appDir, prevDigest)
	if _, err := rm.Activate(appDir, prevDigest); err != nil {
		t.Fatalf("activate prev release: %v", err)
	}

	// Create current release.
	createTestRelease(t, rm, appDir, "sha256:testart123")

	envPath := filepath.Join(appDir, ".deploy.env")
	previous := "APP_IMAGE=ghcr.io/org/myapp:v0.9.0\n"
	os.WriteFile(envPath, []byte(previous), 0640)

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env after failure: %v", err)
	}
	if string(data) != previous {
		t.Fatalf("env not restored:\ngot  %q\nwant %q", string(data), previous)
	}

	// Should have 3 exec calls: pull, up, rollback-up.
	if len(exec.calls) < 3 {
		t.Fatalf("expected at least 3 exec calls, got %d", len(exec.calls))
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestService_Start_RollbackComposeUpCalledAfterHealthFailure(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{output: []byte("started")},
		{output: []byte("rolled back")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{err: model.ErrHealthCheckFailed})

	// Create and activate a previous release.
	prevDigest := "sha256:prevart"
	createTestRelease(t, rm, appDir, prevDigest)
	if _, err := rm.Activate(appDir, prevDigest); err != nil {
		t.Fatalf("activate prev release: %v", err)
	}

	// Create current release.
	createTestRelease(t, rm, appDir, "sha256:testart123")

	envPath := filepath.Join(appDir, ".deploy.env")
	os.WriteFile(envPath, []byte("APP_IMAGE=ghcr.io/org/myapp:v0.9.0\n"), 0640)

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	if len(exec.calls) < 3 {
		t.Fatalf("expected at least 3 exec calls, got %d", len(exec.calls))
	}

	// The 3rd call should be a compose up with the previous release's files.
	prevReleasePath := rm.ReleasePath(appDir, prevDigest)
	prevComposeFiles := []string{filepath.Join(prevReleasePath, "docker-compose.yml")}
	app := svc.apps["testapp"]
	wantRollback := ComposeUpArgs(app, prevComposeFiles)
	if !stringSlicesEqual(exec.calls[2], wantRollback) {
		t.Fatalf("3rd exec call (rollback compose up):\ngot  %v\nwant %v", exec.calls[2], wantRollback)
	}
}

func TestService_Start_RemovesEnvWhenNoPreviousFileExists(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{{err: errors.New("network timeout")}}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	_, err := os.Stat(filepath.Join(appDir, ".deploy.env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected env file to be removed, got err=%v", err)
	}
}

func TestService_Start_ImageWithInvalidChars(t *testing.T) {
	svc, _, _ := newTestService(t, &fakeExecutor{}, &fakeHealthChecker{})
	for _, img := range []string{
		"ghcr.io/org/myapp:v1\nEVIL=yes",
		"ghcr.io/org/myapp:v1\rEVIL=yes",
		"ghcr.io/org/myapp:v1\tEVIL=yes",
		"ghcr.io/org/myapp:v1 =bad",
	} {
		req := validRequest()
		req.Image = img
		_, err := svc.Start(context.Background(), req)
		if !errors.Is(err, model.ErrImageInvalid) {
			t.Fatalf("expected ErrImageInvalid for %q, got: %v", img, err)
		}
	}
}

func TestService_ReconcileOnStartup(t *testing.T) {
	svc, _, _ := newTestService(t, &fakeExecutor{}, &fakeHealthChecker{})

	stale := &model.Deployment{
		ID:        "STALE01",
		AppName:   "testapp",
		Status:    model.StatusPulling,
		StartedAt: time.Now().Add(-10 * time.Minute),
	}
	if err := svc.store.Save(context.Background(), stale); err != nil {
		t.Fatalf("save stale deployment: %v", err)
	}

	svc.ReconcileOnStartup(context.Background())

	d, err := svc.Status(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed after reconcile, got %s", d.Status)
	}
	if d.Error != "interrupted: process restarted" {
		t.Fatalf("unexpected error message: %q", d.Error)
	}
}

func TestService_Shutdown_CancelsActiveDeployment(t *testing.T) {
	blockExec := &blockingExecutor{done: make(chan struct{})}
	svc, appDir, rm := newTestService(t, blockExec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := svc.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed after shutdown, got %s", d.Status)
	}
}

func TestFormatFailure(t *testing.T) {
	cases := []struct {
		step          string
		err           error
		rollbackErr   error
		rollbackUpErr error
		want          string
	}{
		{"step", errors.New("x"), nil, nil, "step: x"},
		{"step", errors.New("x"), errors.New("re"), nil, "step: x; restore env: re"},
		{"step", errors.New("x"), nil, errors.New("rb"), "step: x; rollback compose up: rb"},
		{"step", errors.New("x"), errors.New("re"), errors.New("rb"), "step: x; restore env: re; rollback compose up: rb"},
	}
	for _, tc := range cases {
		got := formatFailure(tc.step, tc.err, tc.rollbackErr, tc.rollbackUpErr)
		if got != tc.want {
			t.Errorf("formatFailure(%q, %v, %v, %v) = %q, want %q", tc.step, tc.err, tc.rollbackErr, tc.rollbackUpErr, got, tc.want)
		}
	}
}

func TestService_Start_RollbackComposeUpFailureRecordedInError(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},            // compose pull — OK
		{output: []byte("started")},           // compose up — OK
		{err: errors.New("rollback exploded")}, // rollback compose up — FAILS
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{err: model.ErrHealthCheckFailed})

	// Create and activate a previous release so rollback compose up is triggered.
	prevDigest := "sha256:prevart"
	createTestRelease(t, rm, appDir, prevDigest)
	if _, err := rm.Activate(appDir, prevDigest); err != nil {
		t.Fatalf("activate prev release: %v", err)
	}

	// Create current release.
	createTestRelease(t, rm, appDir, "sha256:testart123")

	// Pre-place env so envState.Existed == true → rollback compose up is triggered.
	envPath := filepath.Join(appDir, ".deploy.env")
	os.WriteFile(envPath, []byte("APP_IMAGE=ghcr.io/org/myapp:v0.9.0\n"), 0640)

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start: %v", err)
	}
	d := waitForTerminal(t, svc, "testapp", 5*time.Second)

	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed, got %s", d.Status)
	}
	want := "health check: health check failed; rollback compose up: rollback exploded"
	if d.Error != want {
		t.Fatalf("error:\ngot  %q\nwant %q", d.Error, want)
	}
}


type panicExecutor struct{}

func (p *panicExecutor) Exec(_ context.Context, _ string, _ []string) ([]byte, error) {
	panic("simulated executor panic")
}

func TestService_Execute_PanicRecovery(t *testing.T) {
	svc, appDir, rm := newTestService(t, &panicExecutor{}, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	d, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if d == nil {
		t.Fatal("expected deployment record")
	}

	final := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if final.Status != model.StatusFailed {
		t.Fatalf("expected failed after panic, got %s", final.Status)
	}
}

func TestService_Start_HappyPath_ActivatesAfterHealthCheck(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{output: []byte("started")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	active, err := rm.ActiveReleasePath(appDir)
	if err != nil {
		t.Fatalf("ActiveReleasePath: %v", err)
	}
	want := rm.ReleasePath(appDir, "sha256:testart123")
	if active != want {
		t.Fatalf("current symlink:\ngot  %s\nwant %s", active, want)
	}
}

func TestService_Start_PullFails_SymlinkNeverSwitched(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{{err: errors.New("network timeout")}}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{})

	prevDigest := "sha256:prevart"
	createTestRelease(t, rm, appDir, prevDigest)
	if _, err := rm.Activate(appDir, prevDigest); err != nil {
		t.Fatalf("activate prev release: %v", err)
	}
	createTestRelease(t, rm, appDir, "sha256:testart123")

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	active, err := rm.ActiveReleasePath(appDir)
	if err != nil {
		t.Fatalf("ActiveReleasePath: %v", err)
	}
	wantActive := rm.ReleasePath(appDir, prevDigest)
	if active != wantActive {
		t.Fatalf("symlink switched to new release on failure:\ngot  %s\nwant %s", active, wantActive)
	}
}

func TestService_Start_HealthFails_SymlinkNeverSwitched(t *testing.T) {
	exec := &fakeExecutor{results: []fakeExecResult{
		{output: []byte("pulled")},
		{output: []byte("started")},
		{output: []byte("rolled back")},
	}}
	svc, appDir, rm := newTestService(t, exec, &fakeHealthChecker{err: model.ErrHealthCheckFailed})

	prevDigest := "sha256:prevart"
	createTestRelease(t, rm, appDir, prevDigest)
	if _, err := rm.Activate(appDir, prevDigest); err != nil {
		t.Fatalf("activate prev release: %v", err)
	}
	createTestRelease(t, rm, appDir, "sha256:testart123")
	os.WriteFile(filepath.Join(appDir, ".deploy.env"), []byte("APP_IMAGE=ghcr.io/org/myapp:v0.9.0\n"), 0640)

	if _, err := svc.Start(context.Background(), validRequest()); err != nil {
		t.Fatalf("start should not fail: %v", err)
	}
	waitForTerminal(t, svc, "testapp", 5*time.Second)

	active, err := rm.ActiveReleasePath(appDir)
	if err != nil {
		t.Fatalf("ActiveReleasePath: %v", err)
	}
	wantActive := rm.ReleasePath(appDir, prevDigest)
	if active != wantActive {
		t.Fatalf("symlink switched to new release on health failure:\ngot  %s\nwant %s", active, wantActive)
	}
}
