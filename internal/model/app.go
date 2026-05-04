package model

import "time"

// AppConfig is the resolved, validated configuration for a single app.
type AppConfig struct {
	Name            string
	Image           string        // base image ref without tag: ghcr.io/org/myapp
	TagPattern      string        // glob for tags to watch: v*
	AllowedIdentity string        // Sigstore OIDC identity: org/myapp (GitHub repo slug)
	Dir             string        // on-disk location: /srv/apps/myapp
	Domain          string        // Caddy vhost: myapp.example.com
	Port            int
	ComposeFiles    []string      // relative paths: [docker-compose.data.yml, docker-compose.yml]
	EnvFile         string        // relative path: .deploy.env
	ImageVar        string        // env var name: APP_IMAGE
	HealthURL       string        // full URL: http://myapp-web-1:8080/healthz
	HealthTimeout   time.Duration
}
