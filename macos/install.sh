#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  printf 'This installer is for macOS only.\n' >&2
  exit 1
fi

if ! command -v codex >/dev/null 2>&1; then
  printf 'Codex CLI was not found in PATH. Install Codex first, then rerun this script.\n' >&2
  exit 1
fi

if [[ ! -x /usr/bin/security ]]; then
  printf 'macOS Keychain command /usr/bin/security was not found.\n' >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
codex_home="${CODEX_HOME:-$HOME/.codex}"
profile_path="$codex_home/personal-gateway.config.toml"
bin_dir="$HOME/.local/bin"
keychain_account="codex-gateway"
keychain_service="codex-personal-gateway"

mkdir -p "$codex_home" "$bin_dir"

gateway_key=""
if [[ -n "${CODEX_GATEWAY_API_KEY:-}" ]]; then
  gateway_key="$CODEX_GATEWAY_API_KEY"
else
  gateway_key="$(/usr/bin/security find-generic-password -w -a "$keychain_account" -s "$keychain_service" 2>/dev/null || true)"
fi
if [[ -z "$gateway_key" ]]; then
  printf 'Enter CODEX_GATEWAY_API_KEY (input is hidden): '
  IFS= read -r -s gateway_key
  printf '\n'
fi
if [[ -z "$gateway_key" ]]; then
  printf 'Gateway API key cannot be empty.\n' >&2
  exit 1
fi

/usr/bin/security add-generic-password \
  -U \
  -a "$keychain_account" \
  -s "$keychain_service" \
  -T /usr/bin/security \
  -w "$gateway_key" >/dev/null
unset gateway_key

if [[ -f "$profile_path" ]]; then
  cp -p "$profile_path" "$profile_path.bak-macos-install"
fi

tmp_profile="$(mktemp "$codex_home/.personal-gateway.config.toml.XXXXXX")"
cat > "$tmp_profile" <<'TOML'
model = "gpt-5.4"
model_provider = "personal_gateway"

[model_providers.personal_gateway]
name = "Personal Codex Gateway"
base_url = "https://97.64.21.36:8443/v1"
wire_api = "responses"
requires_openai_auth = false

[model_providers.personal_gateway.auth]
command = "/usr/bin/security"
args = ["find-generic-password", "-w", "-a", "codex-gateway", "-s", "codex-personal-gateway"]
timeout_ms = 5000
refresh_interval_ms = 300000
TOML
chmod 600 "$tmp_profile"
mv -f "$tmp_profile" "$profile_path"

install -m 755 "$script_dir/codex-app-bind" "$bin_dir/codex-app-bind"
install -m 755 "$script_dir/codex-gateway" "$bin_dir/codex-gateway"
install -m 755 "$script_dir/codex-teamo" "$bin_dir/codex-teamo"
install -m 755 "$script_dir/codex-gateway-app" "$bin_dir/codex-gateway-app"
install -m 755 "$script_dir/codex-gateway-bind" "$bin_dir/codex-gateway-bind"
install -m 755 "$script_dir/codex-teamo-app" "$bin_dir/codex-teamo-app"
install -m 755 "$script_dir/codex-teamo-bind" "$bin_dir/codex-teamo-bind"
install -m 755 "$script_dir/codex-official" "$bin_dir/codex-official"

if ! /usr/bin/security find-generic-password -w -a "$keychain_account" -s "$keychain_service" >/dev/null 2>&1; then
  printf 'Keychain verification failed.\n' >&2
  exit 1
fi

printf '\nInstalled macOS Codex Gateway helpers in %s\n' "$bin_dir"
printf 'Profile: %s\n' "$profile_path"
if [[ ":$PATH:" != *":$bin_dir:"* ]]; then
  printf '\nAdd this line to ~/.zshrc, then open a new terminal:\n'
  printf '  export PATH="$HOME/.local/bin:$PATH"\n'
fi
printf '\nCommands:\n'
printf '  codex-gateway             Personal Gateway CLI\n'
printf '  codex-teamo               TeamoRouter CLI\n'
printf '  codex-gateway-app [path]  Bind Personal Gateway and open Codex Desktop\n'
printf '  codex-gateway-bind        Bind Desktop to Personal Gateway\n'
printf '  codex-teamo-app [path]    Bind TeamoRouter and open Codex Desktop\n'
printf '  codex-teamo-bind          Bind Desktop to TeamoRouter\n'
printf '  codex-official            Restore official OpenAI provider\n'
