package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5"
)

// rowQuerier is the single operation needed from pgxpool for monthly reads.
// Keeping it private allows tests to supply a mismatching lookup candidate.
type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const lookupOwnerSQL = `
SELECT id, issuer_bytes, subject_bytes
FROM public.budget_owners
WHERE sha256(issuer_bytes) = sha256($1::bytea)
  AND sha256(subject_bytes) = sha256($2::bytea)`

// lookupOwner validates the monthly read's inputs before querying. It
// returns an internal owner ID only after comparing both original identities.
// The caller must supply an already verified and authorized issuer/subject pair.
func lookupOwner(ctx context.Context, query rowQuerier, issuer, subject, month string) (int64, error) {
	if issuer == "" || !utf8.ValidString(issuer) {
		return 0, fmt.Errorf("validate issuer: nonempty UTF-8 required: %w", ErrInvalidInput)
	}
	if subject == "" || !utf8.ValidString(subject) {
		return 0, fmt.Errorf("validate subject: nonempty UTF-8 required: %w", ErrInvalidInput)
	}
	if err := budget.ValidateMonth(month); err != nil {
		return 0, &operationError{"validate month", ErrInvalidInput, err}
	}

	issuerBytes, subjectBytes := []byte(issuer), []byte(subject)
	var ownerID int64
	var storedIssuer, storedSubject []byte
	err := query.QueryRow(ctx, lookupOwnerSQL, issuerBytes, subjectBytes).Scan(
		&ownerID, &storedIssuer, &storedSubject,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, &operationError{"lookup owner", ErrNotFound, err}
	}
	if err != nil {
		return 0, &operationError{"lookup owner", ErrDatabase, err}
	}
	if !bytes.Equal(storedIssuer, issuerBytes) || !bytes.Equal(storedSubject, subjectBytes) {
		return 0, fmt.Errorf("compare owner identity: %w", ErrIdentityIntegrity)
	}
	return ownerID, nil
}
