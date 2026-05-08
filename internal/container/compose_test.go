package container

import (
	"reflect"
	"testing"

	"github.com/go-sum/furnace/internal/model"
)

func TestComposePullArgs(t *testing.T) {
	app := model.AppConfig{
		Dir:     "/srv/apps/myapp",
		EnvFile: ".deploy.env",
	}
	composeFiles := []string{
		"/srv/apps/myapp/.furnace/releases/abc/docker-compose.data.yml",
		"/srv/apps/myapp/.furnace/releases/abc/docker-compose.yml",
	}

	got := ComposePullArgs(app, composeFiles)
	want := []string{
		"docker", "compose",
		"--project-directory", "/srv/apps/myapp",
		"-f", "/srv/apps/myapp/.furnace/releases/abc/docker-compose.data.yml",
		"-f", "/srv/apps/myapp/.furnace/releases/abc/docker-compose.yml",
		"--env-file", "/srv/apps/myapp/.deploy.env",
		"pull",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposePullArgs:\ngot  %v\nwant %v", got, want)
	}
}

func TestComposeUpArgs(t *testing.T) {
	app := model.AppConfig{
		Dir:     "/srv/apps/myapp",
		EnvFile: ".env.deploy",
	}
	composeFiles := []string{
		"/srv/apps/myapp/.furnace/releases/abc/docker-compose.data.yml",
		"/srv/apps/myapp/.furnace/releases/abc/docker-compose.yml",
	}

	got := ComposeUpArgs(app, composeFiles)
	want := []string{
		"docker", "compose",
		"--project-directory", "/srv/apps/myapp",
		"-f", "/srv/apps/myapp/.furnace/releases/abc/docker-compose.data.yml",
		"-f", "/srv/apps/myapp/.furnace/releases/abc/docker-compose.yml",
		"--env-file", "/srv/apps/myapp/.env.deploy",
		"up", "-d", "--remove-orphans",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposeUpArgs:\ngot  %v\nwant %v", got, want)
	}
}

func TestComposePullArgs_SingleFile(t *testing.T) {
	app := model.AppConfig{
		Dir:     "/srv/apps/myapp",
		EnvFile: ".deploy.env",
	}
	composeFiles := []string{"/srv/apps/myapp/.furnace/releases/abc/compose.yml"}

	got := ComposePullArgs(app, composeFiles)
	want := []string{
		"docker", "compose",
		"--project-directory", "/srv/apps/myapp",
		"-f", "/srv/apps/myapp/.furnace/releases/abc/compose.yml",
		"--env-file", "/srv/apps/myapp/.deploy.env",
		"pull",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposePullArgs single file:\ngot  %v\nwant %v", got, want)
	}
}
