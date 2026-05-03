package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/go-sum/foundry/pkg/web"
	"github.com/go-sum/foundry/pkg/web/logging"
	"github.com/go-sum/foundry/pkg/web/ratelimit"
	"github.com/go-sum/foundry/pkg/web/router"
	"github.com/go-sum/foundry/pkg/web/serve"

	"github.com/go-sum/furnace/internal/audit"
	"github.com/go-sum/furnace/internal/auth"
	"github.com/go-sum/furnace/internal/deploy"
	"github.com/go-sum/furnace/internal/handler"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

type App struct {
	Handler       web.Handler
	Config        *Config
	Logger        *slog.Logger
	deployService *deploy.Service
}

func New(ctx context.Context, cfg *Config, logger *slog.Logger) (*App, error) {
	verifier, err := auth.NewOIDCVerifier(ctx, cfg.GitHub.Issuer, cfg.GitHub.Audience)
	if err != nil {
		return nil, fmt.Errorf("create OIDC verifier: %w", err)
	}

	executor := deploy.NewDockerExecutor()
	lock := deploy.NewFileLock(filepath.Join(cfg.DataDir, "locks"))
	health := deploy.NewHTTPHealthChecker()
	store := storage.NewFileDeploymentStore(filepath.Join(cfg.DataDir, "deployments"), logger)

	auditLogger, err := audit.NewFileLogger(filepath.Join(cfg.DataDir, "audit"))
	if err != nil {
		return nil, fmt.Errorf("create audit logger: %w", err)
	}

	apps := make(map[string]model.AppConfig, len(cfg.Apps))
	for name := range cfg.Apps {
		appCfg, _ := cfg.AppConfig(name)
		apps[name] = appCfg
	}

	svc := deploy.NewService(deploy.ServiceConfig{
		Apps:     apps,
		Executor: executor,
		Lock:     lock,
		Health:   health,
		Store:    store,
		Audit:    auditLogger,
		DataDir:  cfg.DataDir,
		Logger:   logger,
		Context:  ctx,
	})
	svc.ReconcileOnStartup(ctx)

	handlers := Handlers{
		Deploy: handler.NewDeployHandler(svc, cfg.AppConfig),
		Status: handler.NewStatusHandler(svc),
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

	rateMW, err := ratelimit.Middleware(ratelimit.MiddlewareConfig{
		Limiter: limiter,
		Profile: "api",
		KeyFunc: ratelimit.RemoteAddrKey,
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

	RegisterRoutes(rt, handlers, verifier, logger)

	return &App{
		Handler:       rt.Serve,
		Config:        cfg,
		Logger:        logger,
		deployService: svc,
	}, nil
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.deployService == nil {
		return nil
	}
	return a.deployService.Shutdown(ctx)
}
