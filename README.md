# Furnace

A pull-based deployment agent for VPS servers. You SSH in once to bootstrap it, then never again — the worker polls your container registry, verifies signatures with Sigstore, and deploys apps automatically.

**The problem:** deploying to a VPS typically requires push-based webhooks, SSH keys, or CI runners with persistent inbound access. These are hard to scope, easy to leak, and add operational overhead.

**The Furnace approach:** the worker polls GHCR for new image tags. When it finds one, it verifies the image's Sigstore signature (checking the GitHub Actions OIDC identity of the signing workflow) and runs `docker compose up`. Public images need no registry secret; private images can use an encrypted systemd credential.

## Contents

- [Architecture](#architecture)
- [How It Works](#how-it-works)
- [App Convention](#app-convention)
- [Installation](#installation)
- [Adding an App](#adding-an-app)
- [The Deploy Hint Endpoint](#the-deploy-hint-endpoint)
- [Monitoring](#monitoring)
- [Teardown](#teardown)
- [CLI Reference](#cli-reference)
- [Configuration Reference](#configuration-reference)
- [Data Layout](#data-layout)
- [Security Model](#security-model)

## Architecture

```
┌─────────────────────────────────────────────────────┐
│ VPS (Docker host)                                   │
│                                                     │
│  caddy_net (Docker bridge network)                  │
│  ├── caddy (:80, :443)                              │
│  │     reverse_proxy furnace-web-1:8080             │
│  │     reverse_proxy myapp-web-1:8080               │
│  ├── furnace-web-1   (furnace web container)        │
│  └── myapp-web-1     (app web container)            │
│                                                     │
│  furnace worker (systemd host binary)               │
│    polls GHCR → verifies Sigstore → compose up      │
└─────────────────────────────────────────────────────┘
```

- **All containers** (Caddy, furnace-web, apps) share `caddy_net`, a Docker bridge network created by `furnace init`.
- **Caddy** routes each domain to `{app}-web-1:{port}` using Docker container DNS — no host-port publishing needed.
- **Worker** runs as the host-installed `furnace` binary under systemd. It uses the system Docker daemon to run `docker compose` commands.
- **furnace-web** is a separate container image deployed by the worker like any other app.

## How It Works

### Poll cycle (every `poll_interval`)

```
worker
  │
  ├─ for each app in furnace.yaml:
  │    1. List tags from GHCR matching tag_pattern
  │    2. Find the newest semver tag not yet deployed
  │    3. If image digest matches last deploy AND artifact digest matches → skip
  │    4. If image changed: verify Sigstore signature of the new image
  │    5. Fetch compose artifact from OCI registry; verify artifact signature
  │    6. Stage artifact files in a new .furnace/releases/.staging-* directory
  │    7. Commit staging → rename to digest-based release directory
  │    8. docker compose pull && docker compose up -d --remove-orphans (from staged release)
  │    9. Poll health_url until 2xx or timeout
  │   10. On success: flip current symlink to new release; prune old releases
  │   11. On failure: mark release .furnace-bad; current symlink unchanged (instant rollback)
  │
  └─ sleep poll_interval, repeat
```

Compose topology is always fetched from the OCI artifact configured per app and staged in `.furnace/releases/` before each deploy. The `current` symlink points to the active release and only moves after a successful health check — if anything fails after that point, the symlink still points to the last known-good release.

The `/v1/apps/{app}/deploy` hint endpoint lets an app's release workflow signal the worker to check immediately, reducing typical deploy latency from `poll_interval` to seconds. It requires no credentials — it is a hint, not a trigger.

### App release workflow

```
GitHub Actions (on release)
  │
  ├─ Build and push image to GHCR
  │    ghcr.io/org/app:v1.2.0
  │
  ├─ Sign image with Sigstore (cosign)
  │    Identity: github.com/org/app via OIDC
  │
  └─ POST /v1/apps/myapp/deploy  (hint — no auth required)
       → worker polls immediately
```

## App Convention

Furnace is purpose-built for apps that follow the [foundry/starter](https://github.com/go-sum/foundry) pattern. Each app directory (`dir` in config) holds the runtime state for deployed releases:

| Path | Purpose |
|------|---------|
| `.deploy.env` | Written by furnace on each deploy: `APP_IMAGE=ghcr.io/org/app@sha256:abcdef...` |
| `.secrets/` | Docker secrets directory (DATABASE_URL, etc.) |
| `.furnace/releases/` | Release directories managed by furnace; compose files are fetched here from the OCI artifact |
| `.furnace/current` | Symlink to the active release directory |

Compose files (`docker-compose.yml`, etc.) are fetched from the configured OCI artifact on every deploy — the operator does not provision them manually.

Compose services must attach to `caddy_net` so Caddy can reach them by container name:

```yaml
# docker-compose.yml (excerpt)
services:
  web:
    image: ${APP_IMAGE}
    networks:
      - caddy_net

networks:
  caddy_net:
    external: true
```

## Installation

Furnace needs a Linux VPS with Docker. You only need to SSH in once.

### 1. Install the binary

The installer downloads the binary, verifies its Sigstore signature against the
Rekor transparency log, then installs it. Docker must be present — it is used to
run cosign for verification and is also required to run furnace.

**One-liner:**

```bash
curl -fsSL https://raw.githubusercontent.com/go-sum/furnace/main/bin/install.sh | sudo bash
```

**Inspect before running:**

```bash
curl -fsSL https://raw.githubusercontent.com/go-sum/furnace/main/bin/install.sh -o install.sh
less install.sh
sudo bash install.sh
```

**What `install.sh` does, step by step:**

| Step | Action |
|------|--------|
| 1 | Detects CPU architecture (`amd64` / `arm64`) |
| 2 | Fetches the latest release tag from the GitHub API |
| 3 | Downloads `furnace-linux-<arch>` and `furnace-linux-<arch>.bundle` from the release |
| 4 | Runs `cosign verify-blob` inside a `cgr.dev/chainguard/cosign` container — verifies the binary was signed by a GitHub Actions workflow in `go-sum/furnace`, that the certificate was issued by GitHub's OIDC provider, and that a valid Rekor transparency log inclusion proof exists |
| 5 | Installs the binary to `/usr/local/bin/furnace` only if step 4 passes |

If the signature check fails, the script exits before touching `/usr/local/bin`. The bundle file (certificate + transparency log entry + signature) is a Sigstore artifact produced by `cosign sign-blob` in the release workflow — the binary cannot be substituted without controlling the `go-sum/furnace` GitHub Actions OIDC identity.

```bash
furnace --version   # confirm install
```

### 2. Initialize the VPS

`furnace init` is idempotent — safe to run multiple times.

```bash
sudo furnace init
```

This creates:
```bash
    user `furnace`  # added to the `docker` group
    
    /etc/furnace/furnace.yaml # config scaffold (only on first run)
    /var/lib/furnace/ # deployment records, audit logs, locks
    /srv/apps/ # app directories
    /srv/furnace/proxy/ # Caddy reverse proxy compose setup
    /srv/furnace/certs/ # TLS certificates
    caddy_net # Docker bridge network
```

### 3. Edit the config

```bash
sudo nano /etc/furnace/furnace.yaml
  or
sudo vi /etc/furnace/furnace.yaml
```

Add each app you want to deploy, including `furnace-web` itself:

```yaml
data_dir: "/var/lib/furnace"
poll_interval: "60s"

apps:
  furnace-web:
    image: "ghcr.io/go-sum/furnace-web"
    tag_pattern: "v*"
    allowed_identity: "go-sum/furnace"
    artifact: "ghcr.io/go-sum/furnace-web:{tag}-compose"
    domain: "furnace.example.com"
    dir: "/srv/apps/furnace-web"
    health_url: "http://furnace-web-web-1:8080/v1/health"

  myapp:
    image: "ghcr.io/yourorg/myapp"
    tag_pattern: "v*"
    allowed_identity: "yourorg/myapp"
    artifact: "ghcr.io/yourorg/myapp:{tag}-compose"
    domain: "myapp.example.com"
    dir: "/srv/apps/myapp"
    health_url: "http://myapp-web-1:8080/healthz"
```

Validate the config before starting:

```bash
sudo furnace validate
```

### 4. TLS certificates

TLS is configured per app via the `tls` field. The default (`false`) assumes external TLS termination — Cloudflare Tunnel or another terminator handles HTTPS, and Caddy proxies plain HTTP internally.

**Local certs — `tls: true`**

Set `tls: true` on any app that needs a furnace-managed local certificate. Furnace generates an ECDSA P-256 CA and a server cert covering all `tls: true` app domains. The CA is installed to the system trust store so containers on the same host trust it.

```yaml
apps:
  myapp:
    domain: "myapp.example.com"
    tls: true   # furnace generates local cert for this domain
```

```bash
sudo furnace mkcert --install      # generate CA, install to system trust store
sudo furnace mkcert                # generate cert for all apps with tls: true
```

To regenerate for specific apps only:

```bash
sudo furnace mkcert furnace-web myapp
```

If no apps have `tls: true`, `furnace mkcert` prints "no certs created" and exits cleanly.

**External TLS — `tls: false` (default)**

Leave `tls` unset or set it to `false`. Use a [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) or another external TLS terminator. No cert generation is needed.

### 5. Start everything

```bash
sudo furnace start

# Or with private registry support (e.g. ghcr.io private images):
sudo furnace start --credential ghp_TOKEN
```

This single command:
1. Writes `/etc/systemd/system/furnace-worker.service`
2. Runs `systemctl daemon-reload`
3. Generates the Caddyfile and writes proxy files to `/srv/furnace/proxy/`
4. Starts the Caddy container (`docker compose up -d`)
5. Enables and starts the worker (`systemctl enable --now furnace-worker`)

The worker begins polling immediately. On the first cycle it will find and deploy all apps in your config, including `furnace-web`.
This is the first end-to-end proof path: the host-installed `furnace` worker deploys the separate `furnace-web` container image and its signed compose artifact before managing any other apps.

### 6. Close SSH

All further updates happen automatically — the worker polls GHCR and deploys new releases as they appear. You can harden the VPS now: close unused ports and disable password authentication.

## Adding an App

To add a new app after initial setup:

1. Add the app entry to `/etc/furnace/furnace.yaml` (set `tls: true` if the app needs a local cert)
2. Run `sudo furnace proxy init` to regenerate the Caddyfile
3. Run `sudo furnace proxy up` to reload Caddy with the new route
4. If `tls: true`, run `sudo furnace mkcert` to regenerate the server cert with the new domain
5. Run `sudo systemctl restart furnace-worker`

The worker reads config once at startup. Adding or removing apps requires a restart. furnace-web redeploys automatically on the next poll cycle.

Minimal app entry:

```yaml
  myapp:
    image: "ghcr.io/yourorg/myapp"
    tag_pattern: "v*"
    allowed_identity: "yourorg/myapp"
    artifact: "ghcr.io/yourorg/myapp:{tag}-compose"
    domain: "myapp.example.com"
    health_url: "http://myapp-web-1:8080/healthz"
```

The `artifact` field is required — compose topology is always fetched from the OCI artifact and Sigstore-verified on every deploy. The artifact must be signed with the same `allowed_identity` as the app image. Use `{tag}` as a placeholder for the image tag being deployed.

## The Deploy Hint Endpoint

```
POST /v1/apps/{app}/deploy
```

No authentication. Signals the worker to check the registry for `{app}` immediately rather than waiting for the next poll interval. The worker still verifies the signature — the hint cannot bypass any security check.

Call it from your release workflow to reduce deploy latency:

```bash
curl -fsSL -X POST https://furnace.example.com/v1/apps/myapp/deploy
```

The endpoint returns `202 Accepted` immediately; the actual deployment happens asynchronously.

## Monitoring

### Proxy status

```bash
furnace proxy status
```

### Worker logs

```bash
furnace worker logs -f
```

Or directly via journalctl:

```bash
journalctl -u furnace-worker -f
```

Furnace emits structured JSON logs to stdout (captured by systemd).

### App status

```bash
curl -s https://furnace.example.com/v1/apps/myapp/status
```

```json
{
  "id": "01JVABCDEF1234567890ABCDEF",
  "app_name": "myapp",
  "image": "ghcr.io/yourorg/myapp@sha256:a1b2c3d4...",
  "status": "completed",
  "started_at": "2025-01-15T10:30:00Z",
  "ended_at": "2025-01-15T10:30:45Z"
}
```

### Audit log

Every deployment start, success, and failure is appended to a JSONL file:

```bash
tail -20 /var/lib/furnace/audit/*.jsonl | jq .
```

## Teardown

`furnace reset` removes everything furnace installed from the VPS. It requires root and prompts for explicit confirmation.

```bash
sudo furnace reset
```

What it removes:
- Stops and disables the `furnace-worker` systemd unit
- Removes `/etc/systemd/system/furnace-worker.service` and reloads systemd
- Brings down the Caddy proxy (`docker compose down`)
- Removes the `caddy_net` Docker network
- Removes the system CA (`/usr/local/share/ca-certificates/furnace-ca.crt`) and runs `update-ca-certificates`
- Removes all furnace directories: `/etc/furnace`, `/var/lib/furnace`, `/srv/apps`, `/srv/furnace`
- Deletes the `furnace` system user

`furnace reset` is the inverse of `furnace init` + `furnace start`. It does not uninstall the `furnace` binary from `/usr/local/bin`.

## CLI Reference

```
furnace [--config <path>] <command>
```

Global flag: `--config` sets the config file path (default: `/etc/furnace/furnace.yaml`).

| Command | Requires root | Description |
|---------|--------------|-------------|
| `furnace init` | yes | Create system user, directories, config scaffold, and `caddy_net` network. Idempotent. |
| `furnace start [--credential TOKEN]` | yes | Write systemd unit, start Caddy proxy, enable and start worker. Pass `--credential` to store an encrypted registry token. |
| `furnace reset` | yes | Remove all furnace state — inverse of `init` + `start`. Prompts for confirmation. |
| `furnace validate` | no | Parse and validate the config file; print app count. |
| `furnace mkcert --install` | yes | Generate ECDSA P-256 CA, write to `/var/lib/furnace/ca/`, install to system trust store. Skips if CA already exists. |
| `furnace mkcert [app...]` | yes | Generate server cert for all apps (or named apps) from config. Writes to `/srv/furnace/certs/local.pem` and `local-key.pem`. Requires CA from `--install`. |
| `furnace proxy init` | yes | Regenerate Caddyfile and `compose.yml` from current config. |
| `furnace proxy up` | yes | Start (or restart) the Caddy container (`docker compose up -d`). |
| `furnace proxy down` | yes | Stop the Caddy container (`docker compose down`). |
| `furnace proxy status` | yes | Show Caddy container status (`docker compose ps`). |
| `furnace proxy logs [-f]` | yes | Show Caddy container logs. Pass `-f` to follow. |
| `furnace worker run` | no | Run the furnace-worker poll loop (used by systemd). Handles graceful shutdown on SIGINT/SIGTERM. |
| `furnace worker stop` | yes | Stop the furnace-worker systemd unit. |
| `furnace worker status` | no | Show furnace-worker systemd unit status. |
| `furnace worker logs [-f]` | no | Show furnace-worker logs via journalctl. Pass `-f` to follow. |

`furnace worker run` is the subcommand used by the systemd unit. The containerized HTTP app is shipped separately as the `ghcr.io/go-sum/furnace-web` image and runs the `furnace-web` binary directly.

## Configuration Reference

`/etc/furnace/furnace.yaml`:

```yaml
# Deployment records, audit logs, locks, env backups.
data_dir: "/var/lib/furnace"

# How often to poll each app's registry. The /deploy hint can short-circuit to 1s.
poll_interval: "60s"

# CIDR ranges of trusted reverse proxies for rate limiting.
# When set, X-Forwarded-For from these IPs is used to identify the real client.
# Typical Docker bridge: ["172.16.0.0/12"].
trusted_proxies:
  - "172.16.0.0/12"

apps:
  myapp:
    # Base image path in GHCR (without tag).
    image: "ghcr.io/yourorg/myapp"

    # Glob pattern for tags to watch (path.Match rules). "v*" matches v1.0.0, etc.
    tag_pattern: "v*"

    # GitHub org/repo whose Sigstore identity must have signed the image.
    allowed_identity: "yourorg/myapp"

    # Public domain for Caddyfile generation. Must be lowercase (e.g. myapp.example.com).
    domain: "myapp.example.com"

    # Absolute path to the app directory on the VPS (default: /srv/apps/{name}).
    dir: "/srv/apps/myapp"

    # Port the app's web container listens on (default: 8080).
    # Caddy routes via container DNS: {name}-web-1:{port}
    port: 8080

    # TLS mode for this app (default: false).
    # true  → furnace generates a local cert; Caddy serves tls /certs/local.pem /certs/local-key.pem
    # false → no TLS directive; relies on Cloudflare Tunnel or external TLS termination
    tls: false

    # Env file and variable written by furnace on each deploy.
    env_file: ".deploy.env"    # default
    image_var: "APP_IMAGE"     # default

    # Health check endpoint polled after docker compose up.
    # Uses container DNS — app containers are reachable on caddy_net.
    health_url: "http://myapp-web-1:8080/healthz"
    health_timeout: "30s"

    # OCI artifact reference for compose files (required).
    # The worker fetches and Sigstore-verifies compose files from this OCI artifact
    # before each deploy, staging them in .furnace/releases/. The artifact must be
    # signed with the same allowed_identity as the app image. Use {tag} as a
    # placeholder for the image tag being deployed.
    artifact: "ghcr.io/yourorg/myapp:{tag}-compose"

    # Number of old release directories to retain under .furnace/releases/ (default: 5).
    keep_releases: 5
```

Use `furnace validate` to check your config file for errors without starting the worker.

## Data Layout

```
/etc/furnace/
  furnace.yaml              # app configuration
  registry-token.cred       # encrypted registry token (created by --credential)

/var/lib/furnace/
  ca/
    ca.pem                  # furnace CA certificate (created by mkcert --install)
    ca-key.pem              # furnace CA private key (0600)
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
    .deploy.env              # written by furnace on each deploy
    .secrets/                # Docker secrets directory
    .furnace/
      current -> releases/sha256-abc123.../  # symlink to active release; flips on successful deploy
      releases/
        sha256-abc123.../             # committed release (compose files fetched from artifact)
        sha256-def456.../             # previous release (retained per keep_releases)
        .staging-abcd1234/            # in-progress staging dir (cleaned up on failure)
        sha256-bad789.../
          .furnace-bad                # marker written when deploy fails health check

/srv/furnace/proxy/
  compose.yml               # Caddy Docker Compose
  Caddyfile                 # regenerated by furnace on app add/remove

/srv/furnace/certs/
  local.pem                 # TLS cert (created by furnace mkcert)
  local-key.pem             # TLS key (0600)

/usr/local/share/ca-certificates/
  furnace-ca.crt            # system trust store entry (created by mkcert --install)
```

## Security Model

- **Pull-based, no inbound webhooks.** The worker initiates all outbound connections to GHCR and Sigstore. No inbound network access is required beyond the hint endpoint.
- **Encrypted credential storage.** Registry tokens are encrypted at rest via systemd-creds. At runtime the worker reads the decrypted token from `CREDENTIALS_DIRECTORY` and materializes an ephemeral Docker auth config in its private tmp area for `docker compose pull`; no persistent Docker auth file is stored under `/var/lib/furnace`.
- **Sigstore signature verification.** Every image must be signed by a GitHub Actions workflow from the configured `allowed_identity` repository. The signature is verified against Sigstore's public transparency log (Rekor). An unsigned or incorrectly signed image is rejected before any deployment step runs.
- **Public mode needs no registry secret.** Public images and artifacts use anonymous pulls. Private images can use an encrypted registry token loaded through systemd credentials. Signing identity is still verified cryptographically, not via the registry token.
- **OCI-sourced compose topology.** Compose files are always fetched from the OCI artifact on every deploy and verified by cosign before any filesystem write. The artifact must be signed with the same `allowed_identity` as the app image — an operator cannot substitute compose files without controlling the signing identity. Files are staged atomically before the `current` symlink flips; a failed health check leaves the symlink pointing to the previous release, making rollback instantaneous.
- **Executor subcommand allowlist.** The worker only permits `docker compose` subcommands. `docker run`, `docker exec`, and similar privileged primitives are rejected at the executor layer.
- **Domain validation.** The `domain` field is validated against an RFC 1123 hostname regex at config load time, preventing Caddyfile directive injection via malformed domain values.
- **caddy_net isolation.** Caddy and app containers share a single Docker bridge network. Containers are reachable only by name within the network; no host ports are published for app-to-Caddy routing.
- **Caddy container hardening.** Caddy runs with `read_only: true`, `cap_drop: [ALL]`, `cap_add: [NET_BIND_SERVICE]`, `no-new-privileges`, and a tmpfs `/tmp`.
- **Distroless furnace-web image.** The `ghcr.io/go-sum/furnace-web` image is packaged on `cgr.dev/chainguard/static:latest` — no shell, no package manager, no setuid binaries.
- **Systemd hardening.** Worker unit includes: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, scoped `ReadWritePaths`, `CapabilityBoundingSet=` (empty), `SystemCallFilter=@system-service`, `PrivateDevices`, `RestrictAddressFamilies`, `LockPersonality`, `MemoryDenyWriteExecute`, `RestrictNamespaces`, `ProtectKernelTunables`, `ProtectControlGroups`, `RestrictSUIDSGID`.
- **Flock-based deploy lock.** One deployment per app at a time, survives process restarts.
- **Env file rollback.** On failure, `.deploy.env` is restored so the next compose up uses the last known-good image.
- **Audit trail.** All deployment events logged to append-only JSONL.
- **Self-signed CA via stdlib crypto.** `furnace mkcert` generates ECDSA P-256 certificates using Go's `crypto/x509` — no external tools or dependencies required. The CA key is stored at `0600`. The system CA entry is removed by `furnace reset`.
- **Status endpoint exposure.** The `/v1/apps/{app}/status` endpoint returns deployment metadata. Restrict access to trusted networks via firewall rules or a Cloudflare Tunnel access policy if your VPS is publicly reachable.

## License

See [LICENSE](LICENSE) for details.
