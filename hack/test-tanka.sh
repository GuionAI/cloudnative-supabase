#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
rendered="$(mktemp)"
trap 'rm -f "${rendered}"' EXIT

cd "${repo_root}"

assert_resource_contains() {
  local target="$1"
  local expected="$2"

  tk show tanka/environments/guion \
    --dangerous-allow-redirect \
    --target "${target}" >"${rendered}"
  grep -Fq -- "${expected}" "${rendered}"
}

assert_resource_contains Namespace/cnsupa-system 'kind: Namespace'
assert_resource_contains CustomResourceDefinition/supabaseprojects.supabase.guion.dev 'name: supabaseprojects.supabase.guion.dev'
assert_resource_contains ClusterRole/cloudnative-supabase-manager 'name: cloudnative-supabase-manager'
assert_resource_contains Deployment/cloudnative-supabase 'namespace: cnsupa-system'
assert_resource_contains Deployment/cloudnative-supabase 'image: ghcr.io/guionai/cloudnative-supabase:latest'
