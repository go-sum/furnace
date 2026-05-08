package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

type SQLiteAppStore struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewSQLiteAppStore(db *sql.DB, logger *slog.Logger) *SQLiteAppStore {
	return &SQLiteAppStore{db: db, logger: logger}
}

func (s *SQLiteAppStore) UpsertApp(ctx context.Context, app model.AppConfig) error {
	if err := app.Validate(); err != nil {
		return fmt.Errorf("SQLiteAppStore.UpsertApp: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO apps (name, image, tag_pattern, allowed_identity, artifact, domain, dir, port, tls, env_file, image_var, container, health_timeout, keep_releases)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    image            = excluded.image,
    tag_pattern      = excluded.tag_pattern,
    allowed_identity = excluded.allowed_identity,
    artifact         = excluded.artifact,
    domain           = excluded.domain,
    dir              = excluded.dir,
    port             = excluded.port,
    tls              = excluded.tls,
    env_file         = excluded.env_file,
    image_var        = excluded.image_var,
    container        = excluded.container,
    health_timeout   = excluded.health_timeout,
    keep_releases    = excluded.keep_releases`,
		app.Name,
		app.Image,
		app.TagPattern,
		app.AllowedIdentity,
		app.Artifact,
		app.Domain,
		app.Dir,
		app.Port,
		boolToInt(app.TLS),
		app.EnvFile,
		app.ImageVar,
		app.Container,
		app.HealthTimeout.String(),
		app.KeepReleases,
	)
	if err != nil {
		return fmt.Errorf("SQLiteAppStore.UpsertApp: %w", err)
	}
	return nil
}

func (s *SQLiteAppStore) GetApp(ctx context.Context, name string) (model.AppConfig, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT name, image, tag_pattern, allowed_identity, artifact, domain, dir, port, tls, env_file, image_var, container, health_timeout, keep_releases
FROM apps WHERE name = ?`, name)
	app, err := scanApp(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AppConfig{}, model.ErrAppNotFound
	}
	if err != nil {
		return model.AppConfig{}, fmt.Errorf("SQLiteAppStore.GetApp: %w", err)
	}
	return app, nil
}

func (s *SQLiteAppStore) ListApps(ctx context.Context) ([]model.AppConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, image, tag_pattern, allowed_identity, artifact, domain, dir, port, tls, env_file, image_var, container, health_timeout, keep_releases
FROM apps ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteAppStore.ListApps: %w", err)
	}
	defer rows.Close()

	var apps []model.AppConfig
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, fmt.Errorf("SQLiteAppStore.ListApps: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteAppStore.ListApps: %w", err)
	}
	return apps, nil
}

func (s *SQLiteAppStore) DeleteApp(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM apps WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("SQLiteAppStore.DeleteApp: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("SQLiteAppStore.DeleteApp: rows affected: %w", err)
	}
	if n == 0 {
		return model.ErrAppNotFound
	}
	return nil
}

func (s *SQLiteAppStore) AppExists(ctx context.Context, name string) (bool, error) {
	var dummy int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM apps WHERE name = ? LIMIT 1`, name).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("SQLiteAppStore.AppExists: %w", err)
	}
	return true, nil
}

func (s *SQLiteAppStore) GetConfigValue(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("SQLiteAppStore.GetConfigValue: %w", err)
	}
	return value, true, nil
}

func (s *SQLiteAppStore) SetConfigValue(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO config (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("SQLiteAppStore.SetConfigValue: %w", err)
	}
	return nil
}

func scanApp(s scanner) (model.AppConfig, error) {
	var (
		app           model.AppConfig
		tlsInt        int
		healthTimeout string
	)
	err := s.Scan(
		&app.Name,
		&app.Image,
		&app.TagPattern,
		&app.AllowedIdentity,
		&app.Artifact,
		&app.Domain,
		&app.Dir,
		&app.Port,
		&tlsInt,
		&app.EnvFile,
		&app.ImageVar,
		&app.Container,
		&healthTimeout,
		&app.KeepReleases,
	)
	if err != nil {
		return model.AppConfig{}, err
	}
	app.TLS = tlsInt != 0
	d, err := time.ParseDuration(healthTimeout)
	if err != nil {
		return model.AppConfig{}, fmt.Errorf("parse health_timeout %q: %w", healthTimeout, err)
	}
	app.HealthTimeout = d
	return app, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
