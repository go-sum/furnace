package cli

import (
	"strings"
	"testing"
)

func TestRenderWorkerUnit_WithCredential(t *testing.T) {
	got, err := renderWorkerUnit(true)
	if err != nil {
		t.Fatalf("renderWorkerUnit(true): %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "LoadCredentialEncrypted=registry-token:/etc/furnace/registry-token.cred") {
		t.Errorf("expected LoadCredentialEncrypted in unit, got:\n%s", s)
	}
	if !strings.Contains(s, "ExecStart=/usr/local/bin/furnace worker run") {
		t.Errorf("expected 'worker run' in ExecStart, got:\n%s", s)
	}
}

func TestRenderWorkerUnit_WithoutCredential(t *testing.T) {
	got, err := renderWorkerUnit(false)
	if err != nil {
		t.Fatalf("renderWorkerUnit(false): %v", err)
	}
	s := string(got)
	if strings.Contains(s, "LoadCredentialEncrypted") {
		t.Errorf("expected no LoadCredentialEncrypted in unit without credential, got:\n%s", s)
	}
	if !strings.Contains(s, "ExecStart=/usr/local/bin/furnace worker run") {
		t.Errorf("expected 'worker run' in ExecStart, got:\n%s", s)
	}
}
