# Infrastructure contract — expense-tracker

No IaC is authored in this repo yet. Infrastructure will be provisioned
later as a **Facets module**. This document is the contract that module
must satisfy: the GCP resources, wiring, and least-privilege IAM the app
expects at runtime.

## Resources

### 1. Cloud Storage bucket — `RECEIPT_BUCKET`
- Holds uploaded receipt images under the `receipts/` prefix.
- **Event trigger:** `google.cloud.storage.object.v1.finalized` on this
  bucket → invokes the **ReceiptUploaded** function.
- Optional lifecycle rule to expire objects after N days (demo hygiene).

### 2. Cloud SQL for PostgreSQL
- One instance, one database, one application user.
- Apply `../backend/schema.sql` to create the `expenses` table.
- Reached by the functions via the `DB_DSN` env var (a libpq/pgx DSN).
  In GCP, prefer the Cloud SQL Go connector or the
  `/cloudsql/<INSTANCE_CONNECTION_NAME>` unix socket.

### 3. Pub/Sub
- **Topic:** carries `expense.created` events (JSON `ExpenseCreated`).
  Referenced by functions via `PUBSUB_TOPIC`.
- **Subscription** feeding the **SummaryConsumer** function (push or the
  Gen2 CloudEvent/Eventarc wiring).

### 4. Cloud Functions (Gen2) — three, one Go source (`../backend`)
| Function          | Trigger                              | Purpose |
|-------------------|--------------------------------------|---------|
| `CreateUploadURL` | HTTPS                                | Mints a V4 signed PUT URL for the browser. |
| `ReceiptUploaded` | GCS `object.finalized` on the bucket | Parse → insert row → publish event. |
| `SummaryConsumer` | Pub/Sub topic                        | Minimal async leg: logs + running total. |

### 5. Service account + IAM (least privilege)
The functions' runtime service account needs:
- `roles/storage.objectAdmin` (or narrower) on `RECEIPT_BUCKET` — read
  uploaded objects; create signed URLs.
- `roles/cloudsql.client` — connect to the instance.
- `roles/pubsub.publisher` on the topic (ReceiptUploaded).
- `roles/pubsub.subscriber` on the subscription (SummaryConsumer).
- **`roles/iam.serviceAccountTokenCreator`** on itself (or
  `iam.serviceAccounts.signBlob`) — required so `CreateUploadURL` can
  produce **V4 signed URLs** via IAM SignBlob without a private key file.

## Environment variables (per function)

| Var               | Used by                        | Example / notes |
|-------------------|--------------------------------|-----------------|
| `RECEIPT_BUCKET`  | CreateUploadURL, ReceiptUploaded | bucket name |
| `PUBSUB_TOPIC`    | ReceiptUploaded                | topic id |
| `GCP_PROJECT`     | all                            | project id |
| `DB_DSN`          | ReceiptUploaded                | pgx/libpq DSN |
| `SIGN_URL_TTL`    | CreateUploadURL                | e.g. `15m` (default) |
| `SIGNER_SA_EMAIL` | CreateUploadURL                | SA email used as `GoogleAccessID` for V4 signing |

## Data flow (summary)

```
Browser → CreateUploadURL (HTTPS) → signed PUT URL
Browser → PUT file → GCS bucket (receipts/…)
GCS object.finalized → ReceiptUploaded → Cloud SQL INSERT + Pub/Sub publish
Pub/Sub → SummaryConsumer → log + running total
```

## Notes for the Facets module author
- The three functions share one Go source dir (`../backend`) but have
  distinct entry points (`CreateUploadURL`, `ReceiptUploaded`,
  `SummaryConsumer`) — deploy as three functions pointing at the same
  source, differing by entry point and trigger.
- The signed-URL path is what avoids routing file bytes through the
  backend; keep the `signBlob`/`TokenCreator` grant in the module or
  signing fails at runtime with a 403.
