package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-sum/foundry/pkg/web"

	"github.com/go-sum/furnace/internal/model"
)

type StatusHandler struct {
	deployer Deployer
}

func NewStatusHandler(deployer Deployer) *StatusHandler {
	return &StatusHandler{deployer: deployer}
}

func (h *StatusHandler) Status(c *web.Context) (web.Response, error) {
	appName := c.Param("app")

	d, err := h.deployer.Status(c.Context(), appName)
	if err != nil {
		if errors.Is(err, model.ErrAppNotFound) {
			return web.JSON(http.StatusNotFound, map[string]string{
				"error": "unknown app",
			}), nil
		}
		return web.Response{}, fmt.Errorf("get deployment status: %w", err)
	}

	if d == nil {
		return web.JSON(http.StatusOK, map[string]string{
			"status": "no deployments",
		}), nil
	}

	return web.JSON(http.StatusOK, d), nil
}
