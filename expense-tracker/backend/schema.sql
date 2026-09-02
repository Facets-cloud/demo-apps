-- expenses table for the expense-tracker demo (design spec §4).
-- Money is stored as integer amount_cents to avoid float drift.
CREATE TABLE IF NOT EXISTS expenses (
  id            UUID PRIMARY KEY,
  vendor        TEXT NOT NULL,
  amount_cents  BIGINT NOT NULL,
  currency      TEXT NOT NULL,
  spent_on      DATE NOT NULL,
  source_object TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
