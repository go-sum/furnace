package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sum/furnace/internal/model"
)

func TestCLI_Verify_PassesExpectedFlags(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.txt")
	cosignPath := filepath.Join(dir, "cosign")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" >" + logPath + "\n"
	if err := os.WriteFile(cosignPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)

	cli := NewCLI([]string{"DOCKER_CONFIG=/tmp/docker-config"})
	if err := cli.Verify(context.Background(), "ghcr.io/org/app:v1.2.3@sha256:abc123", "org/repo"); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"verify",
		"--certificate-identity-regexp",
		"^https://github\\.com/org/repo/",
		"--certificate-github-workflow-repository",
		"org/repo",
		"--certificate-oidc-issuer",
		githubOIDCIssuer,
		"ghcr.io/org/app:v1.2.3@sha256:abc123",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected args to contain %q, got:\n%s", want, got)
		}
	}
}

func TestCLI_Verify_ReportsSignatureError(t *testing.T) {
	dir := t.TempDir()
	cosignPath := filepath.Join(dir, "cosign")
	script := "#!/bin/sh\necho 'no signatures found' >&2\nexit 1\n"
	if err := os.WriteFile(cosignPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)

	cli := NewCLI(nil)
	err := cli.Verify(context.Background(), "ghcr.io/org/app@sha256:abc123", "org/repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no signatures found") {
		t.Fatalf("expected cosign stderr in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), model.ErrSignatureInvalid.Error()) {
		t.Fatalf("expected ErrSignatureInvalid wrapper, got: %v", err)
	}
}
