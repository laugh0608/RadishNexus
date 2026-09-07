#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
server_dir="${repo_root}/server"
web_dir="${repo_root}/web"
postgres_image="${RADISHNEXUS_POSTGRES_TEST_IMAGE:-postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193}"
caddy_image="caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648"
container_name="radishnexus-authenticated-web-${RANDOM}-$$"
caddy_container_name="radishnexus-authenticated-web-caddy-${RANDOM}-$$"
caddy_data_volume="${caddy_container_name}-data"
caddy_config_volume="${caddy_container_name}-config"
database_name="radishnexus_authenticated_web"
database_user="radishnexus_test"
database_password="radishnexus-test-only"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/radishnexus-authenticated-web.XXXXXX")"
state_path="${fixture_dir}/state.json"
stop_path="${fixture_dir}/stop"
ca_path="${fixture_dir}/caddy-root.crt"
go_cache="${TMPDIR:-/tmp}/radishnexus-authenticated-web-go-cache"
fixture_pid=""
https_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
backend_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
public_origin="https://localhost:${https_port}"

cleanup() {
    if [[ -n "${fixture_pid}" ]] && kill -0 "${fixture_pid}" >/dev/null 2>&1; then
        touch -- "${stop_path}"
        wait "${fixture_pid}" >/dev/null 2>&1 || true
    fi
    if docker container inspect "${container_name}" >/dev/null 2>&1; then
        docker stop --time 5 "${container_name}" >/dev/null
    fi
    if docker container inspect "${caddy_container_name}" >/dev/null 2>&1; then
        docker stop --time 5 "${caddy_container_name}" >/dev/null
    fi
    docker volume rm "${caddy_data_volume}" "${caddy_config_volume}" >/dev/null 2>&1 || true
    rm -f -- "${state_path}" "${stop_path}" "${ca_path}"
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
if ! docker image inspect "${caddy_image}" >/dev/null 2>&1; then
    echo "Missing ${caddy_image}. Pull it explicitly before running this fixture." >&2
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
        RADISHNEXUS_BROWSER_FIXTURE_LISTEN_ADDRESS="0.0.0.0:${backend_port}" \
        RADISHNEXUS_BROWSER_FIXTURE_PUBLIC_ORIGIN="${public_origin}" \
        RADISHNEXUS_BROWSER_FIXTURE_DATABASE_CONTAINER="${container_name}" \
        go test \
            -count=1 \
            -v \
            -timeout=35m \
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

docker volume create "${caddy_data_volume}" >/dev/null
docker volume create "${caddy_config_volume}" >/dev/null
docker run \
    --detach \
    --rm \
    --name "${caddy_container_name}" \
    --add-host host.docker.internal:host-gateway \
    --publish "127.0.0.1:${https_port}:${https_port}" \
    --volume "${caddy_data_volume}:/data" \
    --volume "${caddy_config_volume}:/config" \
    "${caddy_image}" \
    caddy reverse-proxy \
        --from "${public_origin}" \
        --to "http://host.docker.internal:${backend_port}" \
        --internal-certs \
        --disable-redirects \
        --access-log >/dev/null

ca_ready=false
for _ in $(seq 1 100); do
    if docker cp \
        "${caddy_container_name}:/data/caddy/pki/authorities/local/root.crt" \
        "${ca_path}" >/dev/null 2>&1; then
        ca_ready=true
        break
    fi
    if ! docker container inspect "${caddy_container_name}" >/dev/null 2>&1; then
        echo "Caddy browser fixture exited before publishing its root certificate." >&2
        exit 1
    fi
    sleep 0.1
done

if [[ "${ca_ready}" != "true" ]]; then
    echo "Caddy browser fixture did not publish its root certificate." >&2
    docker logs "${caddy_container_name}" >&2
    exit 1
fi

https_ready=false
for _ in $(seq 1 100); do
    if curl --fail --silent \
        --cacert "${ca_path}" \
        --output /dev/null \
        "${public_origin}/"; then
        https_ready=true
        break
    fi
    sleep 0.1
done

if [[ "${https_ready}" != "true" ]]; then
    echo "Caddy HTTPS browser fixture did not become ready." >&2
    docker logs "${caddy_container_name}" >&2
    exit 1
fi

echo "Authenticated Web browser fixture is ready."
echo "State: ${state_path}"
echo "Stop: ${stop_path}"
echo "Caddy CA: ${ca_path}"
sed -n '1p' "${state_path}"

wait "${fixture_pid}"
fixture_pid=""
