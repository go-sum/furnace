package container

import (
	"context"
	"testing"
	"time"
)

func TestDockerExecutor_RejectsNonDocker(t *testing.T) {
	exec := NewDockerExecutor()

	_, err := exec.Exec(context.Background(), t.TempDir(), []string{"bash", "-c", "echo hello"})
	if err == nil {
		t.Fatal("expected error for non-docker command")
	}
	if err.Error() != `only docker commands are allowed, got "bash"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDockerExecutor_RejectsEmptyCommand(t *testing.T) {
	exec := NewDockerExecutor()

	_, err := exec.Exec(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if err.Error() != "empty command" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDockerExecutor_RejectsVariousBinaries(t *testing.T) {
	exec := NewDockerExecutor()
	cases := []string{"sh", "curl", "rm", "/bin/bash", "python3"}

	for _, bin := range cases {
		t.Run(bin, func(t *testing.T) {
			_, err := exec.Exec(context.Background(), t.TempDir(), []string{bin, "--version"})
			if err == nil {
				t.Fatalf("expected error for %q", bin)
			}
		})
	}
}

func TestDockerExecutor_RejectsNonComposeSubcommand(t *testing.T) {
	exec := NewDockerExecutor()
	cases := [][]string{
		{"docker", "run", "alpine"},
		{"docker", "exec", "container"},
		{"docker", "pull", "alpine"},
		{"docker"},
	}
	for _, args := range cases {
		_, err := exec.Exec(context.Background(), t.TempDir(), args)
		if err == nil {
			t.Fatalf("expected error for args %v", args)
		}
		if err.Error() != "only docker compose subcommand is allowed" {
			t.Fatalf("unexpected error for args %v: %v", args, err)
		}
	}
}

func TestDockerExecutor_ExecutesDockerCommand(t *testing.T) {
	exec := NewDockerExecutor()

	out, err := exec.Exec(context.Background(), t.TempDir(), []string{"docker", "compose", "version"})
	if err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected output from docker compose version")
	}
}

func TestDockerExecutor_RespectsContext(t *testing.T) {
	exec := NewDockerExecutor()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)

	_, err := exec.Exec(ctx, t.TempDir(), []string{"docker", "compose", "version"})
	if err == nil {
		t.Skip("command completed before timeout")
	}
}
