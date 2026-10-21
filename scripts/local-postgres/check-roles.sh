#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

# TCP via the service name uses password authentication, not local socket trust.
docker compose exec -T -e PGPASSWORD=budget-migrator-local-only postgres \
  psql -X -w -v ON_ERROR_STOP=1 -h postgres -U budget_migrator -d budget_local \
  -c 'SELECT session_user AS migration_login;'

docker compose exec -T -e PGPASSWORD=budget-runtime-local-only postgres \
  psql -X -w -v ON_ERROR_STOP=1 -h postgres -U budget_runtime -d budget_local <<'SQL'
SELECT session_user AS runtime_login;
DO $$
DECLARE target text;
BEGIN
    FOREACH target IN ARRAY ARRAY['budget_migrator', 'budget_local'] LOOP
        BEGIN
            EXECUTE format('SET ROLE %I', target);
            RAISE EXCEPTION 'Runtime login unexpectedly assumed role %', target;
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE NOTICE 'Denied runtime SET ROLE % (42501)', target;
        END;
    END LOOP;
END
$$;
SQL

# All probe objects, data and grants roll back, including on a failed assertion.
# SET LOCAL ROLE checks effective SQL permissions; login/escalation is tested above.
docker compose exec -T postgres \
  psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local <<'SQL'
BEGIN;
DO $$
DECLARE target text;
BEGIN
    IF current_database() <> 'budget_local' OR current_user <> 'budget_local' THEN
        RAISE EXCEPTION 'Wrong database or administrator';
    END IF;
    FOREACH target IN ARRAY ARRAY['budget_migrator', 'budget_runtime'] LOOP
        IF NOT EXISTS (
            SELECT FROM pg_roles WHERE rolname = target AND rolcanlogin
                AND NOT (rolsuper OR rolinherit OR rolcreatedb OR rolcreaterole
                         OR rolreplication OR rolbypassrls)
        ) OR EXISTS (
            SELECT FROM pg_auth_members WHERE member = target::regrole
        ) THEN
            RAISE EXCEPTION 'Unexpected attributes or memberships for %', target;
        END IF;
        IF NOT has_database_privilege(target, 'budget_local', 'CONNECT')
           OR has_database_privilege(target, 'budget_local', 'CREATE')
           OR has_database_privilege(target, 'budget_local', 'TEMPORARY')
           OR NOT has_schema_privilege(target, 'public', 'USAGE') THEN
            RAISE EXCEPTION 'Unexpected database/schema permissions for %', target;
        END IF;
    END LOOP;
    IF NOT has_schema_privilege('budget_migrator', 'public', 'CREATE')
       OR has_schema_privilege('budget_runtime', 'public', 'CREATE') THEN
        RAISE EXCEPTION 'Schema creation permissions are incorrect';
    END IF;
END
$$;

CREATE TABLE public._budget_role_probe_unrelated (value text);
INSERT INTO public._budget_role_probe_unrelated VALUES ('synthetic unrelated data');

SET LOCAL ROLE budget_migrator;
CREATE TABLE public._budget_role_probe_data (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    value text NOT NULL
);
ALTER TABLE public._budget_role_probe_data ADD COLUMN note text;
CREATE TABLE public._budget_role_probe_metadata (version bigint);
INSERT INTO public._budget_role_probe_metadata VALUES (1);
GRANT SELECT, INSERT ON public._budget_role_probe_data TO budget_runtime;
GRANT UPDATE (value) ON public._budget_role_probe_data TO budget_runtime;
GRANT USAGE ON SEQUENCE public._budget_role_probe_data_id_seq TO budget_runtime;

DO $$
BEGIN
    BEGIN
        PERFORM * FROM public._budget_role_probe_unrelated;
        RAISE EXCEPTION 'Migrator unexpectedly read unrelated administrator data';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Denied migrator access to unrelated data (42501)';
    END;
END
$$;

SET LOCAL ROLE budget_runtime;
DO $$
DECLARE inserted_id bigint; observed text; statement text;
BEGIN
    INSERT INTO public._budget_role_probe_data (value)
        VALUES ('synthetic original') RETURNING id INTO inserted_id;
    UPDATE public._budget_role_probe_data SET value = 'synthetic updated'
        WHERE id = inserted_id;
    SELECT value INTO STRICT observed FROM public._budget_role_probe_data
        WHERE id = inserted_id;
    IF observed <> 'synthetic updated' THEN
        RAISE EXCEPTION 'Runtime read/insert/update verification failed';
    END IF;
    PERFORM nextval('public._budget_role_probe_data_id_seq');
    RAISE NOTICE 'Allowed runtime SELECT, INSERT, column UPDATE and sequence USAGE';

    FOREACH statement IN ARRAY ARRAY[
        'CREATE TABLE public._budget_role_probe_forbidden (id integer)',
        'CREATE SCHEMA _budget_role_probe_forbidden',
        'CREATE TEMP TABLE _budget_role_probe_forbidden (id integer)',
        'ALTER TABLE public._budget_role_probe_data ADD COLUMN forbidden integer',
        'DROP TABLE public._budget_role_probe_data',
        'TRUNCATE public._budget_role_probe_data',
        'DELETE FROM public._budget_role_probe_data',
        'UPDATE public._budget_role_probe_data SET note = ''forbidden''',
        'SELECT setval(''public._budget_role_probe_data_id_seq'', 100)',
        'SELECT * FROM public._budget_role_probe_metadata',
        'INSERT INTO public._budget_role_probe_metadata VALUES (2)',
        'UPDATE public._budget_role_probe_metadata SET version = 2',
        'DELETE FROM public._budget_role_probe_metadata',
        'SELECT * FROM public._budget_role_probe_unrelated',
        'INSERT INTO public._budget_role_probe_unrelated VALUES (''forbidden'')'
    ] LOOP
        BEGIN
            EXECUTE statement;
            RAISE EXCEPTION 'Runtime unexpectedly allowed: %', statement;
        EXCEPTION WHEN insufficient_privilege THEN
            RAISE NOTICE 'Denied runtime operation (42501): %', statement;
        END;
    END LOOP;
END
$$;

SET LOCAL ROLE budget_migrator;
DROP TABLE public._budget_role_probe_metadata;
DROP TABLE public._budget_role_probe_data;
ROLLBACK;

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_class WHERE relname LIKE '_budget_role_probe_%') THEN
        RAISE EXCEPTION 'Permission probe objects remain';
    END IF;
END
$$;
SQL

printf '%s\n' 'Local role permission checks passed; probe transaction rolled back.'
