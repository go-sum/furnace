package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_NormalizesRelativePaths(t *testing.T) {
	path := writeConfig(t, `
listen: "127.0.0.1:8080"
data_dir: "/var/lib/furnace"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps:
  myapp:
    repo: "org/repo"
    allowed_ref: "refs/tags/v*"
    workflow: ".github/workflows/release.yml"
    dir: "/srv/apps/myapp"
    compose_files:
      - "nested/../docker-compose.data.yml"
      - "docker-compose.yml"
    env_file: "./configs/../.deploy.env"
    allowed_image_prefix: "ghcr.io/org/repo:"
    health_url: "http://127.0.0.1:8080/health"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if len(appCfg.ComposeFiles) != 2 || appCfg.ComposeFiles[0] != "docker-compose.data.yml" {
		t.Fatalf("expected normalized compose files, got %v", appCfg.ComposeFiles)
	}
	if appCfg.EnvFile != ".deploy.env" {
		t.Fatalf("expected normalized env file, got %q", appCfg.EnvFile)
	}
}

func TestLoadConfig_RejectsTraversingEnvFile(t *testing.T) {
	path := writeConfig(t, `
listen: "127.0.0.1:8080"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps:
  myapp:
    repo: "org/repo"
    allowed_ref: "refs/tags/v*"
    workflow: ".github/workflows/release.yml"
    dir: "/srv/apps/myapp"
    env_file: "../escape.env"
    allowed_image_prefix: "ghcr.io/org/repo:"
    health_url: "http://127.0.0.1:8080/health"
`)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "app \"myapp\": env_file: path must not escape the app directory"
	if err.Error() != expected {
		t.Fatalf("LoadConfig error mismatch:\ngot  %q\nwant %q", err.Error(), expected)
	}
}

func TestLoadConfig_RejectsAbsoluteComposeFile(t *testing.T) {
	path := writeConfig(t, `
listen: "127.0.0.1:8080"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps:
  myapp:
    repo: "org/repo"
    allowed_ref: "refs/tags/v*"
    workflow: ".github/workflows/release.yml"
    dir: "/srv/apps/myapp"
    compose_files:
      - "/tmp/compose.yml"
    allowed_image_prefix: "ghcr.io/org/repo:"
    health_url: "http://127.0.0.1:8080/health"
`)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "app \"myapp\": compose_files[0]: path must be relative"
	if err.Error() != expected {
		t.Fatalf("LoadConfig error mismatch:\ngot  %q\nwant %q", err.Error(), expected)
	}
}

func TestLoadConfig_RejectsInvalidHealthURLScheme(t *testing.T) {
	path := writeConfig(t, `
listen: "127.0.0.1:8080"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps:
  myapp:
    repo: "org/repo"
    allowed_ref: "refs/tags/v*"
    workflow: ".github/workflows/release.yml"
    dir: "/srv/apps/myapp"
    allowed_image_prefix: "ghcr.io/org/repo:"
    health_url: "ftp://127.0.0.1/health"
`)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "app \"myapp\": health_url must use http or https"
	if err.Error() != expected {
		t.Fatalf("LoadConfig error mismatch:\ngot  %q\nwant %q", err.Error(), expected)
	}
}

func validAppConfig(name string) string {
	return `
listen: "127.0.0.1:8080"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps:
  ` + name + `:
    repo: "org/repo"
    allowed_ref: "refs/tags/v*"
    workflow: ".github/workflows/release.yml"
    dir: "/srv/apps/myapp"
    allowed_image_prefix: "ghcr.io/org/repo:"
    health_url: "http://127.0.0.1:8080/health"
`
}

func TestLoadConfig_RejectsInvalidAppName(t *testing.T) {
	cases := []struct {
		name    string
		appName string
	}{
		{"uppercase", "MyApp"},
		{"path traversal", "../evil"},
		{"spaces", "my app"},
		{"starts with dash", "-myapp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, validAppConfig(tc.appName))
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("expected error for app name %q", tc.appName)
			}
		})
	}
}

func TestLoadConfig_AcceptsValidAppNames(t *testing.T) {
	cases := []string{"myapp", "my-app", "my_app", "app1", "a"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, validAppConfig(name))
			_, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("expected valid name %q to pass, got: %v", name, err)
			}
		})
	}
}

func TestLoadConfig_RejectsInvalidImageVar(t *testing.T) {
	path := writeConfig(t, `
listen: "127.0.0.1:8080"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps:
  myapp:
    repo: "org/repo"
    allowed_ref: "refs/tags/v*"
    workflow: ".github/workflows/release.yml"
    dir: "/srv/apps/myapp"
    allowed_image_prefix: "ghcr.io/org/repo:"
    health_url: "http://127.0.0.1:8080/health"
    image_var: "lower_case"
`)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid image_var")
	}
}

func TestLoadConfig_DefaultImageVar(t *testing.T) {
	path := writeConfig(t, validAppConfig("myapp"))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if appCfg.ImageVar != "APP_IMAGE" {
		t.Fatalf("expected default APP_IMAGE, got %q", appCfg.ImageVar)
	}
}

func TestLoadConfig_EmptyApps(t *testing.T) {
	path := writeConfig(t, `
listen: "127.0.0.1:8080"
github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"
apps: {}
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected empty apps to be valid, got: %v", err)
	}
	if len(cfg.Apps) != 0 {
		t.Fatalf("expected zero apps, got %d", len(cfg.Apps))
	}
}

func TestLoadConfig_DefaultComposeFiles(t *testing.T) {
	path := writeConfig(t, validAppConfig("myapp"))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if len(appCfg.ComposeFiles) != 2 {
		t.Fatalf("expected 2 default compose files, got %d", len(appCfg.ComposeFiles))
	}
	if appCfg.ComposeFiles[0] != "docker-compose.data.yml" || appCfg.ComposeFiles[1] != "docker-compose.yml" {
		t.Fatalf("unexpected default compose files: %v", appCfg.ComposeFiles)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "furnace.yaml")
	if err := os.WriteFile(path, []byte(contents), 0640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
