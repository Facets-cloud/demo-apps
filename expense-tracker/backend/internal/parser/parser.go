// Package parser owns the single filename convention that the frontend
// (via CreateUploadURL) and the backend (via ReceiptUploaded) both use to
// pass expense metadata through a GCS object name — no OCR required.
//
// Convention:
//
//	receipts/<YYYY-MM-DD>_<vendorslug>_<amount2dp>_<shortid>.<ext>
//	e.g. receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg
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
// date must be YYYY-MM-DD; ext must be non-empty; amount must be > 0.
func BuildObjectName(date, vendor string, amount float64, ext string) (string, error) {
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
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		return "", fmt.Errorf("ext must be non-empty")
	}
	return fmt.Sprintf("%s%s_%s_%.2f_%s.%s", prefix, date, slug, amount, shortID(), ext), nil
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

	// parts: [date, vendor, amount, (shortid?)...]
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
	return e, nil
}
