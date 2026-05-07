package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureWebReadableConfig_SetsWorldReadableMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "furnace.yaml")
	if err := os.WriteFile(path, []byte("apps: {}\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ensureWebReadableConfig(path); err != nil {
		t.Fatalf("ensureWebReadableConfig: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("mode = %#o, want %#o", got, os.FileMode(0644))
	}
}
