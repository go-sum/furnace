package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-sum/foundry/pkg/web"
	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/model"
)

type fakeVerifier struct {
	claims *GitHubClaims
	err    error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (*GitHubClaims, error) {
	return f.claims, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func TestMiddleware_MissingAuthHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	c := serve.NewContext(req)

	mw := Middleware(&fakeVerifier{}, testLogger())
	called := false
	handler := mw(func(c *web.Context) (web.Response, error) {
		called = true
		return web.Respond(http.StatusOK), nil
	})

	resp, err := handler(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if called {
		t.Fatal("next handler should not have been called")
	}
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Status)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	c := serve.NewContext(req)

	verifier := &fakeVerifier{err: fmt.Errorf("%w: signature mismatch", model.ErrTokenInvalid)}
	mw := Middleware(verifier, testLogger())
	called := false
	handler := mw(func(c *web.Context) (web.Response, error) {
		called = true
		return web.Respond(http.StatusOK), nil
	})

	resp, err := handler(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if called {
		t.Fatal("next handler should not have been called")
	}
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Status)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	c := serve.NewContext(req)

	claims := &GitHubClaims{
		Repository: "org/repo",
		Ref:        "refs/tags/v1.0.0",
		Actor:      "bot",
	}
	verifier := &fakeVerifier{claims: claims}
	mw := Middleware(verifier, testLogger())
	called := false
	handler := mw(func(c *web.Context) (web.Response, error) {
		called = true
		return web.Respond(http.StatusOK), nil
	})

	resp, err := handler(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !called {
		t.Fatal("next handler should have been called")
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Status)
	}

	got := ClaimsFromContext(c)
	if got == nil {
		t.Fatal("expected claims in context")
	}
	if got.Repository != "org/repo" {
		t.Fatalf("expected org/repo, got %s", got.Repository)
	}
}

func TestMiddleware_WrongScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	c := serve.NewContext(req)

	mw := Middleware(&fakeVerifier{}, testLogger())
	called := false
	handler := mw(func(c *web.Context) (web.Response, error) {
		called = true
		return web.Respond(http.StatusOK), nil
	})

	resp, err := handler(c)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if called {
		t.Fatal("next handler should not have been called")
	}
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Status)
	}
}

func TestClaimsFromContext_Nil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	c := serve.NewContext(req)

	got := ClaimsFromContext(c)
	if got != nil {
		t.Fatalf("expected nil claims, got %+v", got)
	}
}
