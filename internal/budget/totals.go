package budget

import (
	"fmt"
	"math/big"
)

// SpendingCategoryIDs returns the required IDs in catalog order. Each call
// returns its own array, so callers cannot alter the domain's category set.
func SpendingCategoryIDs() [15]string {
	return [15]string{
		"groceries", "restaurants", "utilities", "transport", "household",
		"health", "personal-care", "clothing", "subscriptions", "education",
		"tech", "culture", "entertainment", "gifts", "travel",
	}
}

// MonthlyInput contains planned amounts in one budget currency. Nil means
// explicitly unplanned; a non-nil pointer to Money zero means planned zero.
// Spending must contain every required ID, including unplanned entries.
type MonthlyInput struct {
	Currency    Currency
	Income      *Money
	Spending    map[string]*Money
	Investments *Money
}

// Totals contains exact results. Only Remaining may be negative, and computed
// totals may exceed the magnitude allowed for a planned input.
type Totals struct {
	Income      Money
	Spending    Money
	Investments Money
	Remaining   Money
}

// CalculateTotals validates the complete input and calculates exact totals
// without mutating it. Errors return an empty, invalid Totals, never partial
// results. Callers must not modify the input concurrently with this call.
func CalculateTotals(input MonthlyInput) (Totals, error) {
	if err := input.Currency.Validate(); err != nil {
		return Totals{}, err
	}
	ids := SpendingCategoryIDs()
	if len(input.Spending) != len(ids) {
		return Totals{}, ErrInvalidCategories
	}
	for _, id := range ids {
		if _, exists := input.Spending[id]; !exists {
			return Totals{}, ErrInvalidCategories
		}
	}

	income, err := plannedUnits(input.Income, input.Currency)
	if err != nil {
		return Totals{}, fmt.Errorf("income: %w", err)
	}
	spending := new(big.Int)
	for _, id := range ids {
		amount, err := plannedUnits(input.Spending[id], input.Currency)
		if err != nil {
			return Totals{}, fmt.Errorf("spending %s: %w", id, err)
		}
		spending.Add(spending, amount)
	}
	investments, err := plannedUnits(input.Investments, input.Currency)
	if err != nil {
		return Totals{}, fmt.Errorf("investments: %w", err)
	}
	remaining := new(big.Int).Sub(income, spending)
	remaining.Sub(remaining, investments)

	return Totals{
		Income:      Money{units: income, currency: input.Currency},
		Spending:    Money{units: spending, currency: input.Currency},
		Investments: Money{units: investments, currency: input.Currency},
		Remaining:   Money{units: remaining, currency: input.Currency},
	}, nil
}

func plannedUnits(amount *Money, currency Currency) (*big.Int, error) {
	if amount == nil {
		return new(big.Int), nil
	}
	if err := amount.validate(); err != nil {
		return nil, err
	}
	if amount.currency != currency {
		return nil, ErrCurrencyMismatch
	}
	// Computed Money can be passed back by a caller; it must obey planned-input
	// bounds here too. At six-place scale the maximum is 24 nines.
	if amount.units.Sign() < 0 || len(amount.units.String()) > 18+fractionalDigits {
		return nil, ErrInvalidAmount
	}
	return new(big.Int).Set(amount.units), nil
}
