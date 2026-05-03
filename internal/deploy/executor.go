package deploy

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

type CommandExecutor interface {
	Exec(ctx context.Context, dir string, args []string) ([]byte, error)
}

type DockerExecutor struct{}

func NewDockerExecutor() *DockerExecutor {
	return &DockerExecutor{}
}

func (e *DockerExecutor) Exec(ctx context.Context, dir string, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	if args[0] != "docker" {
		return nil, fmt.Errorf("only docker commands are allowed, got %q", args[0])
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
