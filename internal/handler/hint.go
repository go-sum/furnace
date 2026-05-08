package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-sum/foundry/pkg/web"

	"github.com/go-sum/furnace/internal/model"
)

// AppChecker verifies whether a named app exists.
type AppChecker interface {
	AppExists(ctx context.Context, name string) (bool, error)
}

// HintHandler writes a hint file that signals the worker to poll immediately.
type HintHandler struct {
	dataDir string
	apps    AppChecker
}

func NewHintHandler(dataDir string, apps AppChecker) *HintHandler {
	return &HintHandler{dataDir: dataDir, apps: apps}
}

func (h *HintHandler) Hint(c *web.Context) (web.Response, error) {
	appName := c.Param("app")
	if !model.ValidateAppName(appName) {
		return web.JSON(http.StatusBadRequest, map[string]string{"error": "invalid app name"}), nil
	}

	exists, err := h.apps.AppExists(c.Context(), appName)
	if err != nil {
		return web.Response{}, fmt.Errorf("check app: %w", err)
	}
	if !exists {
		return web.JSON(http.StatusNotFound, map[string]string{"error": "unknown app"}), nil
	}

	hintDir := filepath.Join(h.dataDir, "hints")
	if err := os.MkdirAll(hintDir, 0750); err != nil {
		return web.Response{}, fmt.Errorf("create hint dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(hintDir, appName), nil, 0640); err != nil {
		return web.Response{}, fmt.Errorf("write hint file: %w", err)
	}

	return web.JSON(http.StatusAccepted, map[string]string{"status": "ok"}), nil
}
