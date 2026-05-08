package model

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	validAppName       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	validDomain        = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	validContainerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,252}$`)
)

// ValidateAppName reports whether name matches the valid app name pattern.
func ValidateAppName(name string) bool {
	return validAppName.MatchString(name)
}

// containsUnsafe reports whether s contains null bytes, control characters,
// or shell metacharacters that could be injected into shell commands or
// Docker Compose files.
func containsUnsafe(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == 0 || b < 32 || b == 127 {
			return true
		}
	}
	return strings.ContainsAny(s, "\"'$&;|<>`()")
}

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

// Validate returns ErrInvalidConfig (wrapped with detail) if any field is invalid.
func (a AppConfig) Validate() error {
	wrap := func(msg string) error {
		return fmt.Errorf("app %q: %s: %w", a.Name, msg, ErrInvalidConfig)
	}

	if !validAppName.MatchString(a.Name) {
		return wrap("name must be lowercase alphanumeric with hyphens, max 63 chars")
	}
	if a.Image == "" {
		return wrap("image is required")
	}
	if len(a.Image) > 500 {
		return wrap("image must be 500 characters or fewer")
	}
	if strings.ContainsAny(a.Image, " \t\r\n") || !strings.Contains(a.Image, "/") {
		return wrap("image must be a valid OCI reference (e.g. ghcr.io/org/app)")
	}
	if containsUnsafe(a.Image) {
		return wrap("image contains unsafe characters")
	}
	if a.TagPattern == "" {
		return wrap("tag_pattern is required")
	}
	if len(a.TagPattern) > 100 {
		return wrap("tag_pattern must be 100 characters or fewer")
	}
	if containsUnsafe(a.TagPattern) {
		return wrap("tag_pattern contains unsafe characters")
	}
	if a.AllowedIdentity == "" {
		return wrap("allowed_identity is required")
	}
	if len(a.AllowedIdentity) > 200 {
		return wrap("allowed_identity must be 200 characters or fewer")
	}
	if !strings.Contains(a.AllowedIdentity, "/") {
		return wrap("allowed_identity must be in org/repo format")
	}
	if containsUnsafe(a.AllowedIdentity) {
		return wrap("allowed_identity contains unsafe characters")
	}
	if a.Domain == "" {
		return wrap("domain is required")
	}
	if !validDomain.MatchString(a.Domain) {
		return wrap("domain must be a valid lowercase hostname (e.g. app.example.com)")
	}
	if a.Dir == "" || !filepath.IsAbs(a.Dir) {
		return wrap("dir must be an absolute path")
	}
	if len(a.Dir) > 500 {
		return wrap("dir must be 500 characters or fewer")
	}
	if containsUnsafe(a.Dir) {
		return wrap("dir contains unsafe characters")
	}
	if a.Port <= 0 {
		return wrap("port must be greater than 0")
	}
	if a.Container == "" {
		return wrap("container is required")
	}
	if !validContainerName.MatchString(a.Container) {
		return wrap("container must be a valid Docker container name")
	}
	if containsUnsafe(a.Container) {
		return wrap("container name contains unsafe characters")
	}
	if a.Artifact == "" {
		return wrap("artifact is required")
	}
	if len(a.Artifact) > 500 {
		return wrap("artifact must be 500 characters or fewer")
	}
	if containsUnsafe(a.Artifact) {
		return wrap("artifact contains unsafe characters")
	}
	if a.EnvFile != "" {
		if filepath.IsAbs(a.EnvFile) {
			return wrap("env_file must be relative")
		}
		clean := filepath.Clean(a.EnvFile)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return wrap("env_file must not escape the app directory")
		}
	}
	if a.KeepReleases < 1 {
		return wrap("keep_releases must be at least 1")
	}
	return nil
}
