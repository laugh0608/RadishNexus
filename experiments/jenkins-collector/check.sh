#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
extra=(--network none)
if [[ $# -eq 2 && $1 == --export && -d $2 && $2 == /* ]]; then
  extra+=(--mount "type=bind,source=$2,target=/export" --env COLLECTOR_CONTRACT_EXPORT=/export)
elif [[ $# -ne 0 ]]; then
  echo 'usage: check.sh [--export /absolute/empty/test-directory]' >&2
  exit 2
fi
# Cached image only, no Jenkins server, no network or persistent volume.
exec docker run --rm --read-only --tmpfs /tmp:rw,nosuid,nodev,size=256m,uid=1000,gid=1000,mode=0700 \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --mount "type=bind,source=${root},target=/src,readonly" \
  "${extra[@]}" \
  --entrypoint sh \
  jenkins/jenkins:2.568.3-jdk21@sha256:c1e4c349365f6d16d88595b2c5f7e8ff39b8ae1d061f62420bac193b4b9616d0 \
  -c 'cd /tmp && jar xf /usr/share/jenkins/jenkins.war WEB-INF/lib executable/winstone.jar && mkdir classes && java -cp "WEB-INF/lib/*:executable/winstone.jar" org.codehaus.groovy.tools.FileSystemCompiler -d classes /src/StrictJson.groovy /src/Collector.groovy /src/JenkinsCollector.groovy /src/start.groovy /src/stop.groovy /src/offline.groovy && java -cp "classes:WEB-INF/lib/*:executable/winstone.jar" groovy.ui.GroovyMain /src/CollectorTest.groovy'
