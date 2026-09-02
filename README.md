# demo-apps

A collection of small, self-contained, **GCP-native** demo applications.
Each app lives in its own folder (monorepo-style) and exercises real
Google Cloud managed services in a story you can explain in one breath.

## Apps

| App                                  | What it shows | GCP services |
|--------------------------------------|---------------|--------------|
| [`expense-tracker/`](./expense-tracker) | Upload a receipt → it's parsed, stored, and an event fans out for async processing. | Cloud Storage · Cloud Functions (Gen2) · Cloud SQL (Postgres) · Pub/Sub |

More apps may be added over time under their own top-level folders.

## Conventions across apps
- Each app is independently buildable and testable, with its **entire
  test suite runnable offline** (no GCP project, no network) via injected
  fakes at every cloud boundary.
- Infrastructure is intentionally **not** hand-written as Terraform/gcloud
  in these repos — each app ships a `docs/INFRA.md` describing exactly the
  resources it needs, to later be provisioned as a Facets module.
- TDD throughout: tests are written first.

See each app's own `README.md` to build, test, and run it.
