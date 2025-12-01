#!/bin/bash
set -e

# Generate secrets for Supabase deployment
# Creates CNPG role passwords and Supabase application secrets
#
# Usage: ./generate-secrets.sh [NAMESPACE]
#
# OUTPUT FILES:
#   cnpg-secrets.yaml     - CNPG managed role passwords
#   supabase-secrets.yaml - JWT and analytics secrets
#
# CNPG SECRET FORMAT (required by CNPG declarative role management):
#   type: kubernetes.io/basic-auth
#   stringData:
#     username: <role_name>
#     password: <password>
#   labels:
#     cnpg.io/reload: "true"
#
# CNPG SECRETS (see flux/database/base/helmrelease.yaml):
#   cnpg-supabase-admin-password:       Database owner, manages schemas
#   cnpg-authenticator-password:        PostgREST uses this to switch roles
#   cnpg-supabase-auth-password:        GoTrue auth service
#   cnpg-supabase-storage-password:     Storage service
#   cnpg-supabase-realtime-password:    Realtime service
#   cnpg-supabase-analytics-password:   Logflare analytics service
#   cnpg-supabase-functions-password:   Edge Functions service
#   cnpg-pgbouncer-password:            Connection pooling
#   cnpg-sequin-password:               Sequin app internal state
#   cnpg-sequin-replication-password:   CDC logical replication (Sequin/PowerSync)
#
# SUPABASE SECRETS:
#   supabase-jwt:       JWT signing secret and tokens (anonKey, serviceKey)
#   supabase-analytics: Logflare/Analytics API key

NAMESPACE="${1:-supabase}"
CNPG_SECRETS_FILE="cnpg-secrets.yaml"
SUPABASE_SECRETS_FILE="supabase-secrets.yaml"

echo "Generating secrets for namespace: $NAMESPACE..."

# Generate URL-safe passwords (hex = alphanumeric only)
SUPABASE_ADMIN_PASSWORD=$(openssl rand -hex 32)
AUTHENTICATOR_PASSWORD=$(openssl rand -hex 32)
AUTH_PASSWORD=$(openssl rand -hex 32)
STORAGE_PASSWORD=$(openssl rand -hex 32)
REALTIME_PASSWORD=$(openssl rand -hex 32)
ANALYTICS_PASSWORD=$(openssl rand -hex 32)
FUNCTIONS_PASSWORD=$(openssl rand -hex 32)
PGBOUNCER_PASSWORD=$(openssl rand -hex 32)
SEQUIN_PASSWORD=$(openssl rand -hex 32)
SEQUIN_REPLICATION_PASSWORD=$(openssl rand -hex 32)

# Generate Supabase application secrets
JWT_SECRET=$(openssl rand -hex 32)
ANALYTICS_API_KEY=$(openssl rand -hex 32)

cat > "$CNPG_SECRETS_FILE" <<EOF
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
  name: cnpg-supabase-analytics-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: supabase_analytics_admin
  password: "$ANALYTICS_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-supabase-functions-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: supabase_functions_admin
  password: "$FUNCTIONS_PASSWORD"
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
---
# Sequin/PowerSync CDC Roles
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-sequin-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: sequin
  password: "$SEQUIN_PASSWORD"
---
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-sequin-replication-password
  namespace: $NAMESPACE
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: sequin_replication
  password: "$SEQUIN_REPLICATION_PASSWORD"
EOF

echo "Generated $CNPG_SECRETS_FILE"

# Function to create JWT token
create_jwt() {
    local role=$1
    local secret=$2
    local iat=$(date +%s)
    local exp=$((iat + 157680000))  # 5 years from now

    # Header: {"alg":"HS256","typ":"JWT"}
    local header=$(echo -n '{"alg":"HS256","typ":"JWT"}' | base64 | tr -d '=' | tr '/+' '_-' | tr -d '\n')

    # Payload
    local payload=$(echo -n "{\"role\":\"$role\",\"iss\":\"supabase\",\"iat\":$iat,\"exp\":$exp}" | base64 | tr -d '=' | tr '/+' '_-' | tr -d '\n')

    # Signature
    local signature=$(echo -n "$header.$payload" | openssl dgst -sha256 -hmac "$secret" -binary | base64 | tr -d '=' | tr '/+' '_-' | tr -d '\n')

    echo "$header.$payload.$signature"
}

# Generate JWT tokens
ANON_KEY=$(create_jwt "anon" "$JWT_SECRET")
SERVICE_KEY=$(create_jwt "service_role" "$JWT_SECRET")

# Generate Supabase application secrets
cat > "$SUPABASE_SECRETS_FILE" <<EOF
# Supabase Application Secrets
---
apiVersion: v1
kind: Secret
metadata:
  name: supabase-jwt
  namespace: $NAMESPACE
type: Opaque
stringData:
  secret: "$JWT_SECRET"
  anonKey: "$ANON_KEY"
  serviceKey: "$SERVICE_KEY"
---
apiVersion: v1
kind: Secret
metadata:
  name: supabase-analytics
  namespace: $NAMESPACE
type: Opaque
stringData:
  apiKey: "$ANALYTICS_API_KEY"
EOF

echo "Generated $SUPABASE_SECRETS_FILE"
echo ""
echo "JWT Secret: $JWT_SECRET"
echo ""
echo "=== Next Steps ==="
echo "1. Copy files to your deployment repo (e.g., flicknote-deploy/flux/secrets/dev/)"
echo "2. Encrypt with SOPS:"
echo "   sops --encrypt $CNPG_SECRETS_FILE > cnpg-secrets.enc.yaml"
echo "   sops --encrypt $SUPABASE_SECRETS_FILE > supabase-secrets.enc.yaml"
echo "3. Delete unencrypted files: rm $CNPG_SECRETS_FILE $SUPABASE_SECRETS_FILE"
echo "4. Commit the encrypted files"
