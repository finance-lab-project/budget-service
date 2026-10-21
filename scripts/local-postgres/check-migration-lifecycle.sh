#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
postgres_port=${BUDGET_POSTGRES_PORT:-55432}

# One-time lifecycle check against the empty, dedicated local Compose database.
# This never resets a database or runs a downgrade.
migration_dsn="host=127.0.0.1 port=$postgres_port dbname=budget_local user=budget_migrator password=budget-migrator-local-only sslmode=disable connect_timeout=5"

sql() {
  docker compose exec -T -e PGPASSWORD=budget-migrator-local-only postgres \
    psql -X -w -v ON_ERROR_STOP=1 -h postgres -U budget_migrator -d budget_local "$@"
}

run_goose() {
  local migration_dir=$1
  shift
  .tools/bin/goose -env none -dir "$migration_dir" -table public.goose_db_version \
    postgres "$migration_dsn" "$@"
}

schema_snapshot() {
  # Fixed restrict key is for repeatable test output only. Dumps are never executed.
  docker compose exec -T -e PGPASSWORD=budget-migrator-local-only postgres \
    pg_dump -w -h postgres -U budget_migrator -d budget_local \
    --schema-only --restrict-key=BudgetLifecycleProbe
}

version_snapshot() {
  sql -At -c 'SELECT id, version_id, is_applied, tstamp FROM public.goose_db_version ORDER BY id;'
}

[[ "$(.tools/bin/goose -version 2>&1)" == 'goose version: v3.29.0' ]]
[[ "$(docker compose port postgres 5432)" == "127.0.0.1:$postgres_port" ]]
sql <<'SQL'
DO $$
BEGIN
    IF current_database() <> 'budget_local' OR current_user <> 'budget_migrator'
       OR current_setting('server_version_num') <> '180006' THEN
        RAISE EXCEPTION 'Expected local budget_local, budget_migrator and PostgreSQL 18.6';
    END IF;
    IF EXISTS (SELECT FROM pg_class WHERE relnamespace = 'public'::regnamespace)
       OR EXISTS (SELECT FROM pg_type WHERE typnamespace = 'public'::regnamespace)
       OR EXISTS (SELECT FROM pg_proc WHERE pronamespace = 'public'::regnamespace) THEN
        RAISE EXCEPTION 'Requires an empty public schema; no existing database will be reset';
    END IF;
END
$$;
SQL

scratch_dir=$(mktemp -d "${TMPDIR:-/tmp}/budget-migration-lifecycle.XXXXXX")
trap 'rm -rf "$scratch_dir"' EXIT

run_goose migrations up
run_goose migrations version
run_goose migrations status
[[ "$(sql -At -c 'SELECT version_id, is_applied FROM public.goose_db_version ORDER BY id;')" == $'0|t\n1|t' ]]
sql -c "SELECT relname, relkind, pg_get_userbyid(relowner) AS owner
        FROM pg_class WHERE relnamespace = 'public'::regnamespace ORDER BY relname;" \
    -c "SELECT typname, typtype, format_type(typbasetype, typtypmod) AS base_type,
               pg_get_userbyid(typowner) AS owner
        FROM pg_type WHERE oid = 'public.budget_source_amount'::regtype;"
schema_snapshot > "$scratch_dir/schema-before.sql"
version_snapshot > "$scratch_dir/versions-before.txt"

run_goose migrations up
schema_snapshot > "$scratch_dir/schema-after-noop.sql"
version_snapshot > "$scratch_dir/versions-after-noop.txt"
cmp "$scratch_dir/schema-before.sql" "$scratch_dir/schema-after-noop.sql"
cmp "$scratch_dir/versions-before.txt" "$scratch_dir/versions-after-noop.txt"
printf '%s\n' 'Second up: schema and Goose records unchanged.'

# Only this temporary directory contains the intentionally failing version 2.
mkdir "$scratch_dir/migrations"
cp migrations/00001_initial_budget_schema.sql "$scratch_dir/migrations/"
cat > "$scratch_dir/migrations/00002_controlled_failure.sql" <<'SQL'
-- +goose Up
CREATE TABLE public._budget_migration_rollback_probe (id integer PRIMARY KEY, value text NOT NULL);
INSERT INTO public._budget_migration_rollback_probe VALUES (1, 'synthetic rollback probe');
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'budget lifecycle rollback probe';
END
$$;
-- +goose StatementEnd
SQL

if run_goose "$scratch_dir/migrations" up > "$scratch_dir/failure.log" 2>&1; then
  printf '%s\n' 'ERROR: the controlled migration unexpectedly succeeded.' >&2
  exit 1
else
  failure_status=$?
fi
failure_output=$(< "$scratch_dir/failure.log")
if [[ "$failure_output" != *'ERROR: budget lifecycle rollback probe (SQLSTATE P0001)'* ]]; then
  printf '%s\n' "$failure_output" >&2
  printf '%s\n' 'ERROR: migration failed for an unexpected reason.' >&2
  exit 1
fi
printf 'Controlled migration failed as intended: exit %s, SQLSTATE P0001.\n' "$failure_status"

[[ "$(sql -At -c "SELECT to_regclass('public._budget_migration_rollback_probe') IS NULL;")" == 't' ]]
[[ "$(sql -At -c 'SELECT count(*) FROM public.goose_db_version WHERE version_id = 2;')" == '0' ]]
schema_snapshot > "$scratch_dir/schema-after-failure.sql"
version_snapshot > "$scratch_dir/versions-after-failure.txt"
cmp "$scratch_dir/schema-before.sql" "$scratch_dir/schema-after-failure.sql"
cmp "$scratch_dir/versions-before.txt" "$scratch_dir/versions-after-failure.txt"
[[ "$(sql -At -c 'SELECT (SELECT count(*) FROM public.budget_owners)
    + (SELECT count(*) FROM public.monthly_budgets)
    + (SELECT count(*) FROM public.budget_save_receipts);')" == '0' ]]
run_goose migrations version
printf '%s\n' 'Rollback verified: no probe table, no version 2 record, unchanged schema/version records, empty application tables.'
