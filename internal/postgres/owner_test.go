package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestLookupOwnerValidatesBeforeQuery(t *testing.T) {
	for _, tt := range []struct {
		name, issuer, subject, month string
		wantMonthError               bool
	}{
		{"empty issuer", "", "user-1", "2026-01", false},
		{"empty subject", "issuer", "", "2026-01", false},
		{"invalid UTF-8 issuer", "issuer\xff", "user-1", "2026-01", false},
		{"invalid UTF-8 subject", "issuer", "user-\xc3", "2026-01", false},
		{"empty month", "issuer", "user-1", "", true},
		{"noncanonical month", "issuer", "user-1", "2026-1", true},
		{"invalid month", "issuer", "user-1", "2026-13", true},
		{"invalid year", "issuer", "user-1", "0000-01", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			query := ownerQueryFunc(func(context.Context, string, ...any) pgx.Row {
				calls++
				return ownerCandidateRow{err: pgx.ErrNoRows}
			})
			id, err := lookupOwner(context.Background(), query, tt.issuer, tt.subject, tt.month)
			if id != 0 || !errors.Is(err, ErrInvalidInput) || calls != 0 {
				t.Fatalf("got id=%d, error=%v, queries=%d; want invalid input before querying", id, err, calls)
			}
			if errors.Is(err, budget.ErrInvalidMonth) != tt.wantMonthError {
				t.Fatalf("month error cause not preserved: %v", err)
			}
		})
	}
}

func TestLookupOwnerPreservesExactIdentity(t *testing.T) {
	type ownerIdentity struct{ issuer, subject string }
	identities := []ownerIdentity{
		{"https://issuer.example.test/realm", "user-1"},
		{"https://issuer.example.test/realm", "USER-1"},
		{"https://issuer.example.test/realm", " user-1"},
		{"https://issuer.example.test/realm", "user-1 "},
		{"https://ISSUER.example.test/realm", "user-1"},
		{" https://issuer.example.test/realm", "user-1"},
		{"https://issuer.example.test/realm ", "user-1"},
		{"https://other-issuer.example.test/realm", "user-1"},
		{"https://issuer.example.test/realm", "caf\u00e9"},
		{"https://issuer.example.test/realm", "cafe\u0301"},
		{"https://issuer.example.test/caf\u00e9", "user-1"},
		{"https://issuer.example.test/cafe\u0301", "user-1"},
		{"https://issuer.example.test/realm", "non-uuid:subject"},
		{"https://issuer.example.test/realm", strings.Repeat("long-subject-", 1024)},
		{" ", "\t"},
		{"issuer\x00suffix", "user\x00suffix"},
	}
	owners := make(map[ownerIdentity]int64, len(identities))
	for i, identity := range identities {
		owners[identity] = int64(i + 1)
	}
	for i, identity := range identities {
		t.Run(fmt.Sprintf("identity_%d", i), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			query := ownerQueryFunc(func(gotCtx context.Context, _ string, args ...any) pgx.Row {
				calls++
				if gotCtx != ctx {
					t.Fatal("lookup did not pass the caller's context")
				}
				issuer, subject := ownerQueryArguments(t, args)
				id, found := owners[ownerIdentity{issuer, subject}]
				if !found {
					return ownerCandidateRow{err: pgx.ErrNoRows}
				}
				return ownerCandidateRow{id: id, issuer: []byte(issuer), subject: []byte(subject)}
			})
			id, err := lookupOwner(ctx, query, identity.issuer, identity.subject, "0001-01")
			if err != nil || id != int64(i+1) || calls != 1 {
				t.Fatalf("got id=%d, error=%v, queries=%d; want owner %d from one query", id, err, calls, i+1)
			}
		})
	}
}

func TestLookupOwnerUsesParameters(t *testing.T) {
	const issuer = "https://issuer-parameter.example.test/realm'"
	const subject = "synthetic-subject'; SELECT 'not-a-command'"
	query := ownerQueryFunc(func(_ context.Context, sql string, args ...any) pgx.Row {
		if strings.Contains(sql, issuer) || strings.Contains(sql, subject) {
			t.Fatal("identity was interpolated into SQL")
		}
		gotIssuer, gotSubject := ownerQueryArguments(t, args)
		if gotIssuer != issuer || gotSubject != subject {
			t.Fatal("identity parameters were modified")
		}
		return ownerCandidateRow{id: 7, issuer: []byte(issuer), subject: []byte(subject)}
	})
	if id, err := lookupOwner(context.Background(), query, issuer, subject, "9999-12"); id != 7 || err != nil {
		t.Fatalf("got id=%d, error=%v", id, err)
	}
}

func TestLookupOwnerMissing(t *testing.T) {
	calls := 0
	query := ownerQueryFunc(func(context.Context, string, ...any) pgx.Row {
		calls++
		return ownerCandidateRow{err: fmt.Errorf("driver row: %w", pgx.ErrNoRows)}
	})
	id, err := lookupOwner(context.Background(), query, "issuer", "missing-user", "2026-01")
	if id != 0 || !errors.Is(err, ErrNotFound) || !errors.Is(err, pgx.ErrNoRows) || calls != 1 {
		t.Fatalf("got id=%d, error=%v, queries=%d; want missing owner", id, err, calls)
	}
	if errors.Is(err, ErrDatabase) || errors.Is(err, ErrIdentityIntegrity) {
		t.Fatalf("absence has the wrong error category: %v", err)
	}
}

func TestLookupOwnerRejectsMismatchingCandidate(t *testing.T) {
	// These are injected candidates, not generated SHA-256 collisions.
	for _, tt := range []struct{ name, issuer, subject string }{
		{"different issuer", "other-issuer", "caf\u00e9"},
		{"different subject", "issuer", "other-subject"},
		{"both different", "other-issuer", "other-subject"},
		{"canonically equivalent subject", "issuer", "cafe\u0301"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			query := ownerQueryFunc(func(context.Context, string, ...any) pgx.Row {
				calls++
				return ownerCandidateRow{id: 42, issuer: []byte(tt.issuer), subject: []byte(tt.subject)}
			})
			id, err := lookupOwner(context.Background(), query, "issuer", "caf\u00e9", "2026-01")
			if id != 0 || !errors.Is(err, ErrIdentityIntegrity) || calls != 1 {
				t.Fatalf("got id=%d, error=%v, queries=%d; want integrity failure and no owner ID", id, err, calls)
			}
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrDatabase) {
				t.Fatalf("integrity failure has the wrong error category: %v", err)
			}
		})
	}
}

func TestLookupOwnerPreservesDatabaseFailures(t *testing.T) {
	permissionErr := &pgconn.PgError{Code: "42501", Message: "synthetic permission denial"}
	for _, tt := range []struct {
		name  string
		cause error
	}{
		{"permission", permissionErr},
		{"connection", errors.New("synthetic connection failure")},
		{"cancellation", context.Canceled},
		{"deadline", context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			query := ownerQueryFunc(func(context.Context, string, ...any) pgx.Row {
				calls++
				// Even a partial scan must not leak an ID or replace the scan error.
				return ownerCandidateRow{id: 42, issuer: []byte("other-issuer"), subject: []byte("other-subject"), err: fmt.Errorf("scan: %w", tt.cause)}
			})
			id, err := lookupOwner(context.Background(), query, "issuer", "user-1", "2026-01")
			if id != 0 || !errors.Is(err, ErrDatabase) || !errors.Is(err, tt.cause) || calls != 1 {
				t.Fatalf("got id=%d, error=%v, queries=%d; want wrapped database failure", id, err, calls)
			}
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrIdentityIntegrity) {
				t.Fatalf("database failure has the wrong error category: %v", err)
			}
			if tt.cause == permissionErr {
				var got *pgconn.PgError
				if !errors.As(err, &got) || got != permissionErr {
					t.Fatal("PostgreSQL error is not inspectable")
				}
			}
			if strings.Contains(err.Error(), tt.cause.Error()) {
				t.Fatal("displayed error includes raw driver details")
			}
		})
	}
}

type ownerQueryFunc func(context.Context, string, ...any) pgx.Row

func (f ownerQueryFunc) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f(ctx, sql, args...)
}

type ownerCandidateRow struct {
	id              int64
	issuer, subject []byte
	err             error
}

func (r ownerCandidateRow) Scan(dest ...any) error {
	*dest[0].(*int64) = r.id
	*dest[1].(*[]byte) = r.issuer
	*dest[2].(*[]byte) = r.subject
	return r.err
}

func ownerQueryArguments(t *testing.T, args []any) (string, string) {
	t.Helper()
	if len(args) != 2 {
		t.Fatalf("got %d query arguments, want issuer and subject", len(args))
	}
	issuer, issuerOK := args[0].([]byte)
	subject, subjectOK := args[1].([]byte)
	if !issuerOK || !subjectOK {
		t.Fatal("identities must be supplied as byte parameters")
	}
	return string(issuer), string(subject)
}
