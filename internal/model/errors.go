package model

import "errors"

var (
	ErrAppNotFound          = errors.New("app not found")
	ErrDeploymentInProgress = errors.New("deployment already in progress")
	ErrHealthCheckFailed    = errors.New("health check failed")
	ErrImageInvalid         = errors.New("image ref contains invalid characters")
	ErrSignatureInvalid     = errors.New("image signature verification failed")
)
