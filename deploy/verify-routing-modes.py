#!/usr/bin/env python3
"""Run minimal real Responses checks for force_a, force_b, and auto."""

from __future__ import annotations

import json
import os
import shutil
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


BASE_URL = "http://127.0.0.1:18317"
PLUGIN_CONFIG = "/v0/management/plugins/cpa-account-config-manager/config"
ACCOUNTS = "/v0/management/plugins/cpa-account-config-manager/accounts"
VALID_ROLES = {"primary", "backup", "disabled"}
VALID_MODES = {"auto", "force_a", "force_b"}


def request_json(path: str, token: str, method: str = "GET", body: dict | None = None) -> tuple[int, dict]:
    raw = None if body is None else json.dumps(body, separators=(",", ":")).encode()
    request = urllib.request.Request(
        BASE_URL + path,
        data=raw,
        method=method,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            data = json.loads(response.read().decode("utf-8"))
            return response.status, data
    except urllib.error.HTTPError as exc:
        try:
            data = json.loads(exc.read().decode("utf-8"))
        except Exception:
            data = {}
        return exc.code, data


def config_body(management_key: str) -> dict:
    status, payload = request_json(PLUGIN_CONFIG, management_key)
    if status != 200:
        raise RuntimeError(f"plugin config returned HTTP {status}")
    return payload.get("config", payload)


def account_map(management_key: str) -> dict[str, dict]:
    status, payload = request_json(ACCOUNTS, management_key)
    if status != 200:
        raise RuntimeError(f"account list returned HTTP {status}")
    items = payload.get("accounts") or payload.get("items") or []
    return {str(item.get("auth_id") or "").strip(): item for item in items if item.get("auth_id")}


def validate_bindings(config: dict, accounts: dict[str, dict], baseline: tuple[str, str, str, str]) -> tuple[str, str]:
    account_a = str(config.get("gateway_account_a_id") or "").strip()
    account_b = str(config.get("gateway_account_b_id") or "").strip()
    role_a = str(config.get("gateway_role_a") or "").strip().lower()
    role_b = str(config.get("gateway_role_b") or "").strip().lower()
    mode = str(config.get("gateway_mode") or "").strip().lower()
    if not account_a or not account_b or account_a == account_b:
        raise RuntimeError("Personal Gateway A/B binding is invalid")
    if role_a not in VALID_ROLES or role_b not in VALID_ROLES or mode not in VALID_MODES:
        raise RuntimeError("Personal Gateway role or mode is invalid")
    if account_a not in accounts or account_b not in accounts:
        raise RuntimeError("Personal Gateway binding is not present in live Auth accounts")
    if baseline and (account_a, account_b, role_a, role_b) != baseline:
        raise RuntimeError("Personal Gateway binding changed during routing verification")
    return account_a, account_b


def patch_mode(management_key: str, mode: str) -> None:
    status, _ = request_json(PLUGIN_CONFIG, management_key, "PATCH", {"gateway_mode": mode})
    if status != 200:
        raise RuntimeError(f"mode patch returned HTTP {status}")
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        if str(config_body(management_key).get("gateway_mode") or "").strip().lower() == mode:
            return
        time.sleep(0.2)
    raise RuntimeError(f"mode {mode} did not become active")


def response_probe(api_key: str) -> int:
    status, payload = request_json(
        "/v1/responses",
        api_key,
        "POST",
        {
            "model": "gpt-5.4",
            "input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hi"}]}],
            "instructions": "Reply with OK only.",
            "stream": False,
            "max_output_tokens": 16,
        },
    )
    if status not in {200, 429}:
        raise RuntimeError(f"Responses probe returned HTTP {status}")
    return status


def counters(account: dict) -> tuple[int, int]:
    return int(account.get("success") or 0), int(account.get("failed") or 0)


def wait_for_change(management_key: str, target: str, other: str, before: dict[str, dict]) -> tuple[int, int, int, int]:
    deadline = time.monotonic() + 8
    before_target_success, before_target_failed = counters(before[target])
    before_other_success, before_other_failed = counters(before[other])
    while time.monotonic() < deadline:
        after = account_map(management_key)
        target_success, target_failed = counters(after[target])
        other_success, other_failed = counters(after[other])
        deltas = (
            target_success - before_target_success,
            target_failed - before_target_failed,
            other_success - before_other_success,
            other_failed - before_other_failed,
        )
        if deltas[0] >= 1 or deltas[1] >= 1:
            return deltas
        time.sleep(0.25)
    return 0, 0, 0, 0


def quota_exhausted(account: dict) -> bool:
    usage = account.get("usage") or {}
    codex = usage.get("codex") or {}
    for key in ("five_hour", "seven_day"):
        window = codex.get(key) or {}
        try:
            if float(window.get("used_percent") or 0) >= 100:
                return True
        except (TypeError, ValueError):
            pass
    return False


def account_usable(account: dict) -> bool:
    return not bool(account.get("disabled")) and not bool(account.get("unavailable")) and not quota_exhausted(account)


def main() -> int:
    management_key = os.environ.get("MANAGEMENT_KEY", "")
    api_key = os.environ.get("API_KEY", "")
    if not management_key or not api_key:
        raise RuntimeError("API_KEY and MANAGEMENT_KEY are required")
    config_path = Path(os.environ.get("CONFIG_PATH", "/opt/codex-personal-stage/config.yaml"))
    initial = config_body(management_key)
    accounts = account_map(management_key)
    account_a = str(initial.get("gateway_account_a_id") or "").strip()
    account_b = str(initial.get("gateway_account_b_id") or "").strip()
    role_a = str(initial.get("gateway_role_a") or "").strip().lower()
    role_b = str(initial.get("gateway_role_b") or "").strip().lower()
    baseline = (account_a, account_b, role_a, role_b)
    validate_bindings(initial, accounts, baseline)
    original_mode = str(initial.get("gateway_mode") or "").strip().lower()
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup_dir = config_path.parent / "backups" / f"routing-smoke-{stamp}"
    backup_dir.mkdir(parents=True, mode=0o700, exist_ok=False)
    shutil.copy2(config_path, backup_dir / config_path.name)
    os.chmod(backup_dir / config_path.name, 0o600)
    try:
        for mode in ("force_a", "force_b", "auto"):
            patch_mode(management_key, mode)
            live = config_body(management_key)
            current_accounts = account_map(management_key)
            validate_bindings(live, current_accounts, baseline)
            if mode == "force_a":
                target, other = account_a, account_b
            elif mode == "force_b":
                target, other = account_b, account_a
            elif role_a == "primary" and account_usable(current_accounts[account_a]):
                target, other = account_a, account_b
            elif role_b == "primary" and account_usable(current_accounts[account_b]):
                target, other = account_b, account_a
            elif role_a == "backup" and account_usable(current_accounts[account_a]):
                target, other = account_a, account_b
            elif role_b == "backup" and account_usable(current_accounts[account_b]):
                target, other = account_b, account_a
            else:
                raise RuntimeError("auto mode has no usable account")
            status = response_probe(api_key)
            target_success, target_failed, other_success, other_failed = wait_for_change(
                management_key, target, other, current_accounts
            )
            if status == 200:
                if target_success < 1 or target_failed != 0 or other_success != 0 or other_failed != 0:
                    raise RuntimeError(f"{mode} selected an unexpected account")
                print(
                    f"mode={mode} responses_http=200 target_success_delta={target_success} "
                    f"other_success_delta={other_success}"
                )
            elif mode in {"force_a", "force_b"} and quota_exhausted(current_accounts[target]):
                if target_failed < 1 or other_success != 0 or other_failed != 0:
                    raise RuntimeError(f"{mode} quota failure spilled over to another account")
                print(
                    f"mode={mode} responses_http=429 expected_fail_hard=true "
                    f"target_failed_delta={target_failed} other_delta=0"
                )
            else:
                raise RuntimeError(f"{mode} unexpectedly returned HTTP {status}")
    finally:
        if original_mode in VALID_MODES:
            patch_mode(management_key, original_mode)
            validate_bindings(config_body(management_key), account_map(management_key), baseline)
            print(f"restored_mode={original_mode}")
    print(f"routing_smoke=ok backup={backup_dir / config_path.name}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
