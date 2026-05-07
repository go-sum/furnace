package model

import "time"

// AppConfig is the resolved, validated configuration for a single app.
type AppConfig struct {
	Name            string
	Image           string
	TagPattern      string
	AllowedIdentity string
	Dir             string
	Domain          string
	Port            int
	TLS             bool
	EnvFile         string
	ImageVar        string
	Container       string
	HealthTimeout   time.Duration
	Artifact        string
	KeepReleases    int
}
