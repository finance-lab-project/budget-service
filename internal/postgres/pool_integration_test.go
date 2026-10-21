//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

func TestOpenPostgres(t *testing.T) {
	config := requireIntegrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	openIntegrationSetup(t, ctx, config)
	openCtx, cancelOpen := context.WithCancel(ctx)
	defer cancelOpen()
	store, err := Open(openCtx, config.runtimeDSN, 3*time.Second)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.pool.Stat().TotalConns() == 0 {
		t.Fatal("Open returned without establishing a connection")
	}

	// The initialization context must not control the lifetime of the Store.
	cancelOpen()
	if err := store.pool.Ping(ctx); err != nil {
		t.Fatalf("pool unusable after initialization context ended: %v", err)
	}
	checkIntegrationConnection(t, ctx, store.pool, "budget_runtime", config.database)

	store.Close()
	store.Close()
	if store.pool.Stat().TotalConns() != 0 {
		t.Fatal("Close left pool connections open")
	}
	if conn, err := store.pool.Acquire(ctx); err == nil {
		conn.Release()
		t.Fatal("closed pool still accepts acquisition")
	}
}
