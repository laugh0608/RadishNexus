#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
server_dir="${repo_root}/server"
postgres_image="${RADISHNEXUS_SERVER_POSTGRES_IMAGE:-${RADISHNEXUS_POSTGRES_TEST_IMAGE:-postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193}}"
run_dir="$(mktemp -d "${TMPDIR:-/tmp}/radishnexus-backup-restore.XXXXXX")"
run_id="${RANDOM}-$$"
network_name="radishnexus-backup-restore-${run_id}"
source_container="radishnexus-backup-source-${run_id}"
target_container="radishnexus-backup-target-${run_id}"
database_user="radishnexus_test"
database_password="radishnexus-test-only"
source_database="radishnexus_backup_source"
target_database="radishnexus_backup_target"

cleanup() {
    for container in "${source_container}" "${target_container}"; do
        if docker container inspect "${container}" >/dev/null 2>&1; then
            docker stop --time 5 "${container}" >/dev/null
        fi
    done
    if docker network inspect "${network_name}" >/dev/null 2>&1; then
        docker network rm "${network_name}" >/dev/null
    fi
    rm -rf -- "${run_dir}"
}
trap cleanup EXIT INT TERM

if ! docker image inspect "${postgres_image}" >/dev/null 2>&1; then
    echo "Missing ${postgres_image}. Pull it explicitly before running this test." >&2
    echo "docker pull ${postgres_image}" >&2
    exit 1
fi

image_architecture="$(docker image inspect "${postgres_image}" --format '{{.Architecture}}')"
case "${image_architecture}" in
    amd64 | arm64)
        ;;
    *)
        echo "Unsupported PostgreSQL image architecture: ${image_architecture}" >&2
        exit 1
        ;;
esac

cd -- "${server_dir}"
env \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH="${image_architecture}" \
    GOCACHE="${run_dir}/go-cache" \
    go test -c -tags=integration -o "${run_dir}/backuprestore.test" ./internal/backuprestore

docker network create "${network_name}" >/dev/null

docker run \
    --rm \
    --detach \
    --name "${source_container}" \
    --network "${network_name}" \
    --env "POSTGRES_DB=${source_database}" \
    --env "POSTGRES_USER=${database_user}" \
    --env "POSTGRES_PASSWORD=${database_password}" \
    --volume "${run_dir}:/work" \
    "${postgres_image}" >/dev/null

docker run \
    --rm \
    --detach \
    --name "${target_container}" \
    --network "${network_name}" \
    --env "POSTGRES_DB=${target_database}" \
    --env "POSTGRES_USER=${database_user}" \
    --env "POSTGRES_PASSWORD=${database_password}" \
    "${postgres_image}" >/dev/null

for container_database in \
    "${source_container}:${source_database}" \
    "${target_container}:${target_database}"; do
    container="${container_database%%:*}"
    database="${container_database##*:}"
    ready=false
    consecutive_connections=0
    for _ in $(seq 1 30); do
        if docker exec \
            --env "PGPASSWORD=${database_password}" \
            "${container}" \
            psql \
            --host 127.0.0.1 \
            --username "${database_user}" \
            --dbname "${database}" \
            --no-psqlrc \
            --set ON_ERROR_STOP=on \
            --command 'SELECT 1' >/dev/null 2>&1; then
            consecutive_connections=$((consecutive_connections + 1))
            if [[ "${consecutive_connections}" -ge 2 ]]; then
                ready=true
                break
            fi
        else
            consecutive_connections=0
        fi
        sleep 1
    done
    if [[ "${ready}" != "true" ]]; then
        echo "PostgreSQL container ${container} did not become ready within 30 seconds." >&2
        docker logs "${container}" >&2
        exit 1
    fi
done

source_database_url="postgres://${database_user}:${database_password}@127.0.0.1:5432/${source_database}?sslmode=disable"
target_database_url="postgres://${database_user}:${database_password}@${target_container}:5432/${target_database}?sslmode=disable"

docker exec \
    --env "SOURCE_DATABASE_URL=${source_database_url}" \
    --env "TARGET_DATABASE_URL=${target_database_url}" \
    "${source_container}" \
    /work/backuprestore.test \
    -test.v \
    -test.run '^TestBackupRestoreGoldenPath$'
