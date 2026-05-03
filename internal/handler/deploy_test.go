package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/auth"
	"github.com/go-sum/furnace/internal/model"
)

type fakeDeployer struct {
	deployment *model.Deployment
	err        error
	lastReq    model.DeployRequest
}

func (f *fakeDeployer) Start(_ context.Context, req model.DeployRequest) (*model.Deployment, error) {
	f.lastReq = req
	return f.deployment, f.err
}

func (f *fakeDeployer) Status(_ context.Context, appName string) (*model.Deployment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.deployment, nil
}

func testAppLookup() func(string) (model.AppConfig, bool) {
	return func(name string) (model.AppConfig, bool) {
		if name == "myapp" {
			return model.AppConfig{
				Name:               "myapp",
				Repo:               "org/repo",
				AllowedRef:         "refs/tags/v*",
				Workflow:           ".github/workflows/release.yml",
				AllowedImagePrefix: "ghcr.io/org/repo:",
				ComposeFiles:       []string{"docker-compose.data.yml", "docker-compose.yml"},
			}, true
		}
		return model.AppConfig{}, false
	}
}

func validClaims() *auth.GitHubClaims {
	return &auth.GitHubClaims{
		Repository:  "org/repo",
		Ref:         "refs/tags/v1.0.0",
		Workflow:    "Release",
		WorkflowRef: "org/repo/.github/workflows/release.yml@refs/tags/v1.0.0",
		Actor:       "bot",
		RunID:       "999",
	}
}

func TestDeployHandler_Success(t *testing.T) {
	deployer := &fakeDeployer{
		deployment: &model.Deployment{
			ID:      "01ABC",
			AppName: "myapp",
			Image:   "ghcr.io/org/repo:v1.0.0",
			Status:  model.StatusCompleted,
		},
	}

	body := `{"image": "ghcr.io/org/repo:v1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")
	auth.SetClaims(c, validClaims())

	h := NewDeployHandler(deployer, testAppLookup())
	resp, err := h.Deploy(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.Status, body)
	}
	if deployer.lastReq.Image != "ghcr.io/org/repo:v1.0.0" {
		t.Fatalf("expected image passed to service, got %q", deployer.lastReq.Image)
	}
	if deployer.lastReq.Workflow != "org/repo/.github/workflows/release.yml@refs/tags/v1.0.0" {
		t.Fatalf("expected workflow_ref passed to service, got %q", deployer.lastReq.Workflow)
	}
}

func TestDeployHandler_UnknownApp(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/unknown/deploy", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "unknown")
	auth.SetClaims(c, validClaims())

	h := NewDeployHandler(&fakeDeployer{}, testAppLookup())
	resp, err := h.Deploy(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Status)
	}
}

func TestDeployHandler_MissingClaims(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")

	h := NewDeployHandler(&fakeDeployer{}, testAppLookup())
	resp, err := h.Deploy(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Status)
	}
}

func TestDeployHandler_ForbiddenRepo(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")
	auth.SetClaims(c, &auth.GitHubClaims{
		Repository:  "evil/repo",
		Ref:         "refs/tags/v1.0.0",
		WorkflowRef: "evil/repo/.github/workflows/release.yml@refs/tags/v1.0.0",
	})

	h := NewDeployHandler(&fakeDeployer{}, testAppLookup())
	resp, err := h.Deploy(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.Status)
	}
}

func TestDeployHandler_MissingImage(t *testing.T) {
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")
	auth.SetClaims(c, validClaims())

	h := NewDeployHandler(&fakeDeployer{}, testAppLookup())
	resp, err := h.Deploy(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Status)
	}
}

func TestDeployHandler_DeploymentInProgress(t *testing.T) {
	deployer := &fakeDeployer{err: model.ErrDeploymentInProgress}

	body := `{"image": "ghcr.io/org/repo:v1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")
	auth.SetClaims(c, validClaims())

	h := NewDeployHandler(deployer, testAppLookup())
	resp, err := h.Deploy(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.Status)
	}
}
