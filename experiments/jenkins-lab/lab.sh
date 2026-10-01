#!/usr/bin/env bash
set -euo pipefail
lab_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
compose=(docker compose --project-name radishnexus-jenkins-lab --file "${lab_dir}/compose.yaml")
case "${1:-}" in
    config)
        "${compose[@]}" config --quiet
        ;;
    pull)
        # Requires explicit image-download authorization. Never executed by config.
        docker pull jenkins/jenkins:2.568.3-jdk21@sha256:c1e4c349365f6d16d88595b2c5f7e8ff39b8ae1d061f62420bac193b4b9616d0
        docker pull jenkins/inbound-agent@sha256:6a31d728c22ad74adbb6b9a1a210eb9ec2599f644b95edd7f8a3b8a71d20772c
        ;;
    start)
        # Starts two containers and three bounded test builds; see README approval scope.
        if [[ -e "${lab_dir}/data/jenkins_home/nexus-lab" ]]; then
            echo 'lab data already exists; preserve it and authorize a fresh run separately' >&2
            exit 1
        fi
        if [[ -n "$("${compose[@]}" ps --all --quiet)" ]]; then
            echo 'lab containers already exist; preserve and inspect them before reuse' >&2
            exit 1
        fi
        umask 077
        mkdir -p "${lab_dir}/data/jenkins_home"
        "${compose[@]}" up --detach --pull never
        ready=false
        for _ in $(seq 1 120); do
            if "${compose[@]}" exec -T controller test -f /var/jenkins_home/nexus-lab/ready; then
                ready=true
                break
            fi
            sleep 1
        done
        if [[ "${ready}" != true ]]; then
            echo 'controller initialization failed or timed out; preserve state for diagnosis' >&2
            exit 1
        fi
        # Transfer directly between processes; no secret value enters argv or output.
        "${compose[@]}" exec -T controller cat /var/jenkins_home/nexus-lab/agent-secret |
            "${compose[@]}" exec -T agent sh -c 'umask 077; cat > /tmp/nexus-agent-secret.pending; mv /tmp/nexus-agent-secret.pending /tmp/nexus-agent-secret'
        echo 'lab started; use collect after the three probes finish'
        ;;
    collect)
        "${compose[@]}" exec -T controller test -f /var/jenkins_home/nexus-lab/complete
        destination="${lab_dir}/data/snapshots"
        umask 077
        mkdir -p "${destination}"
        for number in 1 2 3; do
            if [[ -e "${destination}/build-${number}.json" ]]; then
                echo 'snapshot destination already exists; preserve it before another run' >&2
                exit 1
            fi
        done
        for number in 1 2 3; do
            "${compose[@]}" cp "controller:/var/jenkins_home/nexus-lab/build-${number}.json" "${destination}/build-${number}.json"
        done
        echo 'three finalized snapshots collected under data/snapshots beside compose.yaml'
        ;;
    stop)
        # Stops and removes this project's containers/network; relative data directory remains.
        "${compose[@]}" down
        ;;
    *)
        echo 'usage: bash lab.sh config|pull|start|collect|stop' >&2
        exit 2
        ;;
esac
