# Supabase self-hosted opaque API keys

## Question

What exact `sb_publishable_*` and `sb_secret_*` format does current self-hosted
Supabase generate, what does its checksum cover, and which part of the stack
enforces it?

This note records the primary-source evidence behind CloudNative Supabase's
project-credential validation. It is not a second configuration source.

## Verified format

Supabase documents these forms:

```text
sb_publishable_<22-character-random>_<8-character-checksum>
sb_secret_<22-character-random>_<8-character-checksum>
```

Both variable segments use the unpadded Base64URL alphabet: ASCII letters,
digits, `-`, and `_`. The total lengths are 46 characters for a publishable key
and 41 for a secret key.

The pinned self-hosted v0.7.0 scripts generate 17 cryptographically random
bytes, encode them as unpadded Base64URL, and take the first 22 characters. For
both key roles they then compute:

```text
base64url(sha256("supabase-self-hosted|" + prefix + random))[0:8]
```

The prefix in that expression includes its trailing underscore. The final
underscore between the random and checksum segments sits at a fixed position;
the random segment may itself contain underscores.

The checksum context is the literal `supabase-self-hosted`. It is not derived
from a Kubernetes namespace, `SupabaseProject` name, hostname, or a hosted
Supabase project reference.

## Enforcement boundary

The upstream self-hosted gateway does not recompute this checksum. Envoy
compares the supplied opaque key with its configured value exactly. The
checksum is therefore a canonical-format and typo-detection mechanism, not an
additional authorization primitive.

CloudNative Supabase deliberately validates the canonical format and checksum
when it reads the external five-field project credential Secret. That catches
bad provisioning before dependent workloads change while leaving Envoy's
runtime exact-match semantics aligned with upstream.

There is no legacy-format fallback. A project using a prefix-only, readable,
or otherwise noncanonical value must rotate both opaque keys and update its
matching client and backend consumers before deploying a strict operator
image. This rotation is independent of the ES256 signing key and does not by
itself invalidate user sessions.

## Boundaries and unknowns

Hosted Supabase uses the same visible key shape, but its platform-side issuance
implementation is not public. CloudNative Supabase follows the documented
self-hosted algorithm and does not infer hosted-project checksum inputs.

This research does not change ES256 signing keys, internal role JWTs, JWKS,
GoTrue's compatibility secret, or Envoy authorization behavior.

## Primary sources

- Format and the explicit gateway checksum note:
  [self-hosted-auth-keys.mdx](https://github.com/supabase/supabase/blob/master/apps/docs/content/guides/self-hosting/self-hosted-auth-keys.mdx#L78-L85)
- Pinned initial-generation implementation:
  [self-hosted/v0.7.0 add-new-auth-keys.sh](https://github.com/supabase/supabase/blob/self-hosted/v0.7.0/docker/utils/add-new-auth-keys.sh#L125-L139)
- Pinned independent rotation implementation:
  [self-hosted/v0.7.0 rotate-new-api-keys.sh](https://github.com/supabase/supabase/blob/self-hosted/v0.7.0/docker/utils/rotate-new-api-keys.sh#L60-L76)
- Envoy's exact-value comparison:
  [lds.template.yaml](https://github.com/supabase/supabase/blob/master/docker/volumes/api/envoy/lds.template.yaml#L732-L751)
- Opaque API-key semantics:
  [api-keys.mdx](https://github.com/supabase/supabase/blob/master/apps/docs/content/guides/getting-started/api-keys.mdx#L46-L57)
