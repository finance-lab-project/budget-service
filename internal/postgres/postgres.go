package postgres

import (
	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store holds the adapter-owned connection pool. Construct it with Open and
// release it with Close. Its zero value is not ready for reads.
type Store struct {
	pool *pgxpool.Pool
}

// MonthlyBudget is a complete monthly read result. It contains no internal
// owner ID or driver-specific values.
type MonthlyBudget struct {
	Month string
	// Revision is positive and must eventually be serialized as a decimal
	// string at the HTTP boundary, including values through math.MaxInt64.
	Revision int64
	// Planned contains all 15 spending categories, including nil entries.
	Planned budget.MonthlyInput
	// Totals is calculated from Planned by budget.CalculateTotals.
	Totals budget.Totals
}
