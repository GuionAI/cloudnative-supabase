#!/usr/bin/env bash
set -euo pipefail

export PATH="/usr/lib/postgresql/18/bin:$PATH"
export PGHOST=/tmp
export PGPORT=5432
mkdir /tmp/archive
initdb -D /tmp/source -A trust --no-instructions >/dev/null
cat >> /tmp/source/postgresql.conf <<'CONF'
shared_preload_libraries = 'pg_stat_statements,pgaudit,auto_explain,pgroonga_wal_resource_manager,pgroonga_crash_safer'
pgroonga.enable_wal_resource_manager = on
pgroonga.enable_crash_safe = on
archive_mode = on
archive_command = 'cp %p /tmp/archive/%f'
wal_level = logical
max_wal_senders = 10
CONF
pg_ctl -D /tmp/source -o '-k /tmp -p 5432' -l /tmp/source.log start >/dev/null
trap 'rc=$?; if [ "$rc" -ne 0 ]; then tail -80 /tmp/source.log; [ ! -f /tmp/backup.log ] || tail -80 /tmp/backup.log; fi; pg_ctl -D /tmp/source -m immediate stop >/dev/null 2>&1 || true; pg_ctl -D /tmp/backup -m immediate stop >/dev/null 2>&1 || true' EXIT
psql -X -v ON_ERROR_STOP=1 -d postgres <<'SQL'
CREATE EXTENSION pgroonga;
DO $$ BEGIN
 IF (SELECT extversion FROM pg_extension WHERE extname = 'pgroonga') <> '4.0.9' THEN RAISE EXCEPTION 'unexpected PGroonga version'; END IF;
 IF current_setting('pgroonga.enable_wal_resource_manager') <> 'on' THEN RAISE EXCEPTION 'WAL manager disabled'; END IF;
 IF current_setting('pgroonga.enable_crash_safe') <> 'on' THEN RAISE EXCEPTION 'crash safer disabled'; END IF;
 IF current_setting('pgroonga.enable_row_level_security') <> 'on' THEN RAISE EXCEPTION 'unsafe RLS logging'; END IF;
END $$;
CREATE ROLE alice LOGIN;
CREATE ROLE bob LOGIN;
CREATE TABLE notes (id integer PRIMARY KEY, owner_name text NOT NULL, body text NOT NULL);
ALTER TABLE notes ENABLE ROW LEVEL SECURITY;
CREATE POLICY own_notes ON notes USING (owner_name = current_user);
GRANT SELECT ON notes TO alice, bob;
CREATE INDEX notes_body_pgroonga ON notes USING pgroonga (body);
INSERT INTO notes VALUES (1, 'alice', '中文 苹果 private-alice'), (2, 'bob', 'English apple private-bob');
SET enable_seqscan = off;
DO $$ BEGIN
 IF (SELECT count(*) FROM notes WHERE body &@~ '苹果 OR apple') <> 2 THEN RAISE EXCEPTION 'Chinese/English OR search failed'; END IF;
END $$;
SET ROLE alice;
DO $$ BEGIN
 IF (SELECT count(*) FROM notes WHERE body &@~ '苹果 OR apple') <> 1 THEN RAISE EXCEPTION 'Alice search leaked or missed rows'; END IF;
 IF EXISTS (SELECT 1 FROM notes WHERE body &@~ '苹果 OR apple' AND body LIKE '%private-bob%') THEN RAISE EXCEPTION 'Alice saw Bob result'; END IF;
 IF EXISTS (SELECT 1 FROM notes WHERE body &@~ '苹果 OR apple' AND array_to_string(pgroonga_snippet_html(body, ARRAY['苹果','apple']), '') LIKE '%private-bob%') THEN RAISE EXCEPTION 'Alice saw Bob snippet'; END IF;
END $$;
RESET ROLE;
SET ROLE bob;
DO $$ BEGIN
 IF (SELECT count(*) FROM notes WHERE body &@~ '苹果 OR apple') <> 1 THEN RAISE EXCEPTION 'Bob search leaked or missed rows'; END IF;
 IF EXISTS (SELECT 1 FROM notes WHERE body &@~ '苹果 OR apple' AND body LIKE '%private-alice%') THEN RAISE EXCEPTION 'Bob saw Alice result'; END IF;
END $$;
RESET ROLE;
UPDATE notes SET body = '中文 香蕉 private-alice' WHERE id = 1;
DELETE FROM notes WHERE id = 2;
DO $$ BEGIN
 IF (SELECT count(*) FROM notes WHERE body &@~ '苹果 OR apple') <> 0 THEN RAISE EXCEPTION 'stale update/delete index'; END IF;
 IF (SELECT count(*) FROM notes WHERE body &@~ '香蕉') <> 1 THEN RAISE EXCEPTION 'update not searchable'; END IF;
END $$;
SQL
pg_ctl -D /tmp/source -m fast stop >/dev/null
pg_ctl -D /tmp/source -o '-k /tmp -p 5432' -l /tmp/source.log start >/dev/null
psql -X -v ON_ERROR_STOP=1 -d postgres -Atc "SELECT count(*) FROM notes WHERE body &@~ '香蕉'" | grep -qx 1
# A cold physical copy gives Groonga and PostgreSQL files one coherent point.
# Subsequent committed writes are recovered from archived PostgreSQL WAL.
psql -X -v ON_ERROR_STOP=1 -d postgres -c "SELECT pgroonga_command('io_flush')" >/dev/null
pg_ctl -D /tmp/source -m fast stop >/dev/null
cp -a /tmp/source /tmp/backup
pg_ctl -D /tmp/source -o '-k /tmp -p 5432' -l /tmp/source.log start >/dev/null
psql -X -v ON_ERROR_STOP=1 -d postgres -c "INSERT INTO notes VALUES (3, 'bob', '恢复 recovery-after-backup')" >/dev/null
psql -X -v ON_ERROR_STOP=1 -d postgres -c "SELECT pgroonga_command('io_flush')" >/dev/null
wal_segment="$(psql -X -v ON_ERROR_STOP=1 -d postgres -Atc 'SELECT pg_walfile_name(pg_current_wal_lsn())')"
psql -X -v ON_ERROR_STOP=1 -d postgres -c 'SELECT pg_switch_wal()' >/dev/null
for i in $(seq 1 60); do
  if [ -f "/tmp/archive/${wal_segment}" ]; then break; fi
  sleep 1
done
test -f "/tmp/archive/${wal_segment}"
pg_ctl -D /tmp/source -m fast stop >/dev/null
cat >> /tmp/backup/postgresql.auto.conf <<CONF
shared_preload_libraries = 'pg_stat_statements,pgaudit,auto_explain,pgroonga_wal_resource_manager'
pgroonga.enable_crash_safe = off
pgroonga.enable_wal_resource_manager = off
restore_command = 'cp /tmp/archive/%f %p'
CONF
touch /tmp/backup/recovery.signal
export PGPORT=5433
pg_ctl -D /tmp/backup -o '-k /tmp -p 5433' -l /tmp/backup.log start >/dev/null
for i in $(seq 1 60); do
  if [ "$(psql -X -v ON_ERROR_STOP=1 -d postgres -Atc 'SELECT pg_is_in_recovery()')" = f ]; then break; fi
  sleep 1
done
test "$(psql -X -v ON_ERROR_STOP=1 -d postgres -Atc 'SELECT pg_is_in_recovery()')" = f
psql -X -v ON_ERROR_STOP=1 -d postgres -Atc 'SELECT count(*) FROM notes WHERE id = 3' | grep -qx 1
psql -X -v ON_ERROR_STOP=1 -d postgres -c 'REINDEX INDEX notes_body_pgroonga' >/dev/null
psql -X -v ON_ERROR_STOP=1 -d postgres -Atc "SELECT count(*) FROM notes WHERE body &@~ '香蕉'" | grep -qx 1
psql -X -v ON_ERROR_STOP=1 -d postgres -Atc "SELECT count(*) FROM notes WHERE body &@~ 'recovery'" | grep -qx 1
psql -X -v ON_ERROR_STOP=1 -d postgres -Atc "SELECT current_setting('shared_preload_libraries') LIKE '%pgroonga_wal_resource_manager%'" | grep -qx t
echo 'PGroonga image: search, RLS, writes, restart and physical WAL recovery passed'
