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

	_ = f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())

	release := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		os.Remove(path)
	}

	return release, nil
}
