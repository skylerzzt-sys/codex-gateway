#!/usr/bin/env bash
set -eu

stage_dir=/opt/codex-personal-stage
service_name=codex-personal-stage.service

if ss -H -lnt 'sport = :18317' | grep -q .; then
  echo 'ERROR: loopback port 18317 is already occupied' >&2
  exit 1
fi
if [ -e "/etc/systemd/system/$service_name" ]; then
  echo "ERROR: $service_name already exists" >&2
  exit 1
fi

systemd-analyze verify "$stage_dir/$service_name"
install -m 0644 "$stage_dir/$service_name" "/etc/systemd/system/$service_name"
systemctl daemon-reload
systemctl enable --now "$service_name"

if ! systemctl is-active --quiet "$service_name"; then
  systemctl status "$service_name" --no-pager || true
  journalctl -u "$service_name" -n 80 --no-pager || true
  exit 1
fi

systemctl is-enabled "$service_name"
systemctl is-active "$service_name"
ss -lntp 'sport = :18317'
systemctl show "$service_name" \
  -p ActiveState -p SubState -p NRestarts \
  -p MemoryCurrent -p MemoryPeak -p CPUUsageNSec --no-pager
free -m
