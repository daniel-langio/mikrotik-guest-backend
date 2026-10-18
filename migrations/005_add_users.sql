CREATE TABLE users (
  username      TEXT PRIMARY KEY,
  password_hash TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE login_history (
  id         SERIAL PRIMARY KEY,
  username   TEXT NOT NULL,
  success    BOOLEAN NOT NULL,
  ip         TEXT,
  user_agent TEXT,
  at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Sessions that predate this migration were all admin logins, hence the default.
ALTER TABLE admin_sessions
  ADD COLUMN username TEXT NOT NULL DEFAULT '',
  ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT true;
