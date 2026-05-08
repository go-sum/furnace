package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

func testSQLiteAppStore(t *testing.T) *SQLiteAppStore {
	t.Helper()
	path := fmt.Sprintf("%s/furnace.db", t.TempDir())
	db, err := OpenDB(path, false, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewSQLiteAppStore(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func sampleApp(name string) model.AppConfig {
	return model.AppConfig{
		Name:            name,
		Image:           "ghcr.io/org/" + name + ":latest",
		TagPattern:      "v*",
		AllowedIdentity: "org/" + name,
		Artifact:        "docker-compose.yml",
		Domain:          name + ".example.com",
		Dir:             "/srv/apps/" + name,
		Port:            8080,
		TLS:             false,
		EnvFile:         ".deploy.env",
		ImageVar:        "APP_IMAGE",
		Container:       name,
		HealthTimeout:   30 * time.Second,
		KeepReleases:    5,
	}
}

func TestSQLiteAppStore_UpsertAndGet(t *testing.T) {
	store := testSQLiteAppStore(t)
	app := sampleApp("myapp")
	app.TLS = true
	app.Port = 9090
	app.HealthTimeout = 45 * time.Second
	app.KeepReleases = 3

	if err := store.UpsertApp(context.Background(), app); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}

	got, err := store.GetApp(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}

	if got.Name != app.Name {
		t.Errorf("Name: got %q want %q", got.Name, app.Name)
	}
	if got.Image != app.Image {
		t.Errorf("Image: got %q want %q", got.Image, app.Image)
	}
	if got.TagPattern != app.TagPattern {
		t.Errorf("TagPattern: got %q want %q", got.TagPattern, app.TagPattern)
	}
	if got.AllowedIdentity != app.AllowedIdentity {
		t.Errorf("AllowedIdentity: got %q want %q", got.AllowedIdentity, app.AllowedIdentity)
	}
	if got.Artifact != app.Artifact {
		t.Errorf("Artifact: got %q want %q", got.Artifact, app.Artifact)
	}
	if got.Domain != app.Domain {
		t.Errorf("Domain: got %q want %q", got.Domain, app.Domain)
	}
	if got.Dir != app.Dir {
		t.Errorf("Dir: got %q want %q", got.Dir, app.Dir)
	}
	if got.Port != app.Port {
		t.Errorf("Port: got %d want %d", got.Port, app.Port)
	}
	if got.TLS != app.TLS {
		t.Errorf("TLS: got %v want %v", got.TLS, app.TLS)
	}
	if got.EnvFile != app.EnvFile {
		t.Errorf("EnvFile: got %q want %q", got.EnvFile, app.EnvFile)
	}
	if got.ImageVar != app.ImageVar {
		t.Errorf("ImageVar: got %q want %q", got.ImageVar, app.ImageVar)
	}
	if got.Container != app.Container {
		t.Errorf("Container: got %q want %q", got.Container, app.Container)
	}
	if got.HealthTimeout != app.HealthTimeout {
		t.Errorf("HealthTimeout: got %v want %v", got.HealthTimeout, app.HealthTimeout)
	}
	if got.KeepReleases != app.KeepReleases {
		t.Errorf("KeepReleases: got %d want %d", got.KeepReleases, app.KeepReleases)
	}
}

func TestSQLiteAppStore_UpsertUpdate(t *testing.T) {
	store := testSQLiteAppStore(t)
	app := sampleApp("myapp")

	if err := store.UpsertApp(context.Background(), app); err != nil {
		t.Fatalf("first UpsertApp: %v", err)
	}

	app.Image = "ghcr.io/org/myapp:v2"
	app.Port = 9999
	if err := store.UpsertApp(context.Background(), app); err != nil {
		t.Fatalf("second UpsertApp: %v", err)
	}

	got, err := store.GetApp(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.Image != "ghcr.io/org/myapp:v2" {
		t.Errorf("Image: got %q want %q", got.Image, "ghcr.io/org/myapp:v2")
	}
	if got.Port != 9999 {
		t.Errorf("Port: got %d want %d", got.Port, 9999)
	}
}

func TestSQLiteAppStore_GetApp_NotFound(t *testing.T) {
	store := testSQLiteAppStore(t)

	_, err := store.GetApp(context.Background(), "missing")
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}

func TestSQLiteAppStore_ListApps(t *testing.T) {
	store := testSQLiteAppStore(t)

	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		if err := store.UpsertApp(context.Background(), sampleApp(name)); err != nil {
			t.Fatalf("UpsertApp %s: %v", name, err)
		}
	}

	list, err := store.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(list))
	}
	// Results ordered by name
	if list[0].Name != "alpha" {
		t.Errorf("list[0].Name: got %q want %q", list[0].Name, "alpha")
	}
	if list[1].Name != "beta" {
		t.Errorf("list[1].Name: got %q want %q", list[1].Name, "beta")
	}
	if list[2].Name != "gamma" {
		t.Errorf("list[2].Name: got %q want %q", list[2].Name, "gamma")
	}
}

func TestSQLiteAppStore_ListApps_Empty(t *testing.T) {
	store := testSQLiteAppStore(t)

	list, err := store.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if list != nil {
		t.Fatalf("expected nil for empty store, got %v", list)
	}
}

func TestSQLiteAppStore_DeleteApp(t *testing.T) {
	store := testSQLiteAppStore(t)

	if err := store.UpsertApp(context.Background(), sampleApp("myapp")); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}

	if err := store.DeleteApp(context.Background(), "myapp"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	exists, err := store.AppExists(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("AppExists: %v", err)
	}
	if exists {
		t.Fatal("expected app to not exist after delete")
	}
}

func TestSQLiteAppStore_DeleteApp_NotFound(t *testing.T) {
	store := testSQLiteAppStore(t)

	err := store.DeleteApp(context.Background(), "nonexistent")
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}

func TestSQLiteAppStore_AppExists(t *testing.T) {
	store := testSQLiteAppStore(t)

	exists, err := store.AppExists(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("AppExists (missing): %v", err)
	}
	if exists {
		t.Fatal("expected app to not exist before upsert")
	}

	if err := store.UpsertApp(context.Background(), sampleApp("myapp")); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}

	exists, err = store.AppExists(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("AppExists (present): %v", err)
	}
	if !exists {
		t.Fatal("expected app to exist after upsert")
	}
}

func TestSQLiteAppStore_ConfigValues(t *testing.T) {
	store := testSQLiteAppStore(t)
	ctx := context.Background()

	// Missing key returns "", false, nil
	val, found, err := store.GetConfigValue(ctx, "missing")
	if err != nil {
		t.Fatalf("GetConfigValue (missing): %v", err)
	}
	if found {
		t.Fatal("expected found=false for missing key")
	}
	if val != "" {
		t.Fatalf("expected empty value for missing key, got %q", val)
	}

	// Set then get
	if err := store.SetConfigValue(ctx, "mykey", "myvalue"); err != nil {
		t.Fatalf("SetConfigValue: %v", err)
	}
	val, found, err = store.GetConfigValue(ctx, "mykey")
	if err != nil {
		t.Fatalf("GetConfigValue: %v", err)
	}
	if !found {
		t.Fatal("expected found=true after set")
	}
	if val != "myvalue" {
		t.Fatalf("expected %q, got %q", "myvalue", val)
	}

	// Overwrite works
	if err := store.SetConfigValue(ctx, "mykey", "newvalue"); err != nil {
		t.Fatalf("SetConfigValue (overwrite): %v", err)
	}
	val, found, err = store.GetConfigValue(ctx, "mykey")
	if err != nil {
		t.Fatalf("GetConfigValue (overwrite): %v", err)
	}
	if !found {
		t.Fatal("expected found=true after overwrite")
	}
	if val != "newvalue" {
		t.Fatalf("expected %q after overwrite, got %q", "newvalue", val)
	}
}
