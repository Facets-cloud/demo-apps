// Package uploadurl holds the CreateUploadURL core: validate expense metadata,
// build the canonical object name via parser, and mint a signed PUT URL via the
// URLSigner seam. It depends only on parser + signer — no GCP client libraries.
package uploadurl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/parser"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/signer"
)

// CreateRequest is the metadata the frontend submits to mint an upload URL.
type CreateRequest struct {
	Vendor      string
	Amount      float64
	Currency    string
	Date        string // YYYY-MM-DD
	Ext         string
	ContentType string
}

// CreateResponse is returned to the frontend so it can PUT the file to GCS.
type CreateResponse struct {
	UploadURL  string            `json:"uploadUrl"`
	ObjectName string            `json:"objectName"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
}

// Handler mints upload URLs.
type Handler struct {
	signer signer.URLSigner
	ttl    time.Duration
}

// New builds a Handler from a URLSigner and the URL time-to-live.
func New(s signer.URLSigner, ttl time.Duration) *Handler {
	return &Handler{signer: s, ttl: ttl}
}

// Create validates the request, builds the object name, and signs a PUT URL.
func (h *Handler) Create(ctx context.Context, req CreateRequest) (CreateResponse, error) {
	if strings.TrimSpace(req.Vendor) == "" {
		return CreateResponse{}, fmt.Errorf("vendor is required")
	}
	if req.Amount <= 0 {
		return CreateResponse{}, fmt.Errorf("amount must be > 0")
	}
	if strings.TrimSpace(req.Currency) == "" {
		return CreateResponse{}, fmt.Errorf("currency is required")
	}
	if strings.TrimSpace(req.Ext) == "" {
		return CreateResponse{}, fmt.Errorf("ext is required")
	}
	if strings.TrimSpace(req.ContentType) == "" {
		return CreateResponse{}, fmt.Errorf("contentType is required")
	}

	object, err := parser.BuildObjectName(req.Date, req.Vendor, req.Amount, req.Currency, req.Ext)
	if err != nil {
		return CreateResponse{}, err // covers bad/empty date and vendor/ext edge cases
	}

	url, err := h.signer.SignPut(ctx, object, req.ContentType, h.ttl)
	if err != nil {
		return CreateResponse{}, err
	}

	return CreateResponse{
		UploadURL:  url,
		ObjectName: object,
		Method:     "PUT",
		Headers:    map[string]string{"Content-Type": req.ContentType},
	}, nil
}
