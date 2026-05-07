package storage

import (
	"context"

	"github.com/go-sum/furnace/internal/model"
)

type DeploymentStore interface {
	Save(ctx context.Context, d *model.Deployment) error
	GetLatest(ctx context.Context, appName string) (*model.Deployment, error)
	GetPrevious(ctx context.Context, appName string) (*model.Deployment, error)
	List(ctx context.Context, appName string, limit int) ([]model.Deployment, error)
	Prune(ctx context.Context, appName string, keep int) (int, error)
}
