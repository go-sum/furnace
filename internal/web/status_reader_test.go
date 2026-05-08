package web

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

type fakeAppChecker struct {
	exists bool
	err    error
}

func (f *fakeAppChecker) AppExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.err
}

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
	reader := newStatusReader(&fakeAppChecker{exists: true}, store)

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
	reader := newStatusReader(&fakeAppChecker{exists: false}, store)

	_, err := reader.Status(context.Background(), "other")
	if !errors.Is(err, model.ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}

func TestStatusReader_AppCheckerError(t *testing.T) {
	store := testStore(t)
	reader := newStatusReader(&fakeAppChecker{err: errors.New("db error")}, store)

	_, err := reader.Status(context.Background(), "myapp")
	if err == nil {
		t.Fatal("expected error from AppChecker failure")
	}
	if errors.Is(err, model.ErrAppNotFound) {
		t.Fatal("expected non-ErrAppNotFound error from AppChecker failure")
	}
}
