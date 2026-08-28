#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
experiment_dir="${repo_root}/experiments/m0-core-contracts"
radish_go_cache="${TMPDIR:-/tmp}/radishnexus-m0-go-cache"

cd -- "${experiment_dir}"
env GOCACHE="${radish_go_cache}" go test ./...
env GOCACHE="${radish_go_cache}" go vet ./...
