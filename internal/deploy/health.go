package deploy

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

type HealthChecker interface {
	Check(ctx context.Context, url string, timeout time.Duration) error
}

type HTTPHealthChecker struct {
	client *http.Client
}

func NewHTTPHealthChecker() *HTTPHealthChecker {
	return &HTTPHealthChecker{
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (h *HTTPHealthChecker) Check(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.After(timeout)
	interval := 1 * time.Second
	maxInterval := 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: context cancelled", model.ErrHealthCheckFailed)
		case <-deadline:
			return fmt.Errorf("%w: timed out after %v", model.ErrHealthCheckFailed, timeout)
		default:
		}

		if err := h.probe(ctx, url); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: context cancelled", model.ErrHealthCheckFailed)
		case <-deadline:
			return fmt.Errorf("%w: timed out after %v", model.ErrHealthCheckFailed, timeout)
		case <-time.After(interval):
		}

		interval = min(interval*2, maxInterval)
	}
}

func (h *HTTPHealthChecker) probe(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}
