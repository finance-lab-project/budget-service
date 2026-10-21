# Go PostgreSQL read adapter

[`internal/postgres`](../internal/postgres/doc.go) implements existing-owner
lookup and monthly reads for [issue #7](https://github.com/finance-lab-project/budget-service/issues/7).
It uses pgx **v5.11.0** and native `pgxpool`; the `budget` domain remains independent
of database packages. The caller supplies an already verified and authorized
identity. Accepting issuer/subject strings does not authenticate a request.

## API and pool ownership

`Open(ctx, runtimeDSN, connectTimeout)` creates an owned pool and verifies it with
`Ping`. The connection string must be nonempty and the timeout positive. An
earlier caller cancellation/deadline or a tighter DSN `connect_timeout` is
respected. Failed initialization returns no Store and starts cleanup; driver
socket cleanup can finish after the call returns. The successful Store outlives
its initialization context.

In a service, open one Store at startup, reuse it for requests, and call `Close`
after in-flight requests finish. `Close` waits for acquired connections and is
safe to repeat. The zero-value Store is not ready for reads. This single-read
example shows the complete lifetime; it can be used from inside this Go module:

```go
package example

import (
	"context"
	"time"

	"github.com/finance-lab-project/budget-service/internal/postgres"
)

func readOnce(ctx context.Context, runtimeDSN, verifiedIssuer, verifiedSubject, month string) (*postgres.MonthlyBudget, error) {
	store, err := postgres.Open(ctx, runtimeDSN, 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return store.ReadMonthly(readCtx, verifiedIssuer, verifiedSubject, month)
}
```

`ReadMonthly(ctx, issuer, subject, month)` rejects empty or invalid UTF-8 identity
values and invalid months before querying. Months use `YYYY-MM`, years 0001–9999
and months 01–12. Other identity bytes are preserved exactly: no trimming, case
folding, Unicode normalization or UUID requirement.

No migration or Goose bookkeeping query runs through the runtime pool. Do not
log connection strings, credentials, private budget values or unwrapped driver
errors. The adapter's displayed errors omit those driver details while retaining
their causes for inspection.

## SQL and result mapping

The read performs at most two parameterized queries:

1. Select the owner candidate using `sha256(issuer_bytes)` and
   `sha256(subject_bytes)`. Compare **both original byte strings** before using
   its internal ID. A mismatching candidate is an integrity failure, not absence.
2. Select the monthly row by that resolved owner ID and requested month. One
   explicit `SELECT` loads month, revision, currency and all 17 planned amounts.

The [storage column mapping](storage.md#monthly-budgets) is authoritative for
column names. `income` and `investments` map to their respective planned fields;
the 15 spending columns map to the complete `Planned.Spending` map. SQL
`personal_care` becomes domain ID `personal-care`; the other spending IDs match
their column names. NULL entries remain present in the map.

| Result | Meaning |
| --- | --- |
| `Month` | Canonical stored month |
| `Revision` | Positive `int64`, including `9223372036854775807` |
| `Planned.Currency` | RUB, USD or EUR for every populated amount |
| Planned `*budget.Money` | `nil` for SQL NULL; non-nil Money for planned zero |
| `Totals` | Income, spending, investments and remaining from `budget.CalculateTotals` |

Unconstrained `::numeric` casts expose amounts as `pgtype.Numeric`. Conversion
uses the integer coefficient and exponent to derive exact millionths, requiring
a zero remainder when division is needed. No floats, rounding or clamping are
used. Stored `1.0000000` becomes canonical `"1"`; the incoming API parser stays
strict. Nonfinite, negative, oversized, malformed or excess-precision stored
values fail without a partial result.

Stored metadata and every amount are validated before the result is returned.
Totals can exceed the source limit of 18 integer digits; remaining may be
negative. Use Money's `Amount()` and `Currency()` methods to read its value.
Eventual HTTP serialization must encode revision as a decimal string, for
example with `strconv.FormatInt(result.Revision, 10)`.

Every failed read returns a nil result. Missing owners/months create no data;
successful and failed reads do not modify owners, budgets, receipts or the owner
sequence. Internal owner IDs and driver numeric types do not escape the adapter.

## Errors

Inspect adapter categories with `errors.Is`; use `errors.As` for typed causes.

| Category | Meaning |
| --- | --- |
| `postgres.ErrInvalidInput` | Invalid connection configuration, identity or month; month failures also wrap `budget.ErrInvalidMonth` |
| `postgres.ErrNotFound` | Owner or monthly row absent |
| `postgres.ErrIdentityIntegrity` | Digest lookup candidate has different original identity bytes |
| `postgres.ErrInvalidStoredData` | Decoded data cannot form a valid complete budget; domain causes remain inspectable |
| `postgres.ErrDatabase` | Connection, query, permission, cancellation or deadline failure |

Operational failures do not become not-found results. For example,
`errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)`
work through wrapping; `errors.As(err, &pgErr)` can inspect a `*pgconn.PgError`.
HTTP status mapping, JWT verification, owner creation, budget/receipt writes and
deployment remain outside this adapter.

## Unit tests

Run from the repository root:

```sh
GOTOOLCHAIN=go1.27.3 go test -count=1 ./...
GOTOOLCHAIN=go1.27.3 go vet ./...
```

Ordinary tests need no PostgreSQL or integration configuration. Pool tests use
loopback TCP listeners. Unit tests include an injected mismatching owner
candidate; they do not claim a real SHA-256 collision.

## Disposable integration run

Install the pinned [Goose v3.29.0 CLI](local-postgres.md#goose) at `.tools/bin/goose`
and have Docker Compose available. Run the following **Bash** block from the
repository root, choosing a free host port. It creates a separate project and
volume using the pinned PostgreSQL 18.6 image, then removes that project on exit.
The passwords below are synthetic credentials for this isolated local target.

```bash
(
  set -euo pipefail
  export GOTOOLCHAIN=go1.27.3
  export COMPOSE_PROJECT_NAME="budget-service-adapter-check-$(date +%s)-$$"
  export BUDGET_POSTGRES_PORT=55434
  trap 'docker compose down --volumes' EXIT
  docker compose config --quiet
  docker compose up -d --wait --wait-timeout 90 postgres
  docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local < scripts/local-postgres/bootstrap-roles.sql
  scripts/local-postgres/check-roles.sh
  scripts/local-postgres/check-migration-lifecycle.sh
  scripts/local-postgres/check-schema.sh
  export BUDGET_TEST_DISPOSABLE_DATABASE=budget_local
  export BUDGET_TEST_SETUP_DSN="host=127.0.0.1 port=$BUDGET_POSTGRES_PORT dbname=budget_local user=budget_migrator password=budget-migrator-local-only sslmode=disable connect_timeout=3"
  export BUDGET_TEST_RUNTIME_DSN="host=127.0.0.1 port=$BUDGET_POSTGRES_PORT dbname=budget_local user=budget_runtime password=budget-runtime-local-only sslmode=disable connect_timeout=3"
  go test -tags=integration -count=1 -timeout=60s ./internal/postgres
)
```

The selected integration suite requires all three environment variables:

| Variable | Required value |
| --- | --- |
| `BUDGET_TEST_DISPOSABLE_DATABASE` | Exact name of the database explicitly selected as disposable |
| `BUDGET_TEST_SETUP_DSN` | `budget_migrator` credentials for fixture setup and schema-version inspection |
| `BUDGET_TEST_RUNTIME_DSN` | `budget_runtime` credentials for adapter reads |

Both connections must use the same host, port and selected database. Tests verify
the actual login, PostgreSQL **18.6**, and applied schema version **1**. Missing
configuration, mismatched targets/roles or an unavailable database fail the
selected suite instead of producing a passing skip.

Run schema checks before inserting adapter fixtures: those checks require empty
application tables and allocate owner sequence values. Adapter fixtures also
require empty tables and never reset existing data. Tests snapshot complete
application-row contents and sequence state **after setup**, compare them after
successful, absent and failed reads, and clean only their own fixture rows.
Cleanup does not rewind the sequence. Permission probes restore the runtime
grants they temporarily revoke.

The suite covers distinct identities, owner/month isolation, all categories and
currencies, maximum revision, NULL versus zero, padded decimals, exact and large
totals, negative remaining, absence, permissions, and cancellation/deadlines
during blocked monthly reads.

## Storage CI

The [storage workflow](../.github/workflows/storage.yml) runs ordinary tests/vet,
provisions its isolated database, runs role/migration/schema checks, then selects
the adapter integration suite with separate setup/runtime credentials. The guide
is included in its pull-request and push path filters. Existing failure logs and
`always()` cleanup apply to integration failures too.

A local run verifies the documented sequence; hosted workflow results must be
read from GitHub Checks. Workflow configuration alone does not establish hosted
CI success or maintainer acceptance.
