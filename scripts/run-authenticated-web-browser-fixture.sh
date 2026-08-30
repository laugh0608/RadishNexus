#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
server_dir="${repo_root}/server"
web_dir="${repo_root}/web"
postgres_image="${RADISHNEXUS_POSTGRES_TEST_IMAGE:-postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193}"
container_name="radishnexus-authenticated-web-${RANDOM}-$$"
database_name="radishnexus_authenticated_web"
database_user="radishnexus_test"
database_password="radishnexus-test-only"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/radishnexus-authenticated-web.XXXXXX")"
state_path="${fixture_dir}/state.json"
stop_path="${fixture_dir}/stop"
go_cache="${TMPDIR:-/tmp}/radishnexus-authenticated-web-go-cache"
fixture_pid=""

cleanup() {
    if [[ -n "${fixture_pid}" ]] && kill -0 "${fixture_pid}" >/dev/null 2>&1; then
        touch -- "${stop_path}"
        wait "${fixture_pid}" >/dev/null 2>&1 || true
    fi
    if docker container inspect "${container_name}" >/dev/null 2>&1; then
        docker stop --time 5 "${container_name}" >/dev/null
    fi
    rm -f -- "${state_path}" "${stop_path}"
    rmdir -- "${fixture_dir}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

if [[ ! -d "${web_dir}/node_modules" ]]; then
    echo "Missing web/node_modules. Install the pinned Web dependencies explicitly first." >&2
    exit 1
fi
if ! docker image inspect "${postgres_image}" >/dev/null 2>&1; then
    echo "Missing ${postgres_image}. Pull it explicitly before running this fixture." >&2
    echo "docker pull ${postgres_image}" >&2
    exit 1
fi

(
    cd -- "${web_dir}"
    npm run build
)

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
consecutive_connections=0
for _ in $(seq 1 30); do
    if docker exec \
        --env "PGPASSWORD=${database_password}" \
        "${container_name}" \
        psql \
        --host 127.0.0.1 \
        --username "${database_user}" \
        --dbname "${database_name}" \
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
    echo "PostgreSQL did not become ready within 30 seconds." >&2
    docker logs "${container_name}" >&2
    exit 1
fi

published_address="$(docker port "${container_name}" 5432/tcp)"
published_port="${published_address##*:}"
database_url="postgres://${database_user}:${database_password}@127.0.0.1:${published_port}/${database_name}?sslmode=disable"

(
    cd -- "${server_dir}"
    env \
        GOCACHE="${go_cache}" \
        DATABASE_URL="${database_url}" \
        RADISHNEXUS_WEB_ROOT="${web_dir}/dist" \
        RADISHNEXUS_BROWSER_FIXTURE_STATE="${state_path}" \
        RADISHNEXUS_BROWSER_FIXTURE_STOP="${stop_path}" \
        go test \
            -count=1 \
            -v \
            -tags='integration browserfixture' \
            -run '^TestAuthenticatedWebBrowserFixture$' \
            ./internal/goldenpath/postgres
) &
fixture_pid=$!

state_ready=false
for _ in $(seq 1 300); do
    if [[ -s "${state_path}" ]]; then
        state_ready=true
        break
    fi
    if ! kill -0 "${fixture_pid}" >/dev/null 2>&1; then
        wait "${fixture_pid}"
        exit 1
    fi
    sleep 0.1
done

if [[ "${state_ready}" != "true" ]]; then
    echo "Authenticated Web browser fixture did not publish state within 30 seconds." >&2
    exit 1
fi

echo "Authenticated Web browser fixture is ready."
echo "State: ${state_path}"
echo "Stop: ${stop_path}"
sed -n '1p' "${state_path}"

wait "${fixture_pid}"
fixture_pid=""
