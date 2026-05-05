package app

import (
	"context"

	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

type statusReader struct {
	apps  map[string]struct{}
	store storage.DeploymentStore
}

func newStatusReader(apps map[string]struct{}, store storage.DeploymentStore) *statusReader {
	return &statusReader{
		apps:  apps,
		store: store,
	}
}

func (s *statusReader) Status(ctx context.Context, appName string) (*model.Deployment, error) {
	if _, ok := s.apps[appName]; !ok {
		return nil, model.ErrAppNotFound
	}
	return s.store.GetLatest(ctx, appName)
}
