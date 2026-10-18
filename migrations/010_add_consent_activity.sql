-- Explicit, versioned consent to activity recording (free access in exchange for data).
CREATE TABLE consents (
  id             BIGSERIAL PRIMARY KEY,
  at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  session_id     INT NOT NULL,
  mac_address    TEXT NOT NULL,
  policy_version TEXT NOT NULL,
  lang           TEXT,
  ip_address     TEXT,
  user_agent     TEXT,
  withdrawn_at   TIMESTAMPTZ
);
CREATE INDEX consents_session ON consents (session_id);

-- One row per router connection of a consenting device (domain, IP, port, bytes).
CREATE TABLE activity_log (
  id          BIGSERIAL PRIMARY KEY,
  session_id  INT NOT NULL,
  mac_address TEXT NOT NULL,
  username    TEXT,
  router      TEXT NOT NULL,
  proto       TEXT,
  dst_ip      TEXT,
  dst_port    INT,
  domain      TEXT,
  bytes_up    BIGINT NOT NULL DEFAULT 0,
  bytes_down  BIGINT NOT NULL DEFAULT 0,
  conn_key    TEXT NOT NULL,
  first_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (session_id, conn_key)
);
CREATE INDEX activity_log_seen ON activity_log (last_seen DESC);
