package app

import (
	"cmp"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-sum/furnace/internal/model"
	"gopkg.in/yaml.v3"
)

var validAppName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
var validDomain = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)

type Config struct {
	DataDir      string            `yaml:"data_dir"`
	PollInterval time.Duration     `yaml:"poll_interval"`
	Apps         map[string]AppRaw `yaml:"apps"`
}

type AppRaw struct {
	Image           string        `yaml:"image"`
	TagPattern      string        `yaml:"tag_pattern"`
	AllowedIdentity string        `yaml:"allowed_identity"`
	Dir             string        `yaml:"dir"`
	Domain          string        `yaml:"domain"`
	Port            int           `yaml:"port"`
	ComposeFiles    []string      `yaml:"compose_files"`
	EnvFile         string        `yaml:"env_file"`
	ImageVar        string        `yaml:"image_var"`
	HealthURL       string        `yaml:"health_url"`
	HealthTimeout   time.Duration `yaml:"health_timeout"`
	ComposeArtifact string        `yaml:"compose_artifact"`
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

	cfg.DataDir = cmp.Or(cfg.DataDir, "/var/lib/furnace")
	cfg.PollInterval = cmp.Or(cfg.PollInterval, 60*time.Second)

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
		Name:            name,
		Image:           raw.Image,
		TagPattern:      raw.TagPattern,
		AllowedIdentity: raw.AllowedIdentity,
		Dir:             raw.Dir,
		Domain:          raw.Domain,
		Port:            raw.Port,
		ComposeFiles:    raw.ComposeFiles,
		EnvFile:         raw.EnvFile,
		ImageVar:        raw.ImageVar,
		HealthURL:       raw.HealthURL,
		HealthTimeout:   cmp.Or(raw.HealthTimeout, 30*time.Second),
		ComposeArtifact: raw.ComposeArtifact,
	}, true
}

func (c *Config) AllAppConfigs() map[string]model.AppConfig {
	out := make(map[string]model.AppConfig, len(c.Apps))
	for name := range c.Apps {
		cfg, _ := c.AppConfig(name)
		out[name] = cfg
	}
	return out
}

func (c *Config) validateApp(name string) (AppRaw, error) {
	raw := c.Apps[name]

	if raw.Image == "" {
		return AppRaw{}, fmt.Errorf("image is required")
	}
	if raw.TagPattern == "" {
		return AppRaw{}, fmt.Errorf("tag_pattern is required")
	}
	if raw.AllowedIdentity == "" {
		return AppRaw{}, fmt.Errorf("allowed_identity is required")
	}
	if !strings.Contains(raw.AllowedIdentity, "/") {
		return AppRaw{}, fmt.Errorf("allowed_identity must be in org/repo format")
	}
	if raw.Dir == "" {
		raw.Dir = "/srv/apps/" + name
	}
	if !filepath.IsAbs(raw.Dir) {
		return AppRaw{}, fmt.Errorf("dir must be an absolute path")
	}
	if raw.Domain == "" {
		return AppRaw{}, fmt.Errorf("domain is required")
	}
	if !validDomain.MatchString(raw.Domain) {
		return AppRaw{}, fmt.Errorf("domain must be a valid lowercase hostname (e.g. app.example.com)")
	}
	raw.Port = cmp.Or(raw.Port, 8080)
	if raw.HealthURL == "" {
		return AppRaw{}, fmt.Errorf("health_url is required")
	}
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
	raw.EnvFile = envFile
	raw.ImageVar = cmp.Or(raw.ImageVar, "APP_IMAGE")

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
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must not escape the app directory")
	}
	return clean, nil
}
