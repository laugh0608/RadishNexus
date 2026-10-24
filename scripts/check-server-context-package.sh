#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/radishnexus-context.XXXXXX")"
cleanup() { rm -rf -- "${fixture_dir}"; }
trap cleanup EXIT INT TERM

export GOPROXY=off GOTOOLCHAIN=local
export GOCACHE="${TMPDIR:-/tmp}/radishnexus-server-go-cache"
export RADISHNEXUS_CONTEXT_CLI="${fixture_dir}/nexus-context"
(
    cd -- "${repo_root}/server"
    GOFLAGS= go build -o "${RADISHNEXUS_CONTEXT_CLI}" ./cmd/nexus-context
)

if [[ "$#" -eq 1 && "$1" == '--browser-review' ]]; then
    export RADISHNEXUS_WEB_ROOT="${repo_root}/web/dist"
    export RADISHNEXUS_CONTEXT_BROWSER_STATE="${fixture_dir}/browser.json"
    (
        cd -- "${repo_root}/web"
        npm run build
    )
    echo "Browser state will be written to ${RADISHNEXUS_CONTEXT_BROWSER_STATE}"
    export GOFLAGS='-run=^TestContextCLIHTTPSAndBrowser$ -v -timeout=30m -count=1'
elif [[ "$#" -eq 0 ]]; then
    export GOFLAGS='-run=^TestContext -v -count=1'
else
    echo 'usage: check-server-context-package.sh [--browser-review]' >&2
    exit 2
fi

# Uses installed Go/cached modules and an already installed image. The runner
# removes its uniquely named container; this wrapper removes its own artifacts.
"${script_dir}/run-postgres-go-integration.sh" \
    "${repo_root}/server" "context-package" "radishnexus_context_test" \
    "radishnexus-server-go-cache"
