package store

import (
	"context"
	"errors"
	"testing"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

func TestFakeStore_Save(t *testing.T) {
	fs := NewFake()
	e := expense.Expense{ID: "id-1", Vendor: "starbucks", AmountCents: 450}
	saved, err := fs.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved.CreatedAt.IsZero() {
		t.Errorf("expected Save to stamp CreatedAt")
	}
	if len(fs.Saved) != 1 || fs.Saved[0].ID != "id-1" {
		t.Errorf("expected 1 saved expense with id-1, got %+v", fs.Saved)
	}
}

func TestFakeStore_Error(t *testing.T) {
	fs := NewFake()
	fs.Err = errors.New("boom")
	if _, err := fs.Save(context.Background(), expense.Expense{}); err == nil {
		t.Fatalf("expected error to propagate")
	}
}

// compile-time assertion that Fake satisfies Store.
var _ Store = (*Fake)(nil)
