package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestHintHandler_MkdirError(t *testing.T) {
	// Use a path inside a file (not a directory) to force MkdirAll failure.
	f, err := os.CreateTemp("", "hint-not-a-dir-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	apps := map[string]struct{}{"myapp": {}}
	req := httptest.NewRequest(http.MethodPost, "/v1/apps/myapp/deploy", nil)
	c := serve.NewContext(req)
	c.SetParam("app", "myapp")

	h := NewHintHandler(f.Name(), apps)
	_, handlerErr := h.Hint(c)
	if handlerErr == nil {
		t.Fatal("expected error when hint dir cannot be created")
	}
	if !strings.Contains(handlerErr.Error(), "create hint dir") {
		t.Fatalf("expected 'create hint dir' in error, got: %v", handlerErr)
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
