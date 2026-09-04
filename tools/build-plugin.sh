#!/usr/bin/env bash
set -eu

root=/mnt/d/CodexWorkspace/codex-personal-gateway
repo="$root/upstream/cpa-account-config-manager"
go="$root/.toolchains/go-1.26.7/bin/go"

export CGO_ENABLED=1
export GOMODCACHE="$root/.cache/go-mod"
export GOCACHE="$root/.cache/go-build"
export GOPATH="$root/.cache/go-path"

cd "$repo"
mkdir -p dist
"$go" build -p 1 -buildvcs=false \
  -ldflags "-X cpa-account-config-manager/internal/manager.PluginVersion=0.3.1333-personal.1" \
  -buildmode=c-shared \
  -o dist/cpa-account-config-manager.so .
