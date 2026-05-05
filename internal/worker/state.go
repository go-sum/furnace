package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AppState records the last successfully deployed version of an app.
type AppState struct {
	Tag            string    `json:"tag"`
	Digest         string    `json:"digest"`
	ArtifactDigest string    `json:"artifact_digest,omitempty"`
	DeployedAt     time.Time `json:"deployed_at"`
}

type stateStore struct {
	dir string
}

func newStateStore(dir string) *stateStore {
	return &stateStore{dir: dir}
}

// Load returns the saved state for appName, or nil if no state exists yet.
func (s *stateStore) Load(_ context.Context, appName string) (*AppState, error) {
	data, err := os.ReadFile(s.path(appName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state for %s: %w", appName, err)
	}
	var st AppState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state for %s: %w", appName, err)
	}
	return &st, nil
}

// Save persists the state for appName atomically.
func (s *stateStore) Save(_ context.Context, appName string, st *AppState) error {
	if err := os.MkdirAll(s.dir, 0750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state for %s: %w", appName, err)
	}
	tmp := s.path(appName) + ".tmp"
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return fmt.Errorf("write state tmp for %s: %w", appName, err)
	}
	if err := os.Rename(tmp, s.path(appName)); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename state for %s: %w", appName, err)
	}
	return nil
}

func (s *stateStore) path(appName string) string {
	return filepath.Join(s.dir, appName+".json")
}
