#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
server_dir="${repo_root}/server"
radish_go_cache="${TMPDIR:-/tmp}/radishnexus-server-go-cache"

cd -- "${server_dir}"
env GOCACHE="${radish_go_cache}" go test ./...
env GOCACHE="${radish_go_cache}" go vet ./...
env GOCACHE="${radish_go_cache}" go mod tidy -diff
env GOCACHE="${radish_go_cache}" go mod verify
