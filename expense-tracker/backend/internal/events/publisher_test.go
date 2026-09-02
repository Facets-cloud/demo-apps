package events

import (
	"context"
	"errors"
	"testing"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

func TestFakePublisher_Publish(t *testing.T) {
	fp := NewFake()
	ev := expense.ExpenseCreated{ID: "id-1", Vendor: "starbucks", AmountCents: 450}
	if err := fp.Publish(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fp.Published) != 1 || fp.Published[0].ID != "id-1" {
		t.Errorf("expected 1 published event with id-1, got %+v", fp.Published)
	}
}

func TestFakePublisher_Error(t *testing.T) {
	fp := NewFake()
	fp.Err = errors.New("boom")
	if err := fp.Publish(context.Background(), expense.ExpenseCreated{}); err == nil {
		t.Fatalf("expected error to propagate")
	}
}

var _ Publisher = (*Fake)(nil)
