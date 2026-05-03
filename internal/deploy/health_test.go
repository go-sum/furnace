package deploy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

func TestHTTPHealthChecker_ImmediateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHTTPHealthChecker()
	err := checker.Check(context.Background(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestHTTPHealthChecker_EventualSuccess(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHTTPHealthChecker()
	err := checker.Check(context.Background(), srv.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("expected eventual success, got: %v", err)
	}
	if attempts.Load() < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempts.Load())
	}
}

func TestHTTPHealthChecker_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	checker := NewHTTPHealthChecker()
	err := checker.Check(context.Background(), srv.URL, 2*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
}

func TestHTTPHealthChecker_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := NewHTTPHealthChecker()
	err := checker.Check(ctx, srv.URL, 30*time.Second)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
}

func TestHTTPHealthChecker_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	checker := NewHTTPHealthChecker()
	err := checker.Check(context.Background(), srv.URL, 2*time.Second)
	if err == nil {
		t.Fatal("expected error for non-200")
	}
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
}
