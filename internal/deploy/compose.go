package deploy

import "github.com/go-sum/furnace/internal/model"

func ComposePullArgs(app model.AppConfig) []string {
	args := []string{"docker", "compose"}
	for _, f := range app.ComposeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "--env-file", app.EnvFile, "pull")
	return args
}

func ComposeUpArgs(app model.AppConfig) []string {
	args := []string{"docker", "compose"}
	for _, f := range app.ComposeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "--env-file", app.EnvFile, "up", "-d", "--remove-orphans")
	return args
}
