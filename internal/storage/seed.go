package storage

import (
	_ "embed"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-sum/furnace/internal/model"
)

//go:embed sql/seed_app.sql
var seedAppSQL string

// SeedIfEmpty inserts default config and app rows using INSERT OR IGNORE,
// so it is safe to call on a DB that already has data.
func SeedIfEmpty(ctx context.Context, db *sql.DB, dataDir, appsDir string) error {
	seedApp := model.AppConfig{
		Name:            "furnace-web",
		Image:           "ghcr.io/go-sum/furnace-web",
		TagPattern:      "v*",
		AllowedIdentity: "go-sum/furnace",
		Artifact:        "ghcr.io/go-sum/furnace-web:{tag}-compose",
		Domain:          "furnace.server",
		Dir:             filepath.Join(appsDir, "furnace-web"),
		Port:            8080,
		Container:       "furnace-web-web-1",
		HealthTimeout:   2 * time.Minute,
		TLS:             true,
		EnvFile:         ".deploy.env",
		ImageVar:        "APP_IMAGE",
		KeepReleases:    5,
	}
	if err := seedApp.Validate(); err != nil {
		return fmt.Errorf("SeedIfEmpty: seed app invalid: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("SeedIfEmpty: begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range []struct {
		sql string
		arg any
	}{
		{`INSERT OR IGNORE INTO config (key, value) VALUES ('data_dir', ?)`, dataDir},
		{`INSERT OR IGNORE INTO config (key, value) VALUES ('poll_interval', '1m0s')`, nil},
		{`INSERT OR IGNORE INTO config (key, value) VALUES ('trusted_proxies', '[]')`, nil},
	} {
		var err error
		if stmt.arg != nil {
			_, err = tx.ExecContext(ctx, stmt.sql, stmt.arg)
		} else {
			_, err = tx.ExecContext(ctx, stmt.sql)
		}
		if err != nil {
			return fmt.Errorf("SeedIfEmpty: exec config seed: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, seedAppSQL,
		seedApp.Name,
		seedApp.Image,
		seedApp.TagPattern,
		seedApp.AllowedIdentity,
		seedApp.Artifact,
		seedApp.Domain,
		seedApp.Dir,
		seedApp.Port,
		boolToInt(seedApp.TLS),
		seedApp.EnvFile,
		seedApp.ImageVar,
		seedApp.Container,
		seedApp.HealthTimeout.String(),
		seedApp.KeepReleases,
	); err != nil {
		return fmt.Errorf("SeedIfEmpty: exec app seed: %w", err)
	}

	return tx.Commit()
}
