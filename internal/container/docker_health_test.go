package container

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/go-sum/furnace/internal/model"
)

type fakeDockerClient struct {
	inspectFn func(ctx context.Context, containerID string) (container.InspectResponse, error)
	calls     int
}

func (f *fakeDockerClient) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	f.calls++
	return f.inspectFn(ctx, containerID)
}

// healthyInspectResponse builds a container.InspectResponse with the given health status.
func healthyInspectResponse(status string) container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			State: &container.State{
				Health: &container.Health{
					Status: status,
				},
			},
		},
		NetworkSettings: &container.NetworkSettings{},
	}
}

func TestDockerHealthChecker_ImmediateHealthy(t *testing.T) {
	fake := &fakeDockerClient{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return healthyInspectResponse(container.Healthy), nil
		},
	}
	checker := NewDockerHealthChecker(fake)
	err := checker.Check(context.Background(), "mycontainer", 5*time.Second)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestDockerHealthChecker_EventuallyHealthy(t *testing.T) {
	fake := &fakeDockerClient{}
	fake.inspectFn = func(_ context.Context, _ string) (container.InspectResponse, error) {
		if fake.calls < 3 {
			return healthyInspectResponse(container.Starting), nil
		}
		return healthyInspectResponse(container.Healthy), nil
	}
	checker := NewDockerHealthChecker(fake)
	err := checker.Check(context.Background(), "mycontainer", 30*time.Second)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if fake.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", fake.calls)
	}
}

func TestDockerHealthChecker_Unhealthy(t *testing.T) {
	fake := &fakeDockerClient{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			resp := container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					State: &container.State{
						Health: &container.Health{
							Status: container.Unhealthy,
							Log: []*container.HealthcheckResult{
								{Output: "connection refused"},
							},
						},
					},
				},
				NetworkSettings: &container.NetworkSettings{},
			}
			return resp, nil
		},
	}
	checker := NewDockerHealthChecker(fake)
	err := checker.Check(context.Background(), "mycontainer", 5*time.Second)
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
}

func TestDockerHealthChecker_Timeout(t *testing.T) {
	fake := &fakeDockerClient{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return healthyInspectResponse(container.Starting), nil
		},
	}
	checker := NewDockerHealthChecker(fake)
	err := checker.Check(context.Background(), "mycontainer", 2*time.Second)
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
}

func TestDockerHealthChecker_ContextCancelled(t *testing.T) {
	fake := &fakeDockerClient{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return healthyInspectResponse(container.Starting), nil
		},
	}
	checker := NewDockerHealthChecker(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := checker.Check(ctx, "mycontainer", 30*time.Second)
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
}

func TestDockerHealthChecker_NoHealthcheck_ReturnsError(t *testing.T) {
	fake := &fakeDockerClient{
		inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					State: &container.State{
						// Health == nil means no HEALTHCHECK directive.
					},
				},
				NetworkSettings: &container.NetworkSettings{},
			}, nil
		},
	}
	checker := NewDockerHealthChecker(fake)
	err := checker.Check(context.Background(), "mycontainer", 5*time.Second)
	if !errors.Is(err, model.ErrHealthCheckFailed) {
		t.Fatalf("expected ErrHealthCheckFailed, got: %v", err)
	}
	want := "health check failed: container has no HEALTHCHECK"
	if err.Error() != want {
		t.Fatalf("error message:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestDockerHealthChecker_InspectError_Retries(t *testing.T) {
	fake := &fakeDockerClient{}
	fake.inspectFn = func(_ context.Context, _ string) (container.InspectResponse, error) {
		if fake.calls == 1 {
			return container.InspectResponse{}, errors.New("connection refused")
		}
		return healthyInspectResponse(container.Healthy), nil
	}
	checker := NewDockerHealthChecker(fake)
	err := checker.Check(context.Background(), "mycontainer", 30*time.Second)
	if err != nil {
		t.Fatalf("expected nil after retry, got: %v", err)
	}
	if fake.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", fake.calls)
	}
}
