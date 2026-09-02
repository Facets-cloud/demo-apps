# expense-tracker

A GCP-native demo: upload a receipt image and the expense is parsed,
recorded in Postgres, and fanned out as an event for async processing —
built on **Cloud Storage, Cloud Functions (Gen2), Cloud SQL (Postgres),
and Pub/Sub**. Monorepo-style: a Go `backend/` and a React `frontend/`.

## Architecture

```
   frontend/ (Vite+React+TS)
     │  1. POST {vendor, amount, date, ext, contentType}
     ▼
   CreateUploadURL (HTTP fn) ──▶ V4 signed PUT URL + objectName
     │  2. browser PUTs file directly to GCS
     ▼
   GCS bucket (receipts/…) ── object.finalized ─▶ ReceiptUploaded (fn)
                                                     parse → store → publish
                                                       │           │
                                     expense.created   │           ▼
                                          ▼            │      Cloud SQL (PG)
                                     Pub/Sub topic ────┘
                                          ▼
                                   SummaryConsumer (fn)  logs + running total
```

The single clever bit: a filename convention
`receipts/YYYY-MM-DD_vendor_amount_CURRENCY_shortid.ext` is the contract
between the two sides — `CreateUploadURL` builds it, `ReceiptUploaded`
parses it. No OCR needed for the demo. (Legacy 4-part names without a
currency segment still parse, with currency left empty.)

## Layout

```
expense-tracker/
  backend/      Go module — the 3 Cloud Functions (see backend/)
  frontend/     Vite + React + TS upload UI (see frontend/)
  docs/INFRA.md the GCP resources + IAM to provision (later, via Facets)
  docs/superpowers/specs/  the design spec
```

## Build & test (offline — no GCP needed)

Backend:
```bash
cd backend
go test ./...      # unit tests via injected fakes
go run ./cmd/local # see the ReceiptUploaded flow end-to-end locally
```

Frontend:
```bash
cd frontend
npm install
npm test           # vitest
npm run dev        # local dev server
```

Set `VITE_UPLOAD_URL_ENDPOINT` (see `frontend/.env.example`) to point the
UI at a deployed `CreateUploadURL` function.

## Deploy

Not provisioned yet by design. `docs/INFRA.md` specifies the bucket,
Cloud SQL instance, Pub/Sub topic/subscription, the three Gen2 functions,
env wiring, and least-privilege IAM (including the `signBlob` grant that
V4 signed URLs require) — to be authored as a Facets module.
