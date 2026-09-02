// Package function registers the three Gen2 Cloud Functions for the
// expense-tracker demo and wires the real GCP-backed implementations from
// environment variables. It intentionally contains no business logic — every
// handler decodes its input and delegates to a core package (service /
// uploadurl) that is fully unit-tested offline.
package function

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
// need — avoids pulling in a heavy genproto dependency.
type storageObjectData struct {
	Bucket      string `json:"bucket"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        string `json:"size"` // GCS encodes size as a string
	TimeCreated string `json:"timeCreated"`
}

func receiptUploadedHandler(ctx context.Context, e event.Event) error {
	var data storageObjectData
	if err := e.DataAs(&data); err != nil {
		return fmt.Errorf("decode storage event: %w", err)
	}

	svc, cleanup, err := newService(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	return svc.Handle(ctx, service.GCSObject{
		Bucket:      data.Bucket,
		Name:        data.Name,
		ContentType: data.ContentType,
	})
}

func newService(ctx context.Context) (*service.Service, func(), error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return nil, nil, fmt.Errorf("DB_DSN is required")
	}
	st, err := store.NewPgxStore(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("store init: %w", err)
	}

	project := os.Getenv("GCP_PROJECT")
	topic := os.Getenv("PUBSUB_TOPIC")
	if project == "" || topic == "" {
		st.Close()
		return nil, nil, fmt.Errorf("GCP_PROJECT and PUBSUB_TOPIC are required")
	}
	pub, err := events.NewPubSubPublisher(ctx, project, topic)
	if err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("publisher init: %w", err)
	}

	cleanup := func() { st.Close() }
	return service.New(st, pub), cleanup, nil
}

// ---------------------------------------------------------------------------
// SummaryConsumer (CloudEvent: Pub/Sub topic)
// ---------------------------------------------------------------------------

// pubSubMessage is the Pub/Sub push envelope: the JSON payload is base64 in
// message.data.
type pubSubMessage struct {
	Message struct {
		Data []byte `json:"data"` // encoding/json base64-decodes []byte automatically
	} `json:"message"`
}

var (
	summaryMu    sync.Mutex
	runningTotal int64 // process-lifetime running total in cents
	runningCount int64
)

func summaryConsumerHandler(_ context.Context, e event.Event) error {
	var msg pubSubMessage
	if err := e.DataAs(&msg); err != nil {
		return fmt.Errorf("decode pubsub envelope: %w", err)
	}
	// Data may already be raw JSON (emulator) or base64 (real). encoding/json
	// handles the base64 case when the field is []byte; fall back to raw.
	payload := msg.Message.Data
	if len(payload) == 0 {
		return fmt.Errorf("empty pubsub message data")
	}
	// Some transports double-encode; try a defensive base64 decode if it's not JSON.
	if payload[0] != '{' {
		if decoded, err := base64.StdEncoding.DecodeString(string(payload)); err == nil {
			payload = decoded
		}
	}

	var ev expense.ExpenseCreated
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal expense.created: %w", err)
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
