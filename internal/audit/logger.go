package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-sum/furnace/internal/model"
)

type Logger interface {
	Log(ctx context.Context, entry model.AuditEntry) error
}

type FileLogger struct {
	dir string
	mu  sync.Mutex
}

func NewFileLogger(dir string) (*FileLogger, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}
	return &FileLogger{dir: dir}, nil
}

func (l *FileLogger) Log(_ context.Context, entry model.AuditEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}
	data = append(data, '\n')

	name := entry.AppName
	if name == "" {
		name = "unknown"
	}
	path := filepath.Join(l.dir, name+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync audit log: %w", err)
	}
	return nil
}
