//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/finance-lab-project/budget-service/internal/budget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestReadMonthlyPostgres(t *testing.T) {
	config := requireIntegrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	setup := openIntegrationSetup(t, ctx, config)
	store, err := Open(ctx, config.runtimeDSN, 3*time.Second)
	if err != nil {
		t.Fatal("open runtime adapter:", err)
	}
	t.Cleanup(store.Close)
	checkIntegrationConnection(t, ctx, store.pool, "budget_runtime", config.database)
	fixtures := seedIntegrationFixtures(t, ctx, setup)
	// Schema probes and fixture insertion allocate sequence values. Only this
	// post-setup snapshot is the baseline for the adapter's read-only behavior.
	before := snapshotApplication(t, setup)
	for _, fixture := range fixtures {
		t.Run(fixture.owner.name+"/"+fixture.month, func(t *testing.T) {
			checkReadOnlyOnCleanup(t, setup, before)
			got, err := store.ReadMonthly(ctx, fixture.owner.issuer, fixture.owner.subject, fixture.month)
			if err != nil {
				t.Fatal("read fixture:", err)
			}
			assertIntegrationBudget(t, got, fixture)
		})
	}
	first := fixtures[0].owner
	for _, tt := range []struct {
		name, issuer, subject, month string
		want                         error
	}{
		{"missing owner", first.issuer, "absent-user", "2026-01", ErrNotFound},
		{"missing month", first.issuer, first.subject, "2027-01", ErrNotFound},
		{"owner without budgets", first.issuer, "no-budget-user", "2026-01", ErrNotFound},
		{"invalid input", first.issuer, first.subject, "2026-13", ErrInvalidInput},
	} {
		t.Run(tt.name, func(t *testing.T) {
			checkReadOnlyOnCleanup(t, setup, before)
			got, err := store.ReadMonthly(ctx, tt.issuer, tt.subject, tt.month)
			if got != nil || !errors.Is(err, tt.want) || errors.Is(err, ErrDatabase) {
				t.Fatalf("got (%v, %v), want nil and %v", got, err, tt.want)
			}
		})
	}
	for _, table := range []string{"budget_owners", "monthly_budgets"} {
		t.Run("permission/"+table, func(t *testing.T) {
			checkReadOnlyOnCleanup(t, setup, before)
			qualified := pgx.Identifier{"public", table}.Sanitize()
			var allowed bool
			if err := setup.QueryRow(ctx, "SELECT has_table_privilege('budget_runtime', $1, 'SELECT')", qualified).Scan(&allowed); err != nil || !allowed {
				t.Fatalf("expected initial runtime SELECT grant: allowed=%t error=%v", allowed, err)
			}
			if _, err := setup.Exec(ctx, "REVOKE SELECT ON "+qualified+" FROM budget_runtime"); err != nil {
				t.Fatal("revoke fixture permission:", err)
			}
			t.Cleanup(func() {
				restoreCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := setup.Exec(restoreCtx, "GRANT SELECT ON "+qualified+" TO budget_runtime"); err != nil {
					t.Error("restore runtime permission:", err)
				}
			})
			got, err := store.ReadMonthly(ctx, first.issuer, first.subject, "2026-01")
			var pgErr *pgconn.PgError
			if got != nil || !errors.Is(err, ErrDatabase) || errors.Is(err, ErrNotFound) ||
				!errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("got (%v, %v), want nil and inspectable permission denial", got, err)
			}
		})
	}
	for _, mode := range []string{"cancellation", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			checkReadOnlyOnCleanup(t, setup, before)
			checkBlockedReadCancellation(t, ctx, config, setup, store, first, mode)
		})
	}
	// A canceled query may retire its connection. The Store must remain usable.
	t.Run("read after cancellation", func(t *testing.T) {
		checkReadOnlyOnCleanup(t, setup, before)
		got, err := store.ReadMonthly(ctx, first.issuer, first.subject, "2026-01")
		if err != nil {
			t.Fatal("read after cancellation:", err)
		}
		assertIntegrationBudget(t, got, fixtures[0])
	})
}

type integrationIdentity struct {
	name, issuer, subject string
}

type integrationBudget struct {
	owner    integrationIdentity
	month    string
	revision int64
	currency budget.Currency
	// Empty entries mean SQL NULL; populated entries are canonical expectations.
	amounts [17]string
	totals  [4]string // income, spending, investments, remaining
}

var integrationAmountColumns = [...]string{
	"income", "investments", "groceries", "restaurants", "utilities", "transport",
	"household", "health", "personal_care", "clothing", "subscriptions", "education",
	"tech", "culture", "entertainment", "gifts", "travel",
}

func seedIntegrationFixtures(t *testing.T, ctx context.Context, setup *pgx.Conn) []integrationBudget {
	t.Helper()
	var populated bool
	if err := setup.QueryRow(ctx, `SELECT EXISTS (SELECT FROM public.budget_owners)
OR EXISTS (SELECT FROM public.monthly_budgets) OR EXISTS (SELECT FROM public.budget_save_receipts)`).Scan(&populated); err != nil {
		t.Fatal("check empty fixture target:", err)
	}
	if populated {
		t.Fatal("integration fixtures require empty application tables; existing data will not be reset")
	}
	const issuer = "https://issuer.example.test/realm"
	var longSubject strings.Builder
	for i := range 128 {
		fmt.Fprintf(&longSubject, "%x", sha256.Sum256([]byte(fmt.Sprint(i))))
	}
	identities := []integrationIdentity{
		{"base", issuer, "user-1"},
		{"other issuer", "https://other.example.test/realm", "user-1"},
		{"subject case", issuer, "USER-1"},
		{"subject leading space", issuer, " user-1"},
		{"subject trailing space", issuer, "user-1 "},
		{"issuer case", "https://ISSUER.example.test/realm", "user-1"},
		{"issuer leading space", " " + issuer, "user-1"},
		{"issuer trailing space", issuer + " ", "user-1"},
		{"subject composed", issuer, "caf\u00e9"},
		{"subject decomposed", issuer, "cafe\u0301"},
		{"issuer composed", "https://issuer.example.test/caf\u00e9", "user-1"},
		{"issuer decomposed", "https://issuer.example.test/cafe\u0301", "user-1"},
		{"non UUID", issuer, "synthetic:non-uuid:subject"},
		{"long subject", issuer, longSubject.String()},
	}
	var fixtures []integrationBudget
	for i, identity := range identities {
		for j, month := range []string{"2026-01", "2026-02"} {
			income := fmt.Sprint(i*10 + j + 1)
			fixtures = append(fixtures, integrationBudget{
				owner: identity, month: month, revision: int64(i*2 + j + 1),
				currency: []budget.Currency{budget.RUB, budget.USD, budget.EUR}[(i+j)%3],
				amounts:  [17]string{income}, totals: [4]string{income, "0", "0", income},
			})
		}
	}
	first := identities[0]
	fixtures = append(fixtures,
		integrationBudget{
			owner: first, month: "2026-03", revision: math.MaxInt64, currency: budget.RUB,
			amounts: [17]string{"100", "0.000001", "0.1", "0.2", "0.3", "0.4", "0.5", "0.6", "0.7", "0.8", "0.9", "1", "1.1", "1.2", "1.3", "1.4", "1.5"},
			totals:  [4]string{"100", "12", "0.000001", "87.999999"},
		},
		integrationBudget{
			owner: first, month: "2026-04", revision: 1, currency: budget.USD,
			amounts: [17]string{"0", "0", "0", "", "0", "", "0", "", "0", "", "0", "", "0", "", "0", "", "0"},
			totals:  [4]string{"0", "0", "0", "0"},
		},
	)
	large := integrationBudget{
		owner: first, month: "2026-05", revision: 1, currency: budget.EUR,
		totals: [4]string{"0", "14999999999999999999.999985", "999999999999999999.999999", "-15999999999999999999.999984"},
	}
	for i := 1; i < len(large.amounts); i++ {
		large.amounts[i] = "999999999999999999.999999"
	}
	fixtures = append(fixtures, large,
		integrationBudget{
			owner: first, month: "2026-06", revision: 1, currency: budget.USD,
			amounts: [17]string{"0.3", "", "0.1", "0.2"}, totals: [4]string{"0.3", "0.3", "0", "0"},
		},
		integrationBudget{
			owner: first, month: "2026-07", revision: 1, currency: budget.EUR,
			totals: [4]string{"0", "0", "0", "0"},
		},
	)
	identities = append(identities, integrationIdentity{"no budgets", issuer, "no-budget-user"})
	tx, err := setup.Begin(ctx)
	if err != nil {
		t.Fatal("begin fixture setup:", err)
	}
	defer tx.Rollback(ctx)
	ownerIDs := make(map[integrationIdentity]int64)
	var insertedIDs []int64
	for _, identity := range identities {
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO public.budget_owners (issuer_bytes, subject_bytes)
VALUES ($1, $2) RETURNING id`, []byte(identity.issuer), []byte(identity.subject)).Scan(&id); err != nil {
			t.Fatal("insert synthetic owner:", err)
		}
		ownerIDs[identity] = id
		insertedIDs = append(insertedIDs, id)
	}
	placeholders := []string{"$1", "$2", "$3", "$4"}
	for i := range integrationAmountColumns {
		placeholders = append(placeholders, fmt.Sprintf("$%d::text::numeric", i+5))
	}
	insertSQL := "INSERT INTO public.monthly_budgets (owner_id, month, revision, currency, " +
		strings.Join(integrationAmountColumns[:], ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	for _, fixture := range fixtures {
		args := []any{ownerIDs[fixture.owner], fixture.month, fixture.revision, string(fixture.currency)}
		for i, value := range fixture.amounts {
			if value == "" {
				args = append(args, nil)
			} else if fixture.month == "2026-03" && i < 2 {
				// Exercise stored representations rejected by the incoming API parser.
				args = append(args, []string{"100.0000000", "0.0000010"}[i])
			} else {
				args = append(args, value)
			}
		}
		if _, err := tx.Exec(ctx, insertSQL, args...); err != nil {
			t.Fatal("insert synthetic monthly budget:", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.budget_save_receipts
(owner_id, idempotency_key, key_digest, request_binding, response_status, response_body, response_generation)
VALUES ($1, 'synthetic-receipt', sha256(convert_to('synthetic-receipt', 'UTF8')),
'{"synthetic":true}', 200, '{ "synthetic": true, "amount": "0.000001" }', 'synthetic-generation')`, ownerIDs[first]); err != nil {
		t.Fatal("insert receipt snapshot fixture:", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit fixture setup:", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanup, err := setup.Begin(cleanupCtx)
		if err != nil {
			t.Error("begin fixture cleanup:", err)
			return
		}
		defer cleanup.Rollback(cleanupCtx)
		// Delete only rows belonging to owners inserted by this test. Do not
		// reset the database or rewind the sequence, even on a failed test.
		for _, statement := range []string{
			"DELETE FROM public.budget_save_receipts WHERE owner_id = ANY($1)",
			"DELETE FROM public.monthly_budgets WHERE owner_id = ANY($1)",
			"DELETE FROM public.budget_owners WHERE id = ANY($1)",
		} {
			if _, err := cleanup.Exec(cleanupCtx, statement, insertedIDs); err != nil {
				t.Error("delete synthetic fixtures:", err)
				return
			}
		}
		if err := cleanup.Commit(cleanupCtx); err != nil {
			t.Error("commit fixture cleanup:", err)
		}
	})
	return fixtures
}

func assertIntegrationBudget(t *testing.T, got *MonthlyBudget, want integrationBudget) {
	t.Helper()
	if got == nil || got.Month != want.month || got.Revision != want.revision || got.Planned.Currency != want.currency {
		t.Fatal("wrong owner/month result metadata")
	}
	if len(got.Planned.Spending) != 15 {
		t.Fatal("result must include all 15 spending keys, even when NULL")
	}
	for i, column := range integrationAmountColumns {
		var amount *budget.Money
		switch column {
		case "income":
			amount = got.Planned.Income
		case "investments":
			amount = got.Planned.Investments
		default:
			var present bool
			amount, present = got.Planned.Spending[strings.ReplaceAll(column, "_", "-")]
			if !present {
				t.Fatalf("missing spending key for column %s", column)
			}
		}
		if want.amounts[i] == "" {
			if amount != nil {
				t.Fatalf("SQL NULL in %s became planned", column)
			}
		} else {
			assertDecodedMoney(t, amount, want.amounts[i], want.currency)
		}
	}
	for i, total := range []*budget.Money{&got.Totals.Income, &got.Totals.Spending, &got.Totals.Investments, &got.Totals.Remaining} {
		assertDecodedMoney(t, total, want.totals[i], want.currency)
	}
}

func checkBlockedReadCancellation(t *testing.T, ctx context.Context, config integrationConfig, setup *pgx.Conn, store *Store, owner integrationIdentity, mode string) {
	t.Helper()
	lockConn, err := pgx.Connect(ctx, config.setupDSN)
	if err != nil {
		t.Fatal("connect fixture lock session")
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := lockConn.Close(closeCtx); err != nil {
			t.Error("close fixture lock session:", err)
		}
	})
	tx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatal("begin fixture lock:", err)
	}
	t.Cleanup(func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := tx.Rollback(rollbackCtx); err != nil {
			t.Error("release fixture lock:", err)
		}
	})
	if _, err := tx.Exec(ctx, "LOCK TABLE public.monthly_budgets IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal("lock monthly table:", err)
	}
	var runtimePID int32
	if err := store.pool.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&runtimePID); err != nil {
		t.Fatal("read runtime backend PID:", err)
	}
	readCtx, cancel := context.WithCancel(ctx)
	wantCause := context.Canceled
	if mode == "deadline" {
		cancel()
		readCtx, cancel = context.WithTimeout(ctx, time.Second)
		wantCause = context.DeadlineExceeded
	}
	defer cancel()
	type readResult struct {
		budget *MonthlyBudget
		err    error
	}
	finished := make(chan readResult, 1)
	go func() {
		got, err := store.ReadMonthly(readCtx, owner.issuer, owner.subject, "2026-01")
		finished <- readResult{got, err}
	}()
	// Observe the actual monthly SELECT waiting on a relation lock. The owner
	// lookup has therefore completed before cancellation/deadline is tested.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	waitCtx, stopWaiting := context.WithTimeout(ctx, 3*time.Second)
	defer stopWaiting()
	for {
		var blocked bool
		if err := setup.QueryRow(waitCtx, `SELECT EXISTS (SELECT FROM pg_locks
WHERE pid = $1 AND relation = 'public.monthly_budgets'::regclass
AND mode = 'AccessShareLock' AND NOT granted)`, runtimePID).Scan(&blocked); err != nil {
			t.Fatal("observe blocked monthly SELECT:", err)
		}
		if blocked {
			break
		}
		select {
		case result := <-finished:
			t.Fatalf("read finished before reaching the locked monthly row: %v", result.err)
		case <-waitCtx.Done():
			t.Fatal("monthly SELECT never reached the fixture lock")
		case <-ticker.C:
		}
	}
	if mode == "cancellation" {
		cancel()
	}
	select {
	case result := <-finished:
		if result.budget != nil || !errors.Is(result.err, ErrDatabase) || !errors.Is(result.err, wantCause) || errors.Is(result.err, ErrNotFound) {
			t.Fatalf("got (%v, %v), want nil and wrapped %v", result.budget, result.err, wantCause)
		}
	case <-waitCtx.Done():
		t.Fatal("blocked read did not respect cancellation/deadline")
	}
}
