package budget

import "errors"

// Domain errors can be identified with errors.Is, including when wrapped with
// the monthly input field that failed validation.
var (
	ErrInvalidAmount       = errors.New("invalid amount")
	ErrUnsupportedCurrency = errors.New("unsupported currency")
	ErrCurrencyMismatch    = errors.New("currency mismatch")
	ErrInvalidMonth        = errors.New("invalid month")
	ErrInvalidCategories   = errors.New("invalid spending categories")
)
