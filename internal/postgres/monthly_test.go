package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestReadMonthlyValidatesBeforeQuery(t *testing.T) {
	for _, tt := range []struct {
		name, issuer, subject, month string
		wantMonthError               bool
	}{
		{"empty issuer", "", "user-1", "2026-01", false},
		{"empty subject", "issuer", "", "2026-01", false},
		{"invalid UTF-8 issuer", "issuer\xff", "user-1", "2026-01", false},
		{"invalid UTF-8 subject", "issuer", "user-\xc3", "2026-01", false},
		{"invalid month", "issuer", "user-1", "2026-13", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Exercise the public method. With no pool, any attempted query
			// fails the test instead of reaching a database.
			var store Store
			got, err := store.ReadMonthly(context.Background(), tt.issuer, tt.subject, tt.month)
			if got != nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got (%v, %v), want no result and invalid input before querying", got, err)
			}
			if errors.Is(err, budget.ErrInvalidMonth) != tt.wantMonthError {
				t.Fatalf("month error cause not preserved: %v", err)
			}
		})
	}
}

func TestReadMonthlyMapsCompleteRow(t *testing.T) {
	for _, currency := range []budget.Currency{budget.RUB, budget.USD, budget.EUR} {
		t.Run(string(currency), func(t *testing.T) {
			row := monthlyTestRow{month: "2026-01", revision: math.MaxInt64, currency: string(currency)}
			for i := range row.amounts {
				row.amounts[i] = testNumeric(t, fmt.Sprint(i+1), -6)
			}
			got, err := readMonthlyFixture(t, row)
			if err != nil {
				t.Fatal(err)
			}
			if got.Month != "2026-01" || got.Revision != math.MaxInt64 || got.Planned.Currency != currency {
				t.Fatalf("incorrect monthly metadata: %+v", got)
			}
			assertDecodedMoney(t, got.Planned.Income, "0.000001", currency)
			assertDecodedMoney(t, got.Planned.Investments, "0.000002", currency)
			wantSpending := map[string]string{
				"groceries": "0.000003", "restaurants": "0.000004", "utilities": "0.000005",
				"transport": "0.000006", "household": "0.000007", "health": "0.000008",
				"personal-care": "0.000009", "clothing": "0.00001", "subscriptions": "0.000011",
				"education": "0.000012", "tech": "0.000013", "culture": "0.000014",
				"entertainment": "0.000015", "gifts": "0.000016", "travel": "0.000017",
			}
			if len(got.Planned.Spending) != len(wantSpending) {
				t.Fatalf("got %d categories, want 15", len(got.Planned.Spending))
			}
			for id, want := range wantSpending {
				assertDecodedMoney(t, got.Planned.Spending[id], want, currency)
			}
			assertDecodedMoney(t, &got.Totals.Income, "0.000001", currency)
			assertDecodedMoney(t, &got.Totals.Spending, "0.00015", currency)
			assertDecodedMoney(t, &got.Totals.Investments, "0.000002", currency)
			assertDecodedMoney(t, &got.Totals.Remaining, "-0.000151", currency)
		})
	}
}

func TestReadMonthlyPreservesUnplannedAndZero(t *testing.T) {
	for _, plannedZero := range []bool{false, true} {
		t.Run(fmt.Sprintf("planned_zero_%t", plannedZero), func(t *testing.T) {
			row := monthlyTestRow{month: "2026-01", revision: 1, currency: "RUB"}
			if plannedZero {
				row.amounts[0] = testNumeric(t, "0", -7) // income
				row.amounts[2] = testNumeric(t, "0", 0)  // groceries
			}
			got, err := readMonthlyFixture(t, row)
			if err != nil {
				t.Fatal(err)
			}
			if got.Planned.Investments != nil || len(got.Planned.Spending) != 15 {
				t.Fatal("unplanned values or complete category set were lost")
			}
			if plannedZero {
				assertDecodedMoney(t, got.Planned.Income, "0", budget.RUB)
			} else if got.Planned.Income != nil {
				t.Fatal("NULL income became planned")
			}
			for _, id := range budget.SpendingCategoryIDs() {
				amount, present := got.Planned.Spending[id]
				if !present {
					t.Fatalf("missing category %s", id)
				}
				if plannedZero && id == "groceries" {
					assertDecodedMoney(t, amount, "0", budget.RUB)
				} else if amount != nil {
					t.Fatalf("NULL %s became planned", id)
				}
			}
			for _, total := range []*budget.Money{&got.Totals.Income, &got.Totals.Spending, &got.Totals.Investments, &got.Totals.Remaining} {
				assertDecodedMoney(t, total, "0", budget.RUB)
			}
		})
	}
}

func TestReadMonthlyCalculatesLargeTotals(t *testing.T) {
	row := monthlyTestRow{month: "2026-01", revision: 1, currency: "EUR"}
	// Leave income unplanned; investments and all 15 spending inputs are at
	// the source maximum. Trailing zero padding must not change their value.
	for i := 1; i < len(row.amounts); i++ {
		row.amounts[i] = testNumeric(t, "9999999999999999999999990", -7)
	}
	got, err := readMonthlyFixture(t, row)
	if err != nil {
		t.Fatal(err)
	}
	assertDecodedMoney(t, got.Planned.Investments, "999999999999999999.999999", budget.EUR)
	assertDecodedMoney(t, &got.Totals.Spending, "14999999999999999999.999985", budget.EUR)
	assertDecodedMoney(t, &got.Totals.Remaining, "-15999999999999999999.999984", budget.EUR)
}

func TestReadMonthlyRejectsInvalidStoredMetadata(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*monthlyTestRow)
		cause  error
	}{
		{"zero revision", func(r *monthlyTestRow) { r.revision = 0 }, nil},
		{"negative revision", func(r *monthlyTestRow) { r.revision = -1 }, nil},
		{"invalid month", func(r *monthlyTestRow) { r.month = "2026-13" }, budget.ErrInvalidMonth},
		{"unsupported currency with all NULL", func(r *monthlyTestRow) { r.currency = "GBP" }, budget.ErrUnsupportedCurrency},
	} {
		t.Run(tt.name, func(t *testing.T) {
			row := monthlyTestRow{month: "2026-01", revision: 1, currency: "RUB"}
			tt.change(&row)
			got, err := readMonthlyFixture(t, row)
			if got != nil || !errors.Is(err, ErrInvalidStoredData) {
				t.Fatalf("got (%v, %v), want nil and invalid stored data", got, err)
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("domain cause not preserved: %v", err)
			}
		})
	}
}

func TestReadMonthlyRejectsInvalidAmountWithoutPartialResult(t *testing.T) {
	fields := []string{"income", "investments", "groceries", "restaurants", "utilities", "transport", "household", "health", "personal-care", "clothing", "subscriptions", "education", "tech", "culture", "entertainment", "gifts", "travel"}
	for i, field := range fields {
		t.Run(field, func(t *testing.T) {
			row := monthlyTestRow{month: "2026-01", revision: 1, currency: "USD"}
			for j := range row.amounts {
				row.amounts[j] = testNumeric(t, "1", 0)
			}
			row.amounts[i] = testNumeric(t, "1", -7)
			got, err := readMonthlyFixture(t, row)
			if got != nil || !errors.Is(err, ErrInvalidStoredData) || !errors.Is(err, budget.ErrInvalidAmount) {
				t.Fatalf("got (%v, %v), want no partial result and invalid amount", got, err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("error lacks field context: %v", err)
			}
		})
	}
}

func TestReadMonthlyPreservesQueryFailures(t *testing.T) {
	permissionErr := &pgconn.PgError{Code: "42501", Message: "synthetic private driver detail"}
	for _, tt := range []struct {
		name, detail string
		cause, want  error
	}{
		{name: "missing month", cause: pgx.ErrNoRows, want: ErrNotFound},
		{name: "permission", detail: permissionErr.Message, cause: permissionErr, want: ErrDatabase},
		{name: "connection", cause: errors.New("connection unavailable"), want: ErrDatabase},
		{name: "cancellation", cause: context.Canceled, want: ErrDatabase},
		{name: "deadline", cause: context.DeadlineExceeded, want: ErrDatabase},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// A failed scan may leave invalid or incomplete fields. Preserve
			// the scan error rather than reporting a stored-data error.
			row := monthlyTestRow{month: "incomplete", currency: "GBP", err: fmt.Errorf("scan: %w", tt.cause)}
			got, err := readMonthlyFixture(t, row)
			if got != nil || !errors.Is(err, tt.want) || !errors.Is(err, tt.cause) {
				t.Fatalf("got (%v, %v), want nil with category and original cause", got, err)
			}
			if errors.Is(err, ErrNotFound) != (tt.want == ErrNotFound) || errors.Is(err, ErrDatabase) != (tt.want == ErrDatabase) {
				t.Fatalf("wrong error category: %v", err)
			}
			if tt.cause == permissionErr {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr != permissionErr || strings.Contains(err.Error(), tt.detail) {
					t.Fatal("driver cause was lost or private details escaped")
				}
			}
		})
	}
}

func TestReadMonthlyStopsBeforeBudgetQueryOnOwnerFailure(t *testing.T) {
	permissionErr := &pgconn.PgError{Code: "42501", Message: "synthetic private owner-query detail"}
	connectionErr := errors.New("synthetic connection failure")
	for _, tt := range []struct {
		name  string
		row   ownerCandidateRow
		want  error
		cause error
	}{
		{"missing owner", ownerCandidateRow{err: fmt.Errorf("scan: %w", pgx.ErrNoRows)}, ErrNotFound, pgx.ErrNoRows},
		{"issuer mismatch", ownerCandidateRow{id: 42, issuer: []byte("other-issuer"), subject: []byte("user-1")}, ErrIdentityIntegrity, nil},
		{"subject mismatch", ownerCandidateRow{id: 42, issuer: []byte("issuer"), subject: []byte("other-user")}, ErrIdentityIntegrity, nil},
		{"both mismatch", ownerCandidateRow{id: 42, issuer: []byte("other-issuer"), subject: []byte("other-user")}, ErrIdentityIntegrity, nil},
		{"permission failure", ownerCandidateRow{err: fmt.Errorf("scan: %w", permissionErr)}, ErrDatabase, permissionErr},
		{"connection failure", ownerCandidateRow{err: fmt.Errorf("scan: %w", connectionErr)}, ErrDatabase, connectionErr},
		{"cancellation", ownerCandidateRow{err: fmt.Errorf("scan: %w", context.Canceled)}, ErrDatabase, context.Canceled},
		{"deadline", ownerCandidateRow{err: fmt.Errorf("scan: %w", context.DeadlineExceeded)}, ErrDatabase, context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			query := ownerQueryFunc(func(_ context.Context, sql string, _ ...any) pgx.Row {
				calls++
				if sql != lookupOwnerSQL || calls != 1 {
					t.Fatal("accessed budgets after a failed owner lookup")
				}
				return tt.row
			})
			got, err := readMonthly(context.Background(), query, "issuer", "user-1", "2026-01")
			if got != nil || !errors.Is(err, tt.want) || calls != 1 {
				t.Fatalf("got result=%v, error=%v, queries=%d", got, err, calls)
			}
			for _, category := range []error{ErrInvalidInput, ErrNotFound, ErrIdentityIntegrity, ErrInvalidStoredData, ErrDatabase} {
				if errors.Is(err, category) != (category == tt.want) {
					t.Fatalf("wrong error category: %v", err)
				}
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("owner-query cause not preserved: %v", err)
			}
			if tt.cause == permissionErr {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr != permissionErr || strings.Contains(err.Error(), permissionErr.Message) {
					t.Fatal("driver cause was lost or private details escaped")
				}
			}
		})
	}
}

// monthlyTestRow supplies driver values in the explicit SELECT column order.
// amounts contains income, investments, then the 15 spending columns.
type monthlyTestRow struct {
	month    string
	revision int64
	currency string
	amounts  [17]pgtype.Numeric
	err      error
}

func (r monthlyTestRow) Scan(dest ...any) error {
	if len(dest) != 20 {
		return fmt.Errorf("got %d scan destinations, want 20", len(dest))
	}
	*dest[0].(*string) = r.month
	*dest[1].(*int64) = r.revision
	*dest[2].(*string) = r.currency
	for i := range r.amounts {
		*dest[i+3].(*pgtype.Numeric) = r.amounts[i]
	}
	return r.err
}

func readMonthlyFixture(t *testing.T, row monthlyTestRow) (*MonthlyBudget, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	query := ownerQueryFunc(func(gotCtx context.Context, sql string, args ...any) pgx.Row {
		calls++
		if gotCtx != ctx {
			t.Fatal("caller context was not passed to query")
		}
		switch calls {
		case 1:
			if sql != lookupOwnerSQL {
				t.Fatal("first query must resolve the owner")
			}
			issuer, subject := ownerQueryArguments(t, args)
			if issuer != "https://issuer.example.test/realm" || subject != "user-1" {
				t.Fatal("read changed the caller's identity")
			}
			return ownerCandidateRow{id: 42, issuer: []byte(issuer), subject: []byte(subject)}
		case 2:
			if len(args) != 2 || args[0] != int64(42) || args[1] != "2026-01" {
				t.Fatalf("monthly query lacks resolved owner/month parameters: %v", args)
			}
			// Guard the positional driver fixture against projection drift and
			// ensure this read stays scoped to both owner and month parameters.
			wantSQL := "SELECT month, revision, currency, income::numeric, investments::numeric, groceries::numeric, restaurants::numeric, utilities::numeric, transport::numeric, household::numeric, health::numeric, personal_care::numeric, clothing::numeric, subscriptions::numeric, education::numeric, tech::numeric, culture::numeric, entertainment::numeric, gifts::numeric, travel::numeric FROM public.monthly_budgets WHERE owner_id = $1 AND month = $2"
			if strings.Join(strings.Fields(sql), " ") != wantSQL {
				t.Fatal("unexpected monthly projection or owner/month predicate")
			}
			return row
		default:
			t.Fatal("read performed more than one owner and one monthly query")
			return nil
		}
	})
	got, err := readMonthly(ctx, query, "https://issuer.example.test/realm", "user-1", "2026-01")
	if calls != 2 {
		t.Fatalf("got %d queries, want one owner and one monthly query", calls)
	}
	return got, err
}
