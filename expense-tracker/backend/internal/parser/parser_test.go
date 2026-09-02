package parser

import (
	"strings"
	"testing"
)

func TestBuildObjectName_Basic(t *testing.T) {
	name, err := BuildObjectName("2026-09-01", "Starbucks", 4.5, "USD", "jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(name, "receipts/2026-09-01_starbucks_4.50_USD_") {
		t.Fatalf("got %q, want prefix receipts/2026-09-01_starbucks_4.50_USD_", name)
	}
	if !strings.HasSuffix(name, ".jpg") {
		t.Fatalf("got %q, want .jpg suffix", name)
	}
	// receipts/2026-09-01_starbucks_4.50_USD_<6char>.jpg
	base := strings.TrimSuffix(strings.TrimPrefix(name, "receipts/"), ".jpg")
	parts := strings.Split(base, "_")
	if len(parts) != 5 {
		t.Fatalf("expected 5 underscore parts, got %d in %q", len(parts), base)
	}
	if parts[3] != "USD" {
		t.Fatalf("currency part = %q, want USD", parts[3])
	}
	if len(parts[4]) != 6 {
		t.Fatalf("expected 6-char shortid, got %q", parts[4])
	}
}

func TestBuildObjectName_NormalizesCurrency(t *testing.T) {
	name, err := BuildObjectName("2026-09-01", "Starbucks", 4.5, "usd", "jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := strings.TrimSuffix(strings.TrimPrefix(name, "receipts/"), ".jpg")
	parts := strings.Split(base, "_")
	if parts[3] != "USD" {
		t.Fatalf("currency = %q, want normalized USD", parts[3])
	}
}

func TestBuildObjectName_SlugifiesVendor(t *testing.T) {
	name, err := BuildObjectName("2026-09-01", "Joe's  Café & Grill!", 12.0, "USD", "png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := strings.TrimSuffix(strings.TrimPrefix(name, "receipts/"), ".png")
	parts := strings.Split(base, "_")
	// vendor slug should be lowercase alnum with single dashes, no leading/trailing dash
	if parts[1] != "joe-s-caf-grill" {
		t.Fatalf("vendor slug = %q, want joe-s-caf-grill", parts[1])
	}
}

func TestBuildObjectName_Validation(t *testing.T) {
	cases := []struct {
		name                      string
		date, ext, vendor, curanc string
		amount                    float64
	}{
		{"bad date", "2026/09/01", "jpg", "v", "USD", 1},
		{"empty date", "", "jpg", "v", "USD", 1},
		{"empty ext", "2026-09-01", "", "v", "USD", 1},
		{"empty vendor", "2026-09-01", "jpg", "", "USD", 1},
		{"empty currency", "2026-09-01", "jpg", "v", "", 1},
		{"negative amount", "2026-09-01", "jpg", "v", "USD", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := BuildObjectName(c.date, c.vendor, c.amount, c.curanc, c.ext); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

func TestParse_Basic(t *testing.T) {
	e, err := Parse("receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Vendor != "starbucks" {
		t.Errorf("vendor = %q, want starbucks", e.Vendor)
	}
	if e.AmountCents != 450 {
		t.Errorf("amount_cents = %d, want 450", e.AmountCents)
	}
	if e.Currency != "USD" {
		t.Errorf("currency = %q, want USD", e.Currency)
	}
	if e.SpentOn.Format("2006-01-02") != "2026-09-01" {
		t.Errorf("spent_on = %v, want 2026-09-01", e.SpentOn)
	}
	if e.SourceObject != "receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg" {
		t.Errorf("source_object = %q", e.SourceObject)
	}
}

func TestParse_LargeAmount(t *testing.T) {
	e, err := Parse("receipts/2026-09-01_acme_1234.50_EUR_zzz999.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.AmountCents != 123450 {
		t.Errorf("amount_cents = %d, want 123450", e.AmountCents)
	}
	if e.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR", e.Currency)
	}
}

func TestRoundTrip(t *testing.T) {
	name, err := BuildObjectName("2026-12-31", "Whole Foods Market", 99.99, "GBP", "jpeg")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	e, err := Parse(name)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if e.Vendor != "whole-foods-market" {
		t.Errorf("vendor = %q, want whole-foods-market", e.Vendor)
	}
	if e.AmountCents != 9999 {
		t.Errorf("amount_cents = %d, want 9999", e.AmountCents)
	}
	if e.Currency != "GBP" {
		t.Errorf("currency = %q, want GBP", e.Currency)
	}
	if e.SpentOn.Format("2006-01-02") != "2026-12-31" {
		t.Errorf("spent_on = %v", e.SpentOn)
	}
}

func TestParse_LegacyNoCurrency(t *testing.T) {
	// A 4-part legacy name (no currency segment) still parses vendor/amount;
	// currency is simply empty.
	e, err := Parse("receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Vendor != "starbucks" || e.AmountCents != 450 {
		t.Errorf("parsed fields wrong: %+v", e)
	}
	if e.Currency != "" {
		t.Errorf("currency = %q, want empty for legacy name", e.Currency)
	}
}

func TestParse_MalformedFallbacks(t *testing.T) {
	// Missing shortid and amount unparseable: lenient fallbacks, no hard error.
	e, err := Parse("receipts/2026-09-01_starbucks_notanumber.jpg")
	if err != nil {
		t.Fatalf("expected lenient parse, got error: %v", err)
	}
	if e.AmountCents != 0 {
		t.Errorf("amount_cents = %d, want 0 fallback", e.AmountCents)
	}
	if e.Vendor != "starbucks" {
		t.Errorf("vendor = %q, want starbucks", e.Vendor)
	}
}

func TestParse_BadDateFallback(t *testing.T) {
	// Unparseable date should fall back to zero-value rather than erroring.
	e, err := Parse("receipts/notadate_starbucks_4.50_USD_a1b2c3.jpg")
	if err != nil {
		t.Fatalf("expected lenient parse, got error: %v", err)
	}
	if e.Vendor != "starbucks" {
		t.Errorf("vendor = %q, want starbucks", e.Vendor)
	}
	if e.AmountCents != 450 {
		t.Errorf("amount_cents = %d, want 450", e.AmountCents)
	}
}

func TestParse_Unparseable(t *testing.T) {
	// A totally structureless name may error.
	if _, err := Parse("garbage"); err == nil {
		t.Fatalf("expected error for unparseable name")
	}
}
