#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/radishnexus-membership.XXXXXX")"
cleanup() { rm -rf -- "${fixture_dir}"; }
trap cleanup EXIT INT TERM
export GOPROXY=off GOTOOLCHAIN=local
if [[ "$#" -eq 1 && "$1" == '--browser-review' ]]; then
    export RADISHNEXUS_MEMBERSHIP_BROWSER_STATE="${fixture_dir}/browser.json"
    (cd -- "${repo_root}/web"; npm run build)
    # The running handler caches index.html. Keep its matching assets immutable
    # while an independent build or review changes the workspace's web/dist.
    cp -R -- "${repo_root}/web/dist" "${fixture_dir}/web"
    export RADISHNEXUS_WEB_ROOT="${fixture_dir}/web"
    echo "Browser state will be written to ${RADISHNEXUS_MEMBERSHIP_BROWSER_STATE}"
    export GOFLAGS='-run=^TestMembershipHTTPSBrowser$ -v -timeout=50m -count=1'
elif [[ "$#" -eq 0 ]]; then
    export GOFLAGS='-run=^TestMembership -v -count=1'
else
    echo 'usage: check-server-membership.sh [--browser-review]' >&2
    exit 2
fi
"${script_dir}/run-postgres-go-integration.sh" \
    "${repo_root}/server" "membership" "radishnexus_membership_test" \
    "radishnexus-server-go-cache"
