#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

for workflow in .github/workflows/ci.yaml .github/workflows/release.yaml; do
  grep -Fq 'platforms: linux/amd64' "${workflow}"
done
if grep -R -q 'linux/arm64' .github/workflows Makefile; then
  echo 'delivery must not build unused arm64 images' >&2
  exit 1
fi
if grep -Fq 'type=raw,value=latest' .github/workflows/release.yaml; then
  echo 'release workflow must not overwrite the main branch latest tag' >&2
  exit 1
fi
if grep -Fq -- '--version 0.1.8' README.md; then
  echo 'README must not point at an unpublished OCI chart version' >&2
  exit 1
fi
grep -Fq 'TANKA_IMAGE=' tanka/README.md
grep -Fq "helm pull \"\${CHART_REGISTRY}/cloudnative-supabase\"" .github/workflows/release.yaml

if TANKA_DANGEROUS_ALLOW_REDIRECT=true make tanka-show TANKA_IMAGE=latest >/dev/null 2>&1; then
  echo 'Tanka targets must reject mutable image tags' >&2
  exit 1
fi
