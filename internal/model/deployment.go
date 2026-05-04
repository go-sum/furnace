package model

import "time"

type DeploymentStatus string

const (
	StatusPending     DeploymentStatus = "pending"
	StatusPulling     DeploymentStatus = "pulling"
	StatusStarting    DeploymentStatus = "starting"
	StatusHealthCheck DeploymentStatus = "health_check"
	StatusCompleted   DeploymentStatus = "completed"
	StatusFailed      DeploymentStatus = "failed"
)

func (s DeploymentStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed
}

type DeployRequest struct {
	AppName string
	Image   string // full ref with tag: ghcr.io/org/myapp:v1.2.0
	Tag     string // just the tag: v1.2.0
	Digest  string // image digest: sha256:...
}

type Deployment struct {
	ID        string           `json:"id"`
	AppName   string           `json:"app_name"`
	Image     string           `json:"image"`
	Tag       string           `json:"tag"`
	Digest    string           `json:"digest"`
	PrevImage string           `json:"prev_image,omitempty"`
	Status    DeploymentStatus `json:"status"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   time.Time        `json:"ended_at,omitempty"`
	Error     string           `json:"error,omitempty"`
}
