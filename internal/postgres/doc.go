// Package postgres manages the PostgreSQL monthly-budget adapter's connection
// pool and reads existing monthly budgets with exact source amounts and domain
// totals. It depends on budget; budget has no database dependencies.
//
// # Usage
//
// Create a Store with Open at service startup, reuse it for request-scoped
// ReadMonthly calls, and Close it after in-flight requests finish. Supply runtime
// credentials to Open and an already verified, authorized issuer/subject pair
// to ReadMonthly. The month must be canonical YYYY-MM. Store's zero value is not
// ready for reads. The repository's docs/postgres-adapter.md includes a usage
// example, SQL/result mapping and a disposable integration-test recipe.
//
// # Pool lifecycle
//
// Open accepts a nonempty connection string from its caller, using runtime
// credentials, and owns the resulting pgxpool pool. It verifies connectivity
// with Ping within a positive connectTimeout while respecting earlier caller
// cancellation or deadlines. This timeout also caps individual pool connection
// attempts; a tighter connect_timeout in the connection string is preserved.
// Failed initialization starts pool cleanup without delaying cancellation;
// driver socket cleanup may finish after Open returns. A successful pool remains
// usable after the Open context ends; callers must Close the Store when finished.
// Close waits for acquired connections to be returned and is safe to repeat.
//
// No migrations or Goose bookkeeping queries run through the runtime pool.
// The adapter does not log connection strings, credentials, identity values or
// private budget contents. Returned connection error messages omit driver
// details; callers must not log unwrapped causes or connection configuration.
//
// # Amount decoding
//
// The private numericToMoney converter maps SQL NULL to nil and finite numeric
// zero to planned zero. It derives integer millionths from pgtype.Numeric's
// coefficient and exponent, requiring zero remainder for division. It rejects
// nonfinite, malformed, negative, oversized and excess-precision source values
// as ErrInvalidStoredData, preserving the underlying domain error. No partial
// Money is returned on failure, and the source integer is never modified.
// Canonical decimal text is then passed to the unchanged budget.ParseMoney;
// stored 1.0000000 becomes "1", while incoming API spelling remains strict.
// Currency validation for populated amounts is performed by that domain parser;
// ReadMonthly also validates the currency when every amount is NULL.
//
// # Monthly reads
//
// ReadMonthly resolves the owner, then uses one parameterized SELECT for the
// requested month, revision, currency and all 17 planned amounts. Unconstrained
// numeric casts expose the source domain values without rounding. The adapter
// validates stored metadata, decodes every amount and calls budget.CalculateTotals
// before returning a result. SQL personal_care maps to domain ID personal-care.
//
// # Identity and result
//
// The private lookupOwner helper accepts issuer and subject from an already
// verified and authorized identity, together with the requested month. Supplying
// these strings is not authentication. Empty or invalid UTF-8 identities and
// invalid months are rejected before querying. Valid identity bytes are preserved
// without trimming, case folding, Unicode normalization or UUID assumptions.
// Its parameterized SELECT uses the existing SHA-256 index expressions, then
// compares both original byte strings before returning the internal owner ID.
// Absence returns ErrNotFound; a mismatching candidate returns
// ErrIdentityIntegrity. Every failed lookup returns zero rather than a candidate
// ID. The helper performs one owner SELECT and no writes or budget queries.
// ReadMonthly uses this lookup flow before accessing any budget row.
//
// Successful reads return month, positive int64 revision, complete planned
// values and domain-calculated totals. All 15 spending categories are present;
// nil amounts mean unplanned, while non-nil zero amounts mean planned zero. Money
// conversion is exact, and driver-specific numeric values do not escape
// the adapter. Eventual HTTP serialization must encode revision as a decimal
// string. Missing owners or months cause no writes. Every failed read will
// return a nil result, never a partial budget.
//
// # Errors
//
// Callers distinguish adapter error categories with errors.Is. Missing
// owners and missing months both return ErrNotFound. A digest candidate
// whose original identity bytes differ returns ErrIdentityIntegrity.
// Invalid caller input uses ErrInvalidInput; invalid decoded database values
// use ErrInvalidStoredData instead. Operational failures use
// ErrDatabase, never ErrNotFound.
//
// Errors add operation context without including private values. Wrapping
// preserves both the category and any underlying cause, supporting errors.Is
// inspection of context.Canceled and context.DeadlineExceeded, and errors.As
// inspection of driver errors. Read errors preserve domain causes in the same
// way. HTTP status mapping, authentication, owner creation and all budget/receipt
// writes remain outside this adapter step.
//
// # Integration verification
//
// Ordinary tests need no PostgreSQL. They use loopback TCP listeners to exercise
// unavailable and stalled connections, cancellation and failed-open cleanup.
// The explicitly selected integration suite requires BUDGET_TEST_RUNTIME_DSN
// (budget_runtime), BUDGET_TEST_SETUP_DSN (budget_migrator) and the exact database
// name in BUDGET_TEST_DISPOSABLE_DATABASE. Both connections must select the same
// disposable PostgreSQL 18.6 target, prepared with schema version 1. Run the
// existing schema checks before this suite, while application tables are empty.
// Setup credentials seed and clean synthetic fixtures; adapter reads use runtime
// credentials. Tests compare row contents and sequence state after setup, cover
// failed reads and pool lifecycle, and refuse to reset existing application data.
// Missing configuration or an unavailable database fails the selected suite:
//
//	go test -tags=integration -count=1 ./internal/postgres
package postgres
