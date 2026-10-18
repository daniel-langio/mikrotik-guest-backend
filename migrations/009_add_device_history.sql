-- hostname / vendor_class come from the router's DHCP lease (see LeaseInfo).
ALTER TABLE pending_sessions ADD COLUMN user_agent TEXT, ADD COLUMN hostname TEXT, ADD COLUMN vendor_class TEXT;

-- Append-only log of every device event (arrived, approved, expired, kicked,
-- deleted). It snapshots the session row at event time, so it survives the
-- session row being deleted.
CREATE TABLE device_history (
  id          BIGSERIAL PRIMARY KEY,
  at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  event       TEXT NOT NULL,   -- pending | approved | expired | disconnected | deleted
  actor       TEXT NOT NULL DEFAULT '', -- admin:<name> | payment | open | expiry
  session_id  INT,
  mac_address TEXT NOT NULL,
  ip_address  TEXT,
  username    TEXT,
  router      TEXT,
  os          TEXT,
  user_agent  TEXT,
  hostname    TEXT,
  vendor_class TEXT,
  provider    TEXT,
  psp_ref     TEXT,
  amount      INT,
  duration    TEXT
);
CREATE INDEX device_history_mac ON device_history (mac_address, at DESC);

-- Backfill from the sessions that already exist.
INSERT INTO device_history (at, event, session_id, mac_address, ip_address, username, router, os, user_agent, hostname, vendor_class, provider, psp_ref, amount)
  SELECT created_at, 'pending', id, mac_address, ip_address, username, router, os, provider, psp_ref, amount
  FROM pending_sessions;
INSERT INTO device_history (at, event, session_id, mac_address, ip_address, username, router, os, provider, psp_ref, amount, duration)
  SELECT approved_at, 'approved', id, mac_address, ip_address, username, router, os, provider, psp_ref, amount, duration
  FROM pending_sessions WHERE approved_at IS NOT NULL;
