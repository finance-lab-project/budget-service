#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
postgres_port=${BUDGET_POSTGRES_PORT:-55432}

# Explicit local target; no database creation, migration, reset or passing skip.
sql() {
  docker compose exec -T -e PGPASSWORD=budget-migrator-local-only -e PGCONNECT_TIMEOUT=5 postgres \
    psql -X -w -v ON_ERROR_STOP=1 -h postgres -U budget_migrator -d budget_local "$@"
}

schema_snapshot() {
  # Repeatable comparison only; this dump is never executed.
  docker compose exec -T -e PGPASSWORD=budget-migrator-local-only -e PGCONNECT_TIMEOUT=5 postgres \
    pg_dump -w -h postgres -U budget_migrator -d budget_local \
    --schema-only --restrict-key=BudgetSchemaProbe
}

version_snapshot() {
  sql -At -c 'SELECT id, version_id, is_applied, tstamp FROM public.goose_db_version ORDER BY id;'
}

[[ "$(docker compose port postgres 5432)" == "127.0.0.1:$postgres_port" ]]
sql <<'SQL'
DO $$
BEGIN
    IF current_database() <> 'budget_local' OR current_user <> 'budget_migrator'
       OR current_setting('server_version_num') <> '180006' THEN
        RAISE EXCEPTION 'Expected isolated budget_local, budget_migrator and PostgreSQL 18.6';
    END IF;
    IF EXISTS (SELECT FROM public.budget_owners)
       OR EXISTS (SELECT FROM public.monthly_budgets)
       OR EXISTS (SELECT FROM public.budget_save_receipts) THEN
        RAISE EXCEPTION 'Expected empty local application tables; no existing data will be reset';
    END IF;
END
$$;
SQL
[[ "$(sql -At -c 'SELECT version_id, is_applied FROM public.goose_db_version ORDER BY id;')" == $'0|t\n1|t' ]]

scratch_dir=$(mktemp -d "${TMPDIR:-/tmp}/budget-schema-check.XXXXXX")
trap 'rm -rf "$scratch_dir"' EXIT
schema_snapshot > "$scratch_dir/schema-before.sql"
version_snapshot > "$scratch_dir/versions-before.txt"

# Required operations and negative cases use the real runtime TCP login.
docker compose exec -T -e PGPASSWORD=budget-runtime-local-only -e PGCONNECT_TIMEOUT=5 postgres \
  psql -X -w -v ON_ERROR_STOP=1 -h postgres -U budget_runtime -d budget_local \
  < scripts/local-postgres/check-schema.sql

# An unrelated administrator-owned table exists only in this transaction.
# SET LOCAL ROLE is needed so it can be checked without committing the fixture.
docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local <<'SQL'
BEGIN;
SET LOCAL statement_timeout = '30s';
SET LOCAL lock_timeout = '5s';
CREATE TABLE public._budget_schema_unrelated (value text);
INSERT INTO public._budget_schema_unrelated VALUES ('synthetic unrelated data');
SET LOCAL ROLE budget_runtime;
DO $$
DECLARE statement text;
BEGIN
    IF current_user <> 'budget_runtime' THEN RAISE EXCEPTION 'Wrong effective role'; END IF;
    FOREACH statement IN ARRAY ARRAY[
        'SELECT * FROM public._budget_schema_unrelated',
        'INSERT INTO public._budget_schema_unrelated VALUES (''forbidden'')',
        'UPDATE public._budget_schema_unrelated SET value = ''forbidden''',
        'DELETE FROM public._budget_schema_unrelated'
    ] LOOP
        BEGIN
            EXECUTE statement;
            RAISE EXCEPTION 'Runtime unexpectedly accessed unrelated data: %', statement;
        EXCEPTION WHEN insufficient_privilege THEN NULL;
        END;
    END LOOP;
    RAISE NOTICE 'PASS unrelated administrator-owned data: SELECT/INSERT/UPDATE/DELETE denied (42501)';
END
$$;
ROLLBACK;
SQL

schema_snapshot > "$scratch_dir/schema-after.sql"
version_snapshot > "$scratch_dir/versions-after.txt"
cmp "$scratch_dir/schema-before.sql" "$scratch_dir/schema-after.sql"
cmp "$scratch_dir/versions-before.txt" "$scratch_dir/versions-after.txt"
[[ "$(sql -At -c 'SELECT (SELECT count(*) FROM public.budget_owners) + (SELECT count(*) FROM public.monthly_budgets) + (SELECT count(*) FROM public.budget_save_receipts);')" == '0' ]]
[[ "$(sql -At -c "SELECT to_regclass('public._budget_schema_unrelated') IS NULL;")" == 't' ]]
printf '%s\n' 'Schema checks passed: test rows rolled back; schema and Goose records unchanged at version 1. Owner sequence allocations are retained.'
