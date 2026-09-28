package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// claim recovers exhausted leases, then leases due events. Attempts also
// identify ownership: an older worker cannot overwrite a newer claim's outcome.
func claim(ctx context.Context, pool *pgxpool.Pool, batchSize, maxAttempts int, leaseFor time.Duration) ([]ClaimedEvent, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit has succeeded

	// An interrupted final attempt must become visible as a failure, even
	// when the handler never ran or its outcome could not be recorded.
	const recoverExhausted = `
		WITH exhausted AS (
			SELECT id
			FROM outbox_events
			WHERE processed_at IS NULL
				AND failed_at IS NULL
				AND attempts >= $1
				AND available_at <= now()
			ORDER BY available_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE outbox_events
		SET failed_at = now(),
			last_error = 'claim expired after final attempt; delivery outcome unknown'
		FROM exhausted
		WHERE outbox_events.id = exhausted.id
	`
	if _, err := tx.Exec(ctx, recoverExhausted, maxAttempts, batchSize); err != nil {
		return nil, fmt.Errorf("recover exhausted outbox claims: %w", err)
	}

	const query = `
		WITH claimed AS (
			SELECT id
			FROM outbox_events
			WHERE processed_at IS NULL
				AND failed_at IS NULL
				AND available_at <= now()
				AND attempts < $2
			ORDER BY available_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE outbox_events
		SET attempts = outbox_events.attempts + 1,
			available_at = now() + ($3 * interval '1 microsecond')
		FROM claimed
		WHERE outbox_events.id = claimed.id
		RETURNING outbox_events.id, outbox_events.type, outbox_events.payload,
			outbox_events.attempts, outbox_events.created_at, outbox_events.available_at
	`

	rows, err := tx.Query(ctx, query, batchSize, maxAttempts, leaseFor.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}

	events, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ClaimedEvent])
	if err != nil {
		return nil, fmt.Errorf("scan claimed outbox events: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim transaction: %w", err)
	}

	return events, nil
}

var errClaimLost = errors.New("outbox claim no longer owned")

func markProcessed(ctx context.Context, pool *pgxpool.Pool, event ClaimedEvent) error {
	const query = `
		UPDATE outbox_events
		SET processed_at = now()
		WHERE id = $1
			AND attempts = $2
			AND processed_at IS NULL
			AND failed_at IS NULL
	`
	tag, err := pool.Exec(ctx, query, event.ID, event.Attempts)
	return checkOutcome(tag, err)
}

func markFailed(ctx context.Context, pool *pgxpool.Pool, event ClaimedEvent, cause error, availableAt time.Time) error {
	const query = `
		UPDATE outbox_events
		SET last_error = $3,
			available_at = $4
		WHERE id = $1
			AND attempts = $2
			AND processed_at IS NULL
			AND failed_at IS NULL
	`
	tag, err := pool.Exec(ctx, query, event.ID, event.Attempts, cause.Error(), availableAt)
	return checkOutcome(tag, err)
}

func markTerminallyFailed(ctx context.Context, pool *pgxpool.Pool, event ClaimedEvent, cause error) error {
	const query = `
		UPDATE outbox_events
		SET last_error = $3,
			failed_at = now()
		WHERE id = $1
			AND attempts = $2
			AND processed_at IS NULL
			AND failed_at IS NULL
	`
	tag, err := pool.Exec(ctx, query, event.ID, event.Attempts, cause.Error())
	return checkOutcome(tag, err)
}

func checkOutcome(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return fmt.Errorf("record outbox outcome: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errClaimLost
	}
	return nil
}
