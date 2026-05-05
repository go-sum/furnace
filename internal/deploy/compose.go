package deploy

import (
	"path/filepath"

	"github.com/go-sum/furnace/internal/model"
)

func ComposePullArgs(app model.AppConfig, composeFiles []string) []string {
	args := []string{"docker", "compose", "--project-directory", app.Dir}
	for _, f := range composeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "--env-file", filepath.Join(app.Dir, app.EnvFile), "pull")
	return args
}

func ComposeUpArgs(app model.AppConfig, composeFiles []string) []string {
	args := []string{"docker", "compose", "--project-directory", app.Dir}
	for _, f := range composeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "--env-file", filepath.Join(app.Dir, app.EnvFile), "up", "-d", "--remove-orphans")
	return args
}
