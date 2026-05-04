package model

import "time"

type AuditEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	AppName    string    `json:"app"`
	Action     string    `json:"action"`
	Status     string    `json:"status"`
	Image      string    `json:"image,omitempty"`
	Tag        string    `json:"tag,omitempty"`
	Digest     string    `json:"digest,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms,omitempty"`
}
