#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ghcr-consume.sh --manifest ghcr-publish.json [options]

Verifies a published image and compose OCI artifact with cosign, then pulls the
image and downloads the compose artifact to prove a consumer host can use the
published GHCR artifacts.

Required:
  --manifest FILE              JSON manifest written by ghcr-publish.sh

Options:
  --output-dir DIR             Directory to download compose files into
                               default: ghcr-consume
  --skip-image-pull            Verify image only; do not docker pull
  --skip-compose-pull          Verify compose only; do not oras pull
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 1
  }
}

manifest=""
output_dir="ghcr-consume"
skip_image_pull=0
skip_compose_pull=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --manifest) manifest="$2"; shift 2 ;;
    --output-dir) output_dir="$2"; shift 2 ;;
    --skip-image-pull) skip_image_pull=1; shift ;;
    --skip-compose-pull) skip_compose_pull=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$manifest" ]]; then
  echo "error: --manifest is required" >&2
  usage
  exit 1
fi
if [[ ! -f "$manifest" ]]; then
  echo "error: manifest not found: $manifest" >&2
  exit 1
fi

require_cmd docker
require_cmd cosign
require_cmd oras
require_cmd jq

image_ref="$(jq -r '.image_ref' "$manifest")"
compose_ref="$(jq -r '.compose_ref' "$manifest")"
certificate_identity_regexp="$(jq -r '.certificate_identity_regexp' "$manifest")"
github_workflow_repository="$(jq -r '.github_workflow_repository' "$manifest")"
github_workflow_ref="$(jq -r '.github_workflow_ref' "$manifest")"
certificate_oidc_issuer="$(jq -r '.certificate_oidc_issuer' "$manifest")"

if [[ -z "$image_ref" || -z "$compose_ref" || -z "$certificate_identity_regexp" || -z "$github_workflow_repository" || -z "$github_workflow_ref" || -z "$certificate_oidc_issuer" ]]; then
  echo "error: manifest is missing required fields" >&2
  exit 1
fi

echo "Verifying ${image_ref}"
cosign verify \
  --certificate-identity-regexp "$certificate_identity_regexp" \
  --certificate-github-workflow-repository "$github_workflow_repository" \
  --certificate-github-workflow-ref "$github_workflow_ref" \
  --certificate-oidc-issuer "$certificate_oidc_issuer" \
  "$image_ref" >/dev/null

echo "Verifying ${compose_ref}"
cosign verify \
  --certificate-identity-regexp "$certificate_identity_regexp" \
  --certificate-github-workflow-repository "$github_workflow_repository" \
  --certificate-github-workflow-ref "$github_workflow_ref" \
  --certificate-oidc-issuer "$certificate_oidc_issuer" \
  "$compose_ref" >/dev/null

if [[ $skip_image_pull -eq 0 ]]; then
  echo "Pulling ${image_ref}"
  docker pull "$image_ref" >/dev/null
  docker image inspect "$image_ref" >/dev/null
fi

if [[ $skip_compose_pull -eq 0 ]]; then
  rm -rf "$output_dir"
  mkdir -p "$output_dir"
  echo "Pulling ${compose_ref} into ${output_dir}"
  oras pull "$compose_ref" -o "$output_dir" >/dev/null
  find "$output_dir" -maxdepth 1 -type f | sort
fi
