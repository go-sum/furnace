# Furnace

A secure deployment agent for VPS servers. Furnace receives deployment requests from GitHub Actions via OIDC-authenticated API calls, then orchestrates Docker Compose to pull and start containers.

Authentication is cryptographic — every request carries a short-lived GitHub Actions OIDC token. Only workflows from the correct repository, branch/tag, and workflow file are accepted. No SSH keys, no shared secrets, no long-lived credentials.

## How It Works

```
GitHub Actions (on release)
    │
    │  Authorization: Bearer <oidc-token>
    │  POST /v1/apps/{app}/deploy  {"image": "ghcr.io/org/app:v1.2.0"}
    ▼
Furnace (loopback listener, behind reverse proxy)
    │
    │  1. Verify JWT signature against GitHub OIDC discovery endpoint
    │  2. Check token claims: repo, ref glob, workflow file identity
    │  3. Check image matches allowed_image_prefix
    │  4. Write image ref to .deploy.env
    │  5. docker compose -f docker-compose.data.yml -f docker-compose.yml pull
    │  6. docker compose -f docker-compose.data.yml -f docker-compose.yml up -d
    │  7. Poll health URL until 2xx or timeout
    │  8. On failure: restore .deploy.env to previous state
    ▼
Your App (running in Docker, proxied by Caddy)
```

## App Convention

Furnace is purpose-built for apps that follow the [foundry/starter](https://github.com/go-sum/foundry) pattern. Apps are expected to have:

| File | Purpose |
|------|---------|
| `docker-compose.data.yml` | Data services: postgres, kv (dragonfly), etc. |
| `docker-compose.yml` | App services: `web`, `worker`. References `${APP_IMAGE}`. |
| `.deploy.env` | Written by furnace on each deploy: `APP_IMAGE=ghcr.io/org/app:v1.2.0` |
| `.secrets/` | Docker secrets directory (DATABASE_URL, etc.) |

The `web` service must be reachable on the `caddy_net` Docker network (default name) for the reverse proxy to route traffic to it.

A production deploy is always:
```bash
docker compose -f docker-compose.data.yml -f docker-compose.yml up -d --remove-orphans
```

## Installation

Furnace needs a Linux VPS with Docker. You only need to SSH in once.

### 1. Download the binary

```bash
# amd64 (most VPS providers)
curl -fsSL https://github.com/go-sum/furnace/releases/latest/download/furnace-linux-amd64 \
  -o /usr/local/bin/furnace && chmod +x /usr/local/bin/furnace

# arm64
curl -fsSL https://github.com/go-sum/furnace/releases/latest/download/furnace-linux-arm64 \
  -o /usr/local/bin/furnace && chmod +x /usr/local/bin/furnace
```

### 2. Verify the installation

```bash
furnace --version
# furnace version v0.x.x
```

If this shows `dev` you are running an untagged build.

### 3. Initialize the VPS

`furnace init` is idempotent — safe to run multiple times.

```bash
sudo furnace init
```

This creates:
- System user `furnace` (added to the `docker` group)
- `/etc/furnace/furnace.yaml` — config scaffold (only on first run)
- `/var/lib/furnace/` — deployment records, audit logs, locks
- `/srv/apps/` — app directories
- `/srv/furnace/proxy/` — Caddy reverse proxy compose setup
- `/srv/furnace/certs/` — TLS certificates
- Docker network `caddy_net`

### 4. Edit the config

```bash
nano /etc/furnace/furnace.yaml
```

Fill in the GitHub audience and your management repo (the ops repo that will call management endpoints):

```yaml
listen: "127.0.0.1:8080"
data_dir: "/var/lib/furnace"

github:
  issuer: "https://token.actions.githubusercontent.com"
  audience: "furnace://prod"

management:
  repo: "yourorg/infra"
  workflow: ".github/workflows/furnace.yml"
  allowed_ref: "refs/heads/main"

apps: {}
```

### 5. Start furnace

```bash
sudo furnace systemd start
```

Writes `/etc/systemd/system/furnace.service`, runs `systemctl enable --now furnace`.

Verify:

```bash
furnace systemd health
# {"status":"ok"}
```

### 6. Start the reverse proxy

Furnace ships a Caddy reverse proxy configuration at `/srv/furnace/proxy/`.

```bash
sudo furnace proxy init   # generates Caddyfile from current app configs
sudo furnace proxy up     # docker compose up -d
```

For staging with mkcert certificates:

```bash
# Install mkcert and generate certs for your domain
mkcert -install
mkcert -cert-file /srv/furnace/certs/local.pem -key-file /srv/furnace/certs/local-key.pem \
  yourdomain.example.com "*.yourdomain.example.com"
```

For production, use a [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) — Cloudflare handles TLS and Caddy runs with `auto_https off`.

### 7. Close SSH

All further management is done remotely via GitHub Actions. You can harden the VPS now — close unused ports and disable password authentication.

To update furnace itself:

```bash
furnace update
```

## Adding an App

Once furnace is running, apps are added via an authenticated API call from your ops repo. No SSH required.

In your `yourorg/infra` repository, create a workflow:

```yaml
# .github/workflows/furnace.yml
name: Furnace Management

on:
  workflow_dispatch:
    inputs:
      app:
        description: "App name (lowercase, hyphens ok)"
        required: true
      repo:
        description: "GitHub repo (org/name)"
        required: true
      domain:
        description: "Public domain for this app"
        required: true

permissions:
  id-token: write

jobs:
  add-app:
    runs-on: ubuntu-latest
    steps:
      - name: Add app to furnace
        run: |
          TOKEN=$(curl -sS \
            -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
            "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=furnace://prod" | jq -r '.value')

          curl -fsSL -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d '{
              "repo": "${{ inputs.repo }}",
              "domain": "${{ inputs.domain }}",
              "allowed_ref": "refs/tags/v*",
              "workflow": ".github/workflows/deploy.yml",
              "allowed_image_prefix": "ghcr.io/${{ inputs.repo }}:",
              "health_url": "http://${{ inputs.app }}-web-1:8080/healthz"
            }' \
            https://furnace.example.com/v1/apps/${{ inputs.app }}/add
```

Furnace will:
1. Validate the management OIDC token against your `management.repo` config
2. Create `/srv/apps/{app}/` on the VPS
3. Add the app to `furnace.yaml`
4. Regenerate the Caddyfile with the new domain
5. Reload Caddy

## Deploying an App

In the **app repository**, create a release workflow:

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  release:
    types: [published]

permissions:
  contents: read
  packages: write
  id-token: write

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push image
        uses: docker/build-push-action@v6
        with:
          push: true
          tags: ghcr.io/${{ github.repository }}:${{ github.ref_name }}

      - name: Deploy
        env:
          FURNACE_URL: https://furnace.example.com
          APP_NAME: myapp
        run: |
          TOKEN=$(curl -sS \
            -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
            "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=furnace://prod" | jq -r '.value')

          curl -fsSL -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"image\": \"ghcr.io/${{ github.repository }}:${{ github.ref_name }}\"}" \
            "$FURNACE_URL/v1/apps/$APP_NAME/deploy"
```

### Deployment lifecycle

| Status | What happens |
|--------|-------------|
| `pending` | Request accepted, deployment record created, lock acquired |
| `pulling` | `docker compose ... pull` |
| `starting` | `docker compose ... up -d --remove-orphans` |
| `health_check` | Polling health URL (backoff: 1s → 2s → 4s → 5s cap) |
| `completed` | Health check passed, lock released |
| `failed` | Stage failed, `.deploy.env` restored to previous image, lock released |

### Checking status

```bash
curl -s https://furnace.example.com/v1/apps/myapp/status
```

```json
{
  "id": "01JVABCDEF1234567890ABCDEF",
  "app_name": "myapp",
  "image": "ghcr.io/yourorg/myapp:v1.2.0",
  "status": "completed",
  "actor": "username",
  "repo": "yourorg/myapp",
  "ref": "refs/tags/v1.2.0",
  "started_at": "2025-01-15T10:30:00Z",
  "ended_at": "2025-01-15T10:30:45Z"
}
```

The status endpoint requires no authentication.

## Monitoring

### Furnace health

```bash
furnace systemd health
# or
curl -s http://127.0.0.1:8080/v1/health
```

### Proxy status

```bash
furnace proxy status
```

### Systemd journal

```bash
furnace systemd status
# or
journalctl -u furnace -f
```

Furnace emits structured JSON logs to stdout (captured by systemd).

### Audit log

Every deployment start, success, and failure is appended to a JSONL file:

```bash
tail -20 /var/lib/furnace/audit/*.jsonl | jq .
```

### Deployment history

Records are stored as JSON files in `<data_dir>/deployments/<app>/`. Furnace keeps the 20 most recent per app.

## API Reference

All endpoints return JSON. Authenticated endpoints require `Authorization: Bearer <oidc-token>`.

### `GET /v1/health`

No auth. Not rate-limited. For uptime monitors.

**Response:** `200 OK` → `{"status":"ok"}`

### `POST /v1/apps/{app}/add`

Register a new app. Requires management repo OIDC token.

**Body:**
```json
{
  "repo": "org/myapp",
  "domain": "myapp.example.com",
  "allowed_ref": "refs/tags/v*",
  "workflow": ".github/workflows/deploy.yml",
  "allowed_image_prefix": "ghcr.io/org/myapp:",
  "health_url": "http://myapp-web-1:8080/healthz",
  "health_timeout": "30s",
  "port": 8080
}
```

| Status | Meaning |
|--------|---------|
| `201 Created` | App registered, Caddyfile updated |
| `400 Bad Request` | Invalid app name, missing required field, invalid ref pattern |
| `401 Unauthorized` | Missing or invalid token |
| `403 Forbidden` | Token is not from the management repo |
| `409 Conflict` | App name already exists |

### `POST /v1/apps/{app}/deploy`

Trigger a deployment. Requires app OIDC token (must match app's `repo`, `allowed_ref`, and `workflow`).

**Body:** `{"image": "ghcr.io/org/app:v1.0.0"}`

| Status | Meaning |
|--------|---------|
| `202 Accepted` | Deployment started (async) |
| `400 Bad Request` | Missing or invalid image |
| `401 Unauthorized` | Missing or invalid token |
| `403 Forbidden` | Repo/ref/workflow/image not allowed |
| `404 Not Found` | Unknown app |
| `409 Conflict` | Deployment already in progress |
| `429 Too Many Requests` | Rate limit exceeded (20 burst, 10/sec) |

### `GET /v1/apps/{app}/status`

Latest deployment record. No auth required.

Returns `{"status":"no deployments"}` if the app has never been deployed.

## Configuration Reference

`/etc/furnace/furnace.yaml`:

```yaml
# Loopback address only — furnace enforces this.
listen: "127.0.0.1:8080"

# Deployment records, audit logs, locks, env backups.
data_dir: "/var/lib/furnace"

github:
  issuer: "https://token.actions.githubusercontent.com"  # fixed value for GitHub Actions
  audience: "furnace://prod"  # must match the audience in your workflows

# Ops repo that can call management endpoints (add app, etc.)
management:
  repo: "yourorg/infra"
  workflow: ".github/workflows/furnace.yml"
  allowed_ref: "refs/heads/main"

apps:
  myapp:
    repo: "yourorg/myapp"                       # required
    allowed_ref: "refs/tags/v*"                 # required — glob (path.Match rules)
    workflow: ".github/workflows/deploy.yml"    # required — must be in .github/workflows/
    dir: "/srv/apps/myapp"                      # required — absolute path on VPS
    domain: "myapp.example.com"                 # required — for Caddyfile generation
    port: 8080                                  # default 8080

    # Compose files, relative to dir (foundry/starter convention)
    compose_files:
      - docker-compose.data.yml
      - docker-compose.yml

    env_file: ".deploy.env"                     # default
    image_var: "APP_IMAGE"                      # default
    allowed_image_prefix: "ghcr.io/yourorg/myapp:"  # required

    health_url: "http://myapp-web-1:8080/healthz"   # required
    health_timeout: "30s"                            # default
```

## Data Layout

```
/var/lib/furnace/
  deployments/
    myapp/
      01JVABCDEF....json    # per-deployment records (latest 20 kept)
  audit/
    myapp.jsonl             # append-only JSONL audit log
  locks/
    myapp.lock              # flock-based concurrency lock
  envbackups/
    myapp/
      1705312200000000000.env  # .deploy.env snapshots (latest 10 kept)

/srv/apps/
  myapp/
    docker-compose.data.yml
    docker-compose.yml
    .deploy.env             # written by furnace on each deploy
    .secrets/               # Docker secrets directory

/srv/furnace/proxy/
  compose.yml               # Caddy Docker Compose
  Caddyfile                 # regenerated by furnace on app add/remove

/srv/furnace/certs/
  local.pem                 # TLS cert (mkcert for staging)
  local-key.pem             # TLS key
```

## Security Model

- **No shared secrets.** Every authenticated request carries a cryptographically signed GitHub OIDC JWT, verified against GitHub's public keys.
- **Loopback-only listener.** Furnace refuses to bind to non-loopback addresses. All external traffic passes through the Caddy reverse proxy.
- **Systemd hardening.** `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, 256MB memory limit.
- **Image prefix validation.** Only images with the configured prefix are accepted — a token from the right repo can't deploy an arbitrary image.
- **Workflow identity pinning.** Both `workflow_ref` and `job_workflow_ref` OIDC claims are checked against the configured workflow path, preventing deployment from any other workflow file.
- **Ref pattern matching.** `allowed_ref` restricts which branches or tags can trigger deploys.
- **Management separation.** App deploy tokens cannot call management endpoints; only tokens from the dedicated management repo can register or remove apps.
- **Flock-based deploy lock.** One deployment per app at a time, survives process restarts.
- **Env file rollback.** On failure, `.deploy.env` is restored so the next `docker compose up` uses the last known-good image.
- **Rate limiting.** 20 burst / 10 per second per IP. Health endpoint excluded.
- **Request size limit.** Bodies capped at 1MB.
- **Audit trail.** All deployment events logged to append-only JSONL.

## License

See [LICENSE](LICENSE) for details.
