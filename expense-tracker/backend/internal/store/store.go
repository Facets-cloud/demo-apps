// Package store persists expenses. It defines the Store seam that service
// depends on, a Cloud SQL (Postgres) implementation backed by pgxpool, and an
// in-memory Fake used by tests and cmd/local.
package store

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

// Store persists an expense and returns the stored row (with CreatedAt set).
type Store interface {
	Save(ctx context.Context, e expense.Expense) (expense.Expense, error)
}

// Fake is an in-memory Store for tests and local runs.
type Fake struct {
	mu    sync.Mutex
	Saved []expense.Expense
	Err   error // if set, Save returns it
	now   func() time.Time
}

// NewFake returns a ready-to-use in-memory store.
func NewFake() *Fake {
	return &Fake{now: time.Now}
}

// Save records the expense, stamping CreatedAt if unset.
func (f *Fake) Save(_ context.Context, e expense.Expense) (expense.Expense, error) {
	if f.Err != nil {
		return expense.Expense{}, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = f.now()
	}
	f.Saved = append(f.Saved, e)
	return e, nil
}

// PgxStore is the real Cloud SQL (Postgres) implementation.
type PgxStore struct {
	pool *pgxpool.Pool
}

// NewPgxStore builds a PgxStore from a DSN (e.g. Cloud SQL connection string).
func NewPgxStore(ctx context.Context, dsn string) (*PgxStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &PgxStore{pool: pool}, nil
}

// Save inserts the expense and returns the row with the DB-assigned created_at.
func (s *PgxStore) Save(ctx context.Context, e expense.Expense) (expense.Expense, error) {
	const q = `
		INSERT INTO expenses (id, vendor, amount_cents, currency, spent_on, source_object)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at`
	err := s.pool.QueryRow(ctx, q,
		e.ID, e.Vendor, e.AmountCents, e.Currency, e.SpentOn, e.SourceObject,
	).Scan(&e.CreatedAt)
	if err != nil {
		return expense.Expense{}, err
	}
	return e, nil
}

// Close releases the underlying connection pool.
func (s *PgxStore) Close() { s.pool.Close() }
