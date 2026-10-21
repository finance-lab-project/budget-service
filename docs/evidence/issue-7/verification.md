# Issue #7: final local verification and maintainer walkthrough

## Tested delivery

- Issue: [Implement Go PostgreSQL owner lookup and monthly budget reads](https://github.com/finance-lab-project/budget-service/issues/7),
  revision 1, last updated `2026-10-21T09:47:26Z` when inspected.
- Verification date: **2026-10-21**. Author: Codex; this is implementer
  verification, not an independent review.
- Git base: `f3a355a024f1d1f3c39e086f5d3f0b376b4fce75`.
- At step 9, the tested delivery was the **uncommitted working tree**, not that base commit alone.
  [SHA256SUMS](SHA256SUMS) identifies 75 source, test, workflow, documentation,
  dependency and contract files. Their bytes were unchanged during verification.
- Manifest SHA-256: `a031f94952444d0cc5c852456eef92e52199a13f7005cb48e1fd26bcfa998480`.
  The manifest excludes itself and this evidence record to avoid self-reference.
- Step 9 adds only these two evidence files. No production fix was needed and
  no commit, push, PR, issue closure or hosted CI run was performed.
- **Maintainer acceptance: ACCEPTED on 2026-10-21.** After this evidence and the
  theory guide were provided, the maintainer explicitly stated, "I accept the
  implementation," and authorized PR creation and CI verification. All 75
  manifest entries were rechecked and still matched before publication.

Any source change invalidates this verification snapshot. The acceptance above
applies to the implementation identified by this manifest digest. The delivering
PR identifies its commit and records hosted CI results separately. The later
theory guide and this acceptance note are documentation outside the manifest.

## Environment and commands

Go **1.27.3** ran on darwin/arm64 with pgx **v5.11.0**. Goose was **v3.29.0**.
The existing contract virtual environment used Python **3.14.5** and the pinned
`requirements-contract.txt` dependencies; the checker verified their versions.
PostgreSQL **18.6** ran in the digest-pinned Linux Compose image, in a fresh
disposable project on loopback port 55434. The established local database was
not reset or used for fixtures.

Commands below are shown from the repository root. `python` denotes the tested
Python 3.14.5 environment. Local interpreter and writable Go-cache paths are
omitted; no global toolchain setting was changed.

```sh
GOTOOLCHAIN=go1.27.3 go version
.tools/bin/goose -version
GOTOOLCHAIN=go1.27.3 go test -count=1 ./...
GOTOOLCHAIN=go1.27.3 go vet ./...
GOTOOLCHAIN=go1.27.3 go vet -tags=integration ./internal/postgres
python scripts/check_contract.py
GOTOOLCHAIN=go1.27.3 go list -m github.com/jackc/pgx/v5
GOTOOLCHAIN=go1.27.3 go list -deps ./internal/budget
git diff --check
git diff --exit-code -- internal/budget migrations scripts README.md
```

The database checks used this order, with synthetic local credentials:

```sh
export GOTOOLCHAIN=go1.27.3
export COMPOSE_PROJECT_NAME=budget-service-issue7-step9-3q1soc
export BUDGET_POSTGRES_PORT=55434
docker compose config --quiet
docker compose up -d --wait --wait-timeout 90 postgres
docker compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U budget_local -d budget_local < scripts/local-postgres/bootstrap-roles.sql
scripts/local-postgres/check-roles.sh
scripts/local-postgres/check-migration-lifecycle.sh
scripts/local-postgres/check-schema.sh
export BUDGET_TEST_DISPOSABLE_DATABASE=budget_local
export BUDGET_TEST_SETUP_DSN="host=127.0.0.1 port=$BUDGET_POSTGRES_PORT dbname=budget_local user=budget_migrator password=budget-migrator-local-only sslmode=disable connect_timeout=3"
export BUDGET_TEST_RUNTIME_DSN="host=127.0.0.1 port=$BUDGET_POSTGRES_PORT dbname=budget_local user=budget_runtime password=budget-runtime-local-only sslmode=disable connect_timeout=3"
go test -tags=integration -count=1 -v ./internal/postgres
```

After the integration suite, two expected-failure probes ran the following
command with overridden environment values:

```sh
GOTOOLCHAIN=go1.27.3 go test -tags=integration -count=1 ./internal/postgres -run '^TestReadMonthlyPostgres$'
```

1. All three `BUDGET_TEST_*` variables empty: nonzero exit with
   `BUDGET_TEST_RUNTIME_DSN is required`, no passing skip.
2. Selected database `budget_local`; setup/runtime DSNs using their respective
   role names on `127.0.0.1:1`, `sslmode=disable connect_timeout=3`: nonzero exit
   with `cannot connect to the disposable setup database`, no passing skip.

The actual disposable instance was then checked with setup credentials: counts
for owners, budgets and receipts were all zero, and runtime SELECT privileges
on owners and budgets were both restored (`0|0|0|t|t`). Finally:

```sh
docker compose down --volumes
```

Container, network and volume removal completed. For a repeatable recipe with
cleanup on exit, use the [adapter guide](../../postgres-adapter.md#disposable-integration-run)
and a fresh project name/free port.

## Observed results

| Check | Observed result |
| --- | --- |
| `go test -count=1 ./...` | Exit 0; domain and adapter ordinary tests passed without cached test results |
| Both `go vet` commands above | Exit 0, no diagnostics, including integration-tagged tests |
| Role checker | Exit 0; login/privilege checks passed; probe transaction rolled back |
| Migration lifecycle checker | Exit 0; version 1 applied; repeated `up` unchanged; deliberate version-2 failure returned SQLSTATE `P0001` and rolled back without schema/version drift |
| Schema checker, before adapter fixtures | Exit 0; positive cases, 284 expected negative cases and unrelated-data permission denials passed; schema and Goose records unchanged at version 1 |
| Tagged integration command | Exit 0; all 42 `TestReadMonthlyPostgres` scenarios and `TestOpenPostgres` passed, together with ordinary adapter tests; no skips |
| Real read target | Tests verified PostgreSQL 18.6, applied schema version 1 through `budget_migrator`, and actual adapter login `budget_runtime` |
| Absence/failure/no-write assertions | Complete application-row contents and owner sequence state matched the post-setup baseline after each read scenario |
| Permission/cancellation tests | Owner and budget permission denials retained SQLSTATE `42501`; cancellation and deadline causes remained inspectable after the monthly SELECT reached an observed lock wait; subsequent read succeeded |
| Negative selection probes | Missing configuration and unavailable PostgreSQL both failed as expected, without skips |
| Contract validator | Exit 0; 35 embedded examples; 63 positive/183 negative schema fixtures; 6 positive/5 negative exact-consistency cases; 27 positive/12 negative response cases; 17 problem anchors |
| Contract inventory checks | 19 raw cases and 41 state/security descriptions passed inventory/integrity checks only; this is not execution of their future API conformance scenarios |
| Formatting and scope | `gofmt -l` over all Go files under `internal` returned no paths; `git diff --check` passed; domain, applied migration, scripts and README remained unchanged |
| Dependency direction | `go list -m` reported pgx v5.11.0; the domain dependency list contained neither pgx packages nor `internal/postgres` |
| Frozen contract artifacts | All 36 issue-3 manifest entries retained their original hashes |
| Delivered snapshot | All 75 manifest entries matched before and after the checks |

The contract validator is an offline schema/fixture check. It does not establish
HTTP, authentication, save/replay, concurrency, retention, restart or recovery
conformance. The PostgreSQL tests establish the narrower adapter read behavior
described here.

## Acceptance-criteria evidence

| Criterion | Evidence and remaining limit |
| --- | --- |
| AC01 | Pinned module, package API/error documentation, dependency inspection |
| AC02 | Ordinary bounded-failure/cancellation/cleanup tests and real `TestOpenPostgres` |
| AC03 | Exact-identity unit tests and 14 distinct integration identities, each with two baseline months |
| AC04 | Injected mismatching candidates fail before a budget query; no real SHA-256 collision is claimed |
| AC05 | Absent owner/month and successful/failed reads leave complete row snapshots and sequence state unchanged |
| AC06 | Owner/month selection, all 15 category keys, all currencies and revision `9223372036854775807` |
| AC07 | Exact numeric unit cases and real NULL/zero, millionth, padded-decimal, source-maximum, large-total and negative-remaining fixtures |
| AC08 | Distinct input/absence/integrity/stored-data/database errors; wrapped driver and context causes |
| AC09 | Separate setup/runtime credentials, explicit disposable target, PostgreSQL 18.6/schema 1, and failing missing/unavailable configurations |
| AC10 | Automated regression and snapshot evidence are complete; maintainer acceptance of the unchanged tested implementation was recorded on 2026-10-21 |

## Maintainer walkthrough

1. **Identity lookup and original-byte comparison.** Follow
   [`lookupOwner`](../../../internal/postgres/owner.go): reject invalid input,
   query the two SHA-256 expressions with byte parameters, then compare both
   original byte strings before returning the internal owner ID. The mismatch
   tests require an integrity error, nil budget and no monthly query. Real tests
   distinguish case, whitespace, composed/decomposed Unicode and equal subjects
   under different issuers. Supplying these strings is still not authentication.
2. **Absence without writes.** Follow
   [`ReadMonthly`](../../../internal/postgres/monthly.go): absent owner stops the
   first query; absent month stops the second. Both return `ErrNotFound` and nil.
   This path contains SELECTs, not owner creation, budget writes or sequence
   allocation. The integration snapshot includes owners, budgets, receipts and
   `last_value`/`is_called`. Its baseline is taken after schema probes and fixture
   insertion, because setup itself legitimately allocates sequence values.
3. **Exact numeric conversion.** Follow
   [`numericToMoney`](../../../internal/postgres/numeric.go): stored `1.0000000`
   has coefficient `10000000` and exponent `-7`. Scaling to millionths divides
   by 10 exactly, producing `1000000` units and canonical `"1"`. A nonzero
   remainder is rejected, never rounded. NULL yields nil; numeric zero yields
   planned Money zero. `budget.ParseMoney` remains strict for incoming amounts,
   and `budget.CalculateTotals` derives exact totals, including negative remaining.
4. **Remaining authentication boundary.** The caller must verify the token and
   authorize the identity before using this adapter. Database role separation
   and byte-exact owner lookup do not provide an authenticated HTTP endpoint.
   HTTP/JWT/BFF integration, atomic saves, conflicts, receipts/replay, retention,
   recovery and deployment remain outside this issue's read implementation.

The implementation and test evidence support this written walkthrough. The
maintainer explicitly accepted the implementation after receiving the evidence
and theory guide. No separate live demonstration or independent review is
claimed; automated PASS results alone do not supply human acceptance.

## External verification limits

At completion of step 9, hosted GitHub Actions and independent review had
**not run for the uncommitted delivery**. The maintainer subsequently accepted
the implementation and authorized publication and CI verification. Consult the
delivering PR for hosted results tied to its commit. This local record does not
claim independent review, merge, issue closure or production deployment.
