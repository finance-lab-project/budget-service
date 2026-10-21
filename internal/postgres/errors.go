package postgres

import "errors"

// Adapter errors identify categories with errors.Is, including when wrapped
// with operation context. Underlying causes must remain inspectable as described
// in the package documentation.
var (
	// ErrInvalidInput identifies invalid caller-supplied configuration,
	// identity or month. Month errors will also wrap budget.ErrInvalidMonth.
	ErrInvalidInput = errors.New("invalid postgres adapter input")

	// ErrNotFound identifies an absent owner or an absent monthly budget.
	ErrNotFound = errors.New("monthly budget not found")

	// ErrIdentityIntegrity identifies a digest lookup candidate whose original
	// issuer or subject bytes differ from the supplied identity.
	ErrIdentityIntegrity = errors.New("owner identity integrity failure")

	// ErrInvalidStoredData identifies a stored value that cannot form a valid
	// complete monthly result. It is not a caller-input validation error.
	ErrInvalidStoredData = errors.New("invalid stored budget data")

	// ErrDatabase identifies connection or query failures, including permission
	// errors and cancellation. The underlying error must also be wrapped.
	ErrDatabase = errors.New("budget database failure")
)

// operationError keeps connection details out of the displayed error while
// preserving the category and original cause for errors.Is and errors.As.
// In particular, malformed driver configuration errors may contain passwords.
type operationError struct {
	operation string
	category  error
	cause     error
}

func (e *operationError) Error() string {
	return e.operation + ": " + e.category.Error()
}

func (e *operationError) Unwrap() []error {
	return []error{e.category, e.cause}
}
