package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sum/furnace/internal/app"
)

func loadTestConfig(t *testing.T, yml string) *app.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "furnace.yaml")
	if err := os.WriteFile(path, []byte(yml), 0640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := app.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

const minimalAppYAML = `
data_dir: "/var/lib/furnace"
apps:
  myapp:
    image: "ghcr.io/org/myapp"
    tag_pattern: "v*"
    allowed_identity: "org/myapp"
    artifact: "ghcr.io/org/myapp:{tag}-compose"
    domain: "myapp.example.com"
    container: "myapp-web-1"
`

func TestGenerateCaddyfile_TLSEnabled(t *testing.T) {
	cfg := loadTestConfig(t, `
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
	out, err := generateCaddyfile(cfg)
	if err != nil {
		t.Fatalf("generateCaddyfile: %v", err)
	}
	if !strings.Contains(string(out), "tls /certs/local.pem /certs/local-key.pem") {
		t.Fatalf("expected tls directive in output:\n%s", out)
	}
}

func TestGenerateCaddyfile_TLSDisabled(t *testing.T) {
	cfg := loadTestConfig(t, minimalAppYAML)
	out, err := generateCaddyfile(cfg)
	if err != nil {
		t.Fatalf("generateCaddyfile: %v", err)
	}
	if strings.Contains(string(out), "tls /certs/local.pem") {
		t.Fatalf("expected no tls directive when tls unset, got:\n%s", out)
	}
}

func TestGenerateCaddyfile_MixedTLS(t *testing.T) {
	cfg := loadTestConfig(t, `
data_dir: "/var/lib/furnace"
apps:
  localapp:
    image: "ghcr.io/org/localapp"
    tag_pattern: "v*"
    allowed_identity: "org/localapp"
    artifact: "ghcr.io/org/localapp:{tag}-compose"
    domain: "local.example.com"
    container: "localapp-web-1"
    tls: true
  cloudapp:
    image: "ghcr.io/org/cloudapp"
    tag_pattern: "v*"
    allowed_identity: "org/cloudapp"
    artifact: "ghcr.io/org/cloudapp:{tag}-compose"
    domain: "cloud.example.com"
    container: "cloudapp-web-1"
`)
	out, err := generateCaddyfile(cfg)
	if err != nil {
		t.Fatalf("generateCaddyfile: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "tls /certs/local.pem /certs/local-key.pem") {
		t.Fatalf("expected tls directive for localapp:\n%s", body)
	}
	// cloud.example.com block must not have a tls directive
	cloudIdx := strings.Index(body, "cloud.example.com")
	localIdx := strings.Index(body, "local.example.com")
	if cloudIdx == -1 || localIdx == -1 {
		t.Fatalf("expected both domains in output:\n%s", body)
	}
	// The only tls directive should appear before or within the localapp block, not cloudapp block.
	tlsIdx := strings.Index(body, "tls /certs/local.pem")
	if tlsIdx == -1 {
		t.Fatalf("expected tls directive somewhere in output:\n%s", body)
	}
	// Ensure tls directive is in localapp block (which comes after localIdx) and not in cloudapp block
	if localIdx > cloudIdx {
		// cloudapp block comes first; the tls directive must be after cloudapp block ends
		cloudEnd := strings.Index(body[cloudIdx:], "}") + cloudIdx
		if tlsIdx < cloudEnd {
			t.Fatalf("tls directive appears inside cloudapp block:\n%s", body)
		}
	} else {
		// localapp block comes first; the tls directive must be before cloudapp block starts
		if tlsIdx > cloudIdx {
			t.Fatalf("tls directive appears inside cloudapp block:\n%s", body)
		}
	}
}
