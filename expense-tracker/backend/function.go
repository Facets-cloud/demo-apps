// Package function registers the three Gen2 Cloud Functions for the
// expense-tracker demo and wires the real GCP-backed implementations from
// environment variables. It intentionally contains no business logic — every
// handler decodes its input and delegates to a core package (service /
// uploadurl) that is fully unit-tested offline.
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
	functions.HTTP("CreateUploadURL", createUploadURLHandler)
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
// CreateUploadURL (HTTP)
// ---------------------------------------------------------------------------

// createRequestJSON is the wire shape the frontend POSTs.
type createRequestJSON struct {
	Vendor      string  `json:"vendor"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Date        string  `json:"date"`
	Ext         string  `json:"ext"`
	ContentType string  `json:"contentType"`
}

func createUploadURLHandler(w http.ResponseWriter, r *http.Request) {
	// CORS: this endpoint is called directly from the browser.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
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

	svc, cleanup, err := newService(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	return svc.Handle(ctx, obj)
}

// memStore is a process-lifetime in-memory store. It exists only as long as the
// Cloud Run instance: data is lost on restart, on scale-to-zero, and is NOT
// shared across instances. This is the "in-memory now, Cloud SQL later" mode —
// set DB_DSN to switch to the durable Postgres store.
var memStore = store.NewFake()

func newService(ctx context.Context) (*service.Service, func(), error) {
	project := os.Getenv("GCP_PROJECT")
	topic := os.Getenv("PUBSUB_TOPIC")
	if project == "" || topic == "" {
		return nil, nil, fmt.Errorf("GCP_PROJECT and PUBSUB_TOPIC are required")
	}
	pub, err := events.NewPubSubPublisher(ctx, project, topic)
	if err != nil {
		return nil, nil, fmt.Errorf("publisher init: %w", err)
	}

	// Durable Postgres when DB_DSN is set; otherwise the ephemeral in-memory store.
	if dsn := os.Getenv("DB_DSN"); dsn != "" {
		st, err := store.NewPgxStore(ctx, dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("store init: %w", err)
		}
		return service.New(st, pub), func() { st.Close() }, nil
	}

	log.Printf("ReceiptUploaded: DB_DSN unset — using in-memory store (ephemeral; data is lost on scale-to-zero/restart)")
	return service.New(memStore, pub), func() {}, nil
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

var (
	summaryMu    sync.Mutex
	runningTotal int64 // process-lifetime running total in cents
	runningCount int64
)

func summaryConsumerHandler(_ context.Context, e event.Event) error {
	ev, err := decodeExpenseCreated(e)
	if err != nil {
		return err
	}

	summaryMu.Lock()
	runningTotal += ev.AmountCents
	runningCount++
	total, count := runningTotal, runningCount
	summaryMu.Unlock()

	log.Printf("SummaryConsumer: expense %s vendor=%s amount_cents=%d | running total=%d cents over %d expenses",
		ev.ID, ev.Vendor, ev.AmountCents, total, count)
	return nil
}
