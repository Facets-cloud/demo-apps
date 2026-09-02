# Expense Tracker — GCP-native demo (design spec)

**Date:** 2026-09-02
**Repo:** `demo-apps` · **App folder:** `expense-tracker/` (backend + frontend, monorepo-style)
**Status:** approved design, pre-implementation

## 1. Purpose

A relatable, self-contained demo that exercises four core GCP managed
services — **Cloud Storage, Cloud Functions (Gen2), Cloud SQL for
PostgreSQL, and Pub/Sub** — in a story anyone understands: uploading a
receipt and tracking the expense.

Goals:
- Read as a clean reference for wiring these services together.
- Run its **entire test suite offline** — no GCP project, no network.
- Be **deploy-ready** without provisioning anything this session
  (infrastructure will later be authored as a Facets module).

Non-goals (deferred): real OCR (Cloud Vision), production auth /
multi-tenancy, in-repo IaC (Terraform/gcloud), and any live deploy.

## 2. Architecture

```
   ┌─────────────────────────────┐
   │  frontend/  (Vite+React+TS) │  pick file + vendor/amount/date
   └───────────┬─────────────────┘
    1. POST metadata │        ▲ 2. { uploadUrl, objectName }
                     ▼        │
          ┌────────────────────────────┐
          │ CreateUploadURL (HTTP fn)  │  mints V4 signed PUT URL,
          │                            │  builds object name from convention
          └────────────────────────────┘
                     │ 3. browser PUTs file directly to GCS
                     ▼
          ┌──────────────────┐  object.finalize (CloudEvent)
          │   GCS bucket     │──────────────┐
          │   receipts/...   │              ▼
          └──────────────────┘   ┌─────────────────────────┐
                                 │ ReceiptUploaded (fn)    │
                                 │  parse → store → publish│
                                 └───────┬─────────┬───────┘
                             expense.created│      │ INSERT
                                 ▼          │      ▼
                          ┌──────────┐      │  ┌──────────────────┐
                          │ Pub/Sub  │      │  │ Cloud SQL (PG)   │
                          └────┬─────┘      │  │ expenses table   │
                               ▼            │  └──────────────────┘
                     ┌────────────────────┐ │
                     │ SummaryConsumer(fn)│◀┘  logs + running total
                     └────────────────────┘
```

Three Cloud Functions (Gen2), all in one Go module:

1. **CreateUploadURL** — HTTP. Validates `{vendor, amount, currency,
   date, ext}`, builds a canonical object name, returns a V4 signed PUT
   URL + the object name. No file bytes pass through it.
2. **ReceiptUploaded** — CloudEvent, triggered by GCS `object.finalize`.
   Parses the object name → persists an expense row → publishes
   `expense.created` to Pub/Sub.
3. **SummaryConsumer** — CloudEvent, triggered by the Pub/Sub topic.
   The minimal async leg: logs the event and a running total.

## 3. The FE↔BE contract: the filename convention

The `parser` package owns a single convention and both sides use it:

```
receipts/<YYYY-MM-DD>_<vendor>_<amount>_<CURRENCY>_<shortid>.<ext>
e.g. receipts/2026-09-01_starbucks_4.50_USD_a1b2c3.jpg
```

- `parser.BuildObjectName(date, vendor, amount, currency, ext) (string, error)`
  — used by **CreateUploadURL**. Slugifies vendor, formats amount,
  normalizes currency to uppercase, appends a short random id + extension.
  (A 4-part legacy name without the currency segment still parses, with
  currency left empty.)
- `parser.Parse(objectName) (Expense, error)` — used by
  **ReceiptUploaded**. Reverses it into a domain `Expense`, with sane
  fallbacks (unknown vendor, zero amount) rather than hard failures.

This is what lets the demo skip OCR while still telling one coherent
story, and it is fully unit-tested from both directions (round-trip).

## 4. Backend layout & boundaries

```
backend/
  go.mod
  function.go              registers the 3 CloudEvent/HTTP functions; wires
                           real impls from env; no business logic here
  schema.sql               expenses table DDL
  cmd/local/main.go        runs ReceiptUploaded locally against a fake event
  internal/
    expense/expense.go     domain model: Expense{ID, Vendor, Amount,
                           Currency, Date, SourceObject, CreatedAt}
    parser/parser.go       BuildObjectName + Parse (the §3 convention)
    signer/signer.go       URLSigner interface + GCS storage impl
    store/store.go         Store interface + pgxpool impl
    events/publisher.go    Publisher interface + Pub/Sub impl
    service/service.go     ReceiptUploaded core: Service.Handle(ctx, evt)
    uploadurl/uploadurl.go CreateUploadURL core: Handler.Create(ctx, req)
```

**Interfaces (the seams that make it testable):**

```go
type Store interface     { Save(ctx, Expense) (Expense, error) }
type Publisher interface { Publish(ctx, event ExpenseCreated) error }
type URLSigner interface { SignPut(ctx, object, contentType string, ttl time.Duration) (string, error) }
type Parser  // package funcs BuildObjectName / Parse
```

- `service.Service` depends on `Parser`, `Store`, `Publisher` — nothing
  GCP-specific. Tests inject fakes.
- `uploadurl.Handler` depends on `Parser`, `URLSigner`. Tests inject a
  fake signer.
- `function.go` is the only file that imports GCP client libraries and
  reads env; it constructs the real impls and hands them to the cores.

**Config (env vars, read only in `function.go`):**
`DB_DSN` (or Cloud SQL instance connection name), `PUBSUB_TOPIC`,
`GCP_PROJECT`, `RECEIPT_BUCKET`, `SIGN_URL_TTL`.

**Persistence:** `pgxpool`; `schema.sql` ships DDL:
```sql
CREATE TABLE IF NOT EXISTS expenses (
  id            UUID PRIMARY KEY,
  vendor        TEXT NOT NULL,
  amount_cents  BIGINT NOT NULL,
  currency      TEXT NOT NULL,
  spent_on      DATE NOT NULL,
  source_object TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```
Money stored as integer `amount_cents` to avoid float drift.

**Event payload (`expense.created`, JSON):**
`{ id, vendor, amount_cents, currency, spent_on, source_object }`.

**GCS event decoding:** define a minimal local struct
`{ Bucket, Name, ContentType, Size, TimeCreated }` decoded from the
CloudEvent data — avoids a heavy genproto dependency and keeps the
handler trivially testable.

## 5. Frontend layout

```
frontend/
  package.json  vite.config.ts  tsconfig.json  index.html
  src/
    main.tsx
    App.tsx            form: file + vendor + amount + currency + date
    api.ts             createUploadUrl(meta) → {uploadUrl, objectName};
                       putFile(uploadUrl, file)
    App.test.tsx       Vitest: renders form, validates, calls api (mocked)
    api.test.ts        Vitest: request shape + error handling (fetch mocked)
```

Flow: user fills the form and picks a file → `api.createUploadUrl` POSTs
metadata to **CreateUploadURL** → browser `PUT`s the file to the returned
signed URL → success state. The `VITE_UPLOAD_URL_ENDPOINT` env var points
at the function; a dev proxy handles it locally.

## 6. Testing strategy (TDD)

Every unit is written test-first: failing test → minimal impl → pass.

- **Backend, offline:**
  - `parser`: round-trip `BuildObjectName`↔`Parse`, edge cases
    (spaces/symbols in vendor, missing fields, weird amounts).
  - `service`: `Handle` calls parse→store→publish in order; propagates
    errors; publishes exactly the stored expense. Fakes for store/pub.
  - `uploadurl`: validation rejects bad input; success returns signed URL
    + object name from the convention. Fake signer.
  - `store`/`events`/`signer`: interface + fake; the real pgx/pubsub/gcs
    impls are thin and covered by an **optional**, build-tagged
    integration test (needs a local Postgres / emulators) that is not
    required for the suite to pass.
- **Frontend:** Vitest + Testing Library with `fetch`/api mocked. No
  network.

`go test ./...` and `npm test` both pass with **zero GCP**.

## 7. `docs/INFRA.md` — the contract for later Facets IaC

Enumerates what must be provisioned (no Terraform authored now):
- GCS bucket `RECEIPT_BUCKET` (+ event trigger to ReceiptUploaded).
- Cloud SQL Postgres instance, database, user; `expenses` table via
  `schema.sql`.
- Pub/Sub topic `expense.created` + subscription feeding SummaryConsumer.
- 3 Gen2 Cloud Functions + a service account with least-privilege IAM
  (storage object admin on the bucket, Cloud SQL client, pub/sub
  publisher+subscriber, `iam.serviceAccounts.signBlob` for V4 signing).
- Env-var wiring per function.

## 8. Build order (for the implementation plan)

1. Repo skeleton: `README`, `docs/INFRA.md`, backend `go.mod`, frontend
   scaffold.
2. `expense` domain + `parser` (TDD) — the contract first.
3. `store`, `events`, `signer` interfaces + fakes (TDD).
4. `service` core (ReceiptUploaded) TDD.
5. `uploadurl` core (CreateUploadURL) TDD.
6. Real GCP-backed impls + `function.go` registration + `cmd/local`.
7. Frontend: `api.ts` + `App.tsx` (TDD with Vitest).
8. READMEs, final `go test ./...` + `npm test` green.
