package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/go-sum/furnace/internal/model"
)

// HealthChecker polls a container until it becomes healthy or the timeout elapses.
type HealthChecker interface {
	Check(ctx context.Context, container string, timeout time.Duration) error
}

// DockerAPIClient is the minimal Docker API surface needed for health checking.
type DockerAPIClient interface {
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
}

// DockerHealthChecker checks container health using the Docker API.
// It polls the Docker HEALTHCHECK status until the container becomes healthy,
// the timeout elapses, or the context is cancelled.
type DockerHealthChecker struct {
	docker DockerAPIClient
}

// NewDockerHealthChecker returns a DockerHealthChecker backed by the given
// Docker API client.
func NewDockerHealthChecker(docker DockerAPIClient) *DockerHealthChecker {
	return &DockerHealthChecker{docker: docker}
}

// Check polls Docker inspect for the named container until it reports healthy.
func (d *DockerHealthChecker) Check(ctx context.Context, containerName string, timeout time.Duration) error {
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

		info, err := d.docker.ContainerInspect(ctx, containerName)
		if err != nil {
			// Container may still be starting — retry after backoff.
		} else if info.State != nil && info.State.Health != nil {
			switch info.State.Health.Status {
			case container.Healthy:
				return nil
			case container.Unhealthy:
				msg := "container unhealthy"
				if len(info.State.Health.Log) > 0 {
					last := info.State.Health.Log[len(info.State.Health.Log)-1]
					msg = fmt.Sprintf("container unhealthy: %s", last.Output)
				}
				return fmt.Errorf("%w: %s", model.ErrHealthCheckFailed, msg)
			// "starting" and any other status: continue polling.
			}
		} else if err == nil {
			return fmt.Errorf("%w: container has no HEALTHCHECK", model.ErrHealthCheckFailed)
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
