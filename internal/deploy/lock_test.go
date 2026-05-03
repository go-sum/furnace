package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/go-sum/furnace/internal/model"
)

func TestFileLock_AcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	release, err := lock.Acquire(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	release()
}

func TestFileLock_DoubleAcquireFails(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	release, err := lock.Acquire(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	_, err = lock.Acquire(context.Background(), "testapp")
	if err == nil {
		t.Fatal("expected error on second acquire")
	}
	if !errors.Is(err, model.ErrDeploymentInProgress) {
		t.Fatalf("expected ErrDeploymentInProgress, got: %v", err)
	}
}

func TestFileLock_DifferentAppsIndependent(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	release1, err := lock.Acquire(context.Background(), "app1")
	if err != nil {
		t.Fatalf("acquire app1: %v", err)
	}
	defer release1()

	release2, err := lock.Acquire(context.Background(), "app2")
	if err != nil {
		t.Fatalf("acquire app2: %v", err)
	}
	defer release2()
}

func TestFileLock_ReleaseAllowsReacquire(t *testing.T) {
	dir := t.TempDir()
	lock := NewFileLock(dir)

	release, err := lock.Acquire(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	release()

	release2, err := lock.Acquire(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	defer release2()
}
