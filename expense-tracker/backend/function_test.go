package function

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/events"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/service"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/store"
)

// storageEvent builds a GCS object.finalized CloudEvent whose data is the given
// object map, mirroring what Eventarc delivers over HTTP.
func storageEvent(t *testing.T, obj map[string]any) cloudevents.Event {
	t.Helper()
	e := cloudevents.NewEvent()
	e.SetType("google.cloud.storage.object.v1.finalized")
	e.SetSource("//storage.googleapis.com/projects/_/buckets/receipts")
	if err := e.SetData(cloudevents.ApplicationJSON, obj); err != nil {
		t.Fatalf("SetData: %v", err)
	}
	return e
}

// -- ReceiptUploaded: GCS finalize decoding ---------------------------------

func TestDecodeStorageObject_SizeAsString(t *testing.T) {
	// GCS encodes object size as a quoted string in the Storage object JSON.
	e := storageEvent(t, map[string]any{
		"bucket":      "receipts-bucket",
		"name":        "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
		"contentType": "image/jpeg",
		"size":        "2048",
		"timeCreated": "2026-09-01T10:00:00Z",
	})

	got, err := decodeStorageObject(e)
	if err != nil {
		t.Fatalf("decodeStorageObject: %v", err)
	}
	want := service.GCSObject{
		Bucket:      "receipts-bucket",
		Name:        "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
		ContentType: "image/jpeg",
		Size:        2048,
	}
	if got != want {
		t.Fatalf("GCSObject mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestDecodeStorageObject_SizeAsNumber(t *testing.T) {
	// Some producers (and the emulator) encode size as a bare JSON number.
	e := storageEvent(t, map[string]any{
		"bucket":      "receipts-bucket",
		"name":        "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
		"contentType": "image/jpeg",
		"size":        4096,
	})

	got, err := decodeStorageObject(e)
	if err != nil {
		t.Fatalf("decodeStorageObject: %v", err)
	}
	if got.Size != 4096 {
		t.Fatalf("Size = %d, want 4096", got.Size)
	}
}

func TestDecodeStorageObject_SizeAbsent(t *testing.T) {
	// A missing size must not error; it defaults to 0.
	e := storageEvent(t, map[string]any{
		"bucket": "receipts-bucket",
		"name":   "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
	})
	got, err := decodeStorageObject(e)
	if err != nil {
		t.Fatalf("decodeStorageObject: %v", err)
	}
	if got.Size != 0 {
		t.Fatalf("Size = %d, want 0", got.Size)
	}
}

// TestReceiptPipeline_WithFakes exercises decode → parse → store → publish end
// to end using in-memory fakes, the same seam function.go wires to GCP.
func TestReceiptPipeline_WithFakes(t *testing.T) {
	e := storageEvent(t, map[string]any{
		"bucket":      "receipts-bucket",
		"name":        "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
		"contentType": "image/jpeg",
		"size":        "2048",
	})

	obj, err := decodeStorageObject(e)
	if err != nil {
		t.Fatalf("decodeStorageObject: %v", err)
	}

	st := store.NewFake()
	pub := events.NewFake()
	svc := service.New(st, pub)
	if err := svc.Handle(context.Background(), obj); err != nil {
		t.Fatalf("svc.Handle: %v", err)
	}

	if len(st.Saved) != 1 {
		t.Fatalf("stored %d expenses, want 1", len(st.Saved))
	}
	saved := st.Saved[0]
	if saved.Vendor != "starbucks" {
		t.Errorf("vendor = %q, want starbucks", saved.Vendor)
	}
	if saved.AmountCents != 450 {
		t.Errorf("amount_cents = %d, want 450", saved.AmountCents)
	}
	if saved.Currency != "USD" {
		t.Errorf("currency = %q, want USD", saved.Currency)
	}
	if len(pub.Published) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.Published))
	}
	if pub.Published[0].AmountCents != 450 {
		t.Errorf("published amount_cents = %d, want 450", pub.Published[0].AmountCents)
	}
}

// -- SummaryConsumer: Pub/Sub messagePublished decoding ---------------------

// pubsubEvent builds a messagePublished CloudEvent whose message.data is the
// base64 of the given raw payload, mirroring Eventarc's MessagePublishedData.
func pubsubEvent(t *testing.T, payload []byte) cloudevents.Event {
	t.Helper()
	envelope := map[string]any{
		"message": map[string]any{
			"data":       base64.StdEncoding.EncodeToString(payload),
			"messageId":  "123456789",
			"attributes": map[string]string{"origin": "test"},
		},
		"subscription": "projects/demo/subscriptions/summary",
	}
	e := cloudevents.NewEvent()
	e.SetType("google.cloud.pubsub.topic.v1.messagePublished")
	e.SetSource("//pubsub.googleapis.com/projects/demo/topics/expense-created")
	if err := e.SetData(cloudevents.ApplicationJSON, envelope); err != nil {
		t.Fatalf("SetData: %v", err)
	}
	return e
}

func TestDecodeExpenseCreated(t *testing.T) {
	want := expense.ExpenseCreated{
		ID:           "abc-123",
		Vendor:       "starbucks",
		AmountCents:  450,
		Currency:     "USD",
		SpentOn:      "2026-09-01",
		SourceObject: "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg",
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := decodeExpenseCreated(pubsubEvent(t, payload))
	if err != nil {
		t.Fatalf("decodeExpenseCreated: %v", err)
	}
	if got != want {
		t.Fatalf("ExpenseCreated mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestDecodeExpenseCreated_EmptyData(t *testing.T) {
	if _, err := decodeExpenseCreated(pubsubEvent(t, nil)); err == nil {
		t.Fatal("expected error for empty message data, got nil")
	}
}

func TestSummaryConsumerHandler_UpdatesRunningTotal(t *testing.T) {
	// Isolate the process-lifetime running total for a deterministic assertion.
	summaryMu.Lock()
	runningTotal, runningCount = 0, 0
	summaryMu.Unlock()

	for i, cents := range []int64{450, 1200} {
		payload, err := json.Marshal(expense.ExpenseCreated{
			ID:          "id-" + string(rune('a'+i)),
			Vendor:      "starbucks",
			AmountCents: cents,
			Currency:    "USD",
			SpentOn:     "2026-09-01",
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := summaryConsumerHandler(context.Background(), pubsubEvent(t, payload)); err != nil {
			t.Fatalf("summaryConsumerHandler: %v", err)
		}
	}

	summaryMu.Lock()
	total, count := runningTotal, runningCount
	summaryMu.Unlock()
	if total != 1650 {
		t.Errorf("runningTotal = %d, want 1650", total)
	}
	if count != 2 {
		t.Errorf("runningCount = %d, want 2", count)
	}
}
