package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-sum/foundry/pkg/web"
)

// HintHandler writes a hint file that signals the worker to poll immediately.
type HintHandler struct {
	dataDir string
	apps    map[string]struct{}
}

func NewHintHandler(dataDir string, apps map[string]struct{}) *HintHandler {
	return &HintHandler{dataDir: dataDir, apps: apps}
}

func (h *HintHandler) Hint(c *web.Context) (web.Response, error) {
	appName := c.Param("app")
	if _, ok := h.apps[appName]; !ok {
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
