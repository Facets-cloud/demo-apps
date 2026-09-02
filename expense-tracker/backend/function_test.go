package function

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/events"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/service"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/store"
)

// withStore installs a Fake as the process-wide store for the duration of a test.
func withStore(t *testing.T) *store.Fake {
	t.Helper()
	fake := store.NewFake()
	storeMu.Lock()
	sharedStore = fake
	storeMu.Unlock()
	t.Cleanup(func() {
		storeMu.Lock()
		sharedStore = nil
		storeMu.Unlock()
	})
	return fake
}

func TestWebHandler_GetSummary(t *testing.T) {
	fake := withStore(t)
	ctx := context.Background()
	_, _ = fake.IncrementSummary(ctx, 450)
	_, _ = fake.IncrementSummary(ctx, 1200)

	rec := httptest.NewRecorder()
	webHandler(rec, httptest.NewRequest(http.MethodGet, "/summary", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var s store.Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if s.Count != 2 || s.TotalCents != 1650 {
		t.Errorf("summary = %+v, want {2,1650}", s)
	}
}

func TestWebHandler_GetExpenses(t *testing.T) {
	fake := withStore(t)
	ctx := context.Background()
	_, _ = fake.Save(ctx, expense.Expense{ID: "1", Vendor: "starbucks", AmountCents: 450, Currency: "USD", CreatedAt: time.Now()})

	rec := httptest.NewRecorder()
	webHandler(rec, httptest.NewRequest(http.MethodGet, "/expenses", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var list []expenseJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if len(list) != 1 || list[0].Vendor != "starbucks" || list[0].AmountCents != 450 {
		t.Errorf("expenses = %+v, want one starbucks/450", list)
	}
}

func TestWebHandler_OptionsPreflight(t *testing.T) {
	rec := httptest.NewRecorder()
	webHandler(rec, httptest.NewRequest(http.MethodOptions, "/upload-url", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("missing CORS header on preflight")
	}
}

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
	// Inject a fresh in-memory store so the assertion is deterministic and
	// isolated from other tests.
	fake := store.NewFake()
	storeMu.Lock()
	sharedStore = fake
	storeMu.Unlock()
	t.Cleanup(func() {
		storeMu.Lock()
		sharedStore = nil
		storeMu.Unlock()
	})

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

	s, err := fake.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if s.TotalCents != 1650 {
		t.Errorf("TotalCents = %d, want 1650", s.TotalCents)
	}
	if s.Count != 2 {
		t.Errorf("Count = %d, want 2", s.Count)
	}
}
