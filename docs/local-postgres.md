# Local PostgreSQL and Goose

This is the local setup and verification guide for steps 1–7 of [issue #5](https://github.com/finance-lab-project/budget-service/issues/5).
It starts an empty, dedicated database with separate local migration/runtime
roles provisioned explicitly below. The initial migration described by the
[storage mapping](storage.md) is now **applied locally at version 1**, with step 5
lifecycle verification and step 6 constraint/permission checks complete. Step 7
adds a focused CI workflow and verifies its database sequence locally. No legacy
data transfer is included; hosted GitHub Actions results remain pending.

## PostgreSQL

Requirements: Docker Engine/Desktop and Docker Compose with `up --wait` support.
Run all commands from the repository root.

The [Compose configuration](../compose.yaml) pins `postgres:18.6-bookworm` to the
multi-platform image index digest
`sha256:ed9475dab2438f7a8ee063fa29a0740e7fbf7185e5549db1866eb722f12b49f8`.
The digest was resolved from the official image on 2026-10-21.

The dedicated Compose project is `budget-service-local`. Its named volume is
`budget-service-local_postgres-data`; no existing database or host data directory
is mounted. PostgreSQL 18 stores its data below `/var/lib/postgresql/18/docker`,
so the volume mounts at `/var/lib/postgresql`, as specified by the
[official image documentation](https://hub.docker.com/_/postgres).

```sh
docker compose config --quiet
docker compose up -d --wait --wait-timeout 90 postgres
docker compose ps
docker compose exec -T postgres pg_isready -U budget_local -d budget_local
docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local -c 'SHOW server_version;'
```

The server must report `18.6` (possibly followed by package build details).
The host endpoint is `127.0.0.1:55432`, database/user `budget_local`, password
`budget-local-only`. These are synthetic local-only credentials. The bootstrap
user is a PostgreSQL superuser; it is not the future service runtime role.
Port 55432 must be free before starting this setup.

If a host `psql` client is installed, verify the published TCP connection with:

```sh
PGPASSWORD=budget-local-only psql -X -w -v ON_ERROR_STOP=1 -h 127.0.0.1 -p 55432 -U budget_local -d budget_local -c 'SELECT current_database(), current_user;'
```

Use the container's client for version-matched database inspection:

```sh
docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local -c "SELECT count(*) AS user_tables FROM pg_catalog.pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema');"
docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local -c "SELECT to_regclass('public.goose_db_version') IS NULL AS no_goose_metadata;"
```

Before migration application, these queries return zero user tables and
`no_goose_metadata = t`. After step 5, they return four user tables (three
application tables plus Goose bookkeeping) and `no_goose_metadata = f`.
No initialization scripts or migrations run at startup.

Stop the container while preserving its isolated volume:

```sh
docker compose stop postgres
```

Start it again with the `up` command above. Do not use volume removal or a
schema downgrade as an application rollback procedure.

## Goose

Install the separately pinned CLI using the existing Go launcher. `GOTOOLCHAIN`
and `GOBIN` apply only to this command; global Go settings and the existing
Goose binary are unchanged. The local tools directory ignores installed binaries.

```sh
GOTOOLCHAIN=go1.27.3 GOBIN="$PWD/.tools/bin" go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
.tools/bin/goose -version
GOTOOLCHAIN=go1.27.3 go version -m .tools/bin/goose
```

Expect Goose `v3.29.0`, built with Go `1.27.3`; the module build metadata must
identify `github.com/pressly/goose/v3 v3.29.0`. This version-qualified install
keeps Goose outside the service's `go.mod` and runtime dependencies. Installation
requires access to the Go module/toolchain downloads.

Always use `.tools/bin/goose` for this repository rather than a different
version on the global PATH. See the [Goose installation documentation](https://pressly.github.io/goose/installation/).

Run Goose database commands explicitly with the migration role. Even `status`
or `version` may initialize Goose's bookkeeping table on a fresh database.
The offline file-only check remains available without a database connection:

```sh
.tools/bin/goose -env none -dir migrations -v validate
```

## Local migration and runtime roles

Run the [administrative bootstrap](../scripts/local-postgres/bootstrap-roles.sql)
**once**, after starting the dedicated Compose database:

```sh
docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local < scripts/local-postgres/bootstrap-roles.sql
```

The bootstrap is transactional and requires the local administrator/database.
Existing role names cause an error and rollback; it does not replace roles or
reset their passwords. Re-run the permission checker, not the bootstrap, to
verify an already-provisioned database. This script is separate from Goose and
is not an application migration or an automatic container initialization script.

All credentials below are synthetic and local-only:

| Role | Password | Permissions in `budget_local` |
| --- | --- | --- |
| `budget_local` | `budget-local-only` | Bootstrap administrator; retains database ownership and control of the existing `public` schema |
| `budget_migrator` | `budget-migrator-local-only` | CONNECT; USAGE/CREATE in `public`; owns objects it creates and can alter/drop/grant access to them |
| `budget_runtime` | `budget-runtime-local-only` | CONNECT; USAGE in `public`; explicit application table/column/sequence grants from version 1; no object creation |

Both new roles are login roles with no memberships, inheritance, superuser,
database/role creation, replication or RLS-bypass capabilities. PUBLIC database
privileges are revoked in `budget_local`, including TEMPORARY; PUBLIC schema
privileges are revoked in its existing `public` schema. Neither new role owns
the database or schema, or has database CREATE/TEMPORARY privileges.

The role bootstrap creates no new schema or application tables. Version 1,
applied in step 5, grants runtime access to **named application tables and
sequences**, including only the required operations/columns listed in the
[storage mapping](storage.md#runtime-grants). No blanket or default table grants
are configured, so migration bookkeeping objects do not
implicitly become readable or writable by the runtime role. Receipt cleanup
permissions and workers are outside this step.

Run the permission verification with:

```sh
scripts/local-postgres/check-roles.sh
```

The checker authenticates each login over TCP using the container's PostgreSQL
client. As the actual runtime login, it verifies that assuming the migrator or
administrator role fails. It then creates generic permission probes in one
administrator-owned transaction and switches effective roles for SQL checks:

- Migrator: CREATE, ALTER, INSERT, GRANT and DROP on its own probe objects work;
  reading an unrelated administrator-owned table fails.
- Runtime: explicitly granted SELECT, INSERT, column UPDATE and sequence USAGE
  work. CREATE TABLE/SCHEMA/TEMP TABLE, ALTER, DROP, TRUNCATE, DELETE, updating an
  ungranted column and sequence `setval` fail.
- Runtime: reading/writing a migrator-owned metadata stand-in or unrelated
  administrator-owned data fails. This does not create Goose's actual table.

Every negative SQL check must raise `insufficient_privilege` (SQLSTATE `42501`);
other errors or unexpected success fail the command. Unreachable PostgreSQL
fails the check rather than skipping it. Probe tables, sequence, data and grants
roll back on success or connection exit after a failed assertion. The checker
asserts that no probe relations remain. It can be rerun without persistent data.

For a managed environment, a database administrator must separately provision
roles, credentials and permissions for the intended database/schema. Keep
administrative bootstrap outside Goose; the migration login needs schema rights
and ownership of the objects it creates, not superuser or role-creation rights.
Use separately managed credentials there, never these local examples. These
local checks establish database-role separation, not application user isolation
or verified managed-environment permissions.

## Local migration lifecycle (step 5)

The [lifecycle checker](../scripts/local-postgres/check-migration-lifecycle.sh)
performs the initial application, Goose version/status checks, a second `up`
with no schema/metadata changes, and a controlled transactional failure:

```sh
scripts/local-postgres/check-migration-lifecycle.sh
```

It requires the running pinned Compose service, the existing roles and an
**empty** local database. It intentionally refuses the now-migrated database
before changing anything; it never deletes or resets a database. Reproducing
initial application requires a separately prepared empty isolated environment.
Do not remove this database or its volume just to rerun the checker.

The failing version 2 exists only in a temporary directory alongside a copy of
version 1. It creates and populates a synthetic probe table before deliberately
raising an error. The checker requires the intended error, absence of the probe,
no version 2 record, and unchanged schema/version snapshots. The temporary
fixture is removed on exit. It never writes into the real migration directory,
applies a downgrade or tests the broader step 6 constraints.

To inspect the current database's recorded version, use migration credentials:

```sh
.tools/bin/goose -env none -dir migrations -table public.goose_db_version \
  postgres 'host=127.0.0.1 port=55432 dbname=budget_local user=budget_migrator password=budget-migrator-local-only sslmode=disable connect_timeout=5' version
```

The step 5 run completed successfully and left version 1 applied with empty
application tables. Exact outcomes and the applied migration checksum are in
[the storage evidence](storage.md#step-5-migration-lifecycle-evidence--2026-10-21).

## Local schema and runtime checks (step 6)

Run the [schema checker](../scripts/local-postgres/check-schema.sh) against the
running, migrated local database with the existing roles:

```sh
scripts/local-postgres/check-schema.sh
```

It tests all amount columns, owner/month and identity constraints, receipt
identity/JSON/metadata/deadline storage, and actual runtime grants. It requires
schema version 1 and empty application tables; an unavailable database fails
the check. It does not run migrations or reset existing data. Test rows and the
unrelated-data fixture roll back. Owner identity sequence allocations remain,
so repeated checks leave ordinary sequence gaps without leaving test rows.

The runtime cases use the actual password-authenticated TCP login. Only the
unrelated administrator-owned fixture uses `SET LOCAL ROLE budget_runtime`
inside its creating transaction, avoiding any committed fixture. Full schema
and Goose record comparisons verify that the checks leave those unchanged.
See [step 6 evidence](storage.md#step-6-constraint-and-privilege-evidence--2026-10-21)
for observed results and boundaries. The focused CI workflow below reuses these
checks.

## Focused storage CI (step 7)

The [storage workflow](../.github/workflows/storage.yml) runs on relevant pull
requests, pushes to `main`, or manual dispatch. It uses Ubuntu 24.04, Go 1.27.3,
Goose v3.29.0 and the same digest-pinned PostgreSQL 18.6 Compose image. Checkout
and Go setup actions are pinned to commit SHAs, with read-only repository
permissions and a 20-minute job timeout. The existing
[contract workflow](../.github/workflows/contract.yml) and its triggers are unchanged.

The job runs Go tests/vet, installs the separate Goose CLI, starts a fresh
Compose project, bootstraps roles, and executes the role, migration lifecycle
and schema checkers above. Startup waits for health; a missing database fails
the check. Failure logs are printed and `always()` cleanup removes only the
job's disposable Compose project and volume. The workflow does not deploy or
run migrations from application startup.

`COMPOSE_PROJECT_NAME` selects the separate project and volume.
`BUDGET_POSTGRES_PORT` selects its loopback-only host port; the default remains
55432 for the existing local setup. CI uses 55433. The lifecycle checker uses
the selected port for Goose as well as the Compose target check.

For a local reproduction with the pinned Goose CLI installed, choose an unused
project name and free port, then run this sequence from the repository root:

```sh
(
  set -euo pipefail
  export COMPOSE_PROJECT_NAME=budget-service-ci-check
  export BUDGET_POSTGRES_PORT=55433
  trap 'docker compose down --volumes' EXIT
  docker compose config --quiet
  docker compose up -d --wait --wait-timeout 90 postgres
  docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local < scripts/local-postgres/bootstrap-roles.sql
  scripts/local-postgres/check-roles.sh
  scripts/local-postgres/check-migration-lifecycle.sh
  scripts/local-postgres/check-schema.sh
)
```

This creates and removes a separate disposable database; it does not reset the
existing `budget-service-local` database. The recorded local run used project
`budget-service-ci-step7-20261021` on port 55433. See
[step 7 evidence](storage.md#step-7-ci-and-regression-verification--2026-10-21)
for completed results and the hosted-CI limitation.

## Observed steps 1–2 verification — 2026-10-21

Setup started from Budget Service `main` at
`8268021eb2c42a398a5a4ad377919a5de8257878`.
Docker Engine/client were 29.4.0 and Compose was v5.0.2.
The installation and verification commands above completed successfully:

| Check | Observed result |
| --- | --- |
| `docker compose config --quiet` | Exit 0; configuration valid |
| `docker compose up -d --wait --wait-timeout 90 postgres` | Exit 0; new dedicated network, volume and healthy container created |
| Container `pg_isready` command above | Exit 0; accepting connections |
| `SHOW server_version;` | `18.6 (Debian 18.6-1.pgdg12+2)` |
| `SHOW server_version_num;` through the same container `psql` command | `180006` |
| Host TCP `psql` command above | Exit 0; database and user both `budget_local` |
| User-table count / Goose metadata queries above | `0` / `t`; no application tables or Goose bookkeeping table |
| `docker inspect budget-service-local-postgres-1` (image, health, ports and mounts) | Pinned image reference, healthy state, only `127.0.0.1:55432`, dedicated named volume at `/var/lib/postgresql` |
| `.tools/bin/goose -version` | `goose version: v3.29.0` |
| `GOTOOLCHAIN=go1.27.3 go version -m .tools/bin/goose` | Built with `go1.27.3`; Goose module `v3.29.0`, `darwin/arm64` |
| `goose -version` on the existing PATH | Still `v3.26.0` |
| `GOTOOLCHAIN=local go version` | Still `go1.26.2 darwin/arm64` |
| `git check-ignore -v .tools/bin/goose` | Binary ignored by `.tools/.gitignore` |
| `shasum -a 256 -c docs/evidence/issue-3/SHA256SUMS` | All existing contract artifact hashes matched |

No Goose database command was executed. At this checkpoint, schema migrations,
constraint tests and CI database verification were unimplemented and untested.
Role verification for step 3 is recorded separately below; step 4 authoring and
offline checks are recorded in the [storage mapping](storage.md).
Go domain tests and the contract validator were not rerun for this local tooling
setup; their source and existing contract artifacts are unchanged.


## Observed step 3 verification — 2026-10-21

| Command / check | Observed result |
| --- | --- |
| `docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local < scripts/local-postgres/bootstrap-roles.sql` | Exit 0; both roles created and grants committed |
| `bash -n scripts/local-postgres/check-roles.sh` | Exit 0 |
| `scripts/local-postgres/check-roles.sh` | Exit 0; both TCP logins worked, permitted operations succeeded, all expected denials raised `42501`, probe transaction rolled back |
| Post-check database/schema privileges | Both roles have CONNECT and schema USAGE; only migrator has schema CREATE; neither has database CREATE/TEMPORARY |
| Post-check ownership | Database remains owned by `budget_local`; `public` remains owned by `pg_database_owner` |
| Post-check catalog queries below | Zero user tables, public relations and custom schemas; Goose metadata absent |

The final catalog inspection used these read-only queries through the container's
administrator `psql` connection:

```sql
SELECT count(*) AS user_tables FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema');
SELECT count(*) AS public_relations FROM pg_class
WHERE relnamespace = 'public'::regnamespace;
SELECT count(*) AS custom_schemas FROM pg_namespace
WHERE nspname NOT IN ('public', 'information_schema') AND nspname !~ '^pg_';
SELECT to_regclass('public.goose_db_version') IS NULL AS no_goose_metadata;
```

Runtime data privileges existed only on rolled-back probes. Actual application
object grants must accompany the future schema. No application schema, Goose
migration or metadata table was created. No managed database, application user
isolation, migration execution or Go runtime behavior was tested in this step.
