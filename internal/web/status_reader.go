package web

import (
	"context"
	"fmt"

	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

type appChecker interface {
	AppExists(ctx context.Context, name string) (bool, error)
}

type statusReader struct {
	apps  appChecker
	store storage.DeploymentStore
}

func newStatusReader(apps appChecker, store storage.DeploymentStore) *statusReader {
	return &statusReader{
		apps:  apps,
		store: store,
	}
}

func (s *statusReader) Status(ctx context.Context, appName string) (*model.Deployment, error) {
	exists, err := s.apps.AppExists(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("check app: %w", err)
	}
	if !exists {
		return nil, model.ErrAppNotFound
	}
	return s.store.GetLatest(ctx, appName)
}
