#!/usr/bin/env bash
set -euo pipefail

IMAGE="${PGROONGA_IMAGE:-cnsupa-postgres-pgroonga:chatgpt-mcp-pg}"
CONTAINER_TOOL="${CONTAINER_TOOL:-podman}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for fixture in pgroonga-image-fixture.sh pgroonga-streaming-fixture.sh; do
  "${CONTAINER_TOOL}" run --rm --network none --volume "${repo_root}/hack/${fixture}:/fixture.sh:ro" \
    --entrypoint timeout "${IMAGE}" 180 bash /fixture.sh
done
