# Expense Tracker — Backend

Go backend for the expense-tracker demo. The receipt filename is the source of
truth for expense metadata (no OCR): the frontend requests a signed upload URL,
uploads the receipt to GCS under a canonical name, GCS finalize triggers
parsing + persistence, and an `expense.created` event fans out to a summary
consumer.

## One image, three services

The backend is built as **one container image** and deployed as **three Cloud
Run services**. Each service runs the same binary (`cmd/server`) and selects
which handler to serve via the **`FUNCTION_TARGET`** env var, read by the
[functions-framework-go](https://github.com/GoogleCloudPlatform/functions-framework-go).

```
                       ┌──────────────────────────────────────────────┐
                       │        expense-tracker-backend image         │
                       │  cmd/server → funcframework.Start($PORT)      │
                       │  serves the ONE function named FUNCTION_TARGET│
                       └──────────────────────────────────────────────┘
                                          │ deployed as 3 services
        ┌─────────────────────────────────┼─────────────────────────────────┐
        ▼                                 ▼                                 ▼
 FUNCTION_TARGET=              FUNCTION_TARGET=                 FUNCTION_TARGET=
   CreateUploadURL               ReceiptUploaded                 SummaryConsumer
        │                                 │                                 │
   HTTPS request                Eventarc trigger                 Eventarc trigger
   (from browser)         google.cloud.storage.object      google.cloud.pubsub.topic
        │                     .v1.finalized                   .v1.messagePublished
        ▼                                 ▼                                 ▼
  returns a signed          parse name → store row →        base64-decode message →
  GCS upload URL            publish expense.created         update running total (log)
```

- **CreateUploadURL** (`functions.HTTP`) — HTTPS. Browser POSTs expense
  metadata; returns a V4 signed PUT URL for the receipt object.
- **ReceiptUploaded** (`functions.CloudEvent`) — Eventarc
  `google.cloud.storage.object.v1.finalized`. The CloudEvent `data` is the
  Storage object JSON (`bucket`, `name`, `contentType`, `size`, `timeCreated`);
  `size` may be a JSON string or number and both are handled. Decodes → parses
  the object name → stores the expense → publishes `expense.created`.
- **SummaryConsumer** (`functions.CloudEvent`) — Eventarc
  `google.cloud.pubsub.topic.v1.messagePublished`. The CloudEvent `data` is the
  `MessagePublishedData` envelope (`{"message":{"data":"<base64>",…},
  "subscription":"…"}`); `message.data` is base64-decoded into the
  `expense.created` JSON and folded into a process-lifetime running total.

The declarative registrations live in `function.go` (`package function`) inside
its `init()`. `cmd/server/main.go` blank-imports that package so the
registrations run, then starts the framework server.

## Environment variables

`PORT` is set by Cloud Run; `FUNCTION_TARGET` is set per service (one of
`CreateUploadURL`, `ReceiptUploaded`, `SummaryConsumer`). Each handler needs a
subset of the rest:

| Variable          | Used by                          | Purpose                                             |
| ----------------- | -------------------------------- | --------------------------------------------------- |
| `FUNCTION_TARGET` | all (framework)                  | Which registered function this service serves       |
| `PORT`            | all (framework)                  | Listen port (Cloud Run injects it; defaults 8080)   |
| `RECEIPT_BUCKET`  | CreateUploadURL                  | GCS bucket receipts are uploaded to                 |
| `SIGNER_SA_EMAIL` | CreateUploadURL                  | Service account used to sign the V4 upload URL      |
| `SIGN_URL_TTL`    | CreateUploadURL (optional)       | Signed-URL lifetime, Go duration (default `15m`)    |
| `DB_DSN`          | ReceiptUploaded                  | Postgres/Cloud SQL DSN for the expenses table       |
| `GCP_PROJECT`     | ReceiptUploaded                  | Project hosting the Pub/Sub topic                   |
| `PUBSUB_TOPIC`    | ReceiptUploaded                  | Topic that `expense.created` is published to        |

## Build & run

```bash
# Tests / vet / build
go test ./...
go vet ./...
go build ./... && go build ./cmd/server

# Offline end-to-end walkthrough (parse → store → publish, all fakes, no GCP)
go run ./cmd/local

# Container image (deployed as all three services, differing only in FUNCTION_TARGET)
docker build -t expense-tracker-backend .

# Run one service locally
docker run --rm -p 8080:8080 \
  -e FUNCTION_TARGET=CreateUploadURL \
  -e RECEIPT_BUCKET=my-receipts -e SIGNER_SA_EMAIL=signer@proj.iam.gserviceaccount.com \
  expense-tracker-backend
```
