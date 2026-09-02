// Package parser owns the single filename convention that the frontend
// (via CreateUploadURL) and the backend (via ReceiptUploaded) both use to
// pass expense metadata through a GCS object name — no OCR required.
//
// Convention:
//
//	receipts/<YYYY-MM-DD>_<vendorslug>_<amount2dp>_<CURRENCY>_<shortid>.<ext>
//	e.g. receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg
//
// Legacy 4-part names without a currency segment still parse (currency empty).
package parser

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/expense"
)

const (
	prefix     = "receipts/"
	dateLayout = "2006-01-02"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var nonAlnumUpper = regexp.MustCompile(`[^A-Z0-9]+`)

// normalizeCurrency uppercases a currency code and strips any character that
// would collide with the "_" delimiter (or otherwise break the convention).
func normalizeCurrency(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return nonAlnumUpper.ReplaceAllString(s, "")
}

// slugify lowercases and replaces any run of non-alphanumeric characters with a
// single dash, trimming leading/trailing dashes.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// shortID returns a 6-char lowercase-hex random id.
func shortID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a deterministic-ish value; ids need not be crypto-strong.
		return "000000"
	}
	return hex.EncodeToString(b)
}

// BuildObjectName constructs a canonical GCS object name from expense metadata.
// date must be YYYY-MM-DD; currency and ext must be non-empty; amount must be > 0.
func BuildObjectName(date, vendor string, amount float64, currency, ext string) (string, error) {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return "", fmt.Errorf("invalid date %q: want YYYY-MM-DD", date)
	}
	slug := slugify(vendor)
	if slug == "" {
		return "", fmt.Errorf("vendor slugifies to empty: %q", vendor)
	}
	if amount <= 0 {
		return "", fmt.Errorf("amount must be > 0, got %v", amount)
	}
	cur := normalizeCurrency(currency)
	if cur == "" {
		return "", fmt.Errorf("currency must be non-empty: %q", currency)
	}
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		return "", fmt.Errorf("ext must be non-empty")
	}
	return fmt.Sprintf("%s%s_%s_%.2f_%s_%s.%s", prefix, date, slug, amount, cur, shortID(), ext), nil
}

// Parse reverses BuildObjectName into a domain Expense. It is lenient: a
// malformed date or amount falls back to safe zero values rather than erroring,
// but a name with no recognizable structure returns an error.
func Parse(objectName string) (expense.Expense, error) {
	e := expense.Expense{SourceObject: objectName}

	base := strings.TrimPrefix(objectName, prefix)
	if dot := strings.LastIndex(base, "."); dot >= 0 {
		base = base[:dot] // strip .ext
	}
	parts := strings.Split(base, "_")
	if len(parts) < 3 {
		return expense.Expense{}, fmt.Errorf("unparseable object name %q", objectName)
	}

	// Canonical: [date, vendor, amount, currency, shortid].
	// Legacy:    [date, vendor, amount, shortid] — no currency segment.
	if t, err := time.Parse(dateLayout, parts[0]); err == nil {
		e.SpentOn = t
	}
	e.Vendor = parts[1]
	if e.Vendor == "" {
		e.Vendor = "unknown"
	}
	if major, err := strconv.ParseFloat(parts[2], 64); err == nil {
		e.AmountCents = int64(math.Round(major * 100))
	}
	if len(parts) >= 5 {
		e.Currency = parts[3]
	}
	return e, nil
}
