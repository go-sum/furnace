package cli

import (
	"strings"
	"testing"

	"github.com/go-sum/furnace/internal/model"
)

func makeTestApps(apps ...model.AppConfig) []model.AppConfig {
	return apps
}

func TestGenerateCaddyfile_TLSEnabled(t *testing.T) {
	apps := makeTestApps(model.AppConfig{
		Name:   "myapp",
		Domain: "myapp.example.com",
		Port:   8080,
		TLS:    true,
	})
	out, err := generateCaddyfile(apps)
	if err != nil {
		t.Fatalf("generateCaddyfile: %v", err)
	}
	if !strings.Contains(string(out), "tls /certs/local.pem /certs/local-key.pem") {
		t.Fatalf("expected tls directive in output:\n%s", out)
	}
}

func TestGenerateCaddyfile_TLSDisabled(t *testing.T) {
	apps := makeTestApps(model.AppConfig{
		Name:   "myapp",
		Domain: "myapp.example.com",
		Port:   8080,
		TLS:    false,
	})
	out, err := generateCaddyfile(apps)
	if err != nil {
		t.Fatalf("generateCaddyfile: %v", err)
	}
	if strings.Contains(string(out), "tls /certs/local.pem") {
		t.Fatalf("expected no tls directive when tls unset, got:\n%s", out)
	}
}

func TestGenerateCaddyfile_MixedTLS(t *testing.T) {
	apps := makeTestApps(
		model.AppConfig{Name: "cloudapp", Domain: "cloud.example.com", Port: 8080, TLS: false},
		model.AppConfig{Name: "localapp", Domain: "local.example.com", Port: 8080, TLS: true},
	)
	out, err := generateCaddyfile(apps)
	if err != nil {
		t.Fatalf("generateCaddyfile: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "tls /certs/local.pem /certs/local-key.pem") {
		t.Fatalf("expected tls directive for localapp:\n%s", body)
	}
	cloudIdx := strings.Index(body, "cloud.example.com")
	localIdx := strings.Index(body, "local.example.com")
	if cloudIdx == -1 || localIdx == -1 {
		t.Fatalf("expected both domains in output:\n%s", body)
	}
	tlsIdx := strings.Index(body, "tls /certs/local.pem")
	if tlsIdx == -1 {
		t.Fatalf("expected tls directive somewhere in output:\n%s", body)
	}
	if localIdx > cloudIdx {
		cloudEnd := strings.Index(body[cloudIdx:], "}") + cloudIdx
		if tlsIdx < cloudEnd {
			t.Fatalf("tls directive appears inside cloudapp block:\n%s", body)
		}
	} else {
		if tlsIdx > cloudIdx {
			t.Fatalf("tls directive appears inside cloudapp block:\n%s", body)
		}
	}
}
