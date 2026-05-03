package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sum/foundry/pkg/web/serve"
)

func TestHealthHandler_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	c := serve.NewContext(req)

	h := NewHealthHandler()
	resp, err := h.Health(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	expected := "{\"status\":\"ok\"}\n"
	if string(body) != expected {
		t.Fatalf("health body mismatch:\ngot  %q\nwant %q", string(body), expected)
	}
}
