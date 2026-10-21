//go:build integration

package postgres

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Select an explicitly disposable database with BUDGET_TEST_DISPOSABLE_DATABASE
// and provide separate BUDGET_TEST_SETUP_DSN (budget_migrator) and
// BUDGET_TEST_RUNTIME_DSN (budget_runtime) connections to that same target.
// Apply schema version 1 and run the existing schema checks BEFORE this suite:
// those checks require empty application tables and allocate sequence values.
// This suite does not migrate, reset tables, reset sequences or skip on failure.
type integrationConfig struct {
	runtimeDSN string
	setupDSN   string
	database   string
}

func requireIntegrationConfig(t *testing.T) integrationConfig {
	t.Helper()
	config := integrationConfig{
		runtimeDSN: os.Getenv("BUDGET_TEST_RUNTIME_DSN"),
		setupDSN:   os.Getenv("BUDGET_TEST_SETUP_DSN"),
		database:   os.Getenv("BUDGET_TEST_DISPOSABLE_DATABASE"),
	}
	for _, item := range []struct{ name, value string }{
		{"BUDGET_TEST_RUNTIME_DSN", config.runtimeDSN},
		{"BUDGET_TEST_SETUP_DSN", config.setupDSN},
		{"BUDGET_TEST_DISPOSABLE_DATABASE", config.database},
	} {
		if item.value == "" {
			t.Fatalf("%s is required for explicitly selected PostgreSQL integration tests", item.name)
		}
	}
	runtimeConfig, err := pgxpool.ParseConfig(config.runtimeDSN)
	if err != nil {
		t.Fatal("invalid BUDGET_TEST_RUNTIME_DSN")
	}
	setupConfig, err := pgx.ParseConfig(config.setupDSN)
	if err != nil {
		t.Fatal("invalid BUDGET_TEST_SETUP_DSN")
	}
	runtimeConn := runtimeConfig.ConnConfig
	if runtimeConn.User != "budget_runtime" || setupConfig.User != "budget_migrator" {
		t.Fatal("separate budget_runtime and budget_migrator credentials are required")
	}
	if runtimeConn.Database != config.database || setupConfig.Database != config.database ||
		runtimeConn.Host != setupConfig.Host || runtimeConn.Port != setupConfig.Port {
		t.Fatal("both connections must select the same host, port and explicitly disposable database")
	}
	return config
}

func openIntegrationSetup(t *testing.T, ctx context.Context, config integrationConfig) *pgx.Conn {
	t.Helper()
	connectCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := pgx.Connect(connectCtx, config.setupDSN)
	if err != nil {
		// Driver connection errors can contain credentials; do not print them.
		t.Fatal("cannot connect to the disposable setup database")
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := conn.Close(closeCtx); err != nil {
			t.Error("close setup connection:", err)
		}
	})
	checkIntegrationConnection(t, ctx, conn, "budget_migrator", config.database)
	var versions []int64
	var applied bool
	if err := conn.QueryRow(ctx, `SELECT array_agg(version_id ORDER BY id), bool_and(is_applied)
FROM public.goose_db_version`).Scan(&versions, &applied); err != nil {
		t.Fatal("read schema version with setup credentials:", err)
	}
	if !applied || !slices.Equal(versions, []int64{0, 1}) {
		t.Fatal("integration target must have applied schema version 1")
	}
	return conn
}

func checkIntegrationConnection(t *testing.T, ctx context.Context, query rowQuerier, role, database string) {
	t.Helper()
	var currentRole, sessionRole, actualDatabase, version string
	err := query.QueryRow(ctx, `SELECT current_user, session_user, current_database(),
current_setting('server_version_num')`).Scan(&currentRole, &sessionRole, &actualDatabase, &version)
	if err != nil {
		t.Fatal("inspect integration connection:", err)
	}
	if currentRole != role || sessionRole != role || actualDatabase != database || version != "180006" {
		t.Fatal("wrong integration login, disposable database or PostgreSQL version (18.6 required)")
	}
}

type applicationSnapshot struct {
	owners, budgets, receipts string
	lastOwnerID               int64
	sequenceCalled            bool
}

func snapshotApplication(t *testing.T, conn *pgx.Conn) applicationSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var snapshot applicationSnapshot
	err := conn.QueryRow(ctx, `SELECT
 (SELECT coalesce(json_agg(row_to_json(o) ORDER BY o.id)::text, '[]') FROM public.budget_owners o),
 (SELECT coalesce(json_agg(row_to_json(b) ORDER BY b.owner_id, b.month)::text, '[]') FROM public.monthly_budgets b),
 (SELECT coalesce(json_agg(row_to_json(r) ORDER BY r.owner_id, r.key_digest)::text, '[]') FROM public.budget_save_receipts r),
 last_value, is_called FROM public.budget_owners_id_seq`).Scan(
		&snapshot.owners, &snapshot.budgets, &snapshot.receipts, &snapshot.lastOwnerID, &snapshot.sequenceCalled,
	)
	if err != nil {
		t.Fatal("snapshot application rows and owner sequence:", err)
	}
	return snapshot
}

func checkReadOnlyOnCleanup(t *testing.T, conn *pgx.Conn, before applicationSnapshot) {
	t.Helper()
	t.Cleanup(func() {
		if after := snapshotApplication(t, conn); after != before {
			t.Error("read changed application row contents or owner sequence state")
		}
	})
}
