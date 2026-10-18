CREATE TABLE claims (
  ifay_id    TEXT PRIMARY KEY,
  claimed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
