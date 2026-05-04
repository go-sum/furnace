package handler

import (
	"context"

	"github.com/go-sum/furnace/internal/model"
)

// Deployer is the subset of deploy.Service used by HTTP handlers.
type Deployer interface {
	Status(ctx context.Context, appName string) (*model.Deployment, error)
}
