#!/bin/bash
set -e

# Generate secrets for CNPG managed roles
# Creates passwords for all Supabase users
#
# Usage: ./generate-secrets.sh [NAMESPACE]
#
# SECRET FORMAT (required by CNPG declarative role management):
#   type: kubernetes.io/basic-auth
#   stringData:
#     username: <role_name>
#     password: <password>
#   labels:
#     cnpg.io/reload: "true"
#
# SECRET USAGE (see flux/database/base/cluster.yaml):
#
# Supabase Core Roles:
#   cnpg-supabase-admin-password:     Database owner, manages schemas
#   cnpg-authenticator-password:      PostgREST uses this to switch roles
#   cnpg-supabase-auth-password:      GoTrue auth service
#   cnpg-supabase-storage-password:   Storage service
#   cnpg-supabase-realtime-password:  Realtime service
#   cnpg-pgbouncer-password:          Connection pooling

NAMESPACE="${1:-supabase}"
SECRETS_FILE="cnpg-secrets.yaml"

echo "Generating CNPG user credentials for namespace: $NAMESPACE..."

# Generate URL-safe passwords (hex = alphanumeric only)
SUPABASE_ADMIN_PASSWORD=$(openssl rand -hex 32)
AUTHENTICATOR_PASSWORD=$(openssl rand -hex 32)
AUTH_PASSWORD=$(openssl rand -hex 32)
STORAGE_PASSWORD=$(openssl rand -hex 32)
REALTIME_PASSWORD=$(openssl rand -hex 32)
PGBOUNCER_PASSWORD=$(openssl rand -hex 32)

cat > "$SECRETS_FILE" <<EOF
# Supabase Core Roles
# CNPG managed roles require type: kubernetes.io/basic-auth with username + password
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-supabase-admin-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: supabase_admin
  password: "$SUPABASE_ADMIN_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-authenticator-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: authenticator
  password: "$AUTHENTICATOR_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-supabase-auth-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: supabase_auth_admin
  password: "$AUTH_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-supabase-storage-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: supabase_storage_admin
  password: "$STORAGE_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-supabase-realtime-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: supabase_realtime_admin
  password: "$REALTIME_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-pgbouncer-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: pgbouncer
  password: "$PGBOUNCER_PASSWORD"
EOF

echo "Generated $SECRETS_FILE"
echo ""
echo "Next steps:"
echo "1. Copy to your deployment repo (e.g., flicknote-deploy/flux/secrets/dev/)"
echo "2. Encrypt with SOPS using the .sops.yaml in that repo:"
echo "   sops --encrypt $SECRETS_FILE > cnpg-secrets.enc.yaml"
echo "3. Delete the unencrypted file: rm $SECRETS_FILE"
echo "4. Commit the encrypted file"
