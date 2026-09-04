#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo="$root/upstream/cpa-account-config-manager"
plugin_version="${PLUGIN_VERSION:-0.3.1333-personal.1}"

find_go() {
  local candidate
  for candidate in \
    "${GO_BIN:-}" \
    "$root/.toolchains/go-1.26.7/bin/go" \
    "$(command -v go 2>/dev/null || true)"; do
    [[ -n "$candidate" && -x "$candidate" ]] || continue
    if "$candidate" version >/dev/null 2>&1; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

go_bin="$(find_go || true)"
if [[ -z "$go_bin" ]]; then
  printf 'Go toolchain not found. On macOS: brew install go zig\n' >&2
  exit 1
fi

export GOMODCACHE="$root/.cache/go-mod"
export GOCACHE="$root/.cache/go-build"
export GOPATH="$root/.cache/go-path"
mkdir -p "$GOMODCACHE" "$GOCACHE" "$GOPATH" "$repo/dist"

host_os="$(uname -s)"
host_arch="$(uname -m)"
case "$host_os" in
  Linux)
    if [[ "$host_arch" != "x86_64" && "$host_arch" != "amd64" ]]; then
      printf 'Linux host architecture %s is not supported by the native VPS plugin build.\n' "$host_arch" >&2
      exit 1
    fi
    export CGO_ENABLED=1
    unset GOOS GOARCH
    ;;
  Darwin)
    if ! command -v zig >/dev/null 2>&1; then
      printf 'macOS cross-build requires Zig. Install it with: brew install zig\n' >&2
      exit 1
    fi
    export GOOS=linux
    export GOARCH=amd64
    export CGO_ENABLED=1
    export CC="zig cc -target x86_64-linux-gnu.2.17"
    export CXX="zig c++ -target x86_64-linux-gnu.2.17"
    ;;
  *)
    printf 'Unsupported build host: %s %s\n' "$host_os" "$host_arch" >&2
    exit 1
    ;;
esac

cd "$repo"
"$go_bin" build -p 1 -buildvcs=false \
  -ldflags "-X cpa-account-config-manager/internal/manager.PluginVersion=$plugin_version" \
  -buildmode=c-shared \
  -o dist/cpa-account-config-manager.so .

printf 'Built Linux amd64 plugin: %s\n' "$repo/dist/cpa-account-config-manager.so"
