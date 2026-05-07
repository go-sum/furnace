package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validAppYAML returns a minimal valid app entry for use in config snippets.
func validAppYAML(name string) string {
	return `
data_dir: "/var/lib/furnace"
apps:
  ` + name + `:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "` + name + `-web-1"
`
}

func TestLoadConfig_Defaults(t *testing.T) {
	path := writeConfig(t, validAppYAML("myapp"))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DataDir != "/var/lib/furnace" {
		t.Fatalf("unexpected data_dir: %q", cfg.DataDir)
	}

	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if appCfg.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", appCfg.Port)
	}
	if appCfg.Dir != "/srv/apps/myapp" {
		t.Fatalf("expected default dir /srv/apps/myapp, got %q", appCfg.Dir)
	}
	if appCfg.ImageVar != "APP_IMAGE" {
		t.Fatalf("expected default image_var APP_IMAGE, got %q", appCfg.ImageVar)
	}
	if appCfg.Artifact != "ghcr.io/org/myapp:{tag}-compose" {
		t.Fatalf("expected artifact ghcr.io/org/myapp:{tag}-compose, got %q", appCfg.Artifact)
	}
	if appCfg.EnvFile != ".deploy.env" {
		t.Fatalf("expected default env_file .deploy.env, got %q", appCfg.EnvFile)
	}
}


func TestLoadConfig_RejectsTraversingEnvFile(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    env_file: "../escape.env"
    container: "myapp-web-1"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := `app "myapp": env_file: path must not escape the app directory`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}


func TestLoadConfig_RejectsInvalidContainerName(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "-invalid"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := `app "myapp": container must be a valid Docker container name`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestLoadConfig_RejectsInvalidAppName(t *testing.T) {
	cases := []struct {
		name    string
		appName string
	}{
		{"uppercase", "MyApp"},
		{"spaces", "my app"},
		{"starts with dash", "-myapp"},
		{"underscore", "my_app"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, validAppYAML(tc.appName))
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("expected error for app name %q", tc.appName)
			}
		})
	}
}

func TestLoadConfig_AcceptsValidAppNames(t *testing.T) {
	for _, name := range []string{"myapp", "my-app", "app1", "a"} {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, validAppYAML(name))
			_, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("expected valid name %q to pass, got: %v", name, err)
			}
		})
	}
}

func TestLoadConfig_RejectsAllowedIdentityWithoutSlash(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "notaslug"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "myapp-web-1"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := `app "myapp": allowed_identity must be in org/repo format`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestLoadConfig_RequiresDomain(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    container: "myapp-web-1"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing domain")
	}
	want := `app "myapp": domain is required`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestLoadConfig_EmptyApps(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
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

func TestLoadConfig_ValidDomains(t *testing.T) {
	for _, domain := range []string{"myapp.example.com", "sub.deep.example.co.uk", "a.io", "furnace.server"} {
		t.Run(domain, func(t *testing.T) {
			path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "`+domain+`"
    container: "myapp-web-1"
`)
			_, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("expected valid domain %q to pass, got: %v", domain, err)
			}
		})
	}
}

func TestLoadConfig_RejectsInvalidDomains(t *testing.T) {
	cases := []struct {
		name   string
		domain string
	}{
		{"newline injection", "evil\n{inject}"},
		{"spaces", "has spaces.com"},
		{"uppercase", "UPPER.COM"},
		{"bare tld", "bare-tld"},
		{"leading dash", "-leading.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    domain: "`+tc.domain+`"
    container: "myapp-web-1"
`)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("expected domain %q to be rejected", tc.domain)
			}
		})
	}
}

func TestLoadConfig_ValidTrustedProxies(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
trusted_proxies:
  - "172.16.0.0/12"
  - "10.0.0.0/8"
apps: {}
`)
	_, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected valid trusted_proxies to pass, got: %v", err)
	}
}

func TestLoadConfig_RejectsInvalidTrustedProxies(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
trusted_proxies:
  - "not-a-cidr"
apps: {}
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	want := `invalid trusted_proxies entry "not-a-cidr": invalid CIDR address: not-a-cidr`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestLoadConfig_TLSDefaultsFalse(t *testing.T) {
	path := writeConfig(t, validAppYAML("myapp"))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if appCfg.TLS {
		t.Fatal("expected TLS to default to false when unset")
	}
}

func TestLoadConfig_TLSTrue(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "myapp-web-1"
    tls: true
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if !appCfg.TLS {
		t.Fatal("expected TLS to be true")
	}
}

func TestLoadConfig_RequiresArtifact(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    domain: "myapp.example.com"
    container: "myapp-web-1"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
	want := `app "myapp": artifact is required`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}


func TestLoadConfig_KeepReleasesDefault(t *testing.T) {
	path := writeConfig(t, validAppYAML("myapp"))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if appCfg.KeepReleases != 5 {
		t.Fatalf("expected default keep_releases 5, got %d", appCfg.KeepReleases)
	}
}

func TestLoadConfig_KeepReleasesExplicit(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "myapp-web-1"
    keep_releases: 10
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	appCfg, ok := cfg.AppConfig("myapp")
	if !ok {
		t.Fatal("expected app config")
	}
	if appCfg.KeepReleases != 10 {
		t.Fatalf("expected keep_releases 10, got %d", appCfg.KeepReleases)
	}
}

func TestLoadConfig_KeepReleasesRejectsZero(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "myapp-web-1"
    keep_releases: -1
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for keep_releases < 1")
	}
	want := `app "myapp": keep_releases must be at least 1`
	if err.Error() != want {
		t.Fatalf("error mismatch:\ngot  %q\nwant %q", err.Error(), want)
	}
}

func TestLoadConfig_RejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
data_dir: "/var/lib/furnace"
alowed_identity: "typo-field"
apps: {}
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("expected 'parse config' in error, got: %v", err)
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
