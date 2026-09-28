-- No secret/token material is stored here. The bearer credential
-- embedded in the verification email is derived by the worker at send
-- time as HMAC-SHA256(AUTH_EMAIL_VERIFICATION_SECRET, id || expires_at)
-- (see identity/infrastructure/security.VerificationSigner.SignVerificationToken)
-- and is never persisted anywhere — not here, not in the outbox event
-- that requests delivery. A full dump of this table (or of
-- outbox_events) hands an attacker only non-secret identifiers and
-- timestamps; without the out-of-band HMAC secret they cannot
-- construct a token that will verify. This also makes retries trivially
-- safe: the same (id, expires_at) always signs to the same token, so a
-- crashed-then-retried delivery links to the exact same credential.
CREATE TABLE email_verifications (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX email_verifications_user_id_idx ON email_verifications (user_id);

-- IMPORTANT — exact meaning: "at most one currently UNCONSUMED
-- credential per user", not "at most one currently ACTIVE (unexpired)
-- credential per user". An expired-but-unconsumed row still satisfies
-- consumed_at IS NULL and therefore still counts against this
-- constraint. A partial index cannot depend on now(), so expiry can't
-- be folded into the predicate itself.
--
-- Consequence for a future resend feature: issuing a new verification
-- credential for a user who already has an unconsumed (possibly
-- expired) row MUST, in the same transaction, first invalidate that
-- row (e.g. UPDATE ... SET consumed_at = now() WHERE user_id = $1 AND
-- consumed_at IS NULL) before INSERTing the new one — never insert
-- first and clean up after, and never treat expiry alone as already
-- having freed the slot. This index's job is exactly to make a resend
-- implementation that skips that step fail loudly (a unique-violation)
-- rather than silently create two concurrently-issuable credentials.
CREATE UNIQUE INDEX email_verifications_one_active_per_user ON email_verifications (user_id) WHERE consumed_at IS NULL;
