\set ON_ERROR_STOP on

-- One-time administrative setup for the dedicated local Compose database.
-- These synthetic credentials must not be used outside this local environment.
BEGIN;

DO $$
BEGIN
    IF current_database() <> 'budget_local' OR current_user <> 'budget_local' THEN
        RAISE EXCEPTION 'Run only as budget_local in the isolated budget_local database';
    END IF;
END
$$;

CREATE ROLE budget_migrator LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE
    NOREPLICATION NOBYPASSRLS PASSWORD 'budget-migrator-local-only';
CREATE ROLE budget_runtime LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE
    NOREPLICATION NOBYPASSRLS PASSWORD 'budget-runtime-local-only';

-- Keep database/schema ownership with the bootstrap administrator.
-- PUBLIC must not give either login CREATE or TEMPORARY database privileges.
REVOKE ALL ON DATABASE budget_local FROM PUBLIC;
GRANT CONNECT ON DATABASE budget_local TO budget_migrator, budget_runtime;

-- Use only the already-existing public schema; create no application objects.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO budget_migrator;
GRANT USAGE ON SCHEMA public TO budget_runtime;

-- Deliberately no blanket table/sequence grants or default privileges:
-- future migrations must grant access to named application objects only.
COMMIT;
