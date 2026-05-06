package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/go-sum/furnace/internal/model"
)

type DeployLock interface {
	Acquire(ctx context.Context, appName string) (release func(), err error)
}

type FileLock struct {
	dir string
}

func NewFileLock(dir string) *FileLock {
	// Best-effort: if the directory cannot be created, Acquire will surface a
	// clear "open lock file" error at the first deployment attempt.
	os.MkdirAll(dir, 0750)
	return &FileLock{dir: dir}
}

func (l *FileLock) Acquire(_ context.Context, appName string) (func(), error) {
	path := filepath.Join(l.dir, appName+".lock")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("%w: open lock file: %v", model.ErrDeploymentInProgress, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, model.ErrDeploymentInProgress
	}

	_ = f.Truncate(0) // best-effort: PID is advisory; lock is held via flock
	fmt.Fprintf(f, "%d\n", os.Getpid())

	// best-effort cleanup: lock release errors are non-fatal; the flock kernel
	// object is released when the fd is closed regardless.
	release := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
		f.Close()                                    //nolint:errcheck
		os.Remove(path)                              //nolint:errcheck
	}

	return release, nil
}
