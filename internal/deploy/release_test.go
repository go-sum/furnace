package deploy

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestReleaseManager() *ReleaseManager {
	return NewReleaseManager(slog.Default())
}

func TestReleaseManager_StageAndCommit(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	stagingDir, err := rm.CreateStagingDir(appDir)
	if err != nil {
		t.Fatalf("CreateStagingDir: %v", err)
	}
	if _, err := os.Stat(stagingDir); err != nil {
		t.Fatalf("staging dir not created: %v", err)
	}

	// Write a file into staging.
	if err := os.WriteFile(filepath.Join(stagingDir, "compose.yml"), []byte("services: {}"), 0644); err != nil {
		t.Fatalf("write compose.yml: %v", err)
	}

	digest := "sha256:abc123def456"
	if err := rm.CommitStaging(appDir, stagingDir, digest); err != nil {
		t.Fatalf("CommitStaging: %v", err)
	}

	relPath := rm.ReleasePath(appDir, digest)
	if _, err := os.Stat(relPath); err != nil {
		t.Fatalf("release dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(relPath, "compose.yml")); err != nil {
		t.Fatalf("compose.yml not found in release dir: %v", err)
	}
	// Staging dir must be gone.
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Fatal("expected staging dir to be removed after commit")
	}
}

func TestReleaseManager_CommitIdempotent(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	stagingDir, _ := rm.CreateStagingDir(appDir)
	os.WriteFile(filepath.Join(stagingDir, "compose.yml"), []byte("services: {}"), 0644)

	digest := "sha256:idempotent"
	rm.CommitStaging(appDir, stagingDir, digest)

	// Call again — create a new staging dir.
	stagingDir2, _ := rm.CreateStagingDir(appDir)
	os.WriteFile(filepath.Join(stagingDir2, "compose.yml"), []byte("services: {}"), 0644)

	if err := rm.CommitStaging(appDir, stagingDir2, digest); err != nil {
		t.Fatalf("second CommitStaging: %v", err)
	}
	// Staging2 should be cleaned up.
	if _, err := os.Stat(stagingDir2); !os.IsNotExist(err) {
		t.Fatal("expected staging dir2 to be removed on idempotent commit")
	}
}

func TestReleaseManager_Activate_FreshApp(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	stagingDir, _ := rm.CreateStagingDir(appDir)
	digest := "sha256:fresh"
	rm.CommitStaging(appDir, stagingDir, digest)

	prevDigest, err := rm.Activate(appDir, digest)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if prevDigest != "" {
		t.Fatalf("expected empty prevDigest on first activate, got %q", prevDigest)
	}

	active, err := rm.ActiveReleasePath(appDir)
	if err != nil {
		t.Fatalf("ActiveReleasePath: %v", err)
	}
	if active != rm.ReleasePath(appDir, digest) {
		t.Fatalf("active path mismatch:\ngot  %s\nwant %s", active, rm.ReleasePath(appDir, digest))
	}
}

func TestReleaseManager_Activate_ReturnsPrevDigest(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	// First release.
	s1, _ := rm.CreateStagingDir(appDir)
	d1 := "sha256:first"
	rm.CommitStaging(appDir, s1, d1)
	rm.Activate(appDir, d1)

	// Second release.
	s2, _ := rm.CreateStagingDir(appDir)
	d2 := "sha256:second"
	rm.CommitStaging(appDir, s2, d2)

	prevDigest, err := rm.Activate(appDir, d2)
	if err != nil {
		t.Fatalf("Activate second: %v", err)
	}
	wantPrev := sanitizeDigest(d1)
	if prevDigest != wantPrev {
		t.Fatalf("prevDigest: got %q want %q", prevDigest, wantPrev)
	}
}

func TestReleaseManager_Deactivate_RestoresPrev(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	s1, _ := rm.CreateStagingDir(appDir)
	d1 := "sha256:v1"
	rm.CommitStaging(appDir, s1, d1)
	rm.Activate(appDir, d1)

	s2, _ := rm.CreateStagingDir(appDir)
	d2 := "sha256:v2"
	rm.CommitStaging(appDir, s2, d2)
	prevDigest, _ := rm.Activate(appDir, d2)

	if err := rm.Deactivate(appDir, prevDigest); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	active, err := rm.ActiveReleasePath(appDir)
	if err != nil {
		t.Fatalf("ActiveReleasePath after deactivate: %v", err)
	}
	if active != rm.ReleasePath(appDir, d1) {
		t.Fatalf("active after deactivate:\ngot  %s\nwant %s", active, rm.ReleasePath(appDir, d1))
	}
}

func TestReleaseManager_Deactivate_NoPrev(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	s1, _ := rm.CreateStagingDir(appDir)
	d1 := "sha256:only"
	rm.CommitStaging(appDir, s1, d1)
	rm.Activate(appDir, d1)

	if err := rm.Deactivate(appDir, ""); err != nil {
		t.Fatalf("Deactivate with no prev: %v", err)
	}

	_, err := rm.ActiveReleasePath(appDir)
	if err == nil {
		t.Fatal("expected error reading symlink after deactivate with no prev")
	}
}

func TestReleaseManager_DiscoverComposeFiles(t *testing.T) {
	rm := newTestReleaseManager()
	releasePath := t.TempDir()

	// Write files.
	os.WriteFile(filepath.Join(releasePath, "b.yml"), []byte(""), 0644)
	os.WriteFile(filepath.Join(releasePath, "a.yaml"), []byte(""), 0644)
	os.WriteFile(filepath.Join(releasePath, ".furnace-files"), []byte(""), 0644) // tracking file, not yml
	os.WriteFile(filepath.Join(releasePath, "notes.txt"), []byte(""), 0644)     // not yml

	files, err := rm.DiscoverComposeFiles(releasePath)
	if err != nil {
		t.Fatalf("DiscoverComposeFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	// Should be sorted by full path, a.yaml < b.yml.
	if filepath.Base(files[0]) != "a.yaml" || filepath.Base(files[1]) != "b.yml" {
		t.Fatalf("unexpected order: %v", files)
	}
	// Should be absolute paths.
	if !filepath.IsAbs(files[0]) {
		t.Fatalf("expected absolute path, got %q", files[0])
	}
}

func TestReleaseManager_DiscoverComposeFiles_Empty(t *testing.T) {
	rm := newTestReleaseManager()
	releasePath := t.TempDir()

	_, err := rm.DiscoverComposeFiles(releasePath)
	if err == nil {
		t.Fatal("expected error for empty release dir")
	}
}

func TestReleaseManager_PruneReleases(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	// Create 5 releases.
	digests := []string{"sha256:r1", "sha256:r2", "sha256:r3", "sha256:r4", "sha256:r5"}
	for _, d := range digests {
		s, _ := rm.CreateStagingDir(appDir)
		os.WriteFile(filepath.Join(s, "compose.yml"), []byte(""), 0644)
		rm.CommitStaging(appDir, s, d)
		rm.Activate(appDir, d)
	}

	// Prune keeping 3 (plus active, which is r5).
	if err := rm.PruneReleases(appDir, 3); err != nil {
		t.Fatalf("PruneReleases: %v", err)
	}

	relDir := filepath.Join(appDir, furnaceDir, releasesDir)
	entries, _ := os.ReadDir(relDir)
	// Count non-staging dirs.
	count := 0
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".staging-") {
			count++
		}
	}
	// active (r5) + 3 kept non-active = 4 total at most
	if count > 4 {
		t.Fatalf("expected at most 4 releases after prune (3 kept + active), got %d", count)
	}

	// Active release (r5) must still exist.
	active, err := rm.ActiveReleasePath(appDir)
	if err != nil {
		t.Fatalf("active path after prune: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active release removed during prune: %v", err)
	}
}

func TestReleaseManager_CleanupStaleStagingDirs(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	// Create two staging dirs and one committed release dir.
	s1, err := rm.CreateStagingDir(appDir)
	if err != nil {
		t.Fatalf("CreateStagingDir 1: %v", err)
	}
	s2, err := rm.CreateStagingDir(appDir)
	if err != nil {
		t.Fatalf("CreateStagingDir 2: %v", err)
	}

	digest := "sha256:committed"
	s3, _ := rm.CreateStagingDir(appDir)
	os.WriteFile(filepath.Join(s3, "compose.yml"), []byte("services: {}"), 0644)
	if err := rm.CommitStaging(appDir, s3, digest); err != nil {
		t.Fatalf("CommitStaging: %v", err)
	}

	if err := rm.CleanupStaleStagingDirs(appDir); err != nil {
		t.Fatalf("CleanupStaleStagingDirs: %v", err)
	}

	// Staging dirs must be gone.
	if _, err := os.Stat(s1); !os.IsNotExist(err) {
		t.Fatalf("expected staging dir 1 to be removed")
	}
	if _, err := os.Stat(s2); !os.IsNotExist(err) {
		t.Fatalf("expected staging dir 2 to be removed")
	}

	// Committed release dir must still exist.
	releasePath := rm.ReleasePath(appDir, digest)
	if _, err := os.Stat(releasePath); err != nil {
		t.Fatalf("committed release dir removed: %v", err)
	}
}

func TestReleaseManager_CleanupStaleStagingDirs_NoReleasesDir(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	// No releases dir yet — must not error.
	if err := rm.CleanupStaleStagingDirs(appDir); err != nil {
		t.Fatalf("expected no error for missing releases dir: %v", err)
	}
}

func TestReleaseManager_MarkBadRelease(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	digest := "sha256:badrelease"
	s, _ := rm.CreateStagingDir(appDir)
	os.WriteFile(filepath.Join(s, "compose.yml"), []byte("services: {}"), 0644)
	rm.CommitStaging(appDir, s, digest)

	reason := "health check: health check failed"
	if err := rm.MarkBadRelease(appDir, digest, reason); err != nil {
		t.Fatalf("MarkBadRelease: %v", err)
	}

	badPath := filepath.Join(rm.ReleasePath(appDir, digest), ".furnace-bad")
	data, err := os.ReadFile(badPath)
	if err != nil {
		t.Fatalf("read .furnace-bad: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "timestamp:") {
		t.Fatalf(".furnace-bad missing timestamp field:\n%s", content)
	}
	if !strings.Contains(content, "reason: "+reason) {
		t.Fatalf(".furnace-bad missing reason:\n%s", content)
	}
}

func TestReleaseManager_MarkBadRelease_NonExistentRelease(t *testing.T) {
	rm := newTestReleaseManager()
	appDir := t.TempDir()

	// Release dir does not exist — must not error.
	if err := rm.MarkBadRelease(appDir, "sha256:nonexistent", "some reason"); err != nil {
		t.Fatalf("expected no error for nonexistent release: %v", err)
	}
}
