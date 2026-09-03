// Package function registers the three Gen2 Cloud Functions for the
// expense-tracker demo and wires the real GCP-backed implementations from
// environment variables. It intentionally contains no business logic — every
// handler decodes its input and delegates to a core package (service /
// uploadurl / store) that is fully unit-tested offline.
package function

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/events"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/service"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/signer"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/store"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/uploadurl"
)

func init() {
	functions.HTTP("CreateUploadURL", webHandler)
	functions.CloudEvent("ReceiptUploaded", receiptUploadedHandler)
	functions.CloudEvent("SummaryConsumer", summaryConsumerHandler)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func signURLTTL() time.Duration {
	if d, err := time.ParseDuration(envOr("SIGN_URL_TTL", "15m")); err == nil {
		return d
	}
	return 15 * time.Minute
}

// ---------------------------------------------------------------------------
// Shared store
// ---------------------------------------------------------------------------

// sharedStore is the process-wide Store. All three services build it the same
// way, so the web tier reads back what the async consumers wrote. Guarded by
// storeMu and lazily initialized; tests inject a Fake by setting it directly.
var (
	storeMu     sync.Mutex
	sharedStore store.Store
)

// getStore returns the shared Firestore (MongoDB-compatible) store when MONGO_URI
// is set, otherwise an ephemeral in-memory store (per-instance; fine for local
// dev, but async results won't be visible across services).
func getStore(ctx context.Context) (store.Store, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if sharedStore != nil {
		return sharedStore, nil
	}
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Printf("MONGO_URI unset — using in-memory store (ephemeral, per-instance; async results will NOT be shared across services)")
		sharedStore = store.NewFake()
		return sharedStore, nil
	}
	st, err := store.NewMongoStore(ctx, uri, os.Getenv("MONGO_DB"))
	if err != nil {
		return nil, fmt.Errorf("store init: %w", err)
	}
	sharedStore = st
	return sharedStore, nil
}

// ---------------------------------------------------------------------------
// Web tier (HTTP): POST /upload-url, GET /expenses, GET /summary
// ---------------------------------------------------------------------------

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// webHandler is the single HTTP entrypoint (behind the nginx /api proxy). It
// routes by method+path to the upload-URL, expenses, and summary handlers.
func webHandler(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/summary"):
		handleSummary(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/expenses"):
		handleExpenses(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/receipt-url"):
		handleReceiptURL(w, r)
	default:
		handleUploadURL(w, r)
	}
}

// validateReceiptObject guards which objects may be signed for GET. Only objects
// under the "receipts/" prefix are allowed, and path traversal is rejected — so
// this endpoint can never mint a URL for anything but an actual receipt.
func validateReceiptObject(object string) error {
	if object == "" {
		return fmt.Errorf("object is required")
	}
	if !strings.HasPrefix(object, "receipts/") {
		return fmt.Errorf("object must be under receipts/")
	}
	if strings.Contains(object, "..") {
		return fmt.Errorf("invalid object path")
	}
	if len(object) > 512 {
		return fmt.Errorf("object name too long")
	}
	return nil
}

// handleReceiptURL mints a short-lived signed GET URL so the browser can view a
// private receipt image without the bucket being public.
func handleReceiptURL(w http.ResponseWriter, r *http.Request) {
	object := r.URL.Query().Get("object")
	if err := validateReceiptObject(object); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	sg, err := newSigner(ctx)
	if err != nil {
		log.Printf("handleReceiptURL: signer init: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "signer unavailable")
		return
	}
	url, err := sg.SignGet(ctx, object, 15*time.Minute)
	if err != nil {
		log.Printf("handleReceiptURL: sign: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "could not sign url")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// createRequestJSON is the wire shape the frontend POSTs.
type createRequestJSON struct {
	Vendor      string  `json:"vendor"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Date        string  `json:"date"`
	Ext         string  `json:"ext"`
	ContentType string  `json:"contentType"`
}

func handleUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var body createRequestJSON
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	sg, err := newSigner(ctx)
	if err != nil {
		log.Printf("CreateUploadURL: signer init: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "signer unavailable")
		return
	}
	h := uploadurl.New(sg, signURLTTL())

	resp, err := h.Create(ctx, uploadurl.CreateRequest{
		Vendor:      body.Vendor,
		Amount:      body.Amount,
		Currency:    body.Currency,
		Date:        body.Date,
		Ext:         body.Ext,
		ContentType: body.ContentType,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// expenseJSON is the wire shape returned by GET /expenses.
type expenseJSON struct {
	ID           string `json:"id"`
	Vendor       string `json:"vendor"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
	SpentOn      string `json:"spent_on"`
	SourceObject string `json:"source_object"`
	CreatedAt    string `json:"created_at"`
}

func handleExpenses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := getStore(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	list, err := st.List(ctx, 100)
	if err != nil {
		log.Printf("handleExpenses: list: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]expenseJSON, 0, len(list))
	for _, e := range list {
		out = append(out, expenseJSON{
			ID:           e.ID,
			Vendor:       e.Vendor,
			AmountCents:  e.AmountCents,
			Currency:     e.Currency,
			SpentOn:      e.SpentOn.Format("2006-01-02"),
			SourceObject: e.SourceObject,
			CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := getStore(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	s, err := st.GetSummary(ctx)
	if err != nil {
		log.Printf("handleSummary: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "summary failed")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func newSigner(ctx context.Context) (signer.URLSigner, error) {
	bucket := os.Getenv("RECEIPT_BUCKET")
	signerSA := os.Getenv("SIGNER_SA_EMAIL")
	if bucket == "" || signerSA == "" {
		return nil, fmt.Errorf("RECEIPT_BUCKET and SIGNER_SA_EMAIL are required")
	}
	return signer.NewGCSSigner(ctx, bucket, signerSA)
}

// ---------------------------------------------------------------------------
// ReceiptUploaded (CloudEvent: GCS object.finalize)
// ---------------------------------------------------------------------------

// storageObjectData is the minimal subset of the GCS finalize event payload we
// need — avoids pulling in a heavy genproto dependency. The Eventarc CloudEvent
// data for google.cloud.storage.object.v1.finalized is the Storage object JSON.
type storageObjectData struct {
	Bucket      string  `json:"bucket"`
	Name        string  `json:"name"`
	ContentType string  `json:"contentType"`
	Size        flexInt `json:"size"` // GCS encodes size as a string; number also tolerated
	TimeCreated string  `json:"timeCreated"`
}

// flexInt decodes a JSON value that is either a quoted string (as GCS encodes
// object size) or a bare number. An absent/empty/null value decodes to 0.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid size %q: %w", s, err)
	}
	*f = flexInt(n)
	return nil
}

// decodeStorageObject decodes an Eventarc GCS-finalize CloudEvent into the
// minimal GCSObject the service needs.
func decodeStorageObject(e event.Event) (service.GCSObject, error) {
	var data storageObjectData
	if err := e.DataAs(&data); err != nil {
		return service.GCSObject{}, fmt.Errorf("decode storage event: %w", err)
	}
	return service.GCSObject{
		Bucket:      data.Bucket,
		Name:        data.Name,
		ContentType: data.ContentType,
		Size:        int64(data.Size),
	}, nil
}

func receiptUploadedHandler(ctx context.Context, e event.Event) error {
	obj, err := decodeStorageObject(e)
	if err != nil {
		return err
	}
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	return svc.Handle(ctx, obj)
}

func newService(ctx context.Context) (*service.Service, error) {
	project := os.Getenv("GCP_PROJECT")
	topic := os.Getenv("PUBSUB_TOPIC")
	if project == "" || topic == "" {
		return nil, fmt.Errorf("GCP_PROJECT and PUBSUB_TOPIC are required")
	}
	pub, err := events.NewPubSubPublisher(ctx, project, topic)
	if err != nil {
		return nil, fmt.Errorf("publisher init: %w", err)
	}
	st, err := getStore(ctx)
	if err != nil {
		return nil, err
	}
	return service.New(st, pub), nil
}

// ---------------------------------------------------------------------------
// SummaryConsumer (CloudEvent: Pub/Sub topic)
// ---------------------------------------------------------------------------

// messagePublishedData is the Eventarc envelope for a
// google.cloud.pubsub.topic.v1.messagePublished CloudEvent. The published
// payload is base64 in message.data; encoding/json base64-decodes a []byte
// field automatically.
type messagePublishedData struct {
	Message struct {
		Data       []byte            `json:"data"`
		MessageID  string            `json:"messageId"`
		Attributes map[string]string `json:"attributes"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// decodeExpenseCreated unwraps the Pub/Sub messagePublished envelope and
// decodes the base64 message.data into an ExpenseCreated.
func decodeExpenseCreated(e event.Event) (expense.ExpenseCreated, error) {
	var env messagePublishedData
	if err := e.DataAs(&env); err != nil {
		return expense.ExpenseCreated{}, fmt.Errorf("decode pubsub envelope: %w", err)
	}
	payload := env.Message.Data
	if len(payload) == 0 {
		return expense.ExpenseCreated{}, fmt.Errorf("empty pubsub message data")
	}
	var ev expense.ExpenseCreated
	if err := json.Unmarshal(payload, &ev); err != nil {
		return expense.ExpenseCreated{}, fmt.Errorf("unmarshal expense.created: %w", err)
	}
	return ev, nil
}

// summaryConsumerHandler updates the shared running aggregate. Reading it back
// via GET /summary is what proves the Pub/Sub hop ran, distinct from GET
// /expenses (which proves the GCS-finalize hop).
func summaryConsumerHandler(ctx context.Context, e event.Event) error {
	ev, err := decodeExpenseCreated(e)
	if err != nil {
		return err
	}
	st, err := getStore(ctx)
	if err != nil {
		return err
	}
	s, err := st.IncrementSummary(ctx, ev.AmountCents)
	if err != nil {
		return fmt.Errorf("increment summary: %w", err)
	}
	log.Printf("SummaryConsumer: expense %s vendor=%s amount_cents=%d | running total=%d cents over %d expenses",
		ev.ID, ev.Vendor, ev.AmountCents, s.TotalCents, s.Count)
	return nil
}
