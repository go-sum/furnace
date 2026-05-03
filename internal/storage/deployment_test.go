package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

func testStore(t *testing.T) *FileDeploymentStore {
	t.Helper()
	return NewFileDeploymentStore(t.TempDir(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func TestFileDeploymentStore_SaveAndGetLatest(t *testing.T) {
	store := testStore(t)

	d := &model.Deployment{
		ID:        "01ABC",
		AppName:   "myapp",
		Image:     "ghcr.io/org/repo:v1.0.0",
		Status:    model.StatusCompleted,
		Actor:     "bot",
		StartedAt: time.Now().Add(-10 * time.Second),
		EndedAt:   time.Now(),
	}

	err := store.Save(context.Background(), d)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.GetLatest(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if got == nil {
		t.Fatal("expected deployment, got nil")
	}
	if got.ID != "01ABC" {
		t.Fatalf("expected ID 01ABC, got %s", got.ID)
	}
	if got.Image != "ghcr.io/org/repo:v1.0.0" {
		t.Fatalf("expected image, got %s", got.Image)
	}
}

func TestFileDeploymentStore_GetLatest_Empty(t *testing.T) {
	store := testStore(t)

	got, err := store.GetLatest(context.Background(), "noapp")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty store, got %+v", got)
	}
}

func TestFileDeploymentStore_GetPrevious(t *testing.T) {
	store := testStore(t)

	d1 := &model.Deployment{
		ID:        "001",
		AppName:   "myapp",
		Image:     "ghcr.io/org/repo:v1.0.0",
		Status:    model.StatusCompleted,
		StartedAt: time.Now().Add(-20 * time.Second),
		EndedAt:   time.Now().Add(-15 * time.Second),
	}
	d2 := &model.Deployment{
		ID:        "002",
		AppName:   "myapp",
		Image:     "ghcr.io/org/repo:v2.0.0",
		Status:    model.StatusCompleted,
		StartedAt: time.Now().Add(-5 * time.Second),
		EndedAt:   time.Now(),
	}

	store.Save(context.Background(), d1)
	store.Save(context.Background(), d2)

	prev, err := store.GetPrevious(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("get previous: %v", err)
	}
	if prev == nil {
		t.Fatal("expected previous deployment")
	}
	if prev.ID != "001" {
		t.Fatalf("expected previous to be 001, got %s", prev.ID)
	}
}

func TestFileDeploymentStore_List(t *testing.T) {
	store := testStore(t)

	for i := 0; i < 5; i++ {
		d := &model.Deployment{
			ID:        "0" + string(rune('A'+i)),
			AppName:   "myapp",
			Status:    model.StatusCompleted,
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		store.Save(context.Background(), d)
	}

	list, err := store.List(context.Background(), "myapp", 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list))
	}
}

func TestFileDeploymentStore_Prune(t *testing.T) {
	store := testStore(t)

	for i := 0; i < 10; i++ {
		d := &model.Deployment{
			ID:        fmt.Sprintf("ID%02d", i),
			AppName:   "myapp",
			Status:    model.StatusCompleted,
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		store.Save(context.Background(), d)
	}

	pruned, err := store.Prune(context.Background(), "myapp", 5)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 5 {
		t.Fatalf("expected 5 pruned, got %d", pruned)
	}

	remaining, err := store.List(context.Background(), "myapp", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 5 {
		t.Fatalf("expected 5 remaining, got %d", len(remaining))
	}
}

func TestFileDeploymentStore_Prune_SkipsNonTerminal(t *testing.T) {
	store := testStore(t)

	for i := 0; i < 5; i++ {
		d := &model.Deployment{
			ID:        fmt.Sprintf("ID%02d", i),
			AppName:   "myapp",
			Status:    model.StatusCompleted,
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		store.Save(context.Background(), d)
	}

	inProgress := &model.Deployment{
		ID:        "RUNNING",
		AppName:   "myapp",
		Status:    model.StatusPulling,
		StartedAt: time.Now().Add(-10 * time.Second),
	}
	store.Save(context.Background(), inProgress)

	pruned, err := store.Prune(context.Background(), "myapp", 3)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("expected 2 pruned (skip non-terminal), got %d", pruned)
	}
}

func TestFileDeploymentStore_UpdateExisting(t *testing.T) {
	store := testStore(t)

	d := &model.Deployment{
		ID:        "01UPD",
		AppName:   "myapp",
		Status:    model.StatusPending,
		StartedAt: time.Now(),
	}
	store.Save(context.Background(), d)

	d.Status = model.StatusCompleted
	d.EndedAt = time.Now()
	store.Save(context.Background(), d)

	got, _ := store.GetLatest(context.Background(), "myapp")
	if got.Status != model.StatusCompleted {
		t.Fatalf("expected completed after update, got %s", got.Status)
	}
}

func TestFileDeploymentStore_CorruptRecord(t *testing.T) {
	dir := t.TempDir()
	store := NewFileDeploymentStore(dir, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	good := &model.Deployment{
		ID:        "GOOD01",
		AppName:   "myapp",
		Status:    model.StatusCompleted,
		StartedAt: time.Now().Add(-10 * time.Second),
		EndedAt:   time.Now(),
	}
	if err := store.Save(context.Background(), good); err != nil {
		t.Fatalf("save good: %v", err)
	}

	// Write a corrupt JSON file directly.
	corruptPath := filepath.Join(dir, "myapp", "CORRUPT.json")
	if err := os.WriteFile(corruptPath, []byte("{not valid json"), 0640); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	// GetLatest should still return the good record, not error.
	latest, err := store.GetLatest(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("expected no error with corrupt record present, got: %v", err)
	}
	if latest == nil || latest.ID != "GOOD01" {
		t.Fatalf("expected good record, got: %+v", latest)
	}
}
