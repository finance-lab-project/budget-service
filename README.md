# Budget Service

Finance Lab Budget Service in Go. The `internal/budget` package implements
Money, month validation and exact monthly totals for
[finance-lab #144](https://github.com/anton415/finance-lab/issues/144), on the
module baseline established by
[finance-lab #143](https://github.com/anton415/finance-lab/issues/143).

## Approved placement and toolchain

The maintainer approved this placement and toolchain on 2026-10-20:

| Setting | Decision |
| --- | --- |
| Repository / checkout | `finance-lab-project/budget-service`, separate from `anton415/finance-lab` |
| Module root | Repository root (`.`) |
| Module import path | `github.com/finance-lab-project/budget-service` |
| Domain package location | `internal/budget` (package `budget`) |
| Supported toolchain | Go `1.27.3` |

The module import path identifies the module. The directory `internal/budget`
adds the package path
`github.com/finance-lab-project/budget-service/internal/budget`; Go's `internal`
rule restricts its importers to this repository's package tree.

The package uses only the Go standard library.
There is no executable service, HTTP layer, database adapter or deployment setup.

## Go Budget v1 contract

The initial [OpenAPI 3.1.1 contract](api/openapi.yaml) defines monthly GET/PUT
wire shapes. The [semantic companion](docs/api-v1-contract.md) records ownership,
exact values, problems, replay/recovery rules and BFF mapping.
[Synthetic fixtures](fixtures/api-v1/README.md) distinguish executable schema
checks from future raw-transport, domain and state/security conformance.

For pinned local setup, the shared local/CI validation command, actual results
and pending acceptance, see [verification evidence](docs/evidence/issue-3/verification.md).
Contract validation is not evidence of a running HTTP, authentication or
persistence implementation.

## Domain behavior

- `ParseMoney(amount, currency)` accepts canonical nonnegative decimal strings,
  up to 18 integer digits and six fractional digits, in explicit RUB, USD or EUR.
  `Money.Amount()` formats exact values; the default Go `Money{}` is invalid.
- `ValidateMonth(month)` accepts exactly `YYYY-MM`, years 0001–9999 and months
  01–12, independently of the current date.
- `CalculateTotals(MonthlyInput)` requires one supported budget currency and
  exactly the IDs returned by `SpendingCategoryIDs()`. Income, each spending
  amount and investments use `*Money`: `nil` means unplanned, while parsed
  `"0"` means planned zero. All populated amounts must match the budget currency.
- Totals contain income, spending, investments and remaining. Unplanned amounts
  contribute zero without changing the source. Remaining is income minus
  spending minus investments. Errors are distinguishable with `errors.Is`;
  failed calculations return no valid or partial totals.

Amounts use private `math/big.Int` storage in millionths: `"12.34"` becomes
`12340000` and formats back to `"12.34"`. The maximum planned amount requires
24 scaled digits, beyond `int64`. Parsing checks spelling and bounds before
constructing the integer. No floats, rounding or input normalization are used.

Computed totals have separate construction: remaining can be negative, and
summing fifteen maximum amounts requires 20 integer digits. These results are
valid totals but cannot bypass planned-input bounds if reused as input.
Money exposes no mutable integer storage; formatting reads it and calculations
allocate independent result storage. Tests exercise shared input values,
repeated calculations and deliberate internal storage mutation to detect aliasing.

## Local verification

From the root of a checkout of this repository, use Go 1.27.3. An existing Go
launcher can select that version with `GOTOOLCHAIN`; it may download the exact
toolchain if it is not already cached. The subshell below selects it only for
these commands, including its formatter, without changing global Go settings or
other projects' toolchains.

```sh
(
  export GOTOOLCHAIN=go1.27.3
  go version
  "$(go env GOROOT)/bin/gofmt" -l internal/budget
  go list ./...
  go test ./...
  go vet ./...
)
```

Expected results:

- `go version` reports `go1.27.3` for the local platform.
- `gofmt -l` prints nothing when the package is formatted. To format it, run
  `"$(go env GOROOT)/bin/gofmt" -w internal/budget` inside the same subshell.
- `go list ./...` discovers
  `github.com/finance-lab-project/budget-service/internal/budget`.
- `go test ./...` runs the domain tests, including strict parsing, month
  boundaries, all reference totals, currency/category errors and input/result
  preservation. For one maximum-input boundary test, run
  `go test ./internal/budget -run 'TestParseMoneyRoundTrip/RUB/999999999999999999' -v`
  inside the same subshell.
- `go vet ./...` succeeds with no diagnostics.
