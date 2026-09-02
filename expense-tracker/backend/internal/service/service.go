// Package service holds the ReceiptUploaded core: given a finalized GCS
// object, parse its name into an expense, persist it, then publish an
// expense.created event. It depends only on the parser, Store, and Publisher
// seams — no GCP client libraries.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/events"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/parser"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/store"
)

// GCSObject is the minimal view of a finalized storage object the handler
// needs, decoded from the CloudEvent data in function.go.
type GCSObject struct {
	Bucket      string
	Name        string
	ContentType string
	Size        int64
}

// Service wires the ReceiptUploaded pipeline.
type Service struct {
	store store.Store
	pub   events.Publisher
	now   func() time.Time
	newID func() string
}

// New builds a Service from a Store and Publisher.
func New(s store.Store, p events.Publisher) *Service {
	return &Service{
		store: s,
		pub:   p,
		now:   time.Now,
		newID: func() string { return uuid.NewString() },
	}
}

// Handle runs parse → store → publish. Any step's error is returned; publish
// is only attempted after a successful save.
func (s *Service) Handle(ctx context.Context, obj GCSObject) error {
	e, err := parser.Parse(obj.Name)
	if err != nil {
		return err
	}
	e.ID = s.newID()
	e.CreatedAt = s.now()

	saved, err := s.store.Save(ctx, e)
	if err != nil {
		return err
	}

	ev := expense.ExpenseCreated{
		ID:           saved.ID,
		Vendor:       saved.Vendor,
		AmountCents:  saved.AmountCents,
		Currency:     saved.Currency,
		SpentOn:      saved.SpentOn.Format("2006-01-02"),
		SourceObject: saved.SourceObject,
	}
	return s.pub.Publish(ctx, ev)
}
