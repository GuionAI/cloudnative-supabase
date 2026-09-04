# CloudNative Supabase

CloudNative Supabase provisions and operates independent Supabase projects on
Kubernetes. This glossary names the project lifecycle promises that callers
can rely on.

## Language

**Supabase project**:
An independently operated Supabase backend with its own database, users,
credentials, and public identity.
_Avoid_: Stack, instance

**Rebuildable project**:
A Supabase project whose data and active sessions may be discarded when it is
recreated.
_Avoid_: Dev project, disposable database

**Preserved project**:
A Supabase project whose data must survive replacement or relocation, even
when active sessions are intentionally invalidated.
_Avoid_: Production project, persistent project

**Managed service profile**:
The product-supported subset of Supabase services that CloudNative Supabase
promises to provision and operate. It is not the complete upstream self-hosted
distribution.
_Avoid_: Full Supabase stack

**Publishable API key**:
A public project credential intended for browsers and native clients. It gives
a request the anonymous database role but does not identify a user.
_Avoid_: Anon JWT, public token

**Secret API key**:
A private project credential intended only for trusted backend services. It
gives a request the privileged service role independently of any user session.
_Avoid_: Service-role JWT, server token

**User session**:
The renewable credentials issued after a user authenticates. A user session is
independent of the API key that identifies the calling client.
_Avoid_: API key, client key

**Project credential bundle**:
The stable credentials that establish a Supabase project's signing identity
and client access. It belongs to the project independently of any particular
workload or database lifecycle.
_Avoid_: JWT secret, deployment secret
