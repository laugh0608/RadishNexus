#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
experiment_dir="${repo_root}/experiments/m0-core-contracts"
postgres_image="${RADISHNEXUS_M0_POSTGRES_IMAGE:-postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193}"
container_name="radishnexus-m0-core-${RANDOM}-$$"
database_name="radishnexus_m0_test"
database_user="radishnexus_test"
database_password="radishnexus-test-only"
radish_go_cache="${TMPDIR:-/tmp}/radishnexus-m0-go-cache"

cleanup() {
    if docker container inspect "${container_name}" >/dev/null 2>&1; then
        docker stop --time 5 "${container_name}" >/dev/null
    fi
}
trap cleanup EXIT INT TERM

if ! docker image inspect "${postgres_image}" >/dev/null 2>&1; then
    echo "Missing ${postgres_image}. Pull it explicitly before running this test." >&2
    echo "docker pull ${postgres_image}" >&2
    exit 1
fi

docker run \
    --rm \
    --detach \
    --name "${container_name}" \
    --env "POSTGRES_DB=${database_name}" \
    --env "POSTGRES_USER=${database_user}" \
    --env "POSTGRES_PASSWORD=${database_password}" \
    --publish 127.0.0.1::5432 \
    "${postgres_image}" >/dev/null

ready=false
for _ in $(seq 1 30); do
    if docker exec "${container_name}" pg_isready \
        --username "${database_user}" \
        --dbname "${database_name}" >/dev/null 2>&1; then
        ready=true
        break
    fi
    sleep 1
done

if [[ "${ready}" != "true" ]]; then
    echo "PostgreSQL did not become ready within 30 seconds." >&2
    docker logs "${container_name}" >&2
    exit 1
fi

published_address="$(docker port "${container_name}" 5432/tcp)"
published_port="${published_address##*:}"
database_url="postgres://${database_user}:${database_password}@127.0.0.1:${published_port}/${database_name}?sslmode=disable"

cd -- "${experiment_dir}"
env \
    GOCACHE="${radish_go_cache}" \
    DATABASE_URL="${database_url}" \
    go test -tags=integration ./...
