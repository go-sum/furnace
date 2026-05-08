package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type DockerExecutor struct {
	extraEnv []string
}

func NewDockerExecutor() *DockerExecutor {
	return &DockerExecutor{}
}

func NewDockerExecutorWithEnv(extraEnv []string) *DockerExecutor {
	return &DockerExecutor{extraEnv: append([]string(nil), extraEnv...)}
}

func (e *DockerExecutor) Exec(ctx context.Context, dir string, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	if args[0] != "docker" {
		return nil, fmt.Errorf("only docker commands are allowed, got %q", args[0])
	}
	if len(args) < 2 || args[1] != "compose" {
		return nil, fmt.Errorf("only docker compose subcommand is allowed")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	if len(e.extraEnv) > 0 {
		cmd.Env = append(os.Environ(), e.extraEnv...)
	}
	return cmd.CombinedOutput()
}
