package storage

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

type SQLiteDeploymentStore struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewSQLiteDeploymentStore(db *sql.DB, logger *slog.Logger) *SQLiteDeploymentStore {
	return &SQLiteDeploymentStore{db: db, logger: logger}
}

func (s *SQLiteDeploymentStore) Save(ctx context.Context, d *model.Deployment) error {
	// Fall back to a fresh context when the caller's is already cancelled.
	// State writes must survive graceful shutdown — mirrors the old FileDeploymentStore
	// which ignored context entirely for saves.
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	endedAt := ""
	if !d.EndedAt.IsZero() {
		endedAt = d.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO deployments
    (id, app_name, image, tag, digest, artifact_digest, prev_image, status, started_at, ended_at, error)
VALUES
    (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    status    = excluded.status,
    ended_at  = excluded.ended_at,
    error     = excluded.error`,
		d.ID,
		d.AppName,
		d.Image,
		d.Tag,
		d.Digest,
		d.ArtifactDigest,
		d.PrevImage,
		string(d.Status),
		d.StartedAt.UTC().Format(time.RFC3339Nano),
		endedAt,
		d.Error,
	)
	return err
}

func (s *SQLiteDeploymentStore) GetLatest(ctx context.Context, appName string) (*model.Deployment, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, app_name, image, tag, digest, artifact_digest, prev_image, status, started_at, ended_at, error
FROM deployments
WHERE app_name = ?
ORDER BY started_at DESC
LIMIT 1`, appName)

	d, err := scanDeployment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *SQLiteDeploymentStore) List(ctx context.Context, appName string, limit int) ([]model.Deployment, error) {
	effectiveLimit := limit
	if effectiveLimit <= 0 {
		effectiveLimit = 1<<31 - 1
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, app_name, image, tag, digest, artifact_digest, prev_image, status, started_at, ended_at, error
FROM deployments
WHERE app_name = ?
ORDER BY started_at DESC
LIMIT ?`, appName, effectiveLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []model.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, *d)
	}
	return deployments, rows.Err()
}

func (s *SQLiteDeploymentStore) Prune(ctx context.Context, appName string, keep int) (int, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM deployments
WHERE app_name = ?
  AND status IN ('completed', 'failed')
  AND id NOT IN (
      SELECT id FROM deployments
      WHERE app_name = ?
      ORDER BY started_at DESC
      LIMIT ?
  )`, appName, appName, keep)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// scanner abstracts *sql.Row and *sql.Rows for shared scan logic.
type scanner interface {
	Scan(dest ...any) error
}

func scanDeployment(s scanner) (*model.Deployment, error) {
	var (
		d         model.Deployment
		status    string
		startedAt string
		endedAt   string
	)
	err := s.Scan(
		&d.ID,
		&d.AppName,
		&d.Image,
		&d.Tag,
		&d.Digest,
		&d.ArtifactDigest,
		&d.PrevImage,
		&status,
		&startedAt,
		&endedAt,
		&d.Error,
	)
	if err != nil {
		return nil, err
	}
	d.Status = model.DeploymentStatus(status)
	if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
		d.StartedAt = t
	}
	if endedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, endedAt); err == nil {
			d.EndedAt = t
		}
	}
	return &d, nil
}
