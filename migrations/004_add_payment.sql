ALTER TABLE pending_sessions
  ADD COLUMN provider TEXT,
  ADD COLUMN psp_ref  TEXT,
  ADD COLUMN amount   INT,
  ADD COLUMN claim_id TEXT;
-- One guest per Mobile Money ref: ifay would otherwise let a second claim overwrite the first.
CREATE UNIQUE INDEX pending_sessions_psp_ref ON pending_sessions (provider, psp_ref);
