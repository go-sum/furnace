package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sum/foundry/pkg/web"
	"github.com/go-sum/foundry/pkg/web/logging"
	"github.com/go-sum/foundry/pkg/web/ratelimit"
	"github.com/go-sum/foundry/pkg/web/router"
	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/handler"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

type App struct {
	Handler web.Handler
	Config  *Config
	Logger  *slog.Logger
}

func New(cfg *Config, db *sql.DB, logger *slog.Logger) (*App, error) {
	store := storage.NewSQLiteDeploymentStore(db, logger)

	apps := make(map[string]model.AppConfig, len(cfg.Apps))
	for name := range cfg.Apps {
		appCfg, _ := cfg.AppConfig(name)
		apps[name] = appCfg
	}
	appNames := make(map[string]struct{}, len(apps))
	for name := range apps {
		appNames[name] = struct{}{}
	}

	handlers := Handlers{
		Hint:   handler.NewHintHandler(cfg.DataDir, appNames),
		Status: handler.NewStatusHandler(newStatusReader(appNames, store)),
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

	keyFunc, err := ratelimit.KeyFuncFromTrustedProxies(cfg.TrustedProxies)
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

	return &App{
		Handler: rt.Serve,
		Config:  cfg,
		Logger:  logger,
	}, nil
}
