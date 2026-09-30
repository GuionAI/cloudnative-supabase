# Opt-in PostgreSQL 18 image with PGroonga

`Dockerfile.postgres-pgroonga` builds a separate CNPG operand image from the
pinned CNPG `18.3-202603230821-standard-trixie` digest. It installs the PGDG
PostgreSQL 18 PGroonga package `4.0.9-1` and its Groonga runtime dependencies.
It preserves the CNPG image's UID 26, PostgreSQL binary path and startup
conventions. It does not change the operator's default PostgreSQL image.
The main and release workflows publish this image separately as
`ghcr.io/guionai/cloudnative-supabase-postgres-pgroonga`, with `latest` and
full commit SHA tags on main, and semantic version tags on releases. PR checks
build and smoke the image without publishing it.

Build and test locally with a container engine:

```bash
podman build --arch amd64 -f Dockerfile.postgres-pgroonga -t cnsupa-postgres-pgroonga:chatgpt-mcp-pg .
hack/test-pgroonga-image.sh
```

The smoke fixture runs a networkless, disposable container. It creates only
container-local data, checks Chinese and English OR search, user-scoped RLS
results and snippets, updates and deletes, then a clean restart and physical
backup/WAL recovery. Set `CONTAINER_TOOL=docker` for Docker, and
`PGROONGA_IMAGE` to test a different locally loaded image. Neither command
uses a real database or Kubernetes cluster.

## Project configuration

After publishing the image, select an immutable SHA tag or digest via the
existing project image field. For a single primary, add the two PGroonga
preloads and GUCs:

```yaml
spec:
  database:
    image: ghcr.io/guionai/cloudnative-supabase-postgres-pgroonga:sha-<full-commit-sha>
    additionalPreloadLibraries:
      - pgroonga_wal_resource_manager
      - pgroonga_crash_safer
    parameters:
      pgroonga.enable_wal_resource_manager: "on"
      pgroonga.enable_crash_safe: "on"
```

`additionalPreloadLibraries` appends to the operator's existing
`pg_stat_statements`, `pgaudit` and `auto_explain` preloads. CNPG manages
`shared_preload_libraries`, so setting that name in `database.parameters`
cannot reliably replace the list. Keep `pgroonga.enable_row_level_security`
at its safe default `on`: PGroonga then limits logs for tables with RLS
policies. Do not set it to `off` for private notes.

The image only provides extension files. A database migration owned by the
application must run `CREATE EXTENSION pgroonga`, grant schema usage as needed,
and create business indexes. `additionalExtensions` does not install or
create extensions. Use the same image on every node and on recovery targets
before starting WAL replay. Apply the project image and preload settings
before enabling PGroonga indexes; changing preload libraries requires a
PostgreSQL restart.

## Physical backup and recovery

PGroonga keeps Groonga files alongside PostgreSQL data. Before a physical
copy, stop writes to PGroonga-indexed tables, run
`SELECT pgroonga_command('io_flush')`, and keep writes stopped until the copy
finishes. The fixture uses a cold physical copy after a clean stop, then
restarts the source, writes new data, archives WAL, and restores the copy to a
new instance. It confirms the post-copy row survived WAL replay, then runs
`REINDEX INDEX` before checking older and newer search results. The PGroonga
index did not reliably return the post-copy row before REINDEX in this test.
The fixture does **not** validate a live CNPG/Barman backup path;
that path needs its own non-production recovery drill before use.

PGroonga's replication guide loads `pgroonga_wal_resource_manager` on a
standby and says `pgroonga_crash_safer` is unnecessary there. In the fixture,
the recovery instance uses WAL resource manager only during replay, then
promotes. A recovery project can declare only the WAL manager during recovery.
The current `SupabaseProject`
preload projection is uniform across all CNPG instances; it does not provide
a primary-versus-standby split. Validate replica and backup behavior before
using this configuration with multiple instances. Crash safer on the recovered
instance made the restored index unsearchable in this fixture, including after
one REINDEX attempt. Do not enable it on a recovered primary without a
separate validated procedure. Do not assume this image alone makes an existing
backup policy PGroonga-aware.

The work here builds and tests an opt-in image. It does not migrate FlickNote
indexes, switch production to this operator, or complete ChatGPT OAuth.
Cross-repository acceptance requires the `flicknote-cli` PR for spec #2809 to
run its real PG integration suite against this image; neither PR should merge
before that evidence is recorded. The fb OAuth Worker resource and full-grant
changes remain a later delivery unit.

Sources: [PGroonga Debian installation](https://pgroonga.github.io/install/debian.html),
[WAL resource manager replication](https://pgroonga.github.io/reference/streaming-replication-wal-resource-manager.html),
[CNPG PostgreSQL preload configuration](https://cloudnative-pg.io/documentation/current/postgresql_conf/).
