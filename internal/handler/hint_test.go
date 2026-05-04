package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-sum/foundry/pkg/web/serve"
)

func TestHintHandler_KnownApp(t *testing.T) {
	dataDir := t.TempDir()
	apps := map[string]struct{}{"myapp": {}}

	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")

	h := NewHintHandler(dataDir, apps)
	resp, err := h.Hint(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.Status)
	}

	body, _ := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	expected := "{\"status\":\"ok\"}\n"
	if string(body) != expected {
		t.Fatalf("body mismatch:\ngot  %q\nwant %q", string(body), expected)
	}

	hintPath := filepath.Join(dataDir, "hints", "myapp")
	if _, err := os.Stat(hintPath); err != nil {
		t.Fatalf("expected hint file at %s: %v", hintPath, err)
	}
}

func TestHintHandler_UnknownApp(t *testing.T) {
	dataDir := t.TempDir()
	apps := map[string]struct{}{"myapp": {}}

	req := httptest.NewRequest(http.MethodPost, "/v1/apps/other/deploy", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "other")

	h := NewHintHandler(dataDir, apps)
	resp, err := h.Hint(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Status)
	}
}
