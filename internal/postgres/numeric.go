package postgres

import (
	"math/big"
	"strings"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5/pgtype"
)

// numericToMoney converts a decoded source amount without modifying its integer.
// Valid=false represents SQL NULL; a finite zero remains a planned Money value.
func numericToMoney(value pgtype.Numeric, currency budget.Currency) (*budget.Money, error) {
	if !value.Valid {
		return nil, nil
	}
	if value.NaN || value.InfinityModifier != pgtype.Finite || value.Int == nil || value.Int.Sign() < 0 {
		return nil, &operationError{"decode planned amount", ErrInvalidStoredData, budget.ErrInvalidAmount}
	}

	const fractionalDigits = 6
	const maxUnitDigits = 18 + fractionalDigits
	var units big.Int
	if value.Int.Sign() != 0 {
		// A Numeric is Int * 10^Exp; Money uses millionths. Widen before
		// adding the scale so an extreme int32 exponent cannot overflow.
		shift := int64(value.Exp) + fractionalDigits
		unitDigits := int64(len(value.Int.String())) + shift
		if unitDigits < 1 || unitDigits > maxUnitDigits {
			return nil, &operationError{"decode planned amount range", ErrInvalidStoredData, budget.ErrInvalidAmount}
		}

		// The digit check bounds multiplication to 24 result digits and
		// prevents constructing a divisor larger than the existing input.
		power := shift
		if power < 0 {
			power = -power
		}
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(power), nil)
		if shift >= 0 {
			units.Mul(value.Int, factor)
		} else {
			var remainder big.Int
			units.QuoRem(value.Int, factor, &remainder)
			if remainder.Sign() != 0 {
				return nil, &operationError{"decode planned amount precision", ErrInvalidStoredData, budget.ErrInvalidAmount}
			}
		}
	}

	// Format exact millionths for the strict domain parser. Removing zero
	// fractional padding here does not relax incoming API amount spelling.
	digits := units.String()
	if len(digits) <= fractionalDigits {
		digits = strings.Repeat("0", fractionalDigits+1-len(digits)) + digits
	}
	point := len(digits) - fractionalDigits
	amount := digits[:point]
	if fraction := strings.TrimRight(digits[point:], "0"); fraction != "" {
		amount += "." + fraction
	}
	money, err := budget.ParseMoney(amount, currency)
	if err != nil {
		return nil, &operationError{"construct planned money", ErrInvalidStoredData, err}
	}
	return &money, nil
}
