# Budget storage mapping

Status: the initial migration for [issue #5](https://github.com/finance-lab-project/budget-service/issues/5),
revision 1, was **applied to the isolated local database in step 5**. Version,
no-op and transactional rollback checks passed. Step 6 local constraint and
application-object privilege checks also passed. Step 7 adds the focused CI job;
its database sequence, Go tests and contract validator passed locally. Hosted CI
and review status are tracked on [draft PR #6](https://github.com/finance-lab-project/budget-service/pull/6);
maintainer acceptance remains pending. This document follows the accepted
[OpenAPI](../api/openapi.yaml), [semantic contract](api-v1-contract.md) and
`internal/budget` domain at baseline `8268021eb2c42a398a5a4ad377919a5de8257878`.

The initial [Goose migration](../migrations/00001_initial_budget_schema.sql)
creates objects in the dedicated Budget database's existing `public` schema.
Start from an empty database: there is no legacy transfer, import, backfill or
upgrade from an old Finance Lab database.

## Versions and prerequisites

- PostgreSQL 18.6, `postgres:18.6-bookworm`, pinned to image index digest
  `sha256:ed9475dab2438f7a8ee063fa29a0740e7fbf7185e5549db1866eb722f12b49f8`.
- Goose v3.29.0 as a separate CLI; service Go baseline remains 1.27.3.
- [Local setup and administrative role bootstrap](local-postgres.md) provide
  `budget_migrator` and `budget_runtime` before any migration is run.
- The migration role needs USAGE/CREATE in `public`; it owns the objects it
  creates. The migration does not create roles, a database, a schema or extensions.
- For managed PostgreSQL 18, administrators must provision equivalent roles and
  schema permissions separately. Actual managed minor version and permissions
  require later verification; local checks do not prove deployment compatibility.

## Source amounts

`public.budget_source_amount` is an **unconstrained NUMERIC domain**, nullable by
default. Separate checks reject NaN/infinities, require `0 <= value < 10^18`,
and require `value = trunc(value, 6)`. The largest permitted value is
`999999999999999999.999999`. No column uses a scale typmod, floating point,
rounding assignment or a zero default.

The truncation result is compared with the original value; it is never stored
in place of invalid input. A declared `NUMERIC(24,6)` could round excess precision
before a constraint sees it. [PostgreSQL numeric documentation](https://www.postgresql.org/docs/18/datatype-numeric.html)

SQL NULL means unplanned; numeric zero means planned zero. Equivalent numeric
spellings such as `1.0000000` may pass these value checks. Canonical Money wire
spelling, explicit Money currency and cross-field currency agreement are checked
by Go/HTTP before conversion. The future adapter must preserve decimal values
without floats and return canonical amount strings via the domain.

## Owners

| Column / index | Mapping and constraint |
| --- | --- |
| `id` | Internal `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY` |
| `issuer_bytes` | Required nonempty BYTEA; UTF-8 bytes of the verified decoded `iss` |
| `subject_bytes` | Required nonempty BYTEA; UTF-8 bytes of the verified decoded `sub` |
| `budget_owners_identity_digest_key` | Unique expression index on `(sha256(issuer_bytes), sha256(subject_bytes))` |

The schema checks nonempty bytes; the future identity boundary supplies UTF-8
from verified claims. It must not trim, case-fold, normalize Unicode or URLs,
match email, or require a UUID subject. The internal ID is never a caller's
owner selector. Original bytes remain stored without a raw-text B-tree index
or an artificial claim-length limit to fit index entries.

PostgreSQL's [built-in SHA-256](https://www.postgresql.org/docs/18/functions-binarystring.html)
returns fixed-size digests; no hashing extension is required. A digest match is
only a lookup candidate. Future repository code must compare **both** original
byte strings before using the owner. A mismatch must fail closed with
`INTERNAL_ERROR`, exposing no other owner's data and making no write. A unique
index violation alone does not establish that two identities are equal.

## Monthly budgets

| Column / key | Mapping and constraint |
| --- | --- |
| `owner_id` | Required BIGINT FK to `budget_owners.id` |
| `month` | Required `TEXT COLLATE "C"`; exactly seven ASCII bytes, `YYYY-MM`, year 0001–9999 and month 01–12 |
| `(owner_id, month)` | Primary key: one budget per owner/month |
| `revision` | Required positive BIGINT, through `9223372036854775807`; maps to API `version` as a decimal string |
| `currency` | Required `TEXT COLLATE "C"`, restricted to RUB/USD/EUR |
| `income`, `investments` | Nullable `budget_source_amount`; respective Money amount or SQL NULL |

All spending columns use the same nullable source-amount domain. The API
`spending` category IDs map as follows:

| API category ID | SQL column |
| --- | --- |
| `groceries` | `groceries` |
| `restaurants` | `restaurants` |
| `utilities` | `utilities` |
| `transport` | `transport` |
| `household` | `household` |
| `health` | `health` |
| `personal-care` | `personal_care` |
| `clothing` | `clothing` |
| `subscriptions` | `subscriptions` |
| `education` | `education` |
| `tech` | `tech` |
| `culture` | `culture` |
| `entertainment` | `entertainment` |
| `gifts` | `gifts` |
| `travel` | `travel` |

The budget currency supplies the currency of every populated amount on reads.
There are no authoritative total columns; Go derives totals. Both owner FKs
use PostgreSQL's default NO ACTION behavior, with no cascading deletion.

## Successful save receipts

| Column | Mapping and constraint |
| --- | --- |
| `owner_id` | Required BIGINT FK to `budget_owners.id` |
| `idempotency_key` | Required `TEXT COLLATE "C"`; 1–128 ASCII characters from `[A-Za-z0-9._:-]` |
| `key_digest` | Required 32-byte BYTEA; CHECK equality to SHA-256 of the original key's UTF-8 bytes |
| `request_binding` | Required JSONB object containing the complete validated request binding |
| `response_status` | Required SMALLINT, 200 or 201 only |
| `response_body` | Required JSON object containing the complete original serialized Budget response |
| `response_generation` | Required `TEXT COLLATE "C"`; original generation, 1–128 ASCII `[A-Za-z0-9_-]` |
| `creation_location` | Nullable `TEXT COLLATE "C"`; nonempty for 201, SQL NULL for 200 |
| `delete_after` | Nullable TIMESTAMPTZ, default NULL; no transaction-start timestamp default |

The primary key `(owner_id, key_digest)` enforces owner-wide key uniqueness
across all months and generations. The supplied digest cannot disagree with
the original key. Future replay logic must still compare the original key and
the full binding; a digest collision must fail closed rather than replay an
unrelated receipt.

The binding has `apiMajor` (v1), `method` (PUT), `month`, `generation`, and `body`.
The body includes the complete validated `expectedVersion`, `currency`, `income`,
15-category `spending`, and `investments`, preserving exact strings and nulls.
JSONB permits structural equality independent of object order, insignificant
whitespace and equivalent escapes. It does not preserve evidence of duplicate
decoded keys, which must be rejected before storage.

JSON preserves the original response text, including Money/revision strings,
nulls and computed totals. Replay must return that historical snapshot rather
than rebuild it from the current budget or recalculate its totals. Do not
convert it to JSONB on retrieval. [PostgreSQL JSON types](https://www.postgresql.org/docs/18/datatype-json.html)

The database rejects absent receipt columns and non-object JSON, including
JSON null. It does **not** duplicate OpenAPI validation inside JSON columns:
complete inner fields, canonical strings, arithmetic, generation agreement and
the correct `/api/v1/budgets/{month}` Location are the future writer's
responsibility. No bearer tokens, arbitrary incoming headers, stored
`Idempotency-Replayed` flag or JSON search indexes are added.

## Runtime grants

The migration grants only the following to the pre-existing `budget_runtime`:

| Object | Granted operations |
| --- | --- |
| `budget_owners` | SELECT; INSERT of issuer/subject bytes only |
| `budget_owners_id_seq` | USAGE, without sequence UPDATE/setval |
| `monthly_budgets` | SELECT, INSERT; UPDATE of revision and the 17 source-amount columns only |
| `budget_save_receipts` | SELECT; INSERT of required snapshot fields and Location, excluding `delete_after`; UPDATE of `delete_after` only |

The runtime role cannot replace an owner's identity, update a monthly owner,
month or currency, or rewrite a saved response/binding. Receipt inserts use
the NULL deadline default. DELETE, TRUNCATE, schema ownership/CREATE, grant
options and Goose bookkeeping access are not granted. There are no blanket
or default table grants; keep administrative role setup separate from Goose.

These grants do not implement request ownership or save semantics. Repository
code must validate identity and immutable currency, enforce expected-version
ordering, increment revisions safely and return `BUDGET_VERSION_EXHAUSTED` at
the limit. The grants were applied in step 5 and tested on the actual application objects
in step 6 using the runtime login. Step 3 tested generic disposable probes.

## Transaction, retention and recovery boundaries

Future saves must commit the budget mutation, revision and complete successful
receipt in **one transaction**, acknowledging only after commit. No durable
pending receipt is represented here. Schema constraints do not prove that a
future writer follows this protocol.

New receipts have `delete_after = NULL`. A subsequent transaction, after
observing a committed receipt, may set an unset deadline to that observation
time plus 24 hours. The writer must condition the update on the deadline still
being NULL; replay must not extend a populated deadline. NULL protects
unanchored receipts from cleanup. Do not anchor retention at the original
transaction's start. There is no anchoring or cleanup worker, timer trigger,
retention index or tested retention-duration guarantee in this change.

Receipt generation is historical request/response data. The authoritative
current generation remains outside the restored Budget database; there is no
current-generation table here. Writer fencing, generation distribution and
restore procedures remain separate work.

## Migration execution and rollback policy

The file uses Goose's default transaction, with no explicit transaction control
or transaction opt-out. Its domain, tables, index and grants belong to the same
Up migration. The Down section raises an exception: it must neither delete
Budget data nor silently mark the version unapplied while leaving objects.
[Goose transaction annotations](https://pressly.github.io/goose/documentation/annotations/)

When separately authorized, the controlled sequence is: verify the dedicated
target and administrative prerequisites, explicitly run the pinned Goose CLI
with migration credentials, inspect the recorded schema version and constraints,
then deploy only an application compatible with that schema. Do not migrate
from application startup. No migration command has been executed in step 4.

After a failed transactional migration, inspect the error and recorded state
before retrying. Once a migration is applied, preserve it and use a new numbered
migration for changes. Future deployments must state which schema versions the
new and rollback application revisions support. Prefer a forward repair;
application rollback is allowed only while its schema compatibility holds.
Recreating an explicitly disposable test database is not production rollback.

## Step 4 verification limits

Only offline file validation is authorized in this step. Goose migration
parsing does not execute PostgreSQL or prove constraint/grant behavior. Initial
application, version/no-op checks, controlled migration failure, real SQL
positive/negative tests and CI execution remain for subsequent authorized steps.


Observed on 2026-10-21:

| Check | Result |
| --- | --- |
| `.tools/bin/goose -env none -dir migrations -v validate` | Exit 0; one transactional SQL migration, 12 Up statements and one Down statement |
| `pglast` v8.7, PostgreSQL parser 18.6: `parse_sql` on both sections and `parse_plpgsql` on Down | Parsed successfully without a database connection |
| Offline AST comparison with `SpendingCategoryIDs()` and this mapping | All 15 categories match; all 17 amount columns are nullable with no defaults or typmods; no total columns |
| Offline grant inspection | Only named application tables/sequence target `budget_runtime`; no grant options, DELETE, TRUNCATE or Goose bookkeeping grants |

The parser was used as temporary verification tooling, not added to the service
or its dependency files. Its syntax check can be reproduced independently:

```sh
uv run --no-project --with pglast==8.7 python - <<'PYTHON'
from pathlib import Path
from pglast import parse_sql, parse_plpgsql
sql = Path("migrations/00001_initial_budget_schema.sql").read_text()
up, down = sql.split("-- +goose Down", 1)
print("Up statements:", len(parse_sql(up)))
print("Down statements:", len(parse_sql(down)))
parse_plpgsql(down)
PYTHON
```

Neither parser resolves database objects/functions or checks effective grants.
No database connection, migration application, schema-version inspection or
positive/negative database test was performed in step 4. Go code and the frozen
API artifacts are unchanged; their test suites were not rerun for this authoring
step. The issue is not complete or accepted on the basis of these offline checks.


## Step 5 migration lifecycle evidence — 2026-10-21

Applied to the existing isolated `budget_local` database only, as the
non-superuser `budget_migrator`, using PostgreSQL 18.6 and Goose v3.29.0.
The database had no user tables, amount domain or Goose metadata before this run.
The migration file was not changed during application:

```text
00001_initial_budget_schema.sql
SHA-256: 8c699ae84ac1f124f6412e0a86f18b1503f0adac5f1842c8db2125227fa60b36
```

Exact verification commands:

```sh
bash -n scripts/local-postgres/check-migration-lifecycle.sh
scripts/local-postgres/check-migration-lifecycle.sh
shasum -a 256 migrations/00001_initial_budget_schema.sql
```

| Check | Observed result |
| --- | --- |
| Initial `up` from the real `migrations/` directory | Succeeded; domain, application tables/indexes/identity sequence and Goose objects owned by `budget_migrator` |
| Goose `version` / `status` | Version 1; initial migration marked applied |
| Version records | Exactly baseline `0, true` and migration `1, true` |
| Second `up` | Reported no migrations to run; full schema-only dump and complete version records, including timestamps, unchanged |
| Controlled version 2 in a temporary migration directory | CREATE TABLE and INSERT completed, then intentional `RAISE EXCEPTION` failed with exit 1 and SQLSTATE `P0001` |
| State after failure | Probe table absent; zero version 2 records; schema dump and version records unchanged; Goose still reports version 1 |
| Application data | All three application tables remain empty |
| Checker exit | 0; temporary fixture directory removed |

The [lifecycle checker](../scripts/local-postgres/check-migration-lifecycle.sh)
contains the exact Goose, catalog-query and dump-comparison commands. It copies
the initial migration into a temporary directory and adds the failing fixture
there; the real migration directory still contains only version 1. Schema dumps
include definitions, ownership and grants and are compared byte-for-byte. The
fixed `pg_dump --restrict-key` makes test output repeatable; dumps are never
executed. See [pg_dump testing options](https://www.postgresql.org/docs/18/app-pgdump.html).

This one-time check requires an empty local database and never resets an
existing database. It leaves the initial schema applied and does not run Down.
The rollback test proves rollback of the failed migration's DDL/DML and absence
of a falsely recorded success; it does not test downgrade or application rollback.

No step 6 amount/identity/receipt constraint cases, real-object runtime privilege
suite, repository behavior or CI checks were run. Go code and the frozen API
artifacts are unchanged; their suites were not rerun. Issue-wide acceptance
remains pending.


## Step 6 constraint and privilege evidence — 2026-10-21

The [schema checker](../scripts/local-postgres/check-schema.sh) executes
[SQL cases](../scripts/local-postgres/check-schema.sql) against the existing
isolated PostgreSQL 18.6 database at schema version 1. It requires empty
application tables, refuses to reset data, and fails on database errors rather
than skipping checks. It does not apply or modify migrations.

```sh
bash -n scripts/local-postgres/check-schema.sh
scripts/local-postgres/check-schema.sh
shasum -a 256 migrations/00001_initial_budget_schema.sql
```

The completed checker exited 0. All 284 expected failures in the runtime-login
suite matched their required SQLSTATE, and four unrelated-data operations were
separately denied with `42501`. Unexpected success or a different error fails
the check; PL/pgSQL assertions are explicitly enabled.

| Area | Observed result |
| --- | --- |
| Owner identities | 12 distinct pairs retained exact bytes, including issuer/subject case, whitespace, URL slash and composed/decomposed Unicode differences; same subject under different issuers, non-UUID subjects and 16 KiB synthetic claims accepted; every duplicate rejected; missing, NULL and empty bytes rejected |
| Monthly keys | Foreign key and owner/month uniqueness enforced; separate owners shared a month; `0001-01` and `9999-12`, all three currencies and revisions 1 through the BIGINT maximum accepted; malformed/non-ASCII months, unsupported currencies, NULL required fields, nonpositive revisions and overflow rejected |
| All 17 source amounts | NULL stayed distinct from zero; zero, one millionth and `999999999999999999.999999` round-tripped exactly; negative, out-of-range, excess-precision, NaN and both infinity values rejected without rounding; equivalent numeric spelling `1.0000000` accepted |
| Column mapping | Live catalog contains exactly the four budget key/metadata columns and 17 nullable domain columns with no amount defaults or typmods and no totals; all 15 categories were also compared with `SpendingCategoryIDs()`, including `personal-care` → `personal_care` |
| Receipt identity | Owner-wide duplicate key rejected even when month/generation changed; independent owners and case-distinct keys accepted; ASCII grammar/length boundaries enforced; wrong/short/long digests and orphan owners rejected |
| Required receipt data | Omitted and SQL NULL required fields, non-object JSON including JSON null, non-success statuses, invalid generations and invalid 200/201 Location combinations rejected |
| Binding and historical response | Complete binding and original JSON text round-tripped with exact amount/revision strings and nulls; reordered/escaped JSON compared structurally equal, changed version/value/month/generation/currency or null versus zero compared unequal; changing the budget left the stored response text and metadata unchanged |
| Deadline | New receipts defaulted to NULL; an explicitly updated timestamp preserved its instant and microseconds across UTC offsets |
| Runtime operations | Actual TCP runtime login performed SELECT/INSERT on the application tables, generated owner IDs, sequence `nextval`, all permitted budget column updates and deadline UPDATE |
| Runtime denials | DDL, role escalation, DELETE/TRUNCATE, owner identity changes, monthly owner/month/currency updates, receipt snapshot rewrites, deadline INSERT, sequence reset and Goose table/sequence access denied with `42501`; grant-option checks passed |
| Unrelated data | An administrator-owned table was created inside a separate transaction; effective runtime SELECT/INSERT/UPDATE/DELETE all failed with `42501`, then the fixture transaction rolled back |
| Final database state | Three application tables empty; unrelated fixture absent; full schema-only dump and complete Goose version records unchanged at version 1 |

Both SQL sessions roll back all fixture rows/objects, including on an assertion
failure when the connection closes. Owner sequence values consumed by identity
inserts and `nextval` remain allocated: PostgreSQL does not roll those back.
The checker never rewinds the sequence. See the
[PostgreSQL sequence documentation](https://www.postgresql.org/docs/18/functions-sequence.html).
The migrated SQL file retains the step 5 SHA-256 above.

This verifies storage constraints and effective local database grants, not
application-level user isolation. Verified JWT claims, collision handling,
request validation inside JSON, transactional save/replay, safe version
increments, post-commit retention anchoring, cleanup and recovery remain later
runtime work. The timestamp test does not establish retention duration or replay
immutability of an already populated deadline. Step 7, CI and delivery were not
started; Go/domain and contract suites were not rerun because their source is
unchanged. Issue-wide acceptance remains pending.


## Step 7 CI and regression verification — 2026-10-21

Added [Budget PostgreSQL storage](../.github/workflows/storage.yml), scoped to
storage tooling/migrations, Go source/module and storage documentation changes.
It runs the existing database checkers plus Go tests/vet using PostgreSQL 18.6
at the pinned digest, Goose v3.29.0 and Go 1.27.3. Each CI run/attempt uses a
separate Compose project. The job prints database logs on failure and always
removes its disposable resources. Port selection is configurable for testing
beside the established local database; its default remains unchanged.

The existing contract workflow, Python/dependency pins, contract checker,
manifest and all frozen artifacts remain byte-for-byte unchanged. Contract
validation remains in its existing focused job.

Observed local checks (not a hosted GitHub Actions run):

| Command / check | Result |
| --- | --- |
| `go version` / `.tools/bin/goose -version` | Go 1.27.3 on darwin/arm64; Goose v3.29.0 |
| `GOTOOLCHAIN=go1.27.3 go test -count=1 ./...` | Exit 0; all Go domain tests passed without using cached test results |
| `GOTOOLCHAIN=go1.27.3 go vet ./...` | Exit 0; no diagnostics |
| `python scripts/check_contract.py` with Python 3.14.5 and `requirements-contract.txt` | Exit 0; manifest integrity, 35 OpenAPI examples, 63 positive/183 negative schema cases, 6 positive/5 negative consistency cases, 27 positive/12 negative response cases and 17 catalog anchors passed; raw/state inventories checked only |
| `actionlint -color .github/workflows/storage.yml .github/workflows/contract.yml` using temporary actionlint v1.7.7 | Exit 0; no workflow diagnostics |
| `bash -n scripts/local-postgres/check-migration-lifecycle.sh scripts/local-postgres/check-schema.sh scripts/local-postgres/check-roles.sh` | Exit 0 |
| `check-schema.sh` before the disposable PostgreSQL service existed | Exit 1 with `service "postgres" is not running`; no passing skip |
| Fresh database sequence documented in the local guide | Exit 0 using project `budget-service-ci-step7-20261021` and port 55433; roles verified, initial migration applied, second `up` unchanged, controlled migration failure fully rolled back |
| `check-schema.sh` on that fresh migrated database | Exit 0; positive storage/permission cases, 284 expected negative cases and four unrelated-data denials passed; empty application tables, unchanged schema and Goose records at version 1 |
| Disposable project cleanup | Container, network and volume removed; established local database was not stopped or reset |

Go used a temporary writable build cache. Contract packages and actionlint were
installed into temporary locations, without adding service dependencies or
changing global toolchain settings. PostgreSQL verification ran in the pinned
Linux container; Go/Goose and orchestration ran on the local macOS arm64 host.
This does not establish a successful Ubuntu GitHub Actions run. The new job
must execute after publication, and its authoritative check result must be
inspected then. No hosted run, PR, deployment or step 8 acceptance work was
started. Runtime adapter/auth/concurrency/replay and retention/recovery behavior
remain outside these checks.


## Draft delivery review (step 8)

[Draft PR #6](https://github.com/finance-lab-project/budget-service/pull/6)
publishes the implementation for issue #5. Implementer review against issue
revision 1 found no in-scope blockers in the migration, storage mapping, grants,
verification scripts or workflow. This is not an independent review or maintainer
acceptance, and the PR remains draft and unmerged.

The implementation and locally verified artifacts are committed in
[ba8ce45](https://github.com/finance-lab-project/budget-service/commit/ba8ce45d4f35e1aab56b474544f7bcd66538859e).
This delivery note does not change those SQL/tooling artifacts. The migration
checksum recorded in step 5 and the frozen contract artifact hashes are preserved.
See the PR's verification section and GitHub Actions checks for completed hosted
results tied to the delivered revision. The contract workflow is dispatched
explicitly on the PR branch because its unchanged path filters exclude these
storage-only changes.

The internal owner ID identifies a storage row; a digest hit still requires
comparison of both original identity byte strings by the future repository.
Unconstrained NUMERIC checks validate the original value without rounding it
into range. JSONB stores structural request bindings while JSON retains the
historical response text. A NULL receipt deadline protects an unanchored receipt;
post-commit anchoring and replay/cleanup behavior remain later runtime work.
