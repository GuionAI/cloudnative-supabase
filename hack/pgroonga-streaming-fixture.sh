#!/usr/bin/env bash
set -euo pipefail

export PATH="/usr/lib/postgresql/18/bin:$PATH"
export PGHOST=/tmp
export PGPORT=5432
initdb -D /tmp/source -A trust --encoding UTF8 --locale C --no-instructions >/dev/null
cat >> /tmp/source/postgresql.conf <<'CONF'
shared_preload_libraries = 'pg_stat_statements,pgaudit,auto_explain,pgroonga_wal_resource_manager,pgroonga_crash_safer'
pgroonga.enable_wal_resource_manager = on
pgroonga.enable_crash_safe = on
wal_level = logical
max_wal_senders = 10
CONF
pg_ctl -D /tmp/source -o '-k /tmp -p 5432' -l /tmp/source.log start >/dev/null
cleanup() {
  local rc=$?
  if [ "$rc" -ne 0 ]; then
    tail -80 /tmp/source.log
    [ ! -f /tmp/backup.log ] || tail -80 /tmp/backup.log
    [ ! -f /tmp/backup/pgroonga.log ] || tail -80 /tmp/backup/pgroonga.log
  fi
  pg_ctl -D /tmp/source -m immediate stop >/dev/null 2>&1 || true
  pg_ctl -D /tmp/backup -m immediate stop >/dev/null 2>&1 || true
}
trap cleanup EXIT
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
 IF EXISTS (SELECT 1 FROM notes WHERE body &@~ '苹果 OR apple' AND array_to_string(pgroonga_snippet_html(body, ARRAY['苹果','apple']), '') LIKE '%private-alice%') THEN RAISE EXCEPTION 'Bob saw Alice snippet'; END IF;
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
PGOPTIONS="-c enable_seqscan=off" psql -X -v ON_ERROR_STOP=1 -d postgres -Atc "SELECT count(*) FROM notes WHERE body &@~ '香蕉'" | grep -qx 1
wait_for_flush() {
  local data_directory=$1 parent_pid=$2 flush_pid
  for ((attempt = 0; attempt < 60; attempt++)); do
    flush_pid="$(awk -F '|' '/pgroonga: crash-safer: ready: 5\/1663/ { split($3, fields, ":"); pid = fields[1] } END { print pid }' "$data_directory/pgroonga.log")"
    if [ -n "$flush_pid" ] && kill -0 "$flush_pid" 2>/dev/null &&
       grep -q "^PPid:[[:space:]]*${parent_pid}$" "/proc/${flush_pid}/status"; then
      printf '%s\n' "$flush_pid"
      return
    fi
    sleep 1
  done
  echo "Crash-safer did not become ready for $data_directory" >&2
  return 1
}
# The deliberate source restart can still be rebuilding native files after SQL
# readiness. Quiesce that writer before io_flush/basebackup, not the replica's
# first read or the promoted primary's first write.
source_flush_pid="$(wait_for_flush /tmp/source "$(head -1 /tmp/source/postmaster.pid)")"
echo "Source crash-safer ${source_flush_pid} ready before quiesced basebackup"
# Flush Groonga and keep indexed tables quiescent until pg_basebackup finishes.
psql -X -v ON_ERROR_STOP=1 -d postgres -c "SELECT pgroonga_command('io_flush')" >/dev/null
pg_basebackup -D /tmp/backup -X stream -c fast -R >/dev/null
export PGPORT=5433
pg_ctl -D /tmp/backup -o '-k /tmp -p 5433' -l /tmp/backup.log start >/dev/null
standby_postmaster_pid="$(head -1 /tmp/backup/postmaster.pid)"

sql() {
  psql -Xq -v ON_ERROR_STOP=1 -d postgres "$@"
}
assert_settings() {
  sql -Atc "LOAD 'pgroonga'; SELECT
    current_setting('shared_preload_libraries') = 'pg_stat_statements,pgaudit,auto_explain,pgroonga_wal_resource_manager,pgroonga_crash_safer'
    AND current_setting('pgroonga.enable_wal_resource_manager') = 'on'
    AND current_setting('pgroonga.enable_crash_safe') = 'on'
    AND current_setting('pgroonga.enable_row_level_security') = 'on'" | grep -qx t
}
# Inspect the actual plan, not just enable_seqscan, for each querying role.
assert_search() {
  local role
  for role in postgres alice bob; do
    PGOPTIONS="-c enable_seqscan=off" sql -Atc "SET ROLE ${role}; EXPLAIN SELECT * FROM notes WHERE body &@~ '香蕉 OR recovery OR promotion'" |
      grep -Eq '(Index Scan using|Bitmap Index Scan on) notes_body_pgroonga'
  done
  sql -v alice_matches="$1" <<'SQL'
SET enable_seqscan = off;
SELECT set_config('fixture.alice_matches', :'alice_matches', false);
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM notes WHERE id IN (2, 5)) THEN RAISE EXCEPTION 'deleted rows survived replay'; END IF;
 IF (SELECT count(*) FROM notes WHERE body &@~ '苹果 OR apple OR obsolete') <> 0 THEN RAISE EXCEPTION 'stale updated/deleted index'; END IF;
 IF (SELECT count(*) FROM notes WHERE body &@~ '香蕉') <> 1 THEN RAISE EXCEPTION 'Chinese update missing'; END IF;
 IF (SELECT count(*) FROM notes WHERE body &@~ 'streamed') <> 1 THEN RAISE EXCEPTION 'streamed update not indexed'; END IF;
 IF (SELECT count(*) FROM notes WHERE body &@~ 'recovery') <> 1 THEN RAISE EXCEPTION 'English insert missing'; END IF;
END $$;
SET ROLE alice;
DO $$ BEGIN
 IF (SELECT count(*) FROM notes WHERE body &@~ '香蕉 OR recovery OR promotion') <> current_setting('fixture.alice_matches')::integer THEN RAISE EXCEPTION 'Alice search leaked or missed rows'; END IF;
 IF EXISTS (SELECT 1 FROM notes WHERE body &@~ '香蕉 OR recovery OR promotion' AND (owner_name <> 'alice' OR array_to_string(pgroonga_snippet_html(body, ARRAY['香蕉','recovery','promotion']), '') LIKE '%private-bob%')) THEN RAISE EXCEPTION 'Alice saw Bob row/snippet'; END IF;
END $$;
RESET ROLE;
SET ROLE bob;
DO $$ BEGIN
 IF (SELECT count(*) FROM notes WHERE body &@~ '香蕉 OR recovery OR promotion') <> 1 THEN RAISE EXCEPTION 'Bob search leaked or missed rows'; END IF;
 IF EXISTS (SELECT 1 FROM notes WHERE body &@~ '香蕉 OR recovery OR promotion' AND (owner_name <> 'bob' OR array_to_string(pgroonga_snippet_html(body, ARRAY['香蕉','recovery','promotion']), '') LIKE '%private-alice%')) THEN RAISE EXCEPTION 'Bob saw Alice row/snippet'; END IF;
END $$;
RESET ROLE;
SQL
}
# A normal first indexed read must be safe before new WAL arrives.
PGOPTIONS="-c enable_seqscan=off" sql -Atc "EXPLAIN SELECT * FROM notes WHERE body &@~ '香蕉'" |
  grep -Eq '(Index Scan using|Bitmap Index Scan on) notes_body_pgroonga'
PGOPTIONS="-c enable_seqscan=off" sql -Atc "SELECT count(*) FROM notes WHERE body &@~ '香蕉'" | grep -qx 1
assert_settings
sql -Atc 'SELECT pg_is_in_recovery()' | grep -qx t
# Mutations after standby startup must arrive through streaming, not basebackup.
PGPORT=5432 sql <<'SQL'
INSERT INTO notes VALUES (3, 'bob', 'English recovery private-bob'), (5, 'bob', 'obsolete private-bob');
UPDATE notes SET body = 'streamed 中文 香蕉 private-alice' WHERE id = 1;
DELETE FROM notes WHERE id = 5;
SQL
replay_lsn="$(PGPORT=5432 sql -Atc 'SELECT pg_current_wal_flush_lsn()')"
for ((attempt = 0; attempt < 60; attempt++)); do
  replayed="$(sql -Atc "SELECT pg_last_wal_replay_lsn() >= '${replay_lsn}'::pg_lsn")"
  if [ "$replayed" = t ]; then break; fi
  sleep 1
done
sql -Atc "SELECT pg_last_wal_replay_lsn() >= '${replay_lsn}'::pg_lsn" | grep -qx t
sql -Atc "SELECT count(*) FROM pg_stat_wal_receiver WHERE status = 'streaming'" | grep -qx 1
PGPORT=5432 assert_settings
assert_search 1
crash_safer_pid="$(sql -Atc "SELECT pid FROM pg_stat_activity WHERE backend_type = 'pgroonga: crash-safer: main'")"
test -n "$crash_safer_pid"
echo 'Streaming standby: uniform settings, insert/update/delete, indexed Chinese/English search and owner RLS passed'

postmaster_pid="$(head -1 /tmp/backup/postmaster.pid)"
postmaster_started="$(sql -Atc 'SELECT pg_postmaster_start_time()')"
# Stop the fixture primary before promotion; this is not CNPG election/fencing.
PGPORT=5432 pg_ctl -D /tmp/source -m fast stop >/dev/null
sql -Atc 'SELECT pg_promote(true, 60)' | grep -qx t
# No readiness polling or intervening checks before the first primary write.
sql -c "INSERT INTO notes VALUES (4, 'alice', 'promotion 中文 private-alice')"
sql -Atc 'SELECT pg_is_in_recovery()' | grep -qx f
test "$(head -1 /tmp/backup/postmaster.pid)" = "${postmaster_pid}"
test "$(sql -Atc 'SELECT pg_postmaster_start_time()')" = "${postmaster_started}"
assert_settings
assert_search 2
echo "Promotion without restart: postmaster PID ${postmaster_pid}, started ${postmaster_started}; settings/search/RLS unchanged"
echo 'Promoted primary: immediate indexed write and owner RLS passed'
# Observe readiness after the write; polling never delays the triggering caller.
flush_pid="$(wait_for_flush /tmp/backup "$standby_postmaster_pid")"
test "$(sql -Atc "SELECT pid FROM pg_stat_activity WHERE backend_type = 'pgroonga: crash-safer: main'")" = "$crash_safer_pid"
echo "Existing crash-safer PID ${crash_safer_pid} has ready flush worker ${flush_pid} on promoted primary"
pg_ctl -D /tmp/backup -m immediate stop >/dev/null
pg_ctl -D /tmp/backup -o '-k /tmp -p 5433' -l /tmp/backup.log start >/dev/null
sql -Atc 'SELECT pg_is_in_recovery()' | grep -qx f
assert_settings
assert_search 2
echo 'Controlled crash/restart: indexed writes and owner RLS survived'
pg_ctl -D /tmp/backup -m fast stop >/dev/null
pg_ctl -D /tmp/backup -o '-k /tmp -p 5433' -l /tmp/backup.log start >/dev/null
test "$(sql -Atc 'SELECT pg_postmaster_start_time()')" != "${postmaster_started}"
sql -Atc 'SELECT pg_is_in_recovery()' | grep -qx f
assert_settings
assert_search 2
echo 'PGroonga image: streaming, promotion without restart, primary crash-safer readiness, indexed writes, crash and clean restart passed'
