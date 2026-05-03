package model

import "time"

type AppConfig struct {
	Name               string        `yaml:"-"`
	Repo               string        `yaml:"repo"`
	AllowedRef         string        `yaml:"allowed_ref"`
	Workflow           string        `yaml:"workflow"`
	Dir                string        `yaml:"dir"`
	ComposeFiles       []string      `yaml:"compose_files"`
	EnvFile            string        `yaml:"env_file"`
	ImageVar           string        `yaml:"image_var"`
	AllowedImagePrefix string        `yaml:"allowed_image_prefix"`
	HealthURL          string        `yaml:"health_url"`
	HealthTimeout      time.Duration `yaml:"health_timeout"`
	Backup             CommandConfig `yaml:"backup"`
	Migrate            CommandConfig `yaml:"migrate"`
}

type CommandConfig struct {
	Enabled bool     `yaml:"enabled"`
	Args    []string `yaml:"args"`
}
