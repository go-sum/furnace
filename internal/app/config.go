package app

import (
	"cmp"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-sum/furnace/internal/auth"
	"github.com/go-sum/furnace/internal/model"
	"gopkg.in/yaml.v3"
)

var (
	validAppName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	validEnvVar  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

type Config struct {
	Listen  string            `yaml:"listen"`
	DataDir string            `yaml:"data_dir"`
	GitHub  GitHubConfig      `yaml:"github"`
	Apps    map[string]AppRaw `yaml:"apps"`
}

type GitHubConfig struct {
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`
}

type AppRaw struct {
	Repo               string              `yaml:"repo"`
	AllowedRef         string              `yaml:"allowed_ref"`
	Workflow           string              `yaml:"workflow"`
	Dir                string              `yaml:"dir"`
	ComposeFiles       []string            `yaml:"compose_files"`
	EnvFile            string              `yaml:"env_file"`
	ImageVar           string              `yaml:"image_var"`
	AllowedImagePrefix string              `yaml:"allowed_image_prefix"`
	HealthURL          string              `yaml:"health_url"`
	HealthTimeout      time.Duration       `yaml:"health_timeout"`
	Backup             model.CommandConfig `yaml:"backup"`
	Migrate            model.CommandConfig `yaml:"migrate"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.Listen = cmp.Or(cfg.Listen, "127.0.0.1:8080")
	cfg.DataDir = cmp.Or(cfg.DataDir, "/var/lib/furnace")

	if !isLoopbackAddr(cfg.Listen) {
		return nil, fmt.Errorf("listen address %q is not a loopback address; furnace must be behind a reverse proxy for TLS", cfg.Listen)
	}

	if cfg.GitHub.Issuer == "" {
		return nil, fmt.Errorf("github.issuer is required")
	}
	if cfg.GitHub.Audience == "" {
		return nil, fmt.Errorf("github.audience is required")
	}

	for name := range cfg.Apps {
		if !validAppName.MatchString(name) {
			return nil, fmt.Errorf("app name %q: must be lowercase alphanumeric with hyphens or underscores, max 63 chars", name)
		}
		raw, err := cfg.validateApp(name)
		if err != nil {
			return nil, fmt.Errorf("app %q: %w", name, err)
		}
		cfg.Apps[name] = raw
	}

	return &cfg, nil
}

func (c *Config) AppConfig(name string) (model.AppConfig, bool) {
	raw, ok := c.Apps[name]
	if !ok {
		return model.AppConfig{}, false
	}

	return model.AppConfig{
		Name:               name,
		Repo:               raw.Repo,
		AllowedRef:         raw.AllowedRef,
		Workflow:           raw.Workflow,
		Dir:                raw.Dir,
		ComposeFiles:       raw.ComposeFiles,
		EnvFile:            raw.EnvFile,
		ImageVar:           raw.ImageVar,
		AllowedImagePrefix: raw.AllowedImagePrefix,
		HealthURL:          raw.HealthURL,
		HealthTimeout:      cmp.Or(raw.HealthTimeout, 30*time.Second),
		Backup:             raw.Backup,
		Migrate:            raw.Migrate,
	}, true
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Config) validateApp(name string) (AppRaw, error) {
	raw := c.Apps[name]

	if raw.Repo == "" {
		return AppRaw{}, fmt.Errorf("repo is required")
	}
	if raw.AllowedRef == "" {
		return AppRaw{}, fmt.Errorf("allowed_ref is required")
	}
	if err := auth.ValidateRefPattern(raw.AllowedRef); err != nil {
		return AppRaw{}, err
	}
	if raw.Workflow == "" {
		return AppRaw{}, fmt.Errorf("workflow is required")
	}
	if !strings.HasPrefix(raw.Workflow, ".github/workflows/") {
		return AppRaw{}, fmt.Errorf("workflow must be a .github/workflows path")
	}
	if raw.Dir == "" {
		return AppRaw{}, fmt.Errorf("dir is required")
	}
	if !filepath.IsAbs(raw.Dir) {
		return AppRaw{}, fmt.Errorf("dir must be an absolute path")
	}
	if raw.AllowedImagePrefix == "" {
		return AppRaw{}, fmt.Errorf("allowed_image_prefix is required")
	}
	if raw.HealthURL == "" {
		return AppRaw{}, fmt.Errorf("health_url is required")
	}
	if len(raw.ComposeFiles) == 0 {
		raw.ComposeFiles = []string{"docker-compose.data.yml", "docker-compose.yml"}
	}
	for i, f := range raw.ComposeFiles {
		normalized, err := normalizeRelativePath(f)
		if err != nil {
			return AppRaw{}, fmt.Errorf("compose_files[%d]: %w", i, err)
		}
		raw.ComposeFiles[i] = normalized
	}
	envFile, err := normalizeRelativePath(cmp.Or(raw.EnvFile, ".deploy.env"))
	if err != nil {
		return AppRaw{}, fmt.Errorf("env_file: %w", err)
	}
	imageVar := cmp.Or(raw.ImageVar, "APP_IMAGE")
	if !validEnvVar.MatchString(imageVar) {
		return AppRaw{}, fmt.Errorf("image_var %q: must be a valid environment variable name (uppercase letters, digits, underscores)", imageVar)
	}
	raw.ImageVar = imageVar
	parsedURL, err := url.Parse(raw.HealthURL)
	if err != nil {
		return AppRaw{}, fmt.Errorf("health_url is invalid: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return AppRaw{}, fmt.Errorf("health_url must use http or https")
	}
	if parsedURL.Host == "" {
		return AppRaw{}, fmt.Errorf("health_url must include a host")
	}

	raw.EnvFile = envFile
	return raw, nil
}

func normalizeRelativePath(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("path must be relative")
	}

	clean := filepath.Clean(value)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must not escape the app directory")
	}

	return clean, nil
}
