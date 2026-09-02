package uploadurl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/signer"
)

func validReq() CreateRequest {
	return CreateRequest{
		Vendor:      "Starbucks",
		Amount:      4.50,
		Currency:    "USD",
		Date:        "2026-09-01",
		Ext:         "jpg",
		ContentType: "image/jpeg",
	}
}

func TestCreate_Success(t *testing.T) {
	sg := signer.NewFake()
	h := New(sg, 15*time.Minute)

	resp, err := h.Create(context.Background(), validReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Method != "PUT" {
		t.Errorf("method = %q, want PUT", resp.Method)
	}
	if !strings.HasPrefix(resp.ObjectName, "receipts/2026-09-01_starbucks_4.50_") {
		t.Errorf("object name = %q", resp.ObjectName)
	}
	if resp.UploadURL == "" || !strings.Contains(resp.UploadURL, resp.ObjectName) {
		t.Errorf("upload url = %q should reference object %q", resp.UploadURL, resp.ObjectName)
	}
	if resp.Headers["Content-Type"] != "image/jpeg" {
		t.Errorf("content-type header = %q", resp.Headers["Content-Type"])
	}
	// Signer must have been called with the object + content type + ttl.
	if len(sg.Calls) != 1 {
		t.Fatalf("expected 1 signer call, got %d", len(sg.Calls))
	}
	if sg.Calls[0].Object != resp.ObjectName || sg.Calls[0].ContentType != "image/jpeg" {
		t.Errorf("signer called with %+v", sg.Calls[0])
	}
	if sg.Calls[0].TTL != 15*time.Minute {
		t.Errorf("ttl = %v", sg.Calls[0].TTL)
	}
}

func TestCreate_Validation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(r *CreateRequest)
	}{
		{"empty vendor", func(r *CreateRequest) { r.Vendor = "" }},
		{"zero amount", func(r *CreateRequest) { r.Amount = 0 }},
		{"negative amount", func(r *CreateRequest) { r.Amount = -5 }},
		{"empty currency", func(r *CreateRequest) { r.Currency = "" }},
		{"empty ext", func(r *CreateRequest) { r.Ext = "" }},
		{"empty content type", func(r *CreateRequest) { r.ContentType = "" }},
		{"bad date", func(r *CreateRequest) { r.Date = "09-01-2026" }},
		{"empty date", func(r *CreateRequest) { r.Date = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sg := signer.NewFake()
			h := New(sg, time.Minute)
			r := validReq()
			c.mutate(&r)
			if _, err := h.Create(context.Background(), r); err == nil {
				t.Fatalf("expected validation error for %s", c.name)
			}
			if len(sg.Calls) != 0 {
				t.Errorf("signer must not be called on invalid input")
			}
		})
	}
}

func TestCreate_SignerErrorPropagates(t *testing.T) {
	sg := signer.NewFake()
	sg.Err = errSentinel{}
	h := New(sg, time.Minute)
	if _, err := h.Create(context.Background(), validReq()); err == nil {
		t.Fatalf("expected signer error to propagate")
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sign failed" }
