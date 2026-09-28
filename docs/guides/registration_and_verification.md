# Registration and email verification

## Boundaries and types

```text
HTTP RegisterRequest -> RegisterInput -> RegisterService
  -> one transaction: user + verification + EmailVerificationRequestedV1
  -> HTTP 201 (account created, delivery queued)

outbox claim -> worker adapter -> DeliverEmailVerificationService
  -> validate payload and check current credential/recipient
  -> sign token -> localize -> mail.Sender -> SMTP
  -> record handled outcome or retry/failure
```

`RegisterRequest` is generated from OpenAPI and stays in HTTP transport.
`RegisterInput` is the use case's input. The persisted event is a separate,
frozen worker contract: its `V1` is independent of the HTTP URL version.
It contains identifiers, recipient, and expiry, never a password or bearer
token. Domain entities are not serialized into events.

`RegistrationStore` commits all registration writes together. Registration
and resend share `insertVerificationDelivery`; neither makes a separate
outbox write after committing business state. Hashing precedes registration
writes. SQL constraint failures roll the entire transaction back.

## Verification and resend

- `POST /api/v1/auth/verify-email`, JSON `{"token":"..."}`, is public and
  rate limited. A valid, current, unconsumed credential for an active user
  returns 204 and sets `email_verified_at` in the same transaction that
  consumes the credential. Invalid, expired, replaced, disabled-account,
  or replayed credentials return 400 / `invalid_email_verification`.
- `POST /api/v1/auth/resend-verification` requires a bearer access token
  and no body. It uses the authenticated account's persisted address,
  invalidates older credentials, and queues a replacement atomically.
  It returns 202; already verified accounts return 202 without new mail.
  Disabled or missing accounts cannot request a delivery.
- Both mutations lock the user before changing credentials. Concurrent
  verification succeeds at most once. Concurrent resend and verification
  cannot leave a newly issued credential on an already verified account.
- Login and existing v1 endpoints continue to allow unverified accounts.
  Email verification is an explicit profile property, not a new global
  authorization requirement. A future feature requiring verified email
  should enforce it in its own use case.
- Verification uses the existing credential table and event version;
  no migration is required. `consumed_at` means consumed or invalidated
  by replacement, matching the existing one-unconsumed-credential index.

The frontend route remains `/verify-email?token=...`. Opening that page
must call the POST API; a GET does not consume credentials. This repository
contains the backend, not that frontend page. Keep tokens out of analytics,
access logs, and error reporting.

## Delivery and recovery

The worker validates payload fields before signing. Malformed/incomplete
payloads are terminal failures. Expired, consumed, superseded, recipient-
mismatched, verified-account, deleted-account, and disabled-account events
are handled without sending; `processed_at` means handled, not necessarily
emailed. Database, localization, and SMTP failures are retried with backoff.
A state change after the eligibility read can still invalidate an email
already being sent; token consumption is authoritative. No DB transaction
is held open during SMTP.

A successful SMTP call means the SMTP server accepted the message, not
that a recipient read it or that it reached an inbox. A lost response or a
crash before recording success can cause duplicate emails. With a fixed
key, every retry uses the same token. There is no exactly-once SMTP promise
or separate sent flag pretending to provide one.

Claims increment `attempts`, which also serves as an ownership version.
Outcome writes require the same attempt and an unfinished row. Stale
workers cannot overwrite newer claims or terminal outcomes. Each poll
recovers a bounded batch of exhausted, expired claims to `failed_at`, with
an explicit unknown-delivery-outcome diagnostic. It does not automatically
requeue a final attempt whose actual SMTP outcome may be unknown.

The worker rejects configurations unless:

```text
OUTBOX_CLAIM_LEASE > OUTBOX_BATCH_SIZE * (MAIL_TIMEOUT + 5 seconds)
```

The 5 seconds covers outcome writes. Each handler gets at most its batch
share minus that reserve, and never beyond the claim's remaining deadline.
Mail, repository I/O, and future handlers must honor the context. The
remaining headroom covers eligibility reads and rendering; keep a practical
margin when tuning. Defaults: 180-second lease, 10 slots, 10-second mail
timeout, up to 13 seconds per handler and 5 seconds per outcome.

## Deployment and inspection

API and worker must receive the same `AUTH_EMAIL_VERIFICATION_SECRET`.
API additionally needs the distinct `AUTH_JWT_SECRET`; worker does not.
The API refuses identical keys. Key rotation invalidates existing links;
users can sign in and request another. No historical key ring is provided.

Stop old worker processes before starting this worker version: old binaries
still perform unconditional outcome updates. No database migration or event
rewrite is needed. Public response shapes remain compatible; the two new
endpoints are documented in OpenAPI 0.3.0 under `/api/v1`.

Inspect terminal failures without selecting payloads:

```sql
SELECT id, type, attempts, failed_at, last_error
FROM outbox_events
WHERE failed_at IS NOT NULL
ORDER BY failed_at DESC;
```

Fix the underlying problem before requesting another verification email.
Do not blindly replay expired credentials or reset ownership counters while
workers are processing them. Registration success remains independent of
mail availability; operational monitoring should watch terminal failures
and the age of pending events.

## Review and validation

Regression coverage includes late registration-write rollback, failed
resend/verification rollback, concurrent consumption and resend, malformed
and obsolete delivery, duplicate-registration rejection, stale claim outcome
writes, final-attempt crash recovery, failure to record a final outcome,
handler registration validation, and API error/auth/rate-limit behavior.

Run `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -race ./...`, `make openapi-check`, and `make test-integration`.
Integration tests use isolated databases with guarded test prefixes and
the dedicated Docker PostgreSQL/Redis stack.

For an uncommitted review, `make openapi-check` compares generated output to
Git's index and therefore reports the intended new types as a diff. Validate
regeneration against an isolated temporary index containing the proposed output;
leave the real index untouched. The ordinary target passes after the proposed
generated file is staged or committed.
