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
| Analytics | `analytics.secret.db.secretRef` | `supabase_admin` |
| Functions | `functions.secret.db.secretRef` | `supabase_functions_admin` |
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

### 3. Analytics API Key (Required if `analytics.enabled=true`)

**Values path:** `secret.analytics.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `apiKey` | Logflare API key |

---

### 4. SMTP Credentials (Optional - for email functionality in Auth)

**Values path:** `secret.smtp.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `username` | SMTP username |
| `password` | SMTP password |

---

### 5. Dashboard Basic Auth (Optional - protect Studio with basic auth)

**Values path:** `secret.dashboard.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `username` | Dashboard username |
| `password` | Dashboard password |

Leave `secretRef` empty to disable dashboard authentication.

---

### 6. S3 Credentials (Required if `storage.environment.STORAGE_BACKEND=s3` or `minio.enabled=true`)

**Values path:** `secret.s3.secretRef`

**Required keys:**
| Key | Description |
|-----|-------------|
| `keyId` | AWS Access Key ID (or Minio root user) |
| `accessKey` | AWS Secret Access Key (or Minio root password) |

---

## Example HelmRelease Values

```yaml
secret:
  jwt:
    secretRef: supabase-jwt
  analytics:
    secretRef: supabase-analytics
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

analytics:
  secret:
    db:
      secretRef: cnpg-supabase-admin-password
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
