package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

func testSQLiteStore(t *testing.T) *SQLiteDeploymentStore {
	t.Helper()
	path := fmt.Sprintf("%s/furnace.db", t.TempDir())
	db, err := OpenDB(path, false, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewSQLiteDeploymentStore(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func TestSQLiteDeploymentStore_SaveAndGetLatest(t *testing.T) {
	store := testSQLiteStore(t)

	d := &model.Deployment{
		ID:        "01ABC",
		AppName:   "myapp",
		Image:     "ghcr.io/org/repo:v1.0.0",
		Tag:       "v1.0.0",
		Status:    model.StatusCompleted,
		StartedAt: time.Now().Add(-10 * time.Second).UTC().Truncate(time.Microsecond),
		EndedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	if err := store.Save(context.Background(), d); err != nil {
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

func TestSQLiteDeploymentStore_GetLatest_Empty(t *testing.T) {
	store := testSQLiteStore(t)

	got, err := store.GetLatest(context.Background(), "noapp")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty store, got %+v", got)
	}
}

func TestSQLiteDeploymentStore_GetPrevious(t *testing.T) {
	store := testSQLiteStore(t)

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

func TestSQLiteDeploymentStore_GetPrevious_SkipsLatestEvenIfFailed(t *testing.T) {
	store := testSQLiteStore(t)

	d1 := &model.Deployment{
		ID:        "001",
		AppName:   "myapp",
		Status:    model.StatusCompleted,
		StartedAt: time.Now().Add(-20 * time.Second),
	}
	d2 := &model.Deployment{
		ID:        "002",
		AppName:   "myapp",
		Status:    model.StatusFailed,
		StartedAt: time.Now().Add(-5 * time.Second),
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
		t.Fatalf("expected 001 as previous, got %s", prev.ID)
	}
}

func TestSQLiteDeploymentStore_List(t *testing.T) {
	store := testSQLiteStore(t)

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

func TestSQLiteDeploymentStore_List_NoLimit(t *testing.T) {
	store := testSQLiteStore(t)

	for i := 0; i < 5; i++ {
		d := &model.Deployment{
			ID:        fmt.Sprintf("LL%02d", i),
			AppName:   "myapp",
			Status:    model.StatusCompleted,
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		store.Save(context.Background(), d)
	}

	list, err := store.List(context.Background(), "myapp", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("expected all 5 items with limit=0, got %d", len(list))
	}
}

func TestSQLiteDeploymentStore_Prune(t *testing.T) {
	store := testSQLiteStore(t)

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

func TestSQLiteDeploymentStore_Prune_SkipsNonTerminal(t *testing.T) {
	store := testSQLiteStore(t)

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

func TestSQLiteDeploymentStore_UpdateExisting(t *testing.T) {
	store := testSQLiteStore(t)

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

func TestSQLiteDeploymentStore_AllFields(t *testing.T) {
	store := testSQLiteStore(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	d := &model.Deployment{
		ID:             "FULL01",
		AppName:        "myapp",
		Image:          "ghcr.io/org/repo:v1.0.0",
		Tag:            "v1.0.0",
		Digest:         "sha256:abc",
		ArtifactDigest: "sha256:def",
		PrevImage:      "ghcr.io/org/repo:v0.9.0",
		Status:         model.StatusCompleted,
		StartedAt:      now.Add(-10 * time.Second),
		EndedAt:        now,
		Error:          "",
	}

	if err := store.Save(context.Background(), d); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.GetLatest(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if got.Tag != "v1.0.0" {
		t.Errorf("Tag: got %q want %q", got.Tag, "v1.0.0")
	}
	if got.Digest != "sha256:abc" {
		t.Errorf("Digest: got %q want %q", got.Digest, "sha256:abc")
	}
	if got.ArtifactDigest != "sha256:def" {
		t.Errorf("ArtifactDigest: got %q want %q", got.ArtifactDigest, "sha256:def")
	}
	if got.PrevImage != "ghcr.io/org/repo:v0.9.0" {
		t.Errorf("PrevImage: got %q want %q", got.PrevImage, "ghcr.io/org/repo:v0.9.0")
	}
	if got.EndedAt.IsZero() {
		t.Error("EndedAt should not be zero")
	}
}

func TestOpenDB_ReadOnly_FailsOnMissingFile(t *testing.T) {
	path := fmt.Sprintf("%s/nonexistent.db", t.TempDir())
	db, err := OpenDB(path, true, slog.Default())
	if err == nil {
		db.Close()
		t.Fatal("expected error opening read-only non-existent file")
	}
}

func TestOpenDB_ReadWrite_CreatesFile(t *testing.T) {
	path := fmt.Sprintf("%s/furnace.db", t.TempDir())
	db, err := OpenDB(path, false, slog.Default())
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	db.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected db file to exist: %v", err)
	}
}
