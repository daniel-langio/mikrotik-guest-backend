-- 'explicit' = ticked a box; 'implied' = used the service after the notice line.
ALTER TABLE consents ADD COLUMN method TEXT NOT NULL DEFAULT 'explicit';
