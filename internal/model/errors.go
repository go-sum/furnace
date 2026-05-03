package model

import "errors"

var (
	ErrAppNotFound          = errors.New("app not found")
	ErrDeploymentInProgress = errors.New("deployment already in progress")
	ErrHealthCheckFailed    = errors.New("health check failed")
	ErrImageNotAllowed      = errors.New("image does not match allowed prefix")
	ErrRefNotAllowed        = errors.New("ref does not match allowed pattern")
	ErrRepoMismatch         = errors.New("token repo does not match app config")
	ErrWorkflowMismatch     = errors.New("token workflow does not match app config")
	ErrWorkflowClaimMissing = errors.New("token workflow identity claim is missing")
	ErrWorkflowClaimInvalid = errors.New("token workflow identity claim is invalid")
	ErrTokenInvalid         = errors.New("token verification failed")
	ErrBackupFailed         = errors.New("backup failed")
	ErrMigrateFailed        = errors.New("migration failed")
	ErrNoPreviousDeployment = errors.New("no previous deployment to rollback to")
)
