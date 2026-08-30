#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web_root="${repo_root}/web"

if [[ ! -d "${web_root}/node_modules" ]]; then
  echo "web/node_modules 不存在；请先在 web/ 运行 npm ci。" >&2
  exit 1
fi

cd "${web_root}"
npm run check
