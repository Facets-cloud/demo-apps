package store

import (
	"context"
	"testing"
	"time"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

func TestFake_ListNewestFirst(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i, v := range []string{"a", "b", "c"} {
		if _, err := f.Save(ctx, expense.Expense{ID: v, Vendor: v, CreatedAt: base.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got, err := f.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d, want 3", len(got))
	}
	if got[0].ID != "c" || got[2].ID != "a" {
		t.Errorf("not newest-first: got %s..%s, want c..a", got[0].ID, got[2].ID)
	}
}

func TestFake_ListRespectsLimit(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = f.Save(ctx, expense.Expense{ID: string(rune('a' + i)), CreatedAt: time.Now().Add(time.Duration(i) * time.Second)})
	}
	got, err := f.List(ctx, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List(2) returned %d, want 2", len(got))
	}
}

func TestFake_SummaryStartsZero(t *testing.T) {
	s, err := NewFake().GetSummary(context.Background())
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if s.Count != 0 || s.TotalCents != 0 {
		t.Errorf("zero summary = %+v, want {0,0}", s)
	}
}

func TestFake_IncrementSummary(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	if _, err := f.IncrementSummary(ctx, 450); err != nil {
		t.Fatalf("IncrementSummary: %v", err)
	}
	s, err := f.IncrementSummary(ctx, 1200)
	if err != nil {
		t.Fatalf("IncrementSummary: %v", err)
	}
	if s.Count != 2 {
		t.Errorf("Count = %d, want 2", s.Count)
	}
	if s.TotalCents != 1650 {
		t.Errorf("TotalCents = %d, want 1650", s.TotalCents)
	}

	// Save must NOT move the summary — only the consumer does.
	if _, err := f.Save(ctx, expense.Expense{ID: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := f.GetSummary(ctx)
	if got.Count != 2 {
		t.Errorf("summary moved on Save: Count = %d, want 2", got.Count)
	}
}
