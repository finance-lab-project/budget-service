package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const readMonthlySQL = `
SELECT month, revision, currency, income::numeric, investments::numeric,
       groceries::numeric, restaurants::numeric, utilities::numeric,
       transport::numeric, household::numeric, health::numeric,
       personal_care::numeric, clothing::numeric, subscriptions::numeric,
       education::numeric, tech::numeric, culture::numeric,
       entertainment::numeric, gifts::numeric, travel::numeric
FROM public.monthly_budgets
WHERE owner_id = $1 AND month = $2`

// ReadMonthly reads an existing budget for an already verified and authorized
// issuer/subject pair. It performs no writes and returns nil on every error.
// Missing owners and missing months both return ErrNotFound.
func (s *Store) ReadMonthly(ctx context.Context, issuer, subject, month string) (*MonthlyBudget, error) {
	return readMonthly(ctx, s.pool, issuer, subject, month)
}

func readMonthly(ctx context.Context, query rowQuerier, issuer, subject, month string) (*MonthlyBudget, error) {
	ownerID, err := lookupOwner(ctx, query, issuer, subject, month)
	if err != nil {
		return nil, err
	}

	var result MonthlyBudget
	var currency string
	var income, investments pgtype.Numeric
	// Keep this order aligned with the SELECT projection. The domain ID for
	// personal_care differs from the SQL column name.
	spending := []struct {
		id     string
		amount pgtype.Numeric
	}{
		{id: "groceries"}, {id: "restaurants"}, {id: "utilities"},
		{id: "transport"}, {id: "household"}, {id: "health"},
		{id: "personal-care"}, {id: "clothing"}, {id: "subscriptions"},
		{id: "education"}, {id: "tech"}, {id: "culture"},
		{id: "entertainment"}, {id: "gifts"}, {id: "travel"},
	}
	dest := []any{&result.Month, &result.Revision, &currency, &income, &investments}
	for i := range spending {
		dest = append(dest, &spending[i].amount)
	}
	err = query.QueryRow(ctx, readMonthlySQL, ownerID, month).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &operationError{"read monthly budget", ErrNotFound, err}
	}
	if err != nil {
		return nil, &operationError{"read monthly budget", ErrDatabase, err}
	}
	if err := budget.ValidateMonth(result.Month); err != nil {
		return nil, &operationError{"validate stored month", ErrInvalidStoredData, err}
	}
	if result.Revision <= 0 {
		return nil, fmt.Errorf("validate stored revision: %w", ErrInvalidStoredData)
	}
	result.Planned.Currency = budget.Currency(currency)
	if err := result.Planned.Currency.Validate(); err != nil {
		return nil, &operationError{"validate stored currency", ErrInvalidStoredData, err}
	}
	result.Planned.Income, err = numericToMoney(income, result.Planned.Currency)
	if err != nil {
		return nil, fmt.Errorf("decode income: %w", err)
	}
	result.Planned.Investments, err = numericToMoney(investments, result.Planned.Currency)
	if err != nil {
		return nil, fmt.Errorf("decode investments: %w", err)
	}
	result.Planned.Spending = make(map[string]*budget.Money, len(spending))
	for _, field := range spending {
		amount, err := numericToMoney(field.amount, result.Planned.Currency)
		if err != nil {
			return nil, fmt.Errorf("decode spending %s: %w", field.id, err)
		}
		result.Planned.Spending[field.id] = amount
	}
	result.Totals, err = budget.CalculateTotals(result.Planned)
	if err != nil {
		return nil, &operationError{"calculate monthly totals", ErrInvalidStoredData, err}
	}
	return &result, nil
}
