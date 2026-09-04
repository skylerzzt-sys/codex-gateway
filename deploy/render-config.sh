#!/usr/bin/env bash
set -eu

stage_dir=${STAGE_DIR:-/opt/codex-personal-stage}
umask 077

# Validate the existing config without rewriting provider entries.
exec python3 - "$stage_dir/config.yaml" <<'PY'
import sys
from pathlib import Path
import yaml
path = Path(sys.argv[1])
document = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
if not isinstance(document, dict):
    raise SystemExit("config root must be a mapping")
print("config_check=ok")
PY
