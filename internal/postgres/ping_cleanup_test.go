package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
)

func TestOpenCancellationDoesNotWaitForPingCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	pingSeen := make(chan struct{})
	cleanupBlocked := make(chan struct{})
	allowCleanup := make(chan struct{})
	releaseCleanup := sync.OnceFunc(func() { close(allowCleanup) })
	t.Cleanup(releaseCleanup)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveBlockedPing(listener, pingSeen, cleanupBlocked, allowCleanup)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	openDone := make(chan error, 1)
	go func() {
		store, err := Open(ctx, testConnectionString(listener.Addr()), 5*time.Second)
		if store != nil {
			store.Close()
			err = errors.New("Open returned an unverified Store")
		}
		openDone <- err
	}()
	select {
	case <-pingSeen:
	case err := <-serverDone:
		t.Fatalf("test server failed before Ping: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Open did not send Ping")
	}
	cancel()
	select {
	case <-cleanupBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("test did not reach driver cancellation cleanup")
	}
	select {
	case err := <-openDone:
		if !errors.Is(err, ErrDatabase) || !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v; want database failure wrapping cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Open cancellation waited for driver cleanup")
	}
	releaseCleanup()
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("connection cleanup failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed Open did not finish connection cleanup")
	}
}

// Finish startup, stall the verification query, and hold cancellation cleanup
// until the test releases it. All protocol data and credentials are synthetic.
func serveBlockedPing(listener net.Listener, pingSeen, cleanupBlocked chan<- struct{}, allowCleanup <-chan struct{}) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return err
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}
	message, err := backend.Receive()
	if err != nil {
		return err
	}
	if query, ok := message.(*pgproto3.Query); !ok || query.String != "-- ping" {
		return fmt.Errorf("expected Ping query, got %T", message)
	}
	close(pingSeen)

	cancelConn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer cancelConn.Close()
	if err := cancelConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	message, err = pgproto3.NewBackend(cancelConn, cancelConn).ReceiveStartupMessage()
	if err != nil {
		return err
	}
	if _, ok := message.(*pgproto3.CancelRequest); !ok {
		return fmt.Errorf("expected CancelRequest, got %T", message)
	}
	close(cleanupBlocked)
	<-allowCleanup
	cancelConn.Close()
	message, err = backend.Receive()
	if err != nil {
		return err
	}
	if _, ok := message.(*pgproto3.Terminate); !ok {
		return fmt.Errorf("expected Terminate, got %T", message)
	}
	return nil
}
