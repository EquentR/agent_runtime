#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$ROOT_DIR/dist"
FRONTEND_DIR="$ROOT_DIR/webapp"
FRONTEND_BUILD_DIR="$FRONTEND_DIR/dist"
EMBEDDED_WEB_DIR="$ROOT_DIR/app/router/embedded_web"
CONFIG_DIR="$DIST_DIR/conf"
APP_NAME="ice_art"

if [[ "$(go env GOOS)" == "windows" ]]; then
  APP_NAME="ice_art.exe"
fi

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$command_name" >&2
    exit 1
  fi
}

git_value_or_default() {
  local command_text="$1"
  local fallback="$2"
  local value

  if value=$(eval "$command_text" 2>/dev/null); then
    printf '%s' "$value"
    return 0
  fi
  printf '%s' "$fallback"
}

require_command go
require_command pnpm

VERSION="$(git_value_or_default 'git describe --tags --always --dirty' 'dev')"
GIT_COMMIT="$(git_value_or_default 'git rev-parse --short HEAD' 'none')"

cleanup_embedded_web() {
  rm -rf "$EMBEDDED_WEB_DIR"
  mkdir -p "$EMBEDDED_WEB_DIR"
  touch "$EMBEDDED_WEB_DIR/.gitkeep"
}

trap cleanup_embedded_web EXIT

printf '==> preparing dist directory\n'
rm -rf "$DIST_DIR"
mkdir -p "$CONFIG_DIR" "$DIST_DIR/data" "$DIST_DIR/logs" "$DIST_DIR/workspace"

printf '==> building frontend\n'
pnpm --dir "$FRONTEND_DIR" build
rm -rf "$EMBEDDED_WEB_DIR"
mkdir -p "$EMBEDDED_WEB_DIR"
cp -R "$FRONTEND_BUILD_DIR"/. "$EMBEDDED_WEB_DIR"/

printf '==> building backend\n'
go build \
  -tags embed_web \
  -trimpath \
  -ldflags "-s -w -X main.Version=$VERSION -X main.GitCommit=$GIT_COMMIT" \
  -o "$DIST_DIR/$APP_NAME" \
  ./cmd/ice_art

printf '==> packaging release defaults\n'
go run ./scripts/releasepack -source "$ROOT_DIR" -dest "$DIST_DIR"

printf '\nBuild completed.\n'
printf 'Output: %s\n' "$DIST_DIR"
printf 'Executable: %s\n' "$DIST_DIR/$APP_NAME"
