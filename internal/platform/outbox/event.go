// Package outbox delivers transactional PostgreSQL messages with retries.
// Persisted event types are versioned and matched exactly. Handlers must tolerate
// duplicates; bounded retries can end in an explicit terminal failure.
package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is the minimal capability Insert needs. Both *pgxpool.Pool
// and pgx.Tx satisfy it, so a caller can insert an event as part of
// its own transaction — the entire point of an outbox, since the
// business write and the event write must commit atomically — or,
// lacking a surrounding transaction, directly against the pool.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ClaimedEvent is an outbox row a Runner has safely claimed for
// processing (see claim in claim.go).
type ClaimedEvent struct {
	ID         uuid.UUID
	Type       string
	Payload    []byte
	Attempts   int
	CreatedAt  time.Time
	LeaseUntil time.Time
}

// Handler must tolerate duplicate invocations. It must respect context deadlines
// for I/O; success followed by a lost outcome write can cause redelivery.
type Handler func(ctx context.Context, event ClaimedEvent) error

// HandlerRegistration binds a Handler to the event type it handles.
// Modules provide these into the "outbox_handlers" Fx group for a
// Runner to collect (see Module).
type HandlerRegistration struct {
	Type    string
	Handler Handler
}

// Insert durably records a new event, available for claiming
// immediately. It provides no exactly-once guarantee on its own —
// combined with Runner's at-least-once processing, a Handler must
// tolerate being invoked more than once for the same event.
func Insert(ctx context.Context, exec Execer, eventType string, payload []byte) error {
	const query = `
		INSERT INTO outbox_events (id, type, payload, created_at, available_at, attempts)
		VALUES ($1, $2, $3, $4, $4, 0)
	`

	now := time.Now().UTC()

	if _, err := exec.Exec(ctx, query, uuid.New(), eventType, payload, now); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
}
