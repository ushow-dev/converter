-- Add claim tracking columns for the scanner HTTP API.

ALTER TABLE scanner_incoming_items
    ADD COLUMN IF NOT EXISTS claimed_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_incoming_claim_expires
    ON scanner_incoming_items (claim_expires_at)
    WHERE status = 'claimed';
