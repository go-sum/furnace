#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ghcr-publish.sh --tag v0.1.23 [options]

Builds and pushes the furnace-web image and compose OCI artifact to GHCR,
signs both with cosign, verifies both using the expected signing identity,
and writes a JSON manifest for a consumer host.

Required:
  --tag TAG                    Release tag to publish, e.g. v0.1.23

Options:
  --image-repo REPO            OCI image repo
                               default: ghcr.io/go-sum/furnace-web
  --compose-repo REPO          OCI compose artifact repo
                               default: same as --image-repo
  --compose-tag TAG            Compose artifact tag
                               default: <tag>-compose
  --compose-dir DIR            Directory containing compose YAML files
                               default: docker/compose
  --artifact-type TYPE         Compose OCI artifact type
                               default: application/vnd.furnace.compose
  --platforms LIST             Buildx platform list
                               default: linux/amd64,linux/arm64
  --github-repo OWNER/REPO     Expected GitHub repo for keyless signing
                               default: $GITHUB_REPOSITORY or go-sum/furnace
  --github-workflow-ref REF    Expected GitHub workflow git ref claim
                               default: refs/tags/<tag>
  --certificate-identity-regexp REGEXP
                               Override identity regexp
  --certificate-oidc-issuer U  OIDC issuer
                               default: https://token.actions.githubusercontent.com
  --output FILE                Manifest output path
                               default: ghcr-publish.json
  --push-latest                Also push image:latest

Environment:
  GHCR_USERNAME                Required if not set by GITHUB_ACTOR
  GHCR_TOKEN                   Required if not set by GITHUB_TOKEN
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 1
  }
}

tag=""
image_repo="ghcr.io/go-sum/furnace-web"
compose_repo=""
compose_tag=""
compose_dir="docker/compose"
artifact_type="application/vnd.furnace.compose"
platforms="linux/amd64,linux/arm64"
github_repo="${GITHUB_REPOSITORY:-go-sum/furnace}"
github_workflow_ref=""
certificate_identity_regexp=""
certificate_oidc_issuer="https://token.actions.githubusercontent.com"
output="ghcr-publish.json"
push_latest=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) tag="$2"; shift 2 ;;
    --image-repo) image_repo="$2"; shift 2 ;;
    --compose-repo) compose_repo="$2"; shift 2 ;;
    --compose-tag) compose_tag="$2"; shift 2 ;;
    --compose-dir) compose_dir="$2"; shift 2 ;;
    --artifact-type) artifact_type="$2"; shift 2 ;;
    --platforms) platforms="$2"; shift 2 ;;
    --github-repo) github_repo="$2"; shift 2 ;;
    --github-workflow-ref) github_workflow_ref="$2"; shift 2 ;;
    --certificate-identity-regexp) certificate_identity_regexp="$2"; shift 2 ;;
    --certificate-oidc-issuer) certificate_oidc_issuer="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    --push-latest) push_latest=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$tag" ]]; then
  echo "error: --tag is required" >&2
  usage
  exit 1
fi

if [[ -z "$compose_repo" ]]; then
  compose_repo="$image_repo"
fi
if [[ -z "$compose_tag" ]]; then
  compose_tag="${tag}-compose"
fi
if [[ -z "$github_workflow_ref" ]]; then
  github_workflow_ref="refs/tags/${tag}"
fi
if [[ -z "$certificate_identity_regexp" ]]; then
  certificate_identity_regexp="^https://github\\.com/${github_repo}/"
fi

ghcr_username="${GHCR_USERNAME:-${GITHUB_ACTOR:-}}"
ghcr_token="${GHCR_TOKEN:-${GITHUB_TOKEN:-}}"

if [[ -z "$ghcr_username" || -z "$ghcr_token" ]]; then
  echo "error: GHCR_USERNAME/GHCR_TOKEN or GITHUB_ACTOR/GITHUB_TOKEN is required" >&2
  exit 1
fi

require_cmd docker
require_cmd cosign
require_cmd oras
require_cmd jq
require_cmd find
require_cmd sort

if [[ ! -d "$compose_dir" ]]; then
  echo "error: compose directory not found: $compose_dir" >&2
  exit 1
fi

mapfile -t compose_files < <(find "$compose_dir" -maxdepth 1 -type f \( -name '*.yml' -o -name '*.yaml' \) | sort)
if [[ ${#compose_files[@]} -eq 0 ]]; then
  echo "error: no compose YAML files found in $compose_dir" >&2
  exit 1
fi

echo "$ghcr_token" | docker login ghcr.io -u "$ghcr_username" --password-stdin >/dev/null
echo "$ghcr_token" | oras login ghcr.io -u "$ghcr_username" --password-stdin >/dev/null

build_args=(
  docker buildx build
  --platform "$platforms"
  --push
  --build-arg "VERSION=${tag}"
  --tag "${image_repo}:${tag}"
)
if [[ $push_latest -eq 1 ]]; then
  build_args+=(--tag "${image_repo}:latest")
fi
build_args+=(.)

echo "Building and pushing ${image_repo}:${tag}"
"${build_args[@]}"

oras_args=(
  oras push "${compose_repo}:${compose_tag}"
  --artifact-type "$artifact_type"
)
for file in "${compose_files[@]}"; do
  oras_args+=("${file}:application/yaml")
done

echo "Pushing compose artifact ${compose_repo}:${compose_tag}"
"${oras_args[@]}"

image_ref="$(oras resolve --full-reference "${image_repo}:${tag}")"
compose_ref="$(oras resolve --full-reference "${compose_repo}:${compose_tag}")"

echo "Signing ${image_ref}"
cosign sign --yes --recursive "$image_ref"
echo "Verifying ${image_ref}"
cosign verify \
  --certificate-identity-regexp "$certificate_identity_regexp" \
  --certificate-github-workflow-repository "$github_repo" \
  --certificate-github-workflow-ref "$github_workflow_ref" \
  --certificate-oidc-issuer "$certificate_oidc_issuer" \
  "$image_ref" >/dev/null

echo "Signing ${compose_ref}"
cosign sign --yes "$compose_ref"
echo "Verifying ${compose_ref}"
cosign verify \
  --certificate-identity-regexp "$certificate_identity_regexp" \
  --certificate-github-workflow-repository "$github_repo" \
  --certificate-github-workflow-ref "$github_workflow_ref" \
  --certificate-oidc-issuer "$certificate_oidc_issuer" \
  "$compose_ref" >/dev/null

compose_json="$(printf '%s\n' "${compose_files[@]}" | jq -R . | jq -s .)"

jq -n \
  --arg tag "$tag" \
  --arg image_repo "$image_repo" \
  --arg image_ref "$image_ref" \
  --arg compose_repo "$compose_repo" \
  --arg compose_tag "$compose_tag" \
  --arg compose_ref "$compose_ref" \
  --arg certificate_identity_regexp "$certificate_identity_regexp" \
  --arg github_workflow_repository "$github_repo" \
  --arg github_workflow_ref "$github_workflow_ref" \
  --arg certificate_oidc_issuer "$certificate_oidc_issuer" \
  --arg artifact_type "$artifact_type" \
  --argjson compose_files "$compose_json" \
  '{
    tag: $tag,
    image_repo: $image_repo,
    image_ref: $image_ref,
    compose_repo: $compose_repo,
    compose_tag: $compose_tag,
    compose_ref: $compose_ref,
    artifact_type: $artifact_type,
    certificate_identity_regexp: $certificate_identity_regexp,
    github_workflow_repository: $github_workflow_repository,
    github_workflow_ref: $github_workflow_ref,
    certificate_oidc_issuer: $certificate_oidc_issuer,
    compose_files: $compose_files
  }' >"$output"

echo "Wrote ${output}"
cat "$output"
