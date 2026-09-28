# Agent Instructions

Short reference for AI agents working in this repo. Full detail:
[docs/conventions/backend_conventions.md](docs/conventions/backend_conventions.md).
Adding an endpoint? Follow
[docs/guides/adding_an_endpoint.md](docs/guides/adding_an_endpoint.md).

## Architecture

- Modular monolith: `internal/{api,apperror,bootstrap,config,platform,modules}`.
- Per module: `domain/ → application/ → infrastructure/ → transport/http/`.
  Dependencies only point downward; domain depends on nothing above it.
- `application/` is organized by use case (`register.go`, `login.go`), one
  small struct + one main method each — not a generic CRUD service.
- Repository/port interfaces live in `application/ports.go`, owned and
  named by the consuming module. Add a method only when a real use case
  needs it.
- An interface goes next to its actual consumer. Only a transport
  adapter calls it? It lives in that adapter's package, not in `ports.go`.
  Verification signing now belongs in `ports.go` because delivery is an application use case.
  Everything in `ports.go` must have a caller in `application/`.
- Bootstrap constructors are process-specific: `bootstrap.NewAPI()`,
  `bootstrap.NewWorker()`, one per binary under `cmd/`.
- A module consumed by several processes exposes its compositions
  explicitly — `identity.CoreModule` + `HTTPModule` / `WorkerModule`
  (`wiring_core.go`, `wiring_http.go`, `wiring_worker.go`) — and each
  process composes the ones it runs. Don't leave it to Fx laziness to
  decide which adapters a binary builds. Shared providers are declared
  once in `CoreModule`; process-only use cases belong in the process composition.
- A secret only one process's adapters need stays out of the core
  composition, so other processes never load it. The worker never loads
  `AUTH_JWT_SECRET`; API verification and worker signing both require
  `AUTH_EMAIL_VERIFICATION_SECRET`, independently of the JWT key.
- Domain code must not import HTTP, pgx, Fx, or any other framework.

## Do not

- Don't add `BaseService`, `BaseRepository`, `GenericCRUD[T]`, a service
  locator, or a god config struct.
- Don't use Gin/Chi/Fiber — stdlib `net/http` only, no concrete
  requirement to change that.
- Don't use Fx outside composition roots (`module.go` / identity's
  `wiring_*.go`, `bootstrap/`).
  Application constructors take plain explicit dependencies.
- Don't put HTTP status codes or JSON in domain/application code.
- Don't let generated OpenAPI types (`internal/api/openapi`) leak past
  a module's `transport/http/` package.
- Don't add abstractions, config knobs, or repository methods ahead of
  an actual need.

## HTTP

- Routes: `httpserver.GET/POST/PUT/PATCH/DELETE(path, handler)`, never
  hand-build a `"METHOD /path"` string.
- Public business endpoints live under `/api/v{major}`. Mount them with
  `httpserver.Prefix("/api/v1", ...)` — never repeat the prefix per
  route, never add a `Version` field to `Route`.
- `/health` and `/ready` are operational and stay unversioned.
- A major version is a breaking-change boundary only. New endpoint, new
  optional field, new optional query param → still v1. Removed/renamed
  field, changed type or semantics, restructured payload → v2.
- Versioning is transport-only. Never give domain/application/
  repositories a v1/v2 notion; both versions call the same use cases.
- No `v1/`/`v2/` directories (handlers, spec, or generated types) until
  a second version actually exists.
- No HATEOAS.
- Handlers: decode → call use case → map response → write. Put response
  mapping in that module's `dto.go`, not inline in the handler.
- Errors: return `apperror.Error` (`Kind`, stable `Code`, `Message`);
  `response.Responder` turns it into the HTTP status + JSON envelope.
  Never leak an internal error's message/cause to the client.
- All response writing goes through `response.Responder`:
  `JSON`, `NoContent`, `Status` (bodiless, e.g. 202), `Error`. No
  `w.WriteHeader`/`json.NewEncoder(w)` in handlers.
- JSON decoding goes through `platform/httpserver/request.DecodeJSON`
  (size limit, Content-Type check, unknown-field rejection) — don't
  hand-roll `json.NewDecoder` in a handler.
- Client IP is resolved once per request by the `middleware.ClientIP`
  (via `clientip.Resolver`) and read with `requestctx.ClientIP(ctx)` —
  never resolve again, never read `X-Forwarded-For`/`X-Real-IP` directly.

## OpenAPI

- `docs/api/openapi.yaml` is the contract; Go types are generated
  (`make openapi-generate`) and never hand-edited.
- Documented paths match runtime exactly, `/api/v1` included — don't
  hide the prefix in `servers`. `info.version` is the document's own
  revision, not the URL major; they move independently.
- No `format: email` on email fields — the generated type it produces
  silently rewrites `"Name <addr>"` input before validation runs.
- Run `make openapi-check` after any spec change.

## Config

- Typed structs parsed with `caarlos0/env`; semantic validation
  (ranges, relationships) happens in Go code after parsing, not in env
  tags.
- One env var per external dependency (`DB_DSN`, not `DB_DSN_HOST` /
  `DB_DSN_DOCKER`).

## Testing

- Stdlib `testing` + `net/http/httptest`. No assertion library, no test
  framework, no fixture factory.
- `go test ./...` must never need Docker/Postgres. Real-DB tests are
  `//go:build integration`, run via `make test-integration`.
- No `time.Sleep` for concurrency tests — use channels/`sync.WaitGroup`.
- Any destructive test DB operation (`CREATE`/`DROP DATABASE`) must use
  a fixed recognizable prefix and reject Postgres's built-in DB names,
  guarded by its own non-integration-tagged unit test.

## Async work & outbox

- Never hand important async work (anything a success response implies
  will happen, e.g. "an email will be sent") to a detached goroutine.
- The durable mechanism is a PostgreSQL outbox (`platform/outbox`), not
  a broker — add Kafka/RabbitMQ only once an operational need actually
  justifies it.
- The business write and the outbox event insert must commit in the
  same transaction (a narrow port like `RegistrationStore`, not a
  generic tx framework).
- Event types are versioned: `<module>.<event>.vN`, e.g.
  `identity.email_verification_requested.v1`. The type is persisted, so
  a shipped value is frozen — a breaking payload change is a new `.v2`
  constant + `...V2` struct registered next to v1, not an edit. Types
  match exactly; an unhandled type fails and retries, never drops. No
  event framework — a constant, a struct, a registration.
- Delivery permits duplicates. Every handler must tolerate repeated execution;
  SMTP retries may deliver the same email twice.
  Prefer deriving a credential deterministically from non-secret
  payload fields (e.g. HMAC over an id+expiry) over generating it once
  and threading the literal secret through the payload — the outbox
  should never hold bearer-capable material at rest.
- Claiming sets a lease; keep `BatchSize * (handler-timeout + outcome-write-timeout) <
  ClaimLease`, and give any handler doing I/O its own timeout well
  under `ClaimLease / BatchSize`.
- A poison event (past the attempt ceiling) gets an explicit terminal
  state (`failed_at`), not just silent exclusion from claim.
- `platform/mail.Sender` is narrow (one message, one send). `SMTPSender`
  (built on `wneessen/go-mail`, confined to `smtp.go`) is the only
  implementation and must never log a message body/token or configured
  credentials, and must never retry internally. A different provider is
  a new type in that package, not a change to identity/application.

## Rate limiting

- Defense in depth, each layer doing only its job:
  CDN/WAF/API gateway (coarse DDoS/IP) → Go global per-IP limit
  (generic) → module endpoint limits (identity's login/register/refresh/
  verify/resend, per IP) → application/domain rules keyed by account
  (e.g. identity's resend cooldown + daily cap). Per-IP limits are
  bypassable by changing IP; anything that must hold per user belongs
  in the last layer.
- `ratelimit.Policy` is the reusable abstraction. A module keeps its
  policies in a plain struct in its transport package (identity's
  `RateLimitPolicies`); no cross-module `Policies`/provider/registry.
- Distributed via Redis (`platform/redis`, go-redis). GCRA comes from
  `redis_rate` and runs as a Lua script — never read-modify-write in Go.
- `redis_rate` stays inside `platform/ratelimit`'s adapter; modules use
  `Policy`/`Key`/`Result`/`Limiter` for HTTP limits. A module's own
  Redis-backed abuse counter is a normal infrastructure adapter (go-redis
  in `modules/<m>/infrastructure/redis`, like pgx in `postgres`) behind
  an application port — not an extension of the HTTP limiter.
- Platform owns mechanics + the generic per-IP limit; modules own their
  endpoints' limits (identity's `AUTH_RATE_LIMIT_*`). Don't add an env
  var per route.
- Client IP comes from `requestctx.ClientIP` (resolved once by
  `middleware.ClientIP`). Never read forwarded headers in a limiter — a
  caller that picks its own IP picks its own bucket.
- Keys are built by `ratelimit` (HTTP limits) or the adapter that owns
  the counter, never by a handler. IPs stay plaintext; a personal
  identifier (email) is only ever stored as HMAC-SHA256 under a dedicated
  secret (`AUTH_ABUSE_KEY_SECRET`) — never raw.
- `/health` and `/ready` are exempt — 429ing a liveness probe restarts
  healthy pods.
- Order: RequestID → ClientIP → AccessLog → Recovery → CORS → RateLimit → router.
- Denied: 429, `rate_limit_exceeded`, `Retry-After` in whole seconds
  rounded up. Only `Retry-After` — no `RateLimit-*` draft headers.
- **Fails open**: Redis down → request proceeds. Report through
  `observability.ProtectionSignal`: an OTel counter per failure
  (`rate_limit_protection_unavailable`, `auth_abuse_protection_unavailable`)
  and logs only on transition / once-a-minute summary / recovery — never
  a log line per request. Never log the subject (IP / account hash).
- Metrics: OpenTelemetry via the `metric.MeterProvider` from
  `platform/observability` (OTLP/HTTP when `OTEL_METRICS_ENABLED`, no-op
  otherwise; endpoint from the standard `OTEL_EXPORTER_OTLP_*` vars). Low-
  cardinality attributes only (a scope, never an IP/user).
- Login: per-IP HTTP limit (before the handler) + failed-login limit per
  account+IP (see "Authentication abuse"). Never a per-account hard
  block on an unauthenticated request — it lets anyone who knows an
  address lock that user out.
- Account lockout is deliberately not built. Throttle; a penalty may only
  ever apply to the offending source (account+IP), never account-wide.
- Redis is ephemeral enforcement state. Never persist limiter state to
  PostgreSQL. HTTP limits never hand-manage TTLs (redis_rate does);
  abuse counters set their expiry atomically with the increment (Lua).
- Account-keyed business limits on authenticated actions (resend
  cooldown/cap) derive from durable records, checked under that
  account's row lock in the write's transaction, with an exact
  `Retry-After` — never a lockout.

## Authentication abuse

- Layers stay separate — never one universal limiter: HTTP per-IP limit
  (`ratelimit`, 15 login req/min/IP) → application rules
  (`LoginRiskEvaluator`: account+IP failures, 5 / 15 min) → future
  account-wide progressive delay/challenge for distributed failures.
- `LoginService` asks `LoginRiskEvaluator` before password hashing and
  reports success; one rule today (`AccountIPFailureRule`). A second rule
  becomes a composite evaluator in wiring, not branches in the service.
  Add `RiskAction`s (Delay, Challenge, RequireMFA) only with the rule
  that returns them.
- Attempts reserve a counter slot atomically before Argon2 (concurrency
  can't bypass the threshold); success resets it. Throttled login
  returns the same `rate_limit_exceeded` 429 as the IP limit — never
  reveal which limit, or whether the account exists.
- Security outcomes go through `application.SecurityEvents`
  (`identity.login.failed`, `identity.login.throttled`); alerting, audit
  and metrics are consumers of that port, never code in the use case or
  handler. Events carry user id / IP / internal reason — no email, no
  password.
- Future pieces map to: Middleware (request-level bot protection);
  Ports & Adapters (CAPTCHA verifier, MFA, passkeys/WebAuthn,
  breached-password checker, external risk intel); evaluator rules
  (trusted devices, account-wide throttling, suspicious-login checks);
  events (challenge_required, risk_high, mfa.failed).
- Trusted devices (future): random, server-signed device token in an
  HttpOnly, Secure, SameSite cookie; never User-Agent as identity; it
  only reduces friction (skips throttles/challenges), never replaces a
  credential.
- Handlers stay decode → metadata → use case → render. No risk logic in
  HTTP code.
- Every login path that names an account (unknown, no password, wrong
  password, disabled) reserves the bucket, costs one password
  verification, and returns `invalid_credentials`; "disabled" only after
  the right password. Never make a path cheaper or observably different.
- Fail-open counts `auth_abuse_protection_unavailable` and logs at Error
  on transition (then sampled) — alert on it; edge/WAF is the outer
  defense while it fires.
- The login dummy hash is precomputed in `NewLoginService` via the
  configured hasher (current Argon2 params); requests only verify. Call
  it timing equalization — never "constant time" (DB/cache paths still
  differ slightly).
- Authentication never depends on observability: recording a metric or
  event can't block, do network I/O, or fail a login.
- OTel attributes stay bounded (a scope); never IP/email/user ID/key.
- Alert on protection state/outage duration, not raw counter volume.
- The login-abuse layer is complete for the starter. Next change must
  be a new capability (trusted devices + account-wide friction,
  MFA/passkeys, challenges) — not new abstractions or IP-counter variants.
- Keep `LoginRiskEvaluator`/`SecurityEvents` tiny: no registry, factory,
  engine or context builder until real rules need one.

## Localization

- Backend localizes only content it generates *and* delivers (email,
  SMS, push). Frontend localizes UI text.
- API error `code` is never localized — no `Accept-Language` branching
  in REST responses, no translated domain/repository errors.
- Engine: `platform/localization` (`Localizer`, `Message`, `Catalog`).
  `go-i18n` is confined to `goi18n.go` — don't import it elsewhere.
- Wording belongs to the module: `modules/<m>/translations/*.toml`,
  `//go:embed`ed, registered via the `localization_catalogs` Fx group.
  No global catalog, no plugin framework.
- Message IDs are `<module>.<feature>.<message>` and are contracts —
  stable when the English changes. Never English sentences as keys.
- Fallback: requested → base language → default. Unknown locale falls
  back; a message that resolves to nothing is an error, never an empty
  subject/body.
- Locale is explicit at the point of use. Workers never read
  `Accept-Language`. Never infer locale from email domain, phone
  prefix, IP, or timezone. No `preferred_locale` column until a feature
  needs it.

## Validation

- `internal/validation` is for syntactic, business-neutral rules only
  (`NormalizeEmail`, `NormalizeE164Phone`): stdlib, ordinary errors, no
  `apperror`, no HTTP/DB/module knowledge.
- Module policy stays in the module — password length, account status,
  TTLs, whether a phone is required, whether email must be verified.
  The application layer maps a plain validation error to its stable
  code (`invalid_email`, `invalid_phone`).
- Used by one module only and not clearly project-wide? Leave it there.
- Concept-specific files (`email.go`, `password_policy.go`). Never
  `common.go`, `helpers.go`, `utils.go`, or a bare `validation.go`.

## File size

- Prefer handwritten production files below ~350–400 lines.
- Treat this as a review threshold, not a hard limit.
- Split by responsibility/use case/domain concept, not by arbitrary
  line count.
- Generated files, migrations, static schemas/tables, and similar
  artifacts are exempt.
- Avoid a large `service.go`, `handler.go`, or `repository.go`
  accumulating unrelated behavior — one file per use case/concept.

## Go version

- Track the latest stable Go release (greenfield project, no legacy
  compatibility need). Keep `go.mod`, both Dockerfiles, and any
  version-specific docs in sync when bumping.

## Licensing

- Our code is Apache-2.0 (`LICENSE`); dependencies keep their own.
- Changed dependencies → `make licenses`, commit
  `THIRD_PARTY_LICENSES.txt` (generated, never hand-edited).
  `make licenses-check` enforces the allowlist in `make/licenses.mk`.
  A license outside it is a human decision — never widen the list,
  `--ignore` it, or remove the dependency to go green.
- Copied third-party code (Go, Lua, templates, vendored/generated code)
  keeps its original copyright/license notice in its own file. Never
  strip upstream headers or paste someone else's implementation into
  our files unattributed.

## Before finishing

`gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -race ./...` must all pass. After a dependency change,
`make licenses-check` must too. Passing tests alone isn't
"correct" — check concurrency, failure ordering, and error/security
behavior too.
