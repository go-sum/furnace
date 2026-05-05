package handler

import (
	"context"

	"github.com/go-sum/furnace/internal/model"
)

// Deployer provides deployment status reads for HTTP handlers.
type Deployer interface {
	Status(ctx context.Context, appName string) (*model.Deployment, error)
}
