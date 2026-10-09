#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Install runtime dependencies only; Go owns the two internal Node processes.
set -euo pipefail
task_source_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "${EUID}" -ne 0 ]]; then
  printf '%s\n' 'Run this dependency installer with sudo.' >&2
  exit 1
fi
apt-get update
apt-get install -y ca-certificates curl xz-utils ffmpeg
task_arch="$(uname -m)"
case "$task_arch" in
  x86_64) task_node_arch=x64 ;;
  aarch64|arm64) task_node_arch=arm64 ;;
  *) printf 'Unsupported Node architecture: %s\n' "$task_arch" >&2; exit 1 ;;
esac
if ! command -v node >/dev/null || [[ "$(node -p 'Number(process.versions.node.split(".")[0])')" -lt 24 ]]; then
  task_temp="$(mktemp -d /tmp/xiaoqi-node.XXXXXXXX)"
  trap 'rm -rf -- "$task_temp"' EXIT
  curl -fsS https://nodejs.org/dist/latest-v24.x/SHASUMS256.txt -o "$task_temp/SHASUMS256.txt"
  task_archive="$(awk -v arch="linux-${task_node_arch}.tar.xz" '$2 ~ arch"$" {print $2;exit}' "$task_temp/SHASUMS256.txt")"
  [[ "$task_archive" =~ ^node-v24\.[0-9]+\.[0-9]+-linux-(x64|arm64)\.tar\.xz$ ]]
  curl -fsS "https://nodejs.org/dist/latest-v24.x/${task_archive}" -o "$task_temp/$task_archive"
  (cd "$task_temp" && awk -v archive="$task_archive" '$2 == archive' SHASUMS256.txt | sha256sum -c -)
  install -d -m 0755 /opt/xiaoqi-node-v24
  tar -xJf "$task_temp/$task_archive" -C /opt/xiaoqi-node-v24 --strip-components=1
  ln -sfn /opt/xiaoqi-node-v24/bin/node /usr/local/bin/node
  ln -sfn /opt/xiaoqi-node-v24/bin/npm /usr/local/bin/npm
  ln -sfn /opt/xiaoqi-node-v24/bin/npx /usr/local/bin/npx
fi
if ! command -v google-chrome >/dev/null && ! command -v chromium >/dev/null; then
  if [[ "$task_node_arch" == x64 ]]; then
    task_chrome_package="$(mktemp /tmp/xiaoqi-chrome.XXXXXXXX.deb)"
    curl -fsS https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb -o "$task_chrome_package"
    apt-get install -y "$task_chrome_package"
    rm -f -- "$task_chrome_package"
  else
    apt-get install -y chromium || apt-get install -y chromium-browser
  fi
fi
cd "$task_source_root"
PUPPETEER_SKIP_DOWNLOAD=true npm ci --omit=dev --no-audit --no-fund
node --test adapter/adapter.test.mjs
printf '\n%s\n' 'Runtime dependencies installed. Keep Node services under the Go supervisor.'
printf 'SUXIN_NODE=%s\nSUXIN_FFMPEG=%s\n' "$(command -v node)" "$(command -v ffmpeg)"
printf '%s\n' 'Use a dedicated service user, independent data directories, and KillMode=control-group.'
