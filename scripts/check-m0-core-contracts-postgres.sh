#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
experiment_dir="${repo_root}/experiments/m0-core-contracts"
export RADISHNEXUS_POSTGRES_TEST_IMAGE="${RADISHNEXUS_M0_POSTGRES_IMAGE:-${RADISHNEXUS_POSTGRES_TEST_IMAGE:-postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193}}"
exec "${script_dir}/run-postgres-go-integration.sh" \
    "${experiment_dir}" \
    "m0-core" \
    "radishnexus_m0_test" \
    "radishnexus-m0-go-cache"
