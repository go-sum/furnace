package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/go-sum/furnace/internal/model"
)

type DeploymentStore interface {
	Save(ctx context.Context, d *model.Deployment) error
	GetLatest(ctx context.Context, appName string) (*model.Deployment, error)
	GetPrevious(ctx context.Context, appName string) (*model.Deployment, error)
	List(ctx context.Context, appName string, limit int) ([]model.Deployment, error)
	Prune(ctx context.Context, appName string, keep int) (int, error)
}

type FileDeploymentStore struct {
	dir    string
	logger *slog.Logger
	mu     sync.RWMutex
}

func NewFileDeploymentStore(dir string, logger *slog.Logger) *FileDeploymentStore {
	return &FileDeploymentStore{dir: dir, logger: logger}
}

func (s *FileDeploymentStore) Save(_ context.Context, d *model.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	appDir := filepath.Join(s.dir, d.AppName)
	if err := os.MkdirAll(appDir, 0750); err != nil {
		return fmt.Errorf("create deployment dir: %w", err)
	}

	path := filepath.Join(appDir, d.ID+".json")
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal deployment: %w", err)
	}

	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("open deployment tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write deployment: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync deployment: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close deployment: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename deployment: %w", err)
	}

	return nil
}

func (s *FileDeploymentStore) GetLatest(_ context.Context, appName string) (*model.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployments, err := s.readAll(appName)
	if err != nil {
		return nil, err
	}
	if len(deployments) == 0 {
		return nil, nil
	}

	return &deployments[0], nil
}

func (s *FileDeploymentStore) GetPrevious(_ context.Context, appName string) (*model.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployments, err := s.readAll(appName)
	if err != nil {
		return nil, err
	}

	for i, d := range deployments {
		if i > 0 && d.Status == model.StatusCompleted {
			return &d, nil
		}
	}

	return nil, nil
}

func (s *FileDeploymentStore) List(_ context.Context, appName string, limit int) ([]model.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployments, err := s.readAll(appName)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(deployments) > limit {
		deployments = deployments[:limit]
	}

	return deployments, nil
}

func (s *FileDeploymentStore) Prune(_ context.Context, appName string, keep int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deployments, err := s.readAll(appName)
	if err != nil {
		return 0, err
	}

	if len(deployments) <= keep {
		return 0, nil
	}

	pruned := 0
	appDir := filepath.Join(s.dir, appName)
	for _, d := range deployments[keep:] {
		if !d.Status.IsTerminal() {
			continue
		}
		path := filepath.Join(appDir, d.ID+".json")
		if err := os.Remove(path); err == nil {
			pruned++
		}
	}
	return pruned, nil
}

func (s *FileDeploymentStore) readAll(appName string) ([]model.Deployment, error) {
	appDir := filepath.Join(s.dir, appName)
	entries, err := os.ReadDir(appDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read deployment dir: %w", err)
	}

	var deployments []model.Deployment
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(appDir, entry.Name()))
		if err != nil {
			s.logger.Warn("skipping unreadable deployment record",
				"file", entry.Name(), "app", appName, "error", err)
			continue
		}

		var d model.Deployment
		if err := json.Unmarshal(data, &d); err != nil {
			s.logger.Warn("skipping corrupt deployment record",
				"file", entry.Name(), "app", appName, "error", err)
			continue
		}
		deployments = append(deployments, d)
	}

	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].StartedAt.After(deployments[j].StartedAt)
	})

	return deployments, nil
}
