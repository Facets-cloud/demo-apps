package signer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFakeSigner_SignPut(t *testing.T) {
	fs := NewFake()
	url, err := fs.SignPut(context.Background(), "receipts/x.jpg", "image/jpeg", 15*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(url, "receipts/x.jpg") {
		t.Errorf("expected signed url to reference the object, got %q", url)
	}
	if len(fs.Calls) != 1 || fs.Calls[0].Object != "receipts/x.jpg" {
		t.Errorf("expected recorded call, got %+v", fs.Calls)
	}
	if fs.Calls[0].ContentType != "image/jpeg" {
		t.Errorf("content type not recorded: %+v", fs.Calls[0])
	}
}

func TestFakeSigner_Error(t *testing.T) {
	fs := NewFake()
	fs.Err = errors.New("boom")
	if _, err := fs.SignPut(context.Background(), "o", "image/png", time.Minute); err == nil {
		t.Fatalf("expected error to propagate")
	}
}

var _ URLSigner = (*Fake)(nil)
