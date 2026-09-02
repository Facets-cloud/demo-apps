// Package store persists expenses and the running summary. It defines the Store
// seam the rest of the backend depends on, a Firestore (MongoDB-compatible)
// implementation in mongo.go, and an in-memory Fake used by tests and cmd/local.
package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

// Summary is the running aggregate maintained by the SummaryConsumer: how many
// expenses have been seen and their total in cents.
type Summary struct {
	Count      int64 `json:"count" bson:"count"`
	TotalCents int64 `json:"total_cents" bson:"total_cents"`
}

// Store persists expenses and the running summary. One concrete store is shared
// by all three services so the web tier can read back what the async consumers
// wrote — that is what makes the pipeline observable in the UI.
type Store interface {
	// Save records a parsed expense (written by ReceiptUploaded).
	Save(ctx context.Context, e expense.Expense) (expense.Expense, error)
	// List returns the most recently saved expenses, newest first (read by the
	// web tier for GET /expenses). limit <= 0 means "a sensible default".
	List(ctx context.Context, limit int) ([]expense.Expense, error)
	// GetSummary returns the running aggregate (read by the web tier for
	// GET /summary). Absent aggregate returns a zero Summary, not an error.
	GetSummary(ctx context.Context) (Summary, error)
	// IncrementSummary atomically adds one expense of amountCents to the running
	// aggregate and returns the new value (written by SummaryConsumer).
	IncrementSummary(ctx context.Context, amountCents int64) (Summary, error)
}

// Fake is an in-memory Store for tests and local runs. Expenses and the summary
// are tracked independently, mirroring production: Save writes an expense; the
// summary only moves via IncrementSummary (driven by the Pub/Sub consumer).
type Fake struct {
	mu      sync.Mutex
	Saved   []expense.Expense
	summary Summary
	Err     error // if set, Save returns it
	now     func() time.Time
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

// List returns saved expenses newest-first, capped at limit (default 100).
func (f *Fake) List(_ context.Context, limit int) ([]expense.Expense, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	out := make([]expense.Expense, len(f.Saved))
	copy(out, f.Saved)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// GetSummary returns the current running aggregate.
func (f *Fake) GetSummary(_ context.Context) (Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.summary, nil
}

// IncrementSummary adds one expense of amountCents to the aggregate.
func (f *Fake) IncrementSummary(_ context.Context, amountCents int64) (Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary.Count++
	f.summary.TotalCents += amountCents
	return f.summary, nil
}
