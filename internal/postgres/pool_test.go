package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOpenInvalidInput(t *testing.T) {
	const validConfig = "host=127.0.0.1 port=1 user=budget_runtime dbname=budget_local sslmode=disable"
	for _, tt := range []struct {
		name    string
		config  string
		timeout time.Duration
	}{
		{"empty configuration", "", time.Second},
		{"zero timeout", validConfig, 0},
		{"negative timeout", validConfig, -time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, err := Open(context.Background(), tt.config, tt.timeout)
			if store != nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got store=%v, error=%v; want nil and ErrInvalidInput", store, err)
			}
		})
	}
}

func TestOpenMalformedConfigurationPreservesCauseWithoutDisplayingSecrets(t *testing.T) {
	const config = "host=127.0.0.1 password='synthetic-secret"
	store, err := Open(context.Background(), config, time.Second)
	if store != nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got store=%v, error=%v; want nil and ErrInvalidInput", store, err)
	}
	if strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatal("displayed error contains connection details")
	}
	var parseErr *pgconn.ParseConfigError
	if !errors.As(err, &parseErr) || parseErr.ConnString != config {
		t.Fatal("original driver configuration error is not inspectable")
	}
}

func TestOpenUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := testConnectionString(listener.Addr())
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	store, err := Open(context.Background(), config, 250*time.Millisecond)
	if store != nil || !errors.Is(err, ErrDatabase) || errors.Is(err, ErrNotFound) {
		t.Fatalf("got store=%v, error=%v; want nil and database failure", store, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("unavailable connection took %s", elapsed)
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) {
		t.Fatal("original driver connection error is not inspectable")
	}
}

func TestOpenAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store, err := Open(ctx, "host=127.0.0.1 port=1 sslmode=disable", time.Second)
	if store != nil || !errors.Is(err, ErrDatabase) || !errors.Is(err, context.Canceled) {
		t.Fatalf("got store=%v, error=%v; want nil and wrapped cancellation", store, err)
	}
}

func TestOpenInterruptedConnection(t *testing.T) {
	for _, mode := range []string{"connect timeout", "earlier caller deadline", "caller cancellation"} {
		t.Run(mode, func(t *testing.T) {
			config, accepted, disconnected := stalledPostgres(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			connectTimeout := 5 * time.Second
			wantCause := context.DeadlineExceeded
			switch mode {
			case "connect timeout":
				connectTimeout = 150 * time.Millisecond
			case "earlier caller deadline":
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 150*time.Millisecond)
				t.Cleanup(deadlineCancel)
			case "caller cancellation":
				wantCause = context.Canceled
				go func() {
					select {
					case <-accepted:
						cancel()
					case <-ctx.Done():
					}
				}()
			}

			started := time.Now()
			store, err := Open(ctx, config, connectTimeout)
			if store != nil {
				store.Close()
				t.Fatal("Open returned a Store without verified connectivity")
			}
			if !errors.Is(err, ErrDatabase) || !errors.Is(err, wantCause) || errors.Is(err, ErrNotFound) {
				t.Fatalf("got %v; want database failure wrapping %v", err, wantCause)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("interrupted Open took %s", elapsed)
			}
			select {
			case <-accepted:
			default:
				t.Fatal("test did not reach connection establishment")
			}
			select {
			case err := <-disconnected:
				if err != nil {
					t.Fatalf("peer did not observe a clean connection close: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("failed Open left the connection open")
			}
		})
	}
}

func TestCloseUninitialized(t *testing.T) {
	var absent *Store
	absent.Close()
	var zero Store
	zero.Close()
	zero.Close()
}

func testConnectionString(address net.Addr) string {
	return fmt.Sprintf("host=127.0.0.1 port=%d user=budget_runtime password=synthetic-test-only dbname=budget_local sslmode=disable", address.(*net.TCPAddr).Port)
}

// stalledPostgres accepts TCP and consumes startup bytes without responding.
// Observing EOF proves that a failed Open released the in-flight connection.
func stalledPostgres(t *testing.T) (string, <-chan struct{}, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	disconnected := make(chan error, 1)
	connections := make(chan net.Conn, 1)
	t.Cleanup(func() {
		listener.Close()
		select {
		case conn := <-connections:
			conn.Close()
		default:
		}
	})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			disconnected <- err
			return
		}
		defer conn.Close()
		connections <- conn
		close(accepted)
		if err := conn.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
			disconnected <- err
			return
		}
		_, err = io.Copy(io.Discard, conn)
		disconnected <- err
	}()
	return testConnectionString(listener.Addr()), accepted, disconnected
}
