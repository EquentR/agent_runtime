#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$ROOT_DIR/dist"
FRONTEND_DIR="$ROOT_DIR/webapp"
FRONTEND_BUILD_DIR="$FRONTEND_DIR/dist"
STATIC_DIR="$DIST_DIR/static/web"
CONFIG_DIR="$DIST_DIR/conf"
APP_NAME="example_agent.exe"

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

printf '==> preparing dist directory\n'
rm -rf "$DIST_DIR"
mkdir -p "$STATIC_DIR" "$CONFIG_DIR" "$DIST_DIR/data" "$DIST_DIR/logs" "$DIST_DIR/workspace"

printf '==> building frontend\n'
pnpm --dir "$FRONTEND_DIR" build
cp -R "$FRONTEND_BUILD_DIR"/. "$STATIC_DIR"/

printf '==> building backend\n'
go build \
  -trimpath \
  -ldflags "-s -w -X main.Version=$VERSION -X main.GitCommit=$GIT_COMMIT" \
  -o "$DIST_DIR/$APP_NAME" \
  ./cmd/example_agent

printf '==> copying config\n'
cp "$ROOT_DIR/conf/app.yaml" "$CONFIG_DIR/app.yaml"
sed -i 's#^\([[:space:]]*dir:\).*#\1 static/web#' "$CONFIG_DIR/app.yaml"

printf '\nBuild completed.\n'
printf 'Output: %s\n' "$DIST_DIR"
printf 'Double-click to start: %s\n' "$DIST_DIR/$APP_NAME"
