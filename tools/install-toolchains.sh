#!/usr/bin/env bash
set -eu

base=/mnt/d/CodexWorkspace/codex-personal-gateway/.toolchains
downloads="$base/downloads"
go_archive=go1.26.7.linux-amd64.tar.gz
node_archive=node-v24.17.0-linux-x64.tar.xz

if [ -e "$base/go-1.26.7" ] || [ -e "$base/node-v24.17.0-linux-x64" ]; then
  echo "ERROR: fixed toolchain target already exists" >&2
  exit 1
fi

mkdir -p "$downloads"
cd "$downloads"

curl -fL --retry 1 --connect-timeout 15 --max-time 600 -sS \
  -o "$go_archive" "https://go.dev/dl/$go_archive"
printf '%s  %s\n' \
  ffb5f8de10c62550dfddab66b36b57030721e0a44a3218e9e1181d7b59f121ca \
  "$go_archive" | sha256sum -c -

curl -fL --retry 1 --connect-timeout 15 --max-time 600 -sS \
  -o "$node_archive" "https://nodejs.org/dist/v24.17.0/$node_archive"
curl -fL --retry 1 --connect-timeout 15 --max-time 120 -sS \
  -o node-v24.17.0-SHASUMS256.txt \
  https://nodejs.org/dist/v24.17.0/SHASUMS256.txt
grep " $node_archive$" node-v24.17.0-SHASUMS256.txt | sha256sum -c -

cd "$base"
tar -xzf "downloads/$go_archive"
mv go go-1.26.7
tar -xJf "downloads/$node_archive"

./go-1.26.7/bin/go version
./node-v24.17.0-linux-x64/bin/node --version
./node-v24.17.0-linux-x64/bin/npm --version
du -sh go-1.26.7 node-v24.17.0-linux-x64 downloads
