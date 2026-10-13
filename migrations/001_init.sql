CREATE TABLE pending_sessions (
  id          SERIAL PRIMARY KEY,
  mac_address TEXT NOT NULL,
  ip_address  TEXT,
  username    TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'pending', -- pending | approved | rejected | expired
  duration    TEXT,                            -- MikroTik limit-uptime format, e.g. "02:00:00"
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  approved_at TIMESTAMPTZ
);
