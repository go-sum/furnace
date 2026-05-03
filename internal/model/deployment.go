package model

import "time"

type DeploymentStatus string

const (
	StatusPending     DeploymentStatus = "pending"
	StatusPulling     DeploymentStatus = "pulling"
	StatusBackingUp   DeploymentStatus = "backing_up"
	StatusMigrating   DeploymentStatus = "migrating"
	StatusStarting    DeploymentStatus = "starting"
	StatusHealthCheck DeploymentStatus = "health_check"
	StatusCompleted   DeploymentStatus = "completed"
	StatusFailed      DeploymentStatus = "failed"
	StatusRolledBack  DeploymentStatus = "rolled_back"
)

func (s DeploymentStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusRolledBack
}

type DeployRequest struct {
	AppName   string
	Image     string
	Actor     string
	Repo      string
	Ref       string
	Workflow  string
	RunID     string
	RequestID string
}

type Deployment struct {
	ID        string           `json:"id"`
	AppName   string           `json:"app_name"`
	Image     string           `json:"image"`
	PrevImage string           `json:"prev_image,omitempty"`
	Status    DeploymentStatus `json:"status"`
	Actor     string           `json:"actor"`
	Repo      string           `json:"repo"`
	Ref       string           `json:"ref"`
	RunID     string           `json:"run_id,omitempty"`
	RequestID string           `json:"request_id,omitempty"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   time.Time        `json:"ended_at,omitempty"`
	Error     string           `json:"error,omitempty"`
}
