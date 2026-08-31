#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
compose_file="${repo_root}/deploy/compose.yaml"
caddy_file="${repo_root}/deploy/Caddyfile"
run_dir="$(mktemp -d "${TMPDIR:-/tmp}/radishnexus-compose.XXXXXX")"
run_id="$(basename -- "${run_dir}" | tr -cd '[:alnum:]' | tr '[:upper:]' '[:lower:]')"
project_name="radishnexus-check-${run_id}"
secret_path="${run_dir}/postgres_password"
ca_path="${run_dir}/caddy-root.crt"
cookie_jar="${run_dir}/cookies.txt"
login_headers="${run_dir}/login.headers"
login_body="${run_dir}/login.json"
session_body="${run_dir}/session.json"

https_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
proxy_octet="$((https_port % 200 + 20))"

export RADISHNEXUS_PUBLIC_ORIGIN="https://localhost:${https_port}"
export RADISHNEXUS_HTTPS_PORT="${https_port}"
export RADISHNEXUS_POSTGRES_PASSWORD_FILE="${secret_path}"
export RADISHNEXUS_PROXY_SUBNET="172.28.${proxy_octet}.0/24"
export RADISHNEXUS_PROXY_IPV4="172.28.${proxy_octet}.10"
export RADISHNEXUS_TRUSTED_PROXY_CIDR="172.28.${proxy_octet}.10/32"
export RADISHNEXUS_OPERATION_UID="$(id -u)"
export RADISHNEXUS_OPERATION_GID="$(id -g)"

compose=(docker compose --project-name "${project_name}" --file "${compose_file}")
finished=false

cleanup() {
  exit_code=$?
  if [[ "${exit_code}" -ne 0 ]]; then
    "${compose[@]}" ps >&2 || true
    "${compose[@]}" logs --no-color postgres app caddy >&2 || true
  fi
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "${run_dir}"
  if [[ "${finished}" == true ]]; then
    echo "Self-hosted Compose check passed."
  fi
  exit "${exit_code}"
}
trap cleanup EXIT

required_images=(
  "caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648"
  "postgres:17.10-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
  "golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57"
  "node:24.16.0-alpine3.23@sha256:2bdb65ed1dab192432bc31c95f94155ca5ad7fc1392fb7eb7526ab682fa5bf14"
  "alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40"
)

for image in "${required_images[@]}"; do
  if ! docker image inspect "${image}" >/dev/null 2>&1; then
    echo "Required fixed image is missing; pull it explicitly before this offline check: ${image}" >&2
    exit 1
  fi
done

openssl rand -base64 -out "${secret_path}" 32
chmod 0600 "${secret_path}"

"${compose[@]}" config --quiet
docker run --rm \
  --env "RADISHNEXUS_PUBLIC_ORIGIN=${RADISHNEXUS_PUBLIC_ORIGIN}" \
  --volume "${caddy_file}:/etc/caddy/Caddyfile:ro" \
  "${required_images[0]}" \
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null

"${compose[@]}" build --pull=false app migrate
"${compose[@]}" up -d --wait postgres
"${compose[@]}" run --rm migrate

bootstrap_password="compose-browser-fixture-password-2026"
printf '%s\n' "${bootstrap_password}" | "${compose[@]}" run --rm -T bootstrap \
  --login admin \
  --display-name "Compose Admin" \
  --workspace-name "Compose Workspace" \
  --password-stdin

if printf '%s\n' "${bootstrap_password}" | "${compose[@]}" run --rm -T bootstrap \
  --login second \
  --display-name "Second Admin" \
  --workspace-name "Second Workspace" \
  --password-stdin >"${run_dir}/second-bootstrap.log" 2>&1; then
  echo "Second bootstrap unexpectedly succeeded." >&2
  exit 1
fi

"${compose[@]}" up -d --wait app caddy
"${compose[@]}" cp caddy:/data/caddy/pki/authorities/local/root.crt "${ca_path}" >/dev/null

curl --fail --silent --show-error \
  --cacert "${ca_path}" \
  --output /dev/null \
  "${RADISHNEXUS_PUBLIC_ORIGIN}/health/ready"

login_status="$({
  printf '{"login_name":"admin","password":"%s"}' "${bootstrap_password}"
} | curl --silent --show-error \
  --cacert "${ca_path}" \
  --cookie-jar "${cookie_jar}" \
  --dump-header "${login_headers}" \
  --header 'Content-Type: application/json' \
  --header "Origin: ${RADISHNEXUS_PUBLIC_ORIGIN}" \
  --header 'X-Forwarded-For: not-an-ip' \
  --header 'X-Forwarded-Host: attacker.example.test' \
  --header 'X-Forwarded-Proto: http' \
  --data-binary @- \
  --output "${login_body}" \
  --write-out '%{http_code}' \
  "${RADISHNEXUS_PUBLIC_ORIGIN}/api/v1/auth/sessions")"
if [[ "${login_status}" != 201 ]]; then
  echo "Login status = ${login_status}, want 201." >&2
  exit 1
fi
session_cookie_header="$(grep -Ei '^set-cookie: __Host-radishnexus-session=' "${login_headers}" || true)"
if [[ "${session_cookie_header}" != *"Secure"* ||
  "${session_cookie_header}" != *"HttpOnly"* ||
  "${session_cookie_header}" != *"SameSite=Strict"* ]]; then
  echo "Session cookie flags are incomplete." >&2
  exit 1
fi

session_status="$(curl --silent --show-error \
  --cacert "${ca_path}" \
  --cookie "${cookie_jar}" \
  --output "${session_body}" \
  --write-out '%{http_code}' \
  "${RADISHNEXUS_PUBLIC_ORIGIN}/api/v1/auth/session")"
if [[ "${session_status}" != 200 ]]; then
  echo "Session status = ${session_status}, want 200." >&2
  exit 1
fi

csrf_token="$(awk '$6 == "__Host-radishnexus-csrf" { print $7 }' "${cookie_jar}")"
if [[ -z "${csrf_token}" ]]; then
  echo "CSRF cookie was not stored." >&2
  exit 1
fi
logout_status="$(curl --silent --show-error \
  --cacert "${ca_path}" \
  --cookie "${cookie_jar}" \
  --request DELETE \
  --header "Origin: ${RADISHNEXUS_PUBLIC_ORIGIN}" \
  --header "X-CSRF-Token: ${csrf_token}" \
  --output /dev/null \
  --write-out '%{http_code}' \
  "${RADISHNEXUS_PUBLIC_ORIGIN}/api/v1/auth/session")"
if [[ "${logout_status}" != 204 ]]; then
  echo "Logout status = ${logout_status}, want 204." >&2
  exit 1
fi

post_logout_status="$(curl --silent --show-error \
  --cacert "${ca_path}" \
  --cookie "${cookie_jar}" \
  --output /dev/null \
  --write-out '%{http_code}' \
  "${RADISHNEXUS_PUBLIC_ORIGIN}/api/v1/auth/session")"
if [[ "${post_logout_status}" != 401 ]]; then
  echo "Post-logout Session status = ${post_logout_status}, want 401." >&2
  exit 1
fi

app_container="$("${compose[@]}" ps --quiet app)"
postgres_container="$("${compose[@]}" ps --quiet postgres)"
app_ports="$(docker port "${app_container}")"
postgres_ports="$(docker port "${postgres_container}")"
if [[ -n "${app_ports}" || -n "${postgres_ports}" ]]; then
  echo "Go server or PostgreSQL unexpectedly published a host port." >&2
  exit 1
fi

finished=true
