# Supabase Helm Chart - Required Secrets

This chart does NOT generate secrets. You must create them before deploying.

## Secret Key Naming Convention

All secrets use **fixed key names** - you cannot customize them. This simplifies configuration and reduces errors.

---

## Required Secrets

### 1. JWT Secret (REQUIRED)

Used by Kong, Studio, Auth, Rest, Realtime, and Storage services for API authentication.

**Values path:** `secret.jwt.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `anonKey` | JWT token for anonymous role |
| `serviceKey` | JWT token for service_role |
| `secret` | JWT signing secret (min 32 characters) |

**Example:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: supabase-jwt
type: Opaque
stringData:
  anonKey: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  serviceKey: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  secret: "your-super-secret-jwt-token-with-at-least-32-characters-long"
```

**Generate JWT tokens:** Use [supabase.com/docs/guides/self-hosting#api-keys](https://supabase.com/docs/guides/self-hosting#api-keys) to generate tokens.

---

### 2. Per-Service Database Secrets (REQUIRED for each enabled service)

Each Supabase service requires its own database credentials. This allows fine-grained access control using PostgreSQL roles.

**Required keys (same for all services):**
| Key | Description |
|-----|-------------|
| `username` | PostgreSQL role name |
| `password` | PostgreSQL role password |

**Services and their values paths:**

| Service | Values Path | Typical Username |
|---------|-------------|------------------|
| Studio | `studio.secret.db.secretRef` | `supabase_admin` |
| Auth | `auth.secret.db.secretRef` | `supabase_auth_admin` |
| Rest (PostgREST) | `rest.secret.db.secretRef` | `authenticator` |
| Realtime | `realtime.secret.db.secretRef` | `supabase_realtime_admin` |
| Meta | `meta.secret.db.secretRef` | `supabase_admin` |
| Storage | `storage.secret.db.secretRef` | `supabase_storage_admin` |
| dbInit hook | `dbInit.secret.db.secretRef` | `supabase_admin` |

**Example (for CNPG-managed roles):**
```yaml
# CNPG automatically creates secrets with username/password keys
# Reference them directly in your HelmRelease values:
auth:
  secret:
    db:
      secretRef: cnpg-supabase-auth-password
rest:
  secret:
    db:
      secretRef: cnpg-authenticator-password
```

---

## Optional Secrets

### 3. SMTP Credentials (Optional - for email functionality in Auth)

**Values path:** `secret.smtp.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `username` | SMTP username |
| `password` | SMTP password |

---

### 4. Dashboard Basic Auth (Optional - protect Studio with basic auth)

**Values path:** `secret.dashboard.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `username` | Dashboard username |
| `password` | Dashboard password |

Leave `secretRef` empty to disable dashboard authentication.

---

### 5. S3 Credentials (Required if `storage.environment.STORAGE_BACKEND=s3`)

**Values path:** `secret.s3.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `keyId` | AWS Access Key ID (or Minio root user) |
| `accessKey` | AWS Secret Access Key (or Minio root password) |

---

### 6. Auth Providers (Optional - for OAuth and email hooks)

**Values path:** `secret.authProviders.secretRef`

This secret is loaded via `envFrom` into the Auth container, so all keys become environment variables.

**Available keys:**
| Key | Description |
|-----|-------------|
| `GOTRUE_HOOK_SEND_EMAIL_SECRETS` | Webhook secret for email hook verification (format: `v1,whsec_<base64_secret>`) |
| `GOTRUE_EXTERNAL_GOOGLE_CLIENT_ID` | Google OAuth client ID |
| `GOTRUE_EXTERNAL_GOOGLE_SECRET` | Google OAuth client secret |
| `GOTRUE_EXTERNAL_APPLE_CLIENT_ID` | Apple service ID |
| `GOTRUE_EXTERNAL_APPLE_KEY_ID` | Apple key ID |
| `GOTRUE_EXTERNAL_APPLE_PRIVATE_KEY` | Apple private key (with `\n` for newlines) |

**Email Hook Secret Format:**

The secret must be in the standard-webhooks format: `v1,whsec_<base64_secret>`

Generate using:
```bash
# Generate 32 random bytes and base64 encode
SECRET=$(openssl rand -base64 32)
echo "v1,whsec_${SECRET}"
```

**Example:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: supabase-auth-providers
type: Opaque
stringData:
  GOTRUE_HOOK_SEND_EMAIL_SECRETS: "v1,whsec_dGhpcyBpcyBhIHNlY3JldCBmb3Igd2ViaG9vayB2ZXJpZmljYXRpb24="
  GOTRUE_EXTERNAL_GOOGLE_CLIENT_ID: "your-client-id.apps.googleusercontent.com"
  GOTRUE_EXTERNAL_GOOGLE_SECRET: "your-google-secret"
```

**Note:** When configuring the email hook, you also need to set these environment variables in `auth.environment`:
- `GOTRUE_HOOK_SEND_EMAIL_ENABLED: "true"`
- `GOTRUE_HOOK_SEND_EMAIL_URI: https://your-api.example.com/api/webhook/email`

---

## Example HelmRelease Values

```yaml
secret:
  jwt:
    secretRef: supabase-jwt
  smtp:
    secretRef: ""  # Disabled
  dashboard:
    secretRef: ""  # No basic auth
  s3:
    secretRef: ""  # Not using S3

dbInit:
  enabled: true
  host: supabase-pg-rw
  secret:
    db:
      secretRef: cnpg-supabase-admin-password

studio:
  secret:
    db:
      secretRef: cnpg-supabase-admin-password

auth:
  secret:
    db:
      secretRef: cnpg-supabase-auth-password

rest:
  secret:
    db:
      secretRef: cnpg-authenticator-password

realtime:
  secret:
    db:
      secretRef: cnpg-supabase-realtime-password

meta:
  secret:
    db:
      secretRef: cnpg-supabase-admin-password

storage:
  secret:
    db:
      secretRef: cnpg-supabase-storage-password
```

---

## Using with CloudNative-PG (CNPG)

When using CNPG with `managed.roles`, secrets are automatically created with `username` and `password` keys. Simply reference them by name.

**CNPG Cluster example:**
```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
spec:
  managed:
    roles:
      - name: supabase_auth_admin
        passwordSecret:
          name: cnpg-supabase-auth-password
```

This creates a secret `cnpg-supabase-auth-password` with keys:
- `username`: `supabase_auth_admin`
- `password`: (auto-generated)
