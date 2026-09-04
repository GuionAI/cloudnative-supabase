---
status: accepted
---

# Use one asymmetric authentication architecture

CloudNative Supabase will provision only the current asymmetric authentication
architecture: ES256 user sessions, opaque publishable and secret API keys, and
public-key verification. It will not expose a legacy or hybrid authentication
mode, even though upstream self-hosting supports gradual migration, because
every project we own can make a coordinated client cut and retaining both
architectures would permanently enlarge the operator, secret, and verification
surface. Sliqs development is a rebuildable project and may be recreated to
adopt this contract; preserved FlickNote projects migrate through replacement
and accept a forced sign-in.
