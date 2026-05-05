package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

func TestFileLogger_SingleEntry(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewFileLogger(dir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	entry := model.AuditEntry{
		Timestamp: time.Now(),
		AppName:   "myapp",
		Action:    "deploy",
		Status:    "completed",
		Image:     "ghcr.io/org/repo:v1.0.0",
		Tag:       "v1.0.0",
	}

	err = logger.Log(context.Background(), entry)
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "myapp.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var decoded model.AuditEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.AppName != "myapp" {
		t.Fatalf("expected myapp, got %s", decoded.AppName)
	}
	if decoded.Action != "deploy" {
		t.Fatalf("expected deploy, got %s", decoded.Action)
	}
}

func TestFileLogger_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewFileLogger(dir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			entry := model.AuditEntry{
				Timestamp: time.Now(),
				AppName:   "myapp",
				Action:    "deploy",
				Status:    "completed",
			}
			logger.Log(context.Background(), entry)
		}()
	}
	wg.Wait()

	f, err := os.Open(filepath.Join(dir, "myapp.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry model.AuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("line %d invalid JSON: %v", count+1, err)
		}
		count++
	}

	if count != n {
		t.Fatalf("expected %d lines, got %d", n, count)
	}
}

func TestFileLogger_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewFileLogger(dir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	for i := 0; i < 3; i++ {
		entry := model.AuditEntry{
			Timestamp: time.Now(),
			AppName:   "myapp",
			Action:    "deploy",
			Status:    "completed",
		}
		logger.Log(context.Background(), entry)
	}

	f, err := os.Open(filepath.Join(dir, "myapp.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 lines, got %d", count)
	}
}

func TestFileLogger_WritesPerAppFiles(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewFileLogger(dir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	entries := []model.AuditEntry{
		{Timestamp: time.Now(), AppName: "app1", Action: "deploy", Status: "completed"},
		{Timestamp: time.Now(), AppName: "app2", Action: "deploy", Status: "failed"},
		{Timestamp: time.Now(), AppName: "app1", Action: "deploy", Status: "started"},
	}
	for _, e := range entries {
		if err := logger.Log(context.Background(), e); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	app1Path := filepath.Join(dir, "app1.jsonl")
	app2Path := filepath.Join(dir, "app2.jsonl")

	if _, err := os.Stat(app1Path); err != nil {
		t.Fatalf("app1.jsonl missing: %v", err)
	}
	if _, err := os.Stat(app2Path); err != nil {
		t.Fatalf("app2.jsonl missing: %v", err)
	}

	countLines := func(path string) int {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		n := 0
		for scanner.Scan() {
			var e model.AuditEntry
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				t.Fatalf("invalid JSON in %s: %v", path, err)
			}
			n++
		}
		return n
	}

	if got := countLines(app1Path); got != 2 {
		t.Fatalf("app1.jsonl: expected 2 lines, got %d", got)
	}
	if got := countLines(app2Path); got != 1 {
		t.Fatalf("app2.jsonl: expected 1 line, got %d", got)
	}
}
