package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-sum/furnace/internal/audit"
	"github.com/go-sum/furnace/internal/model"
	"github.com/go-sum/furnace/internal/storage"
	"github.com/oklog/ulid/v2"
)

type Service struct {
	apps     map[string]model.AppConfig
	executor CommandExecutor
	lock     DeployLock
	health   HealthChecker
	store    storage.DeploymentStore
	audit    audit.Logger
	dataDir  string
	logger   *slog.Logger
	releases *ReleaseManager
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.RWMutex
	closed   bool
}

type ServiceConfig struct {
	Apps     map[string]model.AppConfig
	Executor CommandExecutor
	Lock     DeployLock
	Health   HealthChecker
	Store    storage.DeploymentStore
	Audit    audit.Logger
	DataDir  string
	Logger   *slog.Logger
	Context  context.Context
	Releases *ReleaseManager
}

func NewService(cfg ServiceConfig) *Service {
	rootCtx := cfg.Context
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	serviceCtx, cancel := context.WithCancel(rootCtx)
	return &Service{
		apps:     cfg.Apps,
		executor: cfg.Executor,
		lock:     cfg.Lock,
		health:   cfg.Health,
		store:    cfg.Store,
		audit:    cfg.Audit,
		dataDir:  cfg.DataDir,
		logger:   cfg.Logger,
		releases: cfg.Releases,
		ctx:      serviceCtx,
		cancel:   cancel,
	}
}

// ReconcileOnStartup marks any non-terminal deployments as failed. This handles
// the case where the process crashed mid-deploy.
func (s *Service) ReconcileOnStartup(ctx context.Context) {
	for appName := range s.apps {
		d, err := s.store.GetLatest(ctx, appName)
		if err != nil || d == nil {
			continue
		}
		if !d.Status.IsTerminal() {
			d.Status = model.StatusFailed
			d.Error = "interrupted: process restarted"
			d.EndedAt = time.Now()
			s.saveState(ctx, d)
			s.logger.Warn("reconciled stale deployment", "app", appName, "id", d.ID)
		}
	}
}

func (s *Service) Start(ctx context.Context, req model.DeployRequest) (*model.Deployment, error) {
	s.mu.RLock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.RUnlock()
		return nil, fmt.Errorf("deployment service is shutting down")
	}
	s.wg.Add(1)
	s.mu.RUnlock()

	app, ok := s.apps[req.AppName]
	if !ok {
		s.wg.Done()
		return nil, model.ErrAppNotFound
	}

	if strings.ContainsAny(req.Image, "\n\r\t =") {
		s.wg.Done()
		return nil, fmt.Errorf("%w: %q", model.ErrImageInvalid, req.Image)
	}

	release, err := s.lock.Acquire(ctx, req.AppName)
	if err != nil {
		s.wg.Done()
		return nil, err
	}

	deployment := &model.Deployment{
		ID:             ulid.Make().String(),
		AppName:        req.AppName,
		Image:          req.Image,
		Tag:            req.Tag,
		Digest:         req.Digest,
		ArtifactDigest: req.ArtifactDigest,
		Status:         model.StatusPending,
		StartedAt:      time.Now(),
	}

	if prev := s.readCurrentImage(app); prev != "" {
		deployment.PrevImage = prev
	}

	if err := s.store.Save(ctx, deployment); err != nil {
		release()
		s.wg.Done()
		return nil, fmt.Errorf("record deployment: %w", err)
	}

	s.logAudit(ctx, model.AuditEntry{
		Timestamp: time.Now(),
		AppName:   req.AppName,
		Action:    "deploy",
		Status:    "started",
		Image:     req.Image,
		Tag:       req.Tag,
		Digest:    req.Digest,
	})

	snapshot := *deployment
	go s.execute(app, deployment, release)
	return &snapshot, nil
}

func (s *Service) execute(app model.AppConfig, deployment *model.Deployment, release func()) {
	defer release()
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in deployment goroutine",
				"app", deployment.AppName,
				"deployment_id", deployment.ID,
				"panic", r,
			)
			ctx := context.Background()
			deployment.Status = model.StatusFailed
			deployment.EndedAt = time.Now()
			deployment.Error = fmt.Sprintf("internal panic: %v", r)
			s.saveState(ctx, deployment)
		}
	}()

	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	// Discover previous compose files from current active release before touching anything.
	var prevComposeFiles []string
	if existingPath, err := s.releases.ActiveReleasePath(app.Dir); err == nil {
		if files, err := s.releases.DiscoverComposeFiles(existingPath); err == nil {
			prevComposeFiles = files
		}
	}

	// Resolve compose files from the committed (but not yet active) release dir.
	newReleasePath := s.releases.ReleasePath(app.Dir, deployment.ArtifactDigest)
	composeFiles, err := s.releases.DiscoverComposeFiles(newReleasePath)
	if err != nil {
		s.failDeployment(ctx, app, deployment, "discover compose files", err, nil, prevComposeFiles)
		return
	}

	envState, err := s.writeEnv(app, deployment.Image)
	if err != nil {
		s.failDeployment(ctx, app, deployment, "write env", err, nil, prevComposeFiles)
		return
	}

	deployment.Status = model.StatusPulling
	s.saveState(ctx, deployment)

	output, err := s.executor.Exec(ctx, app.Dir, ComposePullArgs(app, composeFiles))
	if err != nil {
		s.failDeployment(ctx, app, deployment, "compose pull", withOutput(err, output), &envState, prevComposeFiles)
		return
	}

	deployment.Status = model.StatusStarting
	s.saveState(ctx, deployment)

	output, err = s.executor.Exec(ctx, app.Dir, ComposeUpArgs(app, composeFiles))
	if err != nil {
		s.failDeployment(ctx, app, deployment, "compose up", withOutput(err, output), &envState, prevComposeFiles)
		return
	}

	deployment.Status = model.StatusHealthCheck
	s.saveState(ctx, deployment)

	if err := s.health.Check(ctx, app.HealthURL, app.HealthTimeout); err != nil {
		s.failDeployment(ctx, app, deployment, "health check", err, &envState, prevComposeFiles)
		return
	}

	// Activate only after health check passes.
	if _, err := s.releases.Activate(app.Dir, deployment.ArtifactDigest); err != nil {
		s.failDeployment(ctx, app, deployment, "activate release", err, &envState, prevComposeFiles)
		return
	}

	deployment.Status = model.StatusCompleted
	deployment.EndedAt = time.Now()
	s.saveState(ctx, deployment)

	s.logAudit(ctx, model.AuditEntry{
		Timestamp:  time.Now(),
		AppName:    deployment.AppName,
		Action:     "deploy",
		Status:     "completed",
		Image:      deployment.Image,
		Tag:        deployment.Tag,
		DurationMs: deployment.EndedAt.Sub(deployment.StartedAt).Milliseconds(),
	})

	s.logger.Info("deployment completed",
		"app", deployment.AppName,
		"image", deployment.Image,
		"tag", deployment.Tag,
		"duration", deployment.EndedAt.Sub(deployment.StartedAt),
	)

	if pruned, err := s.store.Prune(ctx, deployment.AppName, 20); err != nil {
		s.logger.Warn("failed to prune old deployments", "app", deployment.AppName, "error", err)
	} else if pruned > 0 {
		s.logger.Info("pruned old deployments", "app", deployment.AppName, "count", pruned)
	}

	s.releases.PruneReleases(app.Dir, app.KeepReleases)
	s.pruneEnvBackups(app, 10)
}

func (s *Service) Status(ctx context.Context, appName string) (*model.Deployment, error) {
	if _, ok := s.apps[appName]; !ok {
		return nil, model.ErrAppNotFound
	}
	return s.store.GetLatest(ctx, appName)
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) failDeployment(ctx context.Context, app model.AppConfig, d *model.Deployment, step string, err error, envState *envFileState, prevComposeFiles []string) {
	var rollbackErr error
	var rollbackUpErr error
	if envState != nil {
		rollbackErr = s.restoreEnv(*envState)
		if rollbackErr == nil && envState.Existed {
			// Symlink was never switched; current symlink still points to previous release.
			// Discover compose files from current active release and bring it back up.
			activePath, activeErr := s.releases.ActiveReleasePath(app.Dir)
			if activeErr == nil {
				activeFiles, discoverErr := s.releases.DiscoverComposeFiles(activePath)
				if discoverErr == nil && len(activeFiles) > 0 {
					if _, rbErr := s.executor.Exec(ctx, app.Dir, ComposeUpArgs(app, activeFiles)); rbErr != nil {
						s.logger.Error("rollback compose up failed", "app", d.AppName, "error", rbErr)
						rollbackUpErr = rbErr
					}
				}
			} else if len(prevComposeFiles) > 0 {
				// Fall back to the compose files captured before deploy started.
				if _, rbErr := s.executor.Exec(ctx, app.Dir, ComposeUpArgs(app, prevComposeFiles)); rbErr != nil {
					s.logger.Error("rollback compose up failed", "app", d.AppName, "error", rbErr)
					rollbackUpErr = rbErr
				}
			}
		}
	}
	s.releases.MarkBadRelease(app.Dir, d.ArtifactDigest, step+": "+err.Error())
	d.Status = model.StatusFailed
	d.EndedAt = time.Now()
	d.Error = formatFailure(step, err, rollbackErr, rollbackUpErr)
	s.saveState(ctx, d)

	s.logAudit(ctx, model.AuditEntry{
		Timestamp:  time.Now(),
		AppName:    d.AppName,
		Action:     "deploy",
		Status:     "failed",
		Image:      d.Image,
		Tag:        d.Tag,
		Error:      d.Error,
		DurationMs: d.EndedAt.Sub(d.StartedAt).Milliseconds(),
	})

	s.logger.Error("deployment failed",
		"app", d.AppName,
		"step", step,
		"error", err,
		"rollback_error", rollbackErr,
	)
}

type envFileState struct {
	Path    string
	Existed bool
	Content []byte
}

func (s *Service) writeEnv(app model.AppConfig, image string) (envFileState, error) {
	envPath := filepath.Join(app.Dir, app.EnvFile)
	state := envFileState{Path: envPath}

	backupDir := filepath.Join(s.dataDir, "envbackups", app.Name)
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return envFileState{}, fmt.Errorf("create env backup dir: %w", err)
	}

	if data, err := os.ReadFile(envPath); err == nil {
		state.Existed = true
		state.Content = data
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%d.env", time.Now().UnixNano()))
		if err := os.WriteFile(backupPath, data, 0640); err != nil {
			return envFileState{}, fmt.Errorf("write env backup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return envFileState{}, fmt.Errorf("read env file: %w", err)
	}

	content := fmt.Sprintf("%s=%s\n", app.ImageVar, image)
	tmpPath := envPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return envFileState{}, fmt.Errorf("open env tmp: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return envFileState{}, fmt.Errorf("write env tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return envFileState{}, fmt.Errorf("sync env tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return envFileState{}, fmt.Errorf("close env tmp: %w", err)
	}
	if err := os.Rename(tmpPath, envPath); err != nil {
		os.Remove(tmpPath)
		return envFileState{}, fmt.Errorf("rename env: %w", err)
	}
	return state, nil
}

func (s *Service) pruneEnvBackups(app model.AppConfig, keep int) {
	backupDir := filepath.Join(s.dataDir, "envbackups", app.Name)
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) <= keep {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries[:len(entries)-keep] {
		os.Remove(filepath.Join(backupDir, e.Name()))
	}
}

func (s *Service) readCurrentImage(app model.AppConfig) string {
	envPath := filepath.Join(app.Dir, app.EnvFile)
	data, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	prefix := app.ImageVar + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

func (s *Service) saveState(ctx context.Context, d *model.Deployment) {
	if err := s.store.Save(ctx, d); err != nil {
		s.logger.Warn("failed to persist deployment state",
			"app", d.AppName,
			"deployment_id", d.ID,
			"status", d.Status,
			"error", err,
		)
	}
}

func (s *Service) logAudit(ctx context.Context, entry model.AuditEntry) {
	if err := s.audit.Log(ctx, entry); err != nil {
		s.logger.Error("failed to write audit log", "error", err)
	}
}

func (s *Service) restoreEnv(state envFileState) error {
	if !state.Existed {
		if err := os.Remove(state.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove env file: %w", err)
		}
		return nil
	}
	tmpPath := state.Path + ".restore"
	if err := os.WriteFile(tmpPath, state.Content, 0640); err != nil {
		return fmt.Errorf("write env restore tmp: %w", err)
	}
	if err := os.Rename(tmpPath, state.Path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename env restore: %w", err)
	}
	return nil
}

func formatFailure(step string, err error, rollbackErr error, rollbackUpErr error) string {
	s := fmt.Sprintf("%s: %v", step, err)
	if rollbackErr != nil {
		s += fmt.Sprintf("; restore env: %v", rollbackErr)
	}
	if rollbackUpErr != nil {
		s += fmt.Sprintf("; rollback compose up: %v", rollbackUpErr)
	}
	return s
}

func withOutput(err error, output []byte) error {
	if len(output) == 0 {
		return err
	}
	detail := strings.TrimSpace(string(output))
	if len(detail) > 500 {
		detail = "..." + detail[len(detail)-500:]
	}
	return fmt.Errorf("%w: %s", err, detail)
}
