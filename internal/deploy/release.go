package deploy

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	furnaceDir   = ".furnace"
	releasesDir  = "releases"
	currentLink  = "current"
	trackingFile = ".furnace-files"
)

// ReleaseManager manages the release directory structure for an app.
type ReleaseManager struct {
	logger *slog.Logger
}

// NewReleaseManager creates a ReleaseManager that uses logger for diagnostics.
func NewReleaseManager(logger *slog.Logger) *ReleaseManager {
	return &ReleaseManager{logger: logger}
}

// CreateStagingDir creates a fresh temporary staging directory under appDir/.furnace/releases/.
func (rm *ReleaseManager) CreateStagingDir(appDir string) (string, error) {
	relDir := filepath.Join(appDir, furnaceDir, releasesDir)
	if err := os.MkdirAll(relDir, 0755); err != nil {
		return "", fmt.Errorf("create releases dir: %w", err)
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("random staging suffix: %w", err)
	}
	stagingDir := filepath.Join(relDir, fmt.Sprintf(".staging-%x", buf))
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	return stagingDir, nil
}

// CommitStaging renames stagingDir to appDir/.furnace/releases/<artifactDigest>/.
// If the target already exists (idempotent), removes staging instead.
func (rm *ReleaseManager) CommitStaging(appDir, stagingDir, artifactDigest string) error {
	target := rm.ReleasePath(appDir, artifactDigest)
	if _, err := os.Lstat(target); err == nil {
		// Already committed from a prior run.
		os.RemoveAll(stagingDir)
		return nil
	}
	if err := os.Rename(stagingDir, target); err != nil {
		return fmt.Errorf("commit staging: %w", err)
	}
	return nil
}

// CleanupStagingDir removes a staging directory, ignoring not-found errors.
func (rm *ReleaseManager) CleanupStagingDir(stagingDir string) error {
	if err := os.RemoveAll(stagingDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleanup staging: %w", err)
	}
	return nil
}

// ReleasePath returns the absolute path to the release directory for a given artifact digest.
func (rm *ReleaseManager) ReleasePath(appDir, artifactDigest string) string {
	return filepath.Join(appDir, furnaceDir, releasesDir, sanitizeDigest(artifactDigest))
}

// Activate switches the .furnace/current symlink to the release for artifactDigest.
// Returns the previous target's digest (empty string if no prior link existed).
func (rm *ReleaseManager) Activate(appDir, artifactDigest string) (string, error) {
	linkPath := filepath.Join(appDir, furnaceDir, currentLink)
	target := rm.ReleasePath(appDir, artifactDigest)

	// Read current link target to return previous digest.
	prevDigest := ""
	if prev, err := os.Readlink(linkPath); err == nil {
		prevDigest = filepath.Base(prev)
	}

	// Atomic symlink switch: create temp link, rename over existing.
	tmpLink := linkPath + ".new"
	os.Remove(tmpLink) // clean any leftover
	if err := os.Symlink(target, tmpLink); err != nil {
		return "", fmt.Errorf("create temp symlink: %w", err)
	}
	if err := os.Rename(tmpLink, linkPath); err != nil {
		os.Remove(tmpLink)
		return "", fmt.Errorf("activate release symlink: %w", err)
	}
	return prevDigest, nil
}

// Deactivate restores .furnace/current to point at prevDigest.
// If prevDigest is empty, removes the symlink entirely.
func (rm *ReleaseManager) Deactivate(appDir, prevDigest string) error {
	linkPath := filepath.Join(appDir, furnaceDir, currentLink)
	if prevDigest == "" {
		if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove current symlink: %w", err)
		}
		return nil
	}
	target := rm.ReleasePath(appDir, prevDigest)
	tmpLink := linkPath + ".new"
	os.Remove(tmpLink)
	if err := os.Symlink(target, tmpLink); err != nil {
		return fmt.Errorf("create restore symlink: %w", err)
	}
	if err := os.Rename(tmpLink, linkPath); err != nil {
		os.Remove(tmpLink)
		return fmt.Errorf("restore release symlink: %w", err)
	}
	return nil
}

// ActiveReleasePath resolves the .furnace/current symlink and returns the absolute path.
func (rm *ReleaseManager) ActiveReleasePath(appDir string) (string, error) {
	linkPath := filepath.Join(appDir, furnaceDir, currentLink)
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", fmt.Errorf("read current symlink: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	return target, nil
}

// DiscoverComposeFiles returns sorted absolute paths to all *.yml and *.yaml files in releasePath.
// Returns an error if no compose files are found.
func (rm *ReleaseManager) DiscoverComposeFiles(releasePath string) ([]string, error) {
	entries, err := os.ReadDir(releasePath)
	if err != nil {
		return nil, fmt.Errorf("read release dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
			files = append(files, filepath.Join(releasePath, name))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no compose files found in release dir %s", releasePath)
	}
	sort.Strings(files)
	return files, nil
}

// PruneReleases removes old release directories, keeping at most keep releases
// plus whichever release is currently active. Never removes the active release.
func (rm *ReleaseManager) PruneReleases(appDir string, keep int) error {
	relDir := filepath.Join(appDir, furnaceDir, releasesDir)
	entries, err := os.ReadDir(relDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read releases dir: %w", err)
	}

	// Determine active release name.
	active := ""
	if linkTarget, err := os.Readlink(filepath.Join(appDir, furnaceDir, currentLink)); err == nil {
		active = filepath.Base(linkTarget)
	}

	// Collect non-staging release dirs sorted by mtime (oldest first).
	type releaseEntry struct {
		name  string
		mtime int64
	}
	var releases []releaseEntry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".staging-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		releases = append(releases, releaseEntry{name: e.Name(), mtime: info.ModTime().UnixNano()})
	}

	// Sort oldest first.
	sort.Slice(releases, func(i, j int) bool { return releases[i].mtime < releases[j].mtime })

	// Remove oldest until we have at most `keep` (excluding the active one from counting).
	prunable := make([]string, 0)
	for _, r := range releases {
		if r.name != active {
			prunable = append(prunable, r.name)
		}
	}
	if len(prunable) <= keep {
		return nil
	}
	toRemove := prunable[:len(prunable)-keep]
	for _, name := range toRemove {
		path := filepath.Join(relDir, name)
		if err := os.RemoveAll(path); err != nil {
			rm.logger.Warn("failed to prune release", "path", path, "error", err)
		}
	}
	return nil
}

// CleanupStaleStagingDirs removes any directory matching .staging-* under
// appDir/.furnace/releases/. Call at the start of each poll to remove leftovers
// from crashed prior runs.
func (rm *ReleaseManager) CleanupStaleStagingDirs(appDir string) error {
	relDir := filepath.Join(appDir, furnaceDir, releasesDir)
	entries, err := os.ReadDir(relDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read releases dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ".staging-") {
			continue
		}
		path := filepath.Join(relDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			rm.logger.Warn("failed to remove stale staging dir", "path", path, "error", err)
		}
	}
	return nil
}

// MarkBadRelease writes appDir/.furnace/releases/<digest>/.furnace-bad with
// timestamp and reason so bad releases can be identified during pruning.
func (rm *ReleaseManager) MarkBadRelease(appDir, artifactDigest, reason string) error {
	releasePath := rm.ReleasePath(appDir, artifactDigest)
	if _, err := os.Stat(releasePath); err != nil {
		// Release dir does not exist; nothing to mark.
		return nil
	}
	content := fmt.Sprintf("timestamp: %s\nreason: %s\n", time.Now().UTC().Format(time.RFC3339), reason)
	badPath := filepath.Join(releasePath, ".furnace-bad")
	if err := os.WriteFile(badPath, []byte(content), 0640); err != nil {
		return fmt.Errorf("write bad marker: %w", err)
	}
	return nil
}

// sanitizeDigest strips the "sha256:" prefix and trims to a safe directory name.
func sanitizeDigest(digest string) string {
	d := strings.TrimPrefix(digest, "sha256:")
	if len(d) > 64 {
		d = d[:64]
	}
	return d
}
