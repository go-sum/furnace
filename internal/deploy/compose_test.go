package deploy

import (
	"reflect"
	"testing"

	"github.com/go-sum/furnace/internal/model"
)

func TestComposePullArgs(t *testing.T) {
	app := model.AppConfig{
		ComposeFiles: []string{"docker-compose.data.yml", "docker-compose.yml"},
		EnvFile:      ".deploy.env",
	}

	got := ComposePullArgs(app)
	want := []string{"docker", "compose", "-f", "docker-compose.data.yml", "-f", "docker-compose.yml", "--env-file", ".deploy.env", "pull"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposePullArgs:\ngot  %v\nwant %v", got, want)
	}
}

func TestComposeUpArgs(t *testing.T) {
	app := model.AppConfig{
		ComposeFiles: []string{"docker-compose.data.yml", "docker-compose.yml"},
		EnvFile:      ".env.deploy",
	}

	got := ComposeUpArgs(app)
	want := []string{"docker", "compose", "-f", "docker-compose.data.yml", "-f", "docker-compose.yml", "--env-file", ".env.deploy", "up", "-d", "--remove-orphans"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposeUpArgs:\ngot  %v\nwant %v", got, want)
	}
}

func TestComposePullArgs_SingleFile(t *testing.T) {
	app := model.AppConfig{
		ComposeFiles: []string{"compose.yml"},
		EnvFile:      ".deploy.env",
	}

	got := ComposePullArgs(app)
	want := []string{"docker", "compose", "-f", "compose.yml", "--env-file", ".deploy.env", "pull"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComposePullArgs single file:\ngot  %v\nwant %v", got, want)
	}
}
