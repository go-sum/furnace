package storage

import (
	"context"

	"github.com/go-sum/furnace/internal/model"
)

var _ AppStore = (*SQLiteAppStore)(nil)

type AppStore interface {
	UpsertApp(ctx context.Context, app model.AppConfig) error
	GetApp(ctx context.Context, name string) (model.AppConfig, error)
	ListApps(ctx context.Context) ([]model.AppConfig, error)
	DeleteApp(ctx context.Context, name string) error
	AppExists(ctx context.Context, name string) (bool, error)
	GetConfigValue(ctx context.Context, key string) (string, bool, error)
	SetConfigValue(ctx context.Context, key, value string) error
}
