#!/usr/bin/env bash
# install.sh — download, verify, and install furnace
#
# Usage (one-liner):
#   curl -fsSL https://raw.githubusercontent.com/go-sum/furnace/main/install.sh | sudo bash
#
# Usage (inspect first):
#   curl -fsSL https://raw.githubusercontent.com/go-sum/furnace/main/install.sh -o install.sh
#   less install.sh
#   sudo bash install.sh
#
# What this script does:
#   1. Detects CPU architecture (amd64 / arm64)
#   2. Fetches the latest release tag from the GitHub API
#   3. Downloads the binary and its Sigstore bundle from the release
#   4. Verifies the Sigstore signature using cosign in a Docker container
#      (Docker is a hard prerequisite for furnace — no extra tooling needed)
#   5. Installs the verified binary to /usr/local/bin/furnace
#
# The verification step checks:
#   - The binary was signed by a GitHub Actions workflow in go-sum/furnace
#   - The signing certificate was issued by GitHub's OIDC provider
#   - The signature has a valid Rekor transparency log inclusion proof
#   If any check fails the script exits before touching /usr/local/bin.
#
set -euo pipefail

REPO="go-sum/furnace"
BINARY="furnace"
INSTALL_DIR="/usr/local/bin"
COSIGN_IMAGE="cgr.dev/chainguard/cosign"
OIDC_ISSUER="https://token.actions.githubusercontent.com"
IDENTITY_REGEXP="^https://github\\.com/${REPO}/"

# ── 1. Detect architecture ────────────────────────────────────────────────────

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)        GOARCH="amd64" ;;
  aarch64|arm64) GOARCH="arm64" ;;
  *)
    echo "error: unsupported architecture: ${ARCH}" >&2
    exit 1
    ;;
esac

PLATFORM="linux-${GOARCH}"
echo "Platform: ${PLATFORM}"

# ── 2. Resolve latest release tag ────────────────────────────────────────────

echo "Fetching latest release..."
VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | cut -d'"' -f4)"

if [ -z "${VERSION}" ]; then
  echo "error: could not resolve latest release version" >&2
  exit 1
fi
echo "Version: ${VERSION}"

BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
ASSET="${BINARY}-${PLATFORM}"
BUNDLE="${ASSET}.bundle"

# ── 3. Download binary + Sigstore bundle ──────────────────────────────────────

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

echo "Downloading ${ASSET}..."
curl -fsSL --progress-bar "${BASE_URL}/${ASSET}" -o "${TMPDIR}/${BINARY}"

echo "Downloading ${BUNDLE}..."
curl -fsSL --progress-bar "${BASE_URL}/${BUNDLE}" -o "${TMPDIR}/${BUNDLE}"

# ── 4. Verify Sigstore signature ──────────────────────────────────────────────
# cosign runs inside a Docker container — Docker is already required to run
# furnace so this adds no new dependencies to the bootstrap path.

echo "Verifying Sigstore signature..."

if ! command -v docker &>/dev/null; then
  echo "error: docker not found" >&2
  echo "       Docker is required to run furnace and to verify the install signature." >&2
  echo "       Install Docker first: https://docs.docker.com/engine/install/" >&2
  exit 1
fi

docker run --rm \
  --volume "${TMPDIR}:/work" \
  "${COSIGN_IMAGE}" \
  verify-blob \
  --bundle "/work/${BUNDLE}" \
  --certificate-identity-regexp "${IDENTITY_REGEXP}" \
  --certificate-oidc-issuer "${OIDC_ISSUER}" \
  "/work/${BINARY}"

echo "Signature verified."

# ── 5. Install ────────────────────────────────────────────────────────────────

if [ -w "${INSTALL_DIR}" ]; then
  install -m 755 "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  sudo install -m 755 "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo "Installed: ${INSTALL_DIR}/${BINARY} (${VERSION})"
echo "Run 'furnace --version' to confirm."
