package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open creates an owned pool using the caller's runtime connection string and
// verifies connectivity with Ping. connectTimeout must be positive; an earlier
// context deadline or cancellation also bounds initialization. On failure Open
// returns no Store and starts closing any pool it created. Driver cleanup may
// finish asynchronously so it does not delay a canceled or timed-out Open.
func Open(ctx context.Context, connectionString string, connectTimeout time.Duration) (*Store, error) {
	if connectionString == "" {
		return nil, fmt.Errorf("open budget database: connection string is required: %w", ErrInvalidInput)
	}
	if connectTimeout <= 0 {
		return nil, fmt.Errorf("open budget database: connect timeout must be positive: %w", ErrInvalidInput)
	}

	config, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, &operationError{"parse connection configuration", ErrInvalidInput, err}
	}
	// Also bound background connection attempts, which pgxpool may continue
	// after an Acquire caller stops waiting. Preserve a tighter caller setting.
	if config.ConnConfig.ConnectTimeout == 0 || config.ConnConfig.ConnectTimeout > connectTimeout {
		config.ConnConfig.ConnectTimeout = connectTimeout
	}

	openCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(openCtx, config)
	if err != nil {
		return nil, &operationError{"create connection pool", ErrDatabase, err}
	}
	if err := pool.Ping(openCtx); err != nil {
		cancel()
		// pgx may still be finishing asynchronous cancellation on a connection.
		// Close waits for that cleanup, so keep it off the caller's deadline path.
		go pool.Close()
		return nil, &operationError{"verify database connection", ErrDatabase, err}
	}
	return &Store{pool: pool}, nil
}

// Close releases all pool connections and prevents further use. It waits for
// acquired connections to be returned. Repeated calls, a nil Store and the zero
// Store are safe to close.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
