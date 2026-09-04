#!/usr/bin/env bash
set -eu

stage_dir=${STAGE_DIR:-/opt/codex-personal-stage}
base_url=http://127.0.0.1:18317
. "$stage_dir/secrets.env"

printf 'config_check=ok\n'

for _ in $(seq 1 40); do
  if curl -fsS -o /dev/null -H "Authorization: Bearer $API_KEY" "$base_url/v1/models"; then
    break
  fi
  sleep 0.25
done

printf 'unauth_models_http=%s\n' "$(curl -sS -o /dev/null -w '%{http_code}' "$base_url/v1/models")"
printf 'auth_models_http=%s\n' "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $API_KEY" "$base_url/v1/models")"
printf 'unauth_management_http=%s\n' "$(curl -sS -o /dev/null -w '%{http_code}' "$base_url/v0/management/plugins")"

curl -fsS -H "Authorization: Bearer $MANAGEMENT_KEY" "$base_url/v0/management/plugins" |
  python3 -c 'import json,sys; d=json.load(sys.stdin); print("plugins_enabled="+str(d.get("plugins_enabled")).lower()); [print("plugin="+str(p.get("id"))+" registered="+str(p.get("registered")).lower()+" effective="+str(p.get("effective_enabled")).lower()+" version="+str((p.get("metadata") or {}).get("version"))) for p in d.get("plugins", [])]'

curl -fsS -H "Authorization: Bearer $API_KEY" "$base_url/v1/models" |
  python3 -c 'import json,sys; ids={str(m.get("id", "")) for m in json.load(sys.stdin).get("data", [])}; expected={"gpt-5.6-sol","gpt-5.6-terra","gpt-5.6-luna"}; missing=sorted(expected-ids); print("oauth_models="+str(len(expected)-len(missing))+"/"+str(len(expected))); missing and (_ for _ in ()).throw(SystemExit("missing OAuth models: "+", ".join(missing)))'

if [ "${RUN_ROUTING_SMOKE:-0}" = "1" ]; then
  export API_KEY MANAGEMENT_KEY
  python3 "$stage_dir/verify-routing-modes.py"
fi
