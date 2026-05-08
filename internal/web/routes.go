package web

import (
	"github.com/go-sum/foundry/pkg/web/router"

	"github.com/go-sum/furnace/internal/web/handler"
)

type Handlers struct {
	Hint   *handler.HintHandler
	Status *handler.StatusHandler
	Health *handler.HealthHandler
}

func RegisterRoutes(rt *router.Router, h Handlers) {
	rt.GET("/v1/health", "health.show", h.Health.Health)
	rt.GET("/v1/apps/{app}/status", "app.status", h.Status.Status)
	rt.POST("/v1/apps/{app}/deploy", "app.hint", h.Hint.Hint)
}
