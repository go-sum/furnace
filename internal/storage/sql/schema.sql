CREATE TABLE IF NOT EXISTS deployments (
    id              TEXT PRIMARY KEY,
    app_name        TEXT NOT NULL,
    image           TEXT NOT NULL,
    tag             TEXT NOT NULL DEFAULT '',
    digest          TEXT NOT NULL DEFAULT '',
    artifact_digest TEXT NOT NULL DEFAULT '',
    prev_image      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    started_at      TEXT NOT NULL,
    ended_at        TEXT NOT NULL DEFAULT '',
    error           TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_deployments_app_started
    ON deployments (app_name, started_at DESC);

CREATE TABLE IF NOT EXISTS apps (
    name             TEXT PRIMARY KEY,
    image            TEXT NOT NULL,
    tag_pattern      TEXT NOT NULL,
    allowed_identity TEXT NOT NULL,
    artifact         TEXT NOT NULL,
    domain           TEXT NOT NULL UNIQUE,
    dir              TEXT NOT NULL,
    port             INTEGER NOT NULL DEFAULT 8080,
    tls              INTEGER NOT NULL DEFAULT 0,
    env_file         TEXT NOT NULL DEFAULT '.deploy.env',
    image_var        TEXT NOT NULL DEFAULT 'APP_IMAGE',
    container        TEXT NOT NULL,
    health_timeout   TEXT NOT NULL DEFAULT '30s',
    keep_releases    INTEGER NOT NULL DEFAULT 5
);

CREATE TABLE IF NOT EXISTS config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
