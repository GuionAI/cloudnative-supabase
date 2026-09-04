---
status: accepted
---

# Keep project credentials in Infisical

Infisical is the source of truth for each project's signing identity and API
credentials, independently of the SupabaseProject and database lifecycles. A
project/environment path holds named values for the private signing JWK array,
opaque publishable and secret API keys, and internal ES256 role JWTs; the
Infisical operator syncs them into one orphaned Kubernetes Secret, and
CloudNative Supabase validates that contract and injects only the fields
required by each workload. CloudNative Supabase derives the public JWKS and
generates GoTrue's required fallback secret because neither is an independent
project credential. Keeping named values rather than one outer JSON blob lets
GoTrue consume its JWK JSON directly and avoids giving every consumer the full
bundle or adding runtime parsing.
