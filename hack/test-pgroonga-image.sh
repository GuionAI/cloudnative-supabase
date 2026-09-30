#!/usr/bin/env bash
set -euo pipefail

IMAGE="${PGROONGA_IMAGE:-cnsupa-postgres-pgroonga:chatgpt-mcp-pg}"
CONTAINER_TOOL="${CONTAINER_TOOL:-podman}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"${CONTAINER_TOOL}" run --rm --network none --volume "${repo_root}/hack/pgroonga-image-fixture.sh:/fixture.sh:ro" --entrypoint bash "${IMAGE}" /fixture.sh
