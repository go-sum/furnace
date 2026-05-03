package app

import (
	"log/slog"

	"github.com/go-sum/foundry/pkg/web/router"

	"github.com/go-sum/furnace/internal/auth"
	"github.com/go-sum/furnace/internal/handler"
)

type Handlers struct {
	Deploy *handler.DeployHandler
	Status *handler.StatusHandler
	Health *handler.HealthHandler
}

func RegisterRoutes(rt *router.Router, h Handlers, verifier auth.TokenVerifier, logger *slog.Logger) {
	rt.GET("/v1/health", "health.show", h.Health.Health)
	rt.GET("/v1/apps/{app}/status", "app.status", h.Status.Status)

	api := rt.Group("/v1/apps/{app}", auth.Middleware(verifier, logger))
	api.POST("/deploy", "app.deploy", h.Deploy.Deploy)
}
