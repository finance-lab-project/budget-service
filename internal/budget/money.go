package budget

import (
	"math/big"
	"strings"
)

// Currency is an explicit, case-sensitive currency code.
type Currency string

const (
	RUB Currency = "RUB"
	USD Currency = "USD"
	EUR Currency = "EUR"
)

// Validate rejects missing, noncanonical and unsupported currency codes.
func (c Currency) Validate() error {
	switch c {
	case RUB, USD, EUR:
		return nil
	default:
		return ErrUnsupportedCurrency
	}
}

const fractionalDigits = 6

// Money is an exact amount in integer millionths, with an explicit currency.
// Its zero Go value is invalid. Copies may share private storage safely because
// no method mutates it or exposes it to callers.
type Money struct {
	units    *big.Int
	currency Currency
}

// ParseMoney accepts canonical nonnegative planned amounts with at most 18
// integer digits and six fractional digits. It never normalizes invalid input.
func ParseMoney(amount string, currency Currency) (Money, error) {
	if err := currency.Validate(); err != nil {
		return Money{}, err
	}
	if len(amount) == 0 || len(amount) > 18+1+fractionalDigits {
		return Money{}, ErrInvalidAmount
	}
	integer, fraction, hasPoint := strings.Cut(amount, ".")
	if len(integer) == 0 || len(integer) > 18 || !asciiDigits(integer) ||
		(len(integer) > 1 && integer[0] == '0') {
		return Money{}, ErrInvalidAmount
	}
	if hasPoint && (len(fraction) == 0 || len(fraction) > fractionalDigits ||
		!asciiDigits(fraction) || fraction[len(fraction)-1] == '0') {
		return Money{}, ErrInvalidAmount
	}

	digits := integer + fraction + strings.Repeat("0", fractionalDigits-len(fraction))
	units, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Money{}, ErrInvalidAmount
	}
	return Money{units: units, currency: currency}, nil
}

// Currency returns the amount's currency; an uninitialized Money has no currency.
func (m Money) Currency() Currency {
	return m.currency
}

// Amount returns the canonical decimal representation, including for computed
// negative or large totals. An uninitialized Money returns an error, never "0".
func (m Money) Amount() (string, error) {
	if err := m.validate(); err != nil {
		return "", err
	}
	digits := m.units.String()
	negative := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	if len(digits) <= fractionalDigits {
		digits = strings.Repeat("0", fractionalDigits+1-len(digits)) + digits
	}
	point := len(digits) - fractionalDigits
	amount := digits[:point]
	if fraction := strings.TrimRight(digits[point:], "0"); fraction != "" {
		amount += "." + fraction
	}
	if negative {
		amount = "-" + amount
	}
	return amount, nil
}

func (m Money) validate() error {
	if m.units == nil {
		return ErrInvalidAmount
	}
	return m.currency.Validate()
}

func asciiDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
