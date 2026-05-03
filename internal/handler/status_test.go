package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/model"
)

func TestStatusHandler_WithDeployment(t *testing.T) {
	deployer := &fakeDeployer{
		deployment: &model.Deployment{
			ID:      "01ABC",
			AppName: "myapp",
			Image:   "ghcr.io/org/repo:v1.0.0",
			Status:  model.StatusCompleted,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/apps/myapp/status", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")

	h := NewStatusHandler(deployer)
	resp, err := h.Status(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	expected := "{\"id\":\"01ABC\",\"app_name\":\"myapp\",\"image\":\"ghcr.io/org/repo:v1.0.0\",\"status\":\"completed\",\"actor\":\"\",\"repo\":\"\",\"ref\":\"\",\"started_at\":\"0001-01-01T00:00:00Z\",\"ended_at\":\"0001-01-01T00:00:00Z\"}\n"
	if string(body) != expected {
		t.Fatalf("status body mismatch:\ngot  %q\nwant %q", string(body), expected)
	}
}

func TestStatusHandler_UnknownApp(t *testing.T) {
	deployer := &fakeDeployer{err: model.ErrAppNotFound}

	req := httptest.NewRequest(http.MethodGet, "/v1/apps/unknown/status", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "unknown")

	h := NewStatusHandler(deployer)
	resp, err := h.Status(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Status)
	}
}

func TestStatusHandler_NoDeployments(t *testing.T) {
	deployer := &fakeDeployer{deployment: nil}

	req := httptest.NewRequest(http.MethodGet, "/v1/apps/myapp/status", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")

	h := NewStatusHandler(deployer)
	resp, err := h.Status(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	expected := "{\"status\":\"no deployments\"}\n"
	if string(body) != expected {
		t.Fatalf("status body mismatch:\ngot  %q\nwant %q", string(body), expected)
	}
}
