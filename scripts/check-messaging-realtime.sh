#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
experiment_dir="${repo_root}/experiments/messaging-realtime"
radish_realtime_go_cache="${TMPDIR:-/tmp}/radishnexus-messaging-realtime-go-cache"

cd -- "${experiment_dir}"
env GOCACHE="${radish_realtime_go_cache}" go test -race ./...
env GOCACHE="${radish_realtime_go_cache}" go vet ./...
go mod tidy -diff
