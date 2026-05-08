package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sum/foundry/pkg/web"
	"github.com/go-sum/foundry/pkg/web/logging"
	"github.com/go-sum/foundry/pkg/web/ratelimit"
	"github.com/go-sum/foundry/pkg/web/router"
	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/storage"
	"github.com/go-sum/furnace/internal/web/handler"
)

type App struct {
	Handler web.Handler
	Logger  *slog.Logger
}

func New(ctx context.Context, db *sql.DB, fallbackDataDir string, logger *slog.Logger) (*App, error) {
	appStore := storage.NewSQLiteAppStore(db, logger)
	deployStore := storage.NewSQLiteDeploymentStore(db, logger)

	dataDir, _, err := appStore.GetConfigValue(ctx, "data_dir")
	if err != nil {
		return nil, fmt.Errorf("get data_dir: %w", err)
	}
	if dataDir == "" {
		dataDir = fallbackDataDir
	}

	var trustedProxies []string
	if raw, found, err := appStore.GetConfigValue(ctx, "trusted_proxies"); err != nil {
		return nil, fmt.Errorf("get trusted_proxies: %w", err)
	} else if found {
		if err := json.Unmarshal([]byte(raw), &trustedProxies); err != nil {
			return nil, fmt.Errorf("parse trusted_proxies: %w", err)
		}
	}

	handlers := Handlers{
		Hint:   handler.NewHintHandler(dataDir, appStore),
		Status: handler.NewStatusHandler(newStatusReader(appStore, deployStore)),
		Health: handler.NewHealthHandler(),
	}

	memStore := ratelimit.NewMemoryStore(ratelimit.MemoryStoreConfig{ExpiresIn: 1 * time.Minute})
	limiter, err := ratelimit.New(ratelimit.Config{
		Store: memStore,
		Profiles: map[ratelimit.RateLimitProfile]ratelimit.Policy{
			"api": {Capacity: 20, RefillPer: 100 * time.Millisecond},
		},
		Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create rate limiter: %w", err)
	}

	keyFunc, err := ratelimit.KeyFuncFromTrustedProxies(trustedProxies)
	if err != nil {
		return nil, fmt.Errorf("create rate limit key func: %w", err)
	}

	rateMW, err := ratelimit.Middleware(ratelimit.MiddlewareConfig{
		Limiter: limiter,
		Profile: "api",
		KeyFunc: keyFunc,
		Skipper: func(c *web.Context) bool {
			return c.Request.URL != nil && c.Request.URL.Path == "/v1/health"
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create rate limit middleware: %w", err)
	}

	rt := router.New()
	rt.Use(web.ErrorBoundary(web.BoundaryConfig{Logger: logger}))
	rt.Use(web.WithRequestID())
	rt.Use(serve.AccessLogMiddleware(logger))
	rt.Use(logging.Middleware(logger))
	rt.Use(web.WithMaxBody(1 << 20))
	rt.Use(rateMW)

	RegisterRoutes(rt, handlers)

	return &App{Handler: rt.Serve, Logger: logger}, nil
}
