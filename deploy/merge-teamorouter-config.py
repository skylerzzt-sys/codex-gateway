#!/usr/bin/env python3
"""Safely add or update the TeamoRouter provider in an existing CPA config."""

from __future__ import annotations

import os
import shutil
import stat
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

import yaml

PROVIDER_NAME = "teamorouter"
PROVIDER = {
    "name": PROVIDER_NAME,
    "prefix": "teamo",
    "base-url": "https://api.teamorouter.cn/v1",
    "api-key-entries": [],
    "models": [
        {
            "name": model,
            "alias": model,
            "display-name": f"{label} · TeamoRouter",
            "input-modalities": ["text", "image"],
            "output-modalities": ["text"],
        }
        for model, label in (
            ("gpt-5.6-sol", "GPT-5.6 Sol"),
            ("gpt-5.6-terra", "GPT-5.6 Terra"),
            ("gpt-5.6-luna", "GPT-5.6 Luna"),
        )
    ],
}


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: merge-teamorouter-config.py CONFIG_PATH")
    key = os.environ.get("TEAMOROUTER_API_KEY", "").strip()
    if not key.startswith("sk-teamo-"):
        raise SystemExit("TEAMOROUTER_API_KEY must be set and start with sk-teamo-")

    path = Path(sys.argv[1]).resolve()
    document = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    if not isinstance(document, dict):
        raise SystemExit("config root must be a mapping")

    providers = document.setdefault("openai-compatibility", [])
    if not isinstance(providers, list):
        raise SystemExit("openai-compatibility must be a list")

    provider = dict(PROVIDER)
    provider["api-key-entries"] = [{"api-key": key}]
    matches = [index for index, item in enumerate(providers) if isinstance(item, dict) and item.get("name") == PROVIDER_NAME]
    if len(matches) > 1:
        raise SystemExit("multiple teamorouter providers found")
    if matches:
        providers[matches[0]] = provider
    else:
        providers.append(provider)

    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup = path.with_name(f"{path.name}.before-teamorouter-{stamp}")
    shutil.copy2(path, backup)

    mode = stat.S_IMODE(path.stat().st_mode)
    rendered = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as handle:
        handle.write(rendered)
        temporary = Path(handle.name)
    temporary.chmod(mode)
    temporary.replace(path)
    print(f"backup={backup}")
    print("provider=teamo")
    print("models=teamo/gpt-5.6-sol,teamo/gpt-5.6-terra,teamo/gpt-5.6-luna")


if __name__ == "__main__":
    main()
