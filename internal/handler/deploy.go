package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-sum/foundry/pkg/web"

	"github.com/go-sum/furnace/internal/auth"
	"github.com/go-sum/furnace/internal/model"
)

type Deployer interface {
	Start(ctx context.Context, req model.DeployRequest) (*model.Deployment, error)
	Status(ctx context.Context, appName string) (*model.Deployment, error)
}

type DeployHandler struct {
	deployer Deployer
	apps     func(string) (model.AppConfig, bool)
}

func NewDeployHandler(deployer Deployer, apps func(string) (model.AppConfig, bool)) *DeployHandler {
	return &DeployHandler{deployer: deployer, apps: apps}
}

type deployRequest struct {
	Image string `json:"image"`
}

func (h *DeployHandler) Deploy(c *web.Context) (web.Response, error) {
	appName := c.Param("app")

	app, ok := h.apps(appName)
	if !ok {
		return web.JSON(http.StatusNotFound, map[string]string{
			"error": "unknown app",
		}), nil
	}

	claims := auth.ClaimsFromContext(c)
	if claims == nil {
		return web.JSON(http.StatusUnauthorized, map[string]string{
			"error": "missing claims",
		}), nil
	}

	if err := auth.ValidateClaims(claims, app); err != nil {
		return web.JSON(http.StatusForbidden, map[string]string{
			"error": err.Error(),
		}), nil
	}

	var req deployRequest
	if err := c.Request.JSON(&req); err != nil {
		return web.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		}), nil
	}
	if req.Image == "" {
		return web.JSON(http.StatusBadRequest, map[string]string{
			"error": "image is required",
		}), nil
	}

	deployReq := model.DeployRequest{
		AppName:   appName,
		Image:     req.Image,
		Actor:     claims.Actor,
		Repo:      claims.Repository,
		Ref:       claims.Ref,
		Workflow:  claims.WorkflowRef,
		RunID:     claims.RunID,
		RequestID: web.RequestID(c),
	}

	d, err := h.deployer.Start(c.Context(), deployReq)
	if err != nil {
		return h.mapError(err)
	}

	return web.JSON(http.StatusAccepted, d), nil
}

func (h *DeployHandler) mapError(err error) (web.Response, error) {
	switch {
	case errors.Is(err, model.ErrDeploymentInProgress):
		return web.JSON(http.StatusConflict, map[string]string{
			"error": "deployment already in progress",
		}), nil
	case errors.Is(err, model.ErrImageNotAllowed):
		return web.JSON(http.StatusForbidden, map[string]string{
			"error": err.Error(),
		}), nil
	case errors.Is(err, model.ErrAppNotFound):
		return web.JSON(http.StatusNotFound, map[string]string{
			"error": "unknown app",
		}), nil
	default:
		return web.JSON(http.StatusInternalServerError, map[string]string{
			"error": "deployment failed",
		}), nil
	}
}
