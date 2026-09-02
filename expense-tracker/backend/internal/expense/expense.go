// Package expense holds the domain model shared across the backend.
// It has no GCP dependencies so every other package can import it freely.
package expense

import "time"

// Expense is a single tracked expense parsed from an uploaded receipt.
type Expense struct {
	ID           string    // uuid
	Vendor       string    // vendor slug recovered from the object name
	AmountCents  int64     // integer cents to avoid float drift
	Currency     string    // ISO-4217, e.g. "USD"
	SpentOn      time.Time // date only (time component ignored)
	SourceObject string    // GCS object name the expense was derived from
	CreatedAt    time.Time // when the row was persisted
}

// ExpenseCreated is the JSON payload published to Pub/Sub as expense.created.
type ExpenseCreated struct {
	ID           string `json:"id"`
	Vendor       string `json:"vendor"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
	SpentOn      string `json:"spent_on"` // YYYY-MM-DD
	SourceObject string `json:"source_object"`
}
