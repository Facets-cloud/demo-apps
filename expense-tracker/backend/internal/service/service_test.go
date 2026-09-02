package service

import (
	"context"
	"errors"
	"testing"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/events"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/store"
)

func newObj() GCSObject {
	return GCSObject{
		Bucket:      "receipts-bucket",
		Name:        "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
		ContentType: "image/jpeg",
		Size:        1024,
	}
}

func TestHandle_StoresAndPublishes(t *testing.T) {
	st := store.NewFake()
	pub := events.NewFake()
	svc := New(st, pub)

	if err := svc.Handle(context.Background(), newObj()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(st.Saved) != 1 {
		t.Fatalf("expected 1 saved expense, got %d", len(st.Saved))
	}
	saved := st.Saved[0]
	if saved.ID == "" {
		t.Errorf("expected a generated UUID")
	}
	if saved.CreatedAt.IsZero() {
		t.Errorf("expected CreatedAt to be set")
	}
	if saved.Vendor != "starbucks" || saved.AmountCents != 450 {
		t.Errorf("parsed fields wrong: %+v", saved)
	}
	if saved.Currency != "USD" {
		t.Errorf("currency = %q, want USD", saved.Currency)
	}
	if saved.SourceObject != newObj().Name {
		t.Errorf("source object = %q", saved.SourceObject)
	}

	if len(pub.Published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.Published))
	}
	ev := pub.Published[0]
	// Published event must match the saved expense.
	if ev.ID != saved.ID || ev.Vendor != saved.Vendor || ev.AmountCents != saved.AmountCents {
		t.Errorf("published event does not match saved expense: %+v vs %+v", ev, saved)
	}
	if ev.Currency != "USD" {
		t.Errorf("published currency = %q, want USD", ev.Currency)
	}
	if ev.SpentOn != "2026-09-01" {
		t.Errorf("spent_on = %q, want 2026-09-01", ev.SpentOn)
	}
	if ev.SourceObject != saved.SourceObject {
		t.Errorf("source_object mismatch")
	}
}

func TestHandle_StoreErrorPropagates_NoPublish(t *testing.T) {
	st := store.NewFake()
	st.Err = errors.New("db down")
	pub := events.NewFake()
	svc := New(st, pub)

	if err := svc.Handle(context.Background(), newObj()); err == nil {
		t.Fatalf("expected error when store fails")
	}
	if len(pub.Published) != 0 {
		t.Errorf("must not publish when store failed")
	}
}

func TestHandle_PublishErrorPropagates(t *testing.T) {
	st := store.NewFake()
	pub := events.NewFake()
	pub.Err = errors.New("bus down")
	svc := New(st, pub)

	if err := svc.Handle(context.Background(), newObj()); err == nil {
		t.Fatalf("expected error when publish fails")
	}
	// The expense was still saved (order: save then publish).
	if len(st.Saved) != 1 {
		t.Errorf("expected save to have happened before publish")
	}
}
