package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
)

func testStore(t *testing.T) *storage.SQLiteDeploymentStore {
	t.Helper()
	path := fmt.Sprintf("%s/furnace.db", t.TempDir())
	db, err := storage.OpenDB(path, false, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return storage.NewSQLiteDeploymentStore(db, slog.Default())
}

func TestStatusReader_Status(t *testing.T) {
	store := testStore(t)
	reader := newStatusReader(map[string]struct{}{"myapp": {}}, store)

	deployment := &model.Deployment{
		ID:      "01ABC",
		AppName: "myapp",
		Status:  model.StatusCompleted,
	}
	if err := store.Save(context.Background(), deployment); err != nil {
		t.Fatalf("save deployment: %v", err)
	}

	got, err := reader.Status(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got == nil || got.ID != deployment.ID {
		t.Fatalf("expected deployment %q, got %+v", deployment.ID, got)
	}
}

func TestStatusReader_UnknownApp(t *testing.T) {
	store := testStore(t)
	reader := newStatusReader(map[string]struct{}{"myapp": {}}, store)

	_, err := reader.Status(context.Background(), "other")
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}
