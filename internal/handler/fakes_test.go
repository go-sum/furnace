package handler

import (
	"context"

	"github.com/go-sum/furnace/internal/model"
)

type fakeDeployer struct {
	deployment *model.Deployment
	err        error
}

func (f *fakeDeployer) Status(_ context.Context, _ string) (*model.Deployment, error) {
	return f.deployment, f.err
}

type fakeAppChecker struct {
	exists bool
	err    error
}

func (f *fakeAppChecker) AppExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.err
}
