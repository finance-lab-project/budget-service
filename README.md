# Budget Service

Finance Lab Budget Service in Go. This is the minimal domain-package baseline
for [finance-lab #143](https://github.com/anton415/finance-lab/issues/143).
Domain behavior and behavioral tests are not implemented yet.

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

The package starts with documentation only and no external dependencies.
Money, month and totals implementation belongs to
[finance-lab #144](https://github.com/anton415/finance-lab/issues/144) and is deferred.
There is no executable service, HTTP layer, database adapter or deployment setup.

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
- `go test ./...` succeeds with `[no test files]` for that package. This confirms
  compilation, with **zero behavioral tests**; it does not verify domain behavior.
- `go vet ./...` succeeds with no diagnostics.
