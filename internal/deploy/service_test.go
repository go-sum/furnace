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
	err error
}

func (f *fakeHealthChecker) Check(_ context.Context, _ string, _ time.Duration) error {
	return f.err
}

func newTestService(t *testing.T, executor CommandExecutor, health HealthChecker) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "testapp")
	os.MkdirAll(appDir, 0750)

	store := storage.NewFileDeploymentStore(filepath.Join(dir, "deployments"), slog.Default())
	auditLogger, _ := audit.NewFileLogger(filepath.Join(dir, "audit"))
	lock := NewFileLock(filepath.Join(dir, "locks"))

	apps := map[string]model.AppConfig{
		"testapp": {
			Name:               "testapp",
			Repo:               "org/repo",
			AllowedRef:         "refs/tags/v*",
			Workflow:           ".github/workflows/release.yml",
			Dir:                appDir,
			ComposeFiles:       []string{"docker-compose.data.yml", "docker-compose.yml"},
			EnvFile:            ".deploy.env",
			ImageVar:           "APP_IMAGE",
			AllowedImagePrefix: "ghcr.io/org/repo:",
			HealthURL:          "http://127.0.0.1:8080/health",
			HealthTimeout:      5 * time.Second,
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
	})

	return svc, appDir
}

func validRequest() model.DeployRequest {
	return model.DeployRequest{
		AppName:  "testapp",
		Image:    "ghcr.io/org/repo:v1.0.0",
		Actor:    "bot",
		Repo:     "org/repo",
		Ref:      "refs/tags/v1.0.0",
		Workflow: "org/repo/.github/workflows/release.yml@refs/tags/v1.0.0",
		RunID:    "12345",
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
	health := &fakeHealthChecker{}

	svc, _ := newTestService(t, exec, health)

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
	if final.Image != "ghcr.io/org/repo:v1.0.0" {
		t.Fatalf("expected image ghcr.io/org/repo:v1.0.0, got %s", final.Image)
	}
}

func TestService_Start_ImageNotAllowed(t *testing.T) {
	exec := &fakeExecutor{}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	req := validRequest()
	req.Image = "docker.io/evil/image:latest"

	_, err := svc.Start(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrImageNotAllowed) {
		t.Fatalf("expected ErrImageNotAllowed, got: %v", err)
	}
}

func TestService_Start_UnknownApp(t *testing.T) {
	exec := &fakeExecutor{}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	req := validRequest()
	req.AppName = "nonexistent"

	_, err := svc.Start(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got: %v", err)
	}
}

func TestService_Start_PullFails(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{err: errors.New("network timeout")},
		},
	}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
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
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{err: errors.New("container crash")},
		},
	}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed status, got %s", d.Status)
	}
	if d.Error != "compose up: container crash" {
		t.Fatalf("expected compose up failure error, got %q", d.Error)
	}
}

func TestService_Start_HealthCheckFails(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{output: []byte("started")},
		},
	}
	health := &fakeHealthChecker{err: model.ErrHealthCheckFailed}
	svc, _ := newTestService(t, exec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed status, got %s", d.Status)
	}
	if d.Error != "health check: health check failed" {
		t.Fatalf("expected health check failure error, got %q", d.Error)
	}
}

func TestService_Start_ConcurrentReject(t *testing.T) {
	health := &fakeHealthChecker{}
	blockExec := &blockingExecutor{done: make(chan struct{})}
	svc, _ := newTestService(t, blockExec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("first start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	_, err = svc.Start(context.Background(), validRequest())
	if err == nil {
		t.Fatal("expected error on concurrent deploy")
	}
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
	exec := &fakeExecutor{}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	_, err := svc.Status(context.Background(), "nonexistent")
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got: %v", err)
	}
}

func TestService_Status_AfterDeploy(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{output: []byte("started")},
		},
	}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusCompleted {
		t.Fatalf("expected completed, got %s", d.Status)
	}
}

func TestService_Start_WritesEnvFile(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{output: []byte("started")},
		},
	}
	health := &fakeHealthChecker{}
	svc, appDir := newTestService(t, exec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForTerminal(t, svc, "testapp", 5*time.Second)

	envPath := filepath.Join(appDir, ".deploy.env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}

	expected := "APP_IMAGE=ghcr.io/org/repo:v1.0.0\n"
	if string(data) != expected {
		t.Fatalf("env file:\ngot  %q\nwant %q", string(data), expected)
	}
}

func TestService_Start_RestoresEnvAfterPullFailure(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{err: errors.New("network timeout")},
		},
	}
	health := &fakeHealthChecker{}
	svc, appDir := newTestService(t, exec, health)

	envPath := filepath.Join(appDir, ".deploy.env")
	previous := "APP_IMAGE=ghcr.io/org/repo:previous\nOTHER=value\n"
	if err := os.WriteFile(envPath, []byte(previous), 0640); err != nil {
		t.Fatalf("write initial env: %v", err)
	}

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	waitForTerminal(t, svc, "testapp", 5*time.Second)

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env after failure: %v", err)
	}
	if string(data) != previous {
		t.Fatalf("env after failure:\ngot  %q\nwant %q", string(data), previous)
	}
}

func TestService_Start_RestoresEnvAfterComposeUpFailure(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{err: errors.New("container crash")},
		},
	}
	health := &fakeHealthChecker{}
	svc, appDir := newTestService(t, exec, health)

	envPath := filepath.Join(appDir, ".deploy.env")
	previous := "APP_IMAGE=ghcr.io/org/repo:previous\n"
	if err := os.WriteFile(envPath, []byte(previous), 0640); err != nil {
		t.Fatalf("write initial env: %v", err)
	}

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	waitForTerminal(t, svc, "testapp", 5*time.Second)

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env after failure: %v", err)
	}
	if string(data) != previous {
		t.Fatalf("env after failure:\ngot  %q\nwant %q", string(data), previous)
	}
}

func TestService_Start_RestoresEnvAfterHealthFailure(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{output: []byte("pulled")},
			{output: []byte("started")},
		},
	}
	health := &fakeHealthChecker{err: model.ErrHealthCheckFailed}
	svc, appDir := newTestService(t, exec, health)

	envPath := filepath.Join(appDir, ".deploy.env")
	previous := "APP_IMAGE=ghcr.io/org/repo:previous\n"
	if err := os.WriteFile(envPath, []byte(previous), 0640); err != nil {
		t.Fatalf("write initial env: %v", err)
	}

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	waitForTerminal(t, svc, "testapp", 5*time.Second)

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env after failure: %v", err)
	}
	if string(data) != previous {
		t.Fatalf("env after failure:\ngot  %q\nwant %q", string(data), previous)
	}
}

func TestService_Start_RemovesEnvWhenNoPreviousFileExists(t *testing.T) {
	exec := &fakeExecutor{
		results: []fakeExecResult{
			{err: errors.New("network timeout")},
		},
	}
	health := &fakeHealthChecker{}
	svc, appDir := newTestService(t, exec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start should not fail: %v", err)
	}

	waitForTerminal(t, svc, "testapp", 5*time.Second)

	envPath := filepath.Join(appDir, ".deploy.env")
	_, err = os.Stat(envPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected env file to be removed, got err=%v", err)
	}
}

func TestService_Start_ImageWithNewlineRejected(t *testing.T) {
	exec := &fakeExecutor{}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	cases := []string{
		"ghcr.io/org/repo:v1\nEVIL=yes",
		"ghcr.io/org/repo:v1\rEVIL=yes",
		"ghcr.io/org/repo:v1\tEVIL=yes",
		"ghcr.io/org/repo:v1 =bad",
	}
	for _, img := range cases {
		req := validRequest()
		req.Image = img
		_, err := svc.Start(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error for image %q", img)
		}
		if !errors.Is(err, model.ErrImageNotAllowed) {
			t.Fatalf("expected ErrImageNotAllowed for image %q, got: %v", img, err)
		}
	}
}

func TestService_ReconcileOnStartup(t *testing.T) {
	exec := &fakeExecutor{}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

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

func TestService_ReconcileOnStartup_SkipsTerminal(t *testing.T) {
	exec := &fakeExecutor{}
	health := &fakeHealthChecker{}
	svc, _ := newTestService(t, exec, health)

	completed := &model.Deployment{
		ID:        "DONE01",
		AppName:   "testapp",
		Status:    model.StatusCompleted,
		StartedAt: time.Now().Add(-5 * time.Minute),
		EndedAt:   time.Now().Add(-4 * time.Minute),
	}
	if err := svc.store.Save(context.Background(), completed); err != nil {
		t.Fatalf("save completed deployment: %v", err)
	}

	svc.ReconcileOnStartup(context.Background())

	d, err := svc.Status(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if d.Status != model.StatusCompleted {
		t.Fatalf("expected completed to be untouched, got %s", d.Status)
	}
}

func TestService_Shutdown_CancelsActiveDeployment(t *testing.T) {
	health := &fakeHealthChecker{}
	blockExec := &blockingExecutor{done: make(chan struct{})}
	svc, _ := newTestService(t, blockExec, health)

	_, err := svc.Start(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := svc.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	d := waitForTerminal(t, svc, "testapp", 5*time.Second)
	if d.Status != model.StatusFailed {
		t.Fatalf("expected failed status after shutdown, got %s", d.Status)
	}
	if d.Error != "compose pull: context canceled" {
		t.Fatalf("expected cancellation error, got %q", d.Error)
	}
}

type panicExecutor struct{}

func (p *panicExecutor) Exec(_ context.Context, _ string, _ []string) ([]byte, error) {
	panic("simulated executor panic")
}

func TestService_Execute_PanicRecovery(t *testing.T) {
	svc, _ := newTestService(t, &panicExecutor{}, &fakeHealthChecker{})

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
	if final.Error == "" {
		t.Fatal("expected error message after panic")
	}
}
