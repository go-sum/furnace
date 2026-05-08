package handler

import (
	"net/http"

	"github.com/go-sum/foundry/pkg/web"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) Health(c *web.Context) (web.Response, error) {
	return web.JSON(http.StatusOK, map[string]string{"status": "ok"}), nil
}
