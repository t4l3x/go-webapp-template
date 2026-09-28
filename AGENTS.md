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
- JSON decoding goes through `platform/httpserver/request.DecodeJSON`
  (size limit, Content-Type check, unknown-field rejection) — don't
  hand-roll `json.NewDecoder` in a handler.
- Client IP goes through `platform/httpserver/clientip.Resolver` — never
  read `X-Forwarded-For`/`X-Real-IP` directly.

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

- Distributed via Redis (`platform/redis`, go-redis). GCRA comes from
  `redis_rate` and runs as a Lua script — never read-modify-write in Go.
- `redis_rate`/go-redis types stay inside `platform/ratelimit`'s adapter.
  Modules use `Policy`/`Key`/`Result`/`Limiter`. Never call `redis_rate`
  from a feature module.
- Platform owns mechanics + the generic per-IP limit; modules own their
  endpoints' limits (identity's `AUTH_RATE_LIMIT_*`). Don't add an env
  var per route.
- Client IP comes from `clientip.Resolver`. Never read forwarded headers
  in a limiter — a caller that picks its own IP picks its own bucket.
- Keys are built by `ratelimit`, never by a handler. Only per-IP keys
  exist today, and IPs stay plaintext. Don't add a keyed-hash mechanism
  for other subjects until a limit actually needs one.
- `/health` and `/ready` are exempt — 429ing a liveness probe restarts
  healthy pods.
- Order: RequestID → AccessLog → Recovery → CORS → RateLimit → router.
- Denied: 429, `rate_limit_exceeded`, `Retry-After` in whole seconds
  rounded up. Only `Retry-After` — no `RateLimit-*` draft headers.
- **Fails open**: Redis down → request proceeds, logged at Warn with the
  scope. Never log the subject (IP / account hash).
- Login is limited per IP only, consumed before credential verification.
  No account-keyed hard block on an unauthenticated request — it lets
  anyone who knows an address throttle that user's own logins.
- Account lockout is a different thing and is deliberately not built.
  Throttle, never a fixed penalty window (that's a DoS on other users).
- Redis is ephemeral enforcement state. Never persist limiter state to
  PostgreSQL; never hand-manage TTLs.

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

## Before finishing

`gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`,
`go test -race ./...` must all pass. Passing tests alone isn't
"correct" — check concurrency, failure ordering, and error/security
behavior too.
