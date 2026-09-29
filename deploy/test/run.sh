#!/usr/bin/env bash
# Runs the setup-vds.sh test suites in throw-away Ubuntu 24.04 containers.
# Suites 02 and 03 reach github.com (anonymous clone, deploy-key rejection).
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
status=0
for suite in "$here"/[0-9]*.sh; do
  name=$(basename "$suite")
  out=$(docker run --rm -v "$here/..:/s:ro" ubuntu:24.04 bash -c \
    "apt-get update -qq >/dev/null 2>&1; apt-get install -y -qq tzdata sudo git openssh-client curl ca-certificates >/dev/null 2>&1; bash /s/test/$name" 2>&1) && rc=0 || rc=$?
  printf '%s\n' "$out" | grep -v "requested image's platform" | sed "s/^/$name: /"
  (( rc == 0 )) || status=1
done
exit "$status"
