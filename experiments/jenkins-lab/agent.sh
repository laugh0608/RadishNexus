#!/bin/sh
set -eu
# The host transfers this one test-only credential through stdin after bootstrap.
i=0
while [ ! -s /tmp/nexus-agent-secret ]; do
    i=$((i + 1))
    if [ "$i" -gt 180 ]; then
        echo 'lab agent credential timed out' >&2
        exit 1
    fi
    sleep 1
done
exec java -Xmx384m -cp /usr/share/jenkins/agent.jar /lab/Agent.java
