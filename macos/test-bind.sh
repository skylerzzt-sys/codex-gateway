#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_home="$(mktemp -d)"
trap 'rm -rf "$test_home"' EXIT
mkdir -p "$test_home/.codex"

cat > "$test_home/.codex/config.toml" <<'TOML'
model = "gpt-5.6-sol"
model_provider = "openai"
model_reasoning_effort = "high"
# UTF-8 回归：中文必须完整保留

[features]
multi_agent_v2 = true

[model_providers.personal_gateway]
name = "STALE"
base_url = "https://old.invalid/v1"

[model_providers.personal_gateway.auth]
command = "/bin/false"

[mcp_servers.demo]
command = "echo"
args = ["你好，Mac"]
TOML

cat > "$test_home/.codex/personal-gateway.config.toml" <<'TOML'
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

HOME="$test_home" CODEX_HOME="$test_home/.codex" "$script_dir/codex-app-bind" gateway >/dev/null
config="$test_home/.codex/config.toml"
grep -Fq 'model = "gpt-5.4"' "$config"
grep -Fq 'model_provider = "personal_gateway"' "$config"
grep -Fq '# UTF-8 回归：中文必须完整保留' "$config"
grep -Fq 'args = ["你好，Mac"]' "$config"
[[ "$(grep -Fc '[model_providers.personal_gateway]' "$config")" -eq 1 ]]
[[ "$(grep -Fc '[model_providers.personal_gateway.auth]' "$config")" -eq 1 ]]
if grep -Fq 'https://old.invalid/v1' "$config"; then
  printf 'stale provider block survived gateway bind\n' >&2
  exit 1
fi

HOME="$test_home" CODEX_HOME="$test_home/.codex" "$script_dir/codex-app-bind" official >/dev/null
grep -Fq 'model = "gpt-5.6-sol"' "$config"
grep -Fq 'model_provider = "openai"' "$config"
grep -Fq '# UTF-8 回归：中文必须完整保留' "$config"
grep -Fq 'args = ["你好，Mac"]' "$config"

printf 'macOS bind UTF-8 regression test passed.\n'
