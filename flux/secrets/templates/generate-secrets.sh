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
# CNPG SECRETS (see flux/database/base/cluster.yaml):
#   cnpg-supabase-admin-password:       Database owner, manages schemas
#   cnpg-authenticator-password:        PostgREST uses this to switch roles
#   cnpg-supabase-auth-password:        GoTrue auth service
#   cnpg-supabase-storage-password:     Storage service
#   cnpg-supabase-realtime-password:    Realtime service
#   cnpg-supabase-analytics-password:   Logflare analytics service
#   cnpg-supabase-functions-password:   Edge Functions service
#   cnpg-pgbouncer-password:            Connection pooling
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
EOF

echo "Generated $CNPG_SECRETS_FILE"

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
  # JWT signing secret (used to generate anonKey and serviceKey)
  secret: "$JWT_SECRET"
  # IMPORTANT: Generate these tokens using the secret above
  # Use: https://supabase.com/docs/guides/self-hosting#api-keys
  # Or run: npx @anthropic-ai/claude-code-jwt-gen (if available)
  anonKey: "REPLACE_WITH_GENERATED_ANON_KEY"
  serviceKey: "REPLACE_WITH_GENERATED_SERVICE_KEY"
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
echo "=== IMPORTANT: JWT Token Generation ==="
echo "The JWT secret has been generated, but you must create the anonKey and serviceKey tokens."
echo ""
echo "JWT Secret: $JWT_SECRET"
echo ""
echo "Generate tokens at: https://supabase.com/docs/guides/self-hosting#api-keys"
echo "Or use this payload for anonKey:    {\"role\": \"anon\", \"iss\": \"supabase\", \"iat\": $(date +%s), \"exp\": $(($(date +%s) + 157680000))}"
echo "Or use this payload for serviceKey: {\"role\": \"service_role\", \"iss\": \"supabase\", \"iat\": $(date +%s), \"exp\": $(($(date +%s) + 157680000))}"
echo ""
echo "=== Next Steps ==="
echo "1. Generate JWT tokens and update $SUPABASE_SECRETS_FILE"
echo "2. Copy files to your deployment repo (e.g., flicknote-deploy/flux/secrets/dev/)"
echo "3. Encrypt with SOPS:"
echo "   sops --encrypt $CNPG_SECRETS_FILE > cnpg-secrets.enc.yaml"
echo "   sops --encrypt $SUPABASE_SECRETS_FILE > supabase-secrets.enc.yaml"
echo "4. Delete unencrypted files: rm $CNPG_SECRETS_FILE $SUPABASE_SECRETS_FILE"
echo "5. Commit the encrypted files"
