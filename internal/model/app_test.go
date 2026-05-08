package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validApp() AppConfig {
	return AppConfig{
		Name:            "myapp",
		Image:           "ghcr.io/org/myapp:latest",
		TagPattern:      "v*",
		AllowedIdentity: "org/myapp",
		Artifact:        "ghcr.io/org/myapp:compose",
		Domain:          "myapp.example.com",
		Dir:             "/srv/apps/myapp",
		Port:            8080,
		Container:       "myapp-web-1",
		HealthTimeout:   30 * time.Second,
		TLS:             false,
		EnvFile:         ".deploy.env",
		ImageVar:        "APP_IMAGE",
		KeepReleases:    5,
	}
}

func TestValidate_ValidApp(t *testing.T) {
	if err := validApp().Validate(); err != nil {
		t.Fatalf("expected valid app to pass, got: %v", err)
	}
}

func TestValidate_Name(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"lowercase alphanum", "myapp", false},
		{"with hyphen", "my-app", false},
		{"starts with digit", "1app", false},
		{"max length 63", strings.Repeat("a", 63), false},
		{"empty", "", true},
		{"uppercase", "MyApp", true},
		{"starts with hyphen", "-app", true},
		{"too long 64 chars", strings.Repeat("a", 64), true},
		{"contains underscore", "my_app", true},
		{"contains dot", "my.app", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Name = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for name %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for name %q, got: %v", tc.value, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("expected ErrInvalidConfig, got: %v", err)
			}
		})
	}
}

func TestValidate_Image(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid OCI ref", "ghcr.io/org/app:latest", false},
		{"empty", "", true},
		{"no slash", "justanimage", true},
		{"with space", "ghcr.io/org/app latest", true},
		{"with tab", "ghcr.io/org/app\tlatest", true},
		{"with semicolon", "ghcr.io/org/app;rm -rf /", true},
		{"with dollar sign", "ghcr.io/org/app$cmd", true},
		{"with backtick", "ghcr.io/org/app`cmd`", true},
		{"with pipe", "ghcr.io/org/app|cmd", true},
		{"with null byte", "ghcr.io/org/app\x00", true},
		{"with control char", "ghcr.io/org/app\x01", true},
		{"too long", "ghcr.io/org/" + strings.Repeat("a", 490), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Image = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for image %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for image %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_TagPattern(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"glob star", "v*", false},
		{"literal", "v1.0.0", false},
		{"empty", "", true},
		{"with dollar", "v$cmd", true},
		{"with semicolon", "v1;cmd", true},
		{"too long", strings.Repeat("v", 101), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.TagPattern = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for tag_pattern %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for tag_pattern %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_AllowedIdentity(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"org/repo", "org/repo", false},
		{"empty", "", true},
		{"no slash", "orgrepo", true},
		{"with semicolon", "org/repo;cmd", true},
		{"too long", strings.Repeat("a", 201), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.AllowedIdentity = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for allowed_identity %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for allowed_identity %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_Domain(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid subdomain", "app.example.com", false},
		{"multi-level", "a.b.example.com", false},
		{"empty", "", true},
		{"no dot", "localhost", true},
		{"uppercase", "App.Example.com", true},
		{"leading dot", ".example.com", true},
		{"ip address", "192.168.1.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Domain = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for domain %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for domain %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_Dir(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"absolute path", "/srv/apps/myapp", false},
		{"empty", "", true},
		{"relative path", "srv/apps/myapp", true},
		{"with semicolon", "/srv/apps;cmd", true},
		{"with dollar", "/srv/apps/$cmd", true},
		{"too long", "/" + strings.Repeat("a", 500), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Dir = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for dir %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for dir %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_Port(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"positive port", 8080, false},
		{"port 1", 1, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Port = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for port %d, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for port %d, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_Container(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid name", "myapp-web-1", false},
		{"with underscore", "myapp_web", false},
		{"with dot", "myapp.web", false},
		{"empty", "", true},
		{"starts with hyphen", "-myapp", true},
		{"max length 253", "a" + strings.Repeat("b", 252), false},
		{"too long 254 chars", "a" + strings.Repeat("b", 253), true},
		{"with semicolon", "myapp;cmd", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Container = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for container %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for container %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_Artifact(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"OCI ref", "ghcr.io/org/app:compose", false},
		{"with tag template", "ghcr.io/org/app:{tag}-compose", false},
		{"empty", "", true},
		{"with semicolon", "ghcr.io/org/app;cmd", true},
		{"too long", "ghcr.io/org/" + strings.Repeat("a", 490), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.Artifact = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for artifact %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for artifact %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_EnvFile(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty (optional)", "", false},
		{"relative file", ".deploy.env", false},
		{"nested relative", "config/.env", false},
		{"absolute path", "/etc/myapp.env", true},
		{"path traversal", "../secrets.env", true},
		{"dot dot in middle", "foo/../../etc/passwd", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.EnvFile = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for env_file %q, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for env_file %q, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidate_KeepReleases(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"one", 1, false},
		{"five", 5, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validApp()
			a.KeepReleases = tc.value
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for keep_releases %d, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for keep_releases %d, got: %v", tc.value, err)
			}
		})
	}
}

func TestValidateAppName(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid", "myapp", true},
		{"with hyphen", "my-app", true},
		{"empty", "", false},
		{"uppercase", "MyApp", false},
		{"starts with hyphen", "-app", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateAppName(tc.input); got != tc.want {
				t.Errorf("ValidateAppName(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
