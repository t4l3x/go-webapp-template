# Backend Conventions

This document defines the architectural and coding conventions for this Go backend.

The goal is to keep the codebase modular, explicit, testable, production-safe, and suitable for future extraction into services without prematurely building microservice complexity.

For a step-by-step walkthrough of adding one new REST endpoint under
these rules, see
[`docs/guides/adding_an_endpoint.md`](../guides/adding_an_endpoint.md).

---

## 1. Architecture

The backend is a modular monolith.

Top-level structure:

```text
internal/
├── api/
├── apperror/
├── bootstrap/
├── config/
├── platform/
└── modules/
```

Responsibilities:

- `api/`
    - Generated transport contract types (e.g. `api/openapi/`, generated
      from `docs/api/openapi.yaml`). Never hand-edited. Consumed only by
      modules' `transport/http/` packages — never by application, domain,
      or infrastructure.

- `bootstrap/`
    - Fx composition root.
    - Wires platform and application modules.
    - Contains no business logic.
    - Constructors are process-specific — one per binary under `cmd/`:
      `bootstrap.NewAPI().Run()`, `bootstrap.NewWorker().Run()`. Each
      lists the platform modules that process needs and, per business
      module, exactly which of its compositions that process runs.

- `config/`
    - Global application-level configuration.
    - Examples: app identity, HTTP configuration, logging.

- `platform/`
    - Shared technical infrastructure.
    - Examples: PostgreSQL, HTTP server, observability, Redis, the
      durable outbox (`platform/outbox`), mail (`platform/mail`).
    - Contains no game/business logic.

- `modules/`
    - Business capabilities.
    - Examples: player, auth, simulation, room.
    - Modules are added only when needed.

---

## 2. Module Structure

Business modules follow this structure:

```text
internal/modules/player/
├── domain/
├── application/
├── infrastructure/
├── transport/
│   ├── http/
│   └── worker/
└── module.go
```

`module.go` holds the module's Fx wiring. Once more than one process
consumes the module, it is replaced by one file per composition (see
below): `wiring_core.go`, `wiring_http.go`, `wiring_worker.go`.

`transport/` holds one package per inbound boundary — `http/` for
requests, `worker/` for outbox-consumer handlers. A module only grows
the ones it actually has.

Dependency direction:

```text
transport
    ↓
application
    ↓
domain
```

Infrastructure implements interfaces required by the domain/application layer.

Domain code must not depend on:

- HTTP
- Gin
- PostgreSQL
- pgx
- Redis
- RabbitMQ
- OpenAI
- Fx

Modules should not directly use another module's infrastructure.

Cross-module communication should happen through explicit application contracts.

`application/` is organized by use case, not by CRUD-style generic
service. Each use case gets its own file named for what it does
(`register.go`, `login.go`, `refresh.go`), holding a small struct with
only the dependencies that use case needs and one primary method.
Ports it depends on (repositories, hashers, token managers, ...) are
declared in `application/ports.go`, owned and named by the consuming
module — not defined by infrastructure and not shared across modules.

An interface belongs next to the code that actually calls it, not
wherever binding it happens to be convenient. A port a use case calls
belongs in `application/ports.go`; a port only a transport adapter
calls belongs in that adapter's own package. Verification signing belongs
in `application/ports.go` because `DeliverEmailVerificationService` calls it.
Every application port must have a real application consumer.

When more than one process consumes different adapters of the same
module, the module exposes its compositions explicitly rather than
leaving the outcome to which providers Fx happens to find a consumer
for:

```text
identity.CoreModule    wiring_core.go    shared persistence providers
identity.HTTPModule    wiring_http.go    registration/session/verification use cases and HTTP adapters
identity.WorkerModule  wiring_worker.go  delivery use case, signer, catalog, outbox adapter
```

A process composes `CoreModule` plus its process module. Shared providers
are declared once; process-specific providers are composed explicitly.
The worker never loads JWT settings. The API and worker both compose the
email-verification key provider: API checks tokens, worker signs them.
The API validates that its JWT and verification keys differ.

See [registration and verification](../guides/registration_and_verification.md)
for the complete flow, contracts, retry semantics, and operational limits.

Constructors take the narrowest config shape a use case actually needs
(e.g. a small `FooConfig{MinLength int}` local to `application`),
rather than the module's root `Config`. This keeps `application` and
`infrastructure` free of any dependency on the module's root package,
which also avoids an import cycle with the module's wiring (root
package → application/infrastructure for wiring, so the reverse
direction is not available). The wiring adapts the root `Config` into
each narrow shape via small inline provider functions.

---

## 3. Dependency Injection

Uber Fx is the application DI and lifecycle framework.

Fx belongs primarily at composition boundaries:

```text
bootstrap
platform modules
business module wiring
```

Business constructors remain normal Go constructors.

Do not pass a giant global dependency struct into modules.

Each constructor receives only the dependencies it requires.

Example:

```go
func NewService(
    repo Repository,
    logger *slog.Logger,
) *Service
```

Avoid service locator patterns.

A concrete type is bound to its consumer-owned interface with `fx.As`
or a one-line adapter:

```go
fx.Annotate(security.NewTokenManager, fx.As(new(application.TokenManager))),

security.NewVerificationSigner, // *security.VerificationSigner
func(s *security.VerificationSigner) application.VerificationSigner { return s },
```

Each binding is an ordinary compile-time conversion — not a runtime
type assertion that would only fail once the process is already
starting. Don't merge unrelated capabilities into one concrete type
just so one instance can satisfy several interfaces: each
implementation should hold only the secrets its own purpose needs.

---

## 4. Configuration

Configuration is strongly typed.

Environment parsing uses:

```text
github.com/caarlos0/env/v11
```

Example:

```go
type HTTP struct {
    Port int `env:"HTTP_PORT" envDefault:"8080"`
}
```

The environment package handles:

- string parsing
- integers
- booleans
- durations
- defaults
- required fields

Our code remains responsible for semantic validation.

Example:

```go
if cfg.Port < 1 || cfg.Port > 65535 {
    return HTTP{}, fmt.Errorf("HTTP_PORT must be between 1 and 65535")
}
```

Platform-specific configuration belongs to the platform package.

Example:

```text
platform/database/config.go
```

Application-global configuration belongs under:

```text
internal/config/
```

The application should know only one variable per dependency.

Use:

```text
DB_DSN
REDIS_URL
OTEL_EXPORTER_OTLP_ENDPOINT
```

Do not introduce:

```text
DB_DSN_HOST
DB_DSN_DOCKER
REDIS_URL_HOST
REDIS_URL_DOCKER
```

Deployment environments decide the actual value.

Docker Compose overrides container-network-specific values.

Locally, `.env` holds the host-side value (`localhost:<published port>`),
which host tooling (`make run`, `make migrate-*`) uses as is; Compose's
`environment:` block replaces only the addresses that differ inside the
Docker network. Integration tests get their own variables
(`DB_DSN_TEST_ADMIN`, `REDIS_URL_TEST`) because they target separate,
disposable instances — a different dependency, not a different address.

---

## 5. HTTP Architecture

The standard HTTP abstraction is:

```go
http.Handler
```

The HTTP server must not depend on a specific router/framework.

This allows:

```text
net/http ─┐
Chi      ─┼─> http.Handler ─> HTTP server
Gin      ─┘
```

Do not create a custom generic router abstraction unless a real requirement appears.

Module HTTP handlers live under:

```text
modules/<module>/transport/http/
```

The shared low-level server/router remains under:

```text
platform/httpserver/
```

---

## 6. Routes

Feature routes are registered through the Fx route group:

```go
Routes []Route `group:"http_routes"`
```

Modules register routes without modifying the central router.

The central router owns only platform-level endpoints such as:

```text
/health
/ready
```

A module mounts its routes with `httpserver.Prefix` rather than
repeating `/api/v1` on every line:

```go
return httpserver.Prefix(
    "/api/v1",

    httpserver.POST("/auth/register", http.HandlerFunc(handler.Register)),
    httpserver.GET("/auth/me", authenticated(handler.GetMe)),
)
```

`Prefix` returns `([]Route, error)`, so a route constructor returns one
too and Fx surfaces a bad mount point at startup. It rejects a prefix
that is empty, lacks a leading `/`, or has a trailing `/`, and a
sub-path lacking a leading `/` — the combinations where plain
concatenation would otherwise produce a path that silently never
matches. `Route` keeps exactly `Method`, `Path`, `Handler`: there is no
`Version` field, and only `platform/httpserver` knows how a route
becomes a ServeMux pattern.

Keep it at that. No nested groups, no per-group middleware stacks, no
route builder — a prefix is the whole feature until something real
needs more.

---

## 7. HTTP Middleware

General middleware belongs under:

```text
platform/httpserver/middleware/
```

Current middleware:

- request ID
- access logging
- panic recovery
- CORS

Middleware uses the standard contract:

```go
type Middleware func(http.Handler) http.Handler
```

It must remain independent of Gin, Chi, or other routing frameworks.

Module-specific HTTP middleware belongs near the module when appropriate.

Example:

```text
modules/room/transport/http/middleware.go
```

Generic authentication may eventually become shared infrastructure.

Business authorization remains application/module logic.

Example:

```text
"Is this user authenticated?"       -> shared/auth infrastructure
"Can this player modify this room?" -> room/application
```

---

## 8. HTTP Responses

Shared HTTP serialization belongs under:

```text
platform/httpserver/response/
```

`response.Responder` owns every response write: status, content type,
JSON encoding (encoded before any header is sent, so an encoding failure
still becomes a clean 500) and the related logging. Handlers never call
`w.WriteHeader` or encode JSON themselves.

```go
h.responder.JSON(w, r, http.StatusOK, newSomethingResponse(out))
h.responder.NoContent(w, r)                    // 204
h.responder.Status(w, r, http.StatusAccepted)  // other bodiless statuses
h.responder.Error(w, r, err)
```

Error responses follow one shape:

```json
{
  "error": {
    "code": "player_not_found",
    "message": "Player not found"
  }
}
```

`code` is stable and machine-readable.

`message` is presentation text.

Do not expose internal infrastructure errors to clients.

---

## 9. Error Architecture

`internal/apperror` is the shared application-facing error model.

It is transport-independent.

Example:

```go
apperror.Error{
    Kind:    apperror.KindNotFound,
    Code:    "player_not_found",
    Message: "Player not found",
}
```

Generic error kinds include:

```text
validation
unauthorized
forbidden
not_found
conflict
unavailable
internal
```

Application error kinds are mapped by transports.

Example:

```text
NotFound
   ├─ HTTP -> 404
   └─ gRPC -> codes.NotFound
```

Application/domain code must not contain HTTP status codes.

---

## 10. Domain Errors

Domain errors represent business rules.

Example:

```go
var ErrRoomAlreadyStarted = errors.New("room already started")
```

Domain errors must not know about:

- HTTP
- gRPC
- JSON
- status codes

Application code may translate domain errors into `apperror`.

Example:

```text
domain.ErrRoomAlreadyStarted
        ↓
application
        ↓
apperror.KindConflict
code: room_already_started
```

---

## 11. Infrastructure Errors

Infrastructure-specific errors should not leak upward unnecessarily.

Example:

```text
pgx.ErrNoRows
      ↓
repository
      ↓
domain/application meaningful error
```

Unexpected failures should preserve the original cause using wrapping.

Example:

```go
return apperror.Wrap(
    apperror.KindInternal,
    "player_lookup_failed",
    "Failed to load player",
    err,
)
```

Internal error details are logged but not returned to clients.

---

## 12. PostgreSQL

PostgreSQL uses:

```text
pgx/v5
pgxpool
```

`*pgxpool.Pool` is valid infrastructure.

It may be injected into PostgreSQL repository implementations.

Do not inject `*pgxpool.Pool` directly into:

- HTTP handlers
- business services
- domain objects

Correct:

```text
pgxpool.Pool
    ↓
player/infrastructure/postgres
    ↓
PlayerRepository
    ↓
player/application
```

Avoid generic database abstractions such as:

```text
Database
BaseRepository
GenericRepository[T]
```

Repository abstractions should describe business persistence needs.

Examples:

```text
PlayerRepository
RoomRepository
AgentRepository
```

Create repository interfaces only when a real module needs them.

---

## 13. Database Migrations

Database migrations use `golang-migrate`.

Migrations live under:

```text
db/migrations/
```

Use one ordered migration stream for the modular monolith.

Migrations are deployment artifacts.

Production flow:

```text
build
  ↓
migration job
  ↓
deploy application
```

Do not automatically migrate the production database during API startup.

For large data transformations:

- schema migration first
- application-compatible rollout
- separate batched backfill
- cleanup migration later

Use expand-and-contract for high-risk schema changes.

---

## 14. Logging

Structured logging uses:

```text
log/slog
```

Prefer child loggers:

```go
logger.With("component", "postgres")
logger.With("component", "http")
```

rather than repeatedly attaching the component field.

HTTP access logs should include useful stable fields such as:

```text
request_id
method
path
status
duration
bytes
```

Do not log sensitive query parameters, credentials, tokens, or secrets.

---

## 15. Request IDs

Each HTTP request receives an `X-Request-ID`.

If the client supplies a valid request ID, it may be propagated.

Otherwise the server generates one.

The ID is:

- returned in the HTTP response
- stored in request context
- included in HTTP logs

Future asynchronous work should propagate correlation IDs where appropriate.

---

## 16. CORS

CORS configuration is explicit.

Example:

```env
HTTP_CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:63342
```

Do not enable credentialed CORS unless the authentication design requires browser cookies.

---

## 17. Health and Readiness

`/health` means:

```text
the process is alive
```

It should remain cheap.

`/ready` means:

```text
the process can currently serve real traffic
```

Readiness may eventually validate required dependencies such as:

- PostgreSQL
- Redis
- queue infrastructure

Do not overload `/health` with expensive dependency checks.

Both paths are unversioned and registered by the router itself, never
through a module's route group — see section 30.

---

## 18. Testing

### Philosophy

Use Go's standard `testing` package as the foundation: `testing`, `net/http/httptest`,
`errors`, explicit `if` conditions. Do not add an assertion library merely for
shorter syntax.

Allowed later, only where they provide real value over a handwritten check:
`testify/require`, `google/go-cmp`, `go.uber.org/mock/gomock`, `go.uber.org/fx/fxtest`.
Never: `testify/suite`, Ginkgo/Gomega, a custom test framework, a fixture
factory, or global mutable test state. An abstraction earns its place through
repeated real need, not because it might be convenient later.

Coverage is a diagnostic, not a target — do not write a test solely to move
the percentage. Prioritize, in order: business rules, error semantics,
security boundaries, information leakage, HTTP contracts, configuration
validation, concurrency, infrastructure integration, failure behavior.

### Package and file convention

Prefer black-box tests (`package foo_test`) whenever the public API is
sufficient — this is the default for infrastructure packages (`response`,
`middleware`, `requestctx`, `config`, application services). Use the internal
`package foo` only when exercising package-private behavior is genuinely
necessary; never switch to it just for convenience.

Tests live next to the code they test (`error.go` / `error_test.go`). Don't
create a global `/tests` directory for ordinary unit tests — a different
structure can be introduced later if a real integration/E2E suite needs one.

### Naming

Test functions: `Test<TypeOrFunction>_<Scenario>` — e.g.
`TestResponder_Error_InternalErrorHidesCause`, `TestRequestID_GeneratesWhenMissing`,
`TestLoadConfig_RequiresDSN`. Avoid vague names (`TestWorks`, `TestBasic`,
`TestHandler`) and avoid narrating the whole test in the name.

Table cases use short, concise names (`"not_found"`, `"invalid_port"`), run via
`t.Run(tc.name, ...)`.

Variables: prefer `got`/`want`/`err`/`req`/`rec`/`logs`/`responder` over noisy
alternatives like `actualResponseStatusCode`. Table slices: `tests`, cases: `tc`.

### Table-driven tests

Use a table when the same behavior is checked against multiple inputs and only
inputs/outputs change (`apperror.Kind` → HTTP status, accepted/rejected config
values, multiple CORS origins). Do not merge unrelated behaviors into one table
just to cut lines — keep separate tests for separate policies, e.g. "client
errors are not logged", "internal errors are logged", "internal causes are
hidden", and "unknown errors become a generic 500" are four different tests,
not four cases of one.

### Assertions

Prefer independent, explicit conditions so a failure names the actual problem:

```go
if rec.Code != http.StatusInternalServerError {
    t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
}
```

Do not fold unrelated checks into one boolean (`if status != 500 || code != "x" || logs.Len() == 0`).
Multiple assertions in one test are fine only when they describe the same
observable contract.

### Error testing

Prefer semantic checks over string comparison:

```go
var appErr *apperror.Error

if !errors.As(err, &appErr) {
    t.Fatalf("expected *apperror.Error, got %T", err)
}
if appErr.Kind != apperror.KindNotFound {
    t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindNotFound)
}
```

Don't assert the full output of `err.Error()` unless `Error()`'s formatting is
specifically the behavior under test — wrapped implementation details change.

For internal errors, test both sides of the boundary in separate tests: the
cause is available to the server (logged), and hidden from the client (stable
generic code/message only).

### HTTP testing

Use `httptest.NewRequest`/`httptest.NewRecorder`. Never start a real listening
server for handler, middleware, router, or response unit tests. Assert status,
relevant headers, the parsed JSON body (decode into a struct, don't compare
raw JSON strings), request-context behavior, and relevant logs.

### Domain-neutral test data

This is a domain-neutral starter template. Generic infrastructure tests
(`apperror`, `response`, `middleware`, `requestctx`, `httpserver`) must not
invent business concepts — no `/players/1`, `/orders/123`, `player_not_found`.
Use neutral values instead: `/resource/1`, `/example`, `resource_not_found`,
`some_code`. A future business module's own tests should of course use its
real domain terms — this rule is only for the shared infrastructure.

### Deterministic data

Use fixed values (`resource-1`, `user@example.com`), never faker-generated
data. Randomness is only used when randomness is the thing under test (e.g. a
generated request ID) — assert its properties (non-empty, correctly
propagated/returned), never its exact value.

### Log testing

Don't compare a whole structured log line — timestamps and field ordering
aren't stable. Assert only what matters: a stable event message exists
(`"unhandled request error"`), the expected severity was used, a field is
present (`request_id`, `code`), and — for security-sensitive logging —
that an unwanted value is *absent* (e.g. a query-string token never appears
when the logger intentionally records only `URL.Path`). Never assert an exact
duration value.

### Test helpers

Extract a helper only for genuinely repeated mechanics, and only where it
improves clarity — three-plus identical occurrences in one file is a
reasonable bar; two is usually not. Keep helpers in the test file that needs
them rather than promoting them; only lift one into `testkit` once it's
genuinely reusable infrastructure, not tied to one package's types. Put
`TestXxx` functions first in the file, small supporting helpers after. Name
helpers for exactly what they provide (`okHandler`, `decodeErrorResponse`,
`requireHeader`, `testResponder`), never `helper`/`setup`/`common`/`util`.
Call `t.Helper()` on any helper that takes `*testing.T`.

### internal/testkit

Allowed only for infrastructure genuinely needed by multiple independent
packages, and kept small and concept-specific — current examples:
`testkit.NewLogger()` (a buffered `*slog.Logger`, needed by `response`,
`middleware`, and `httpserver` tests) and `testkit.UnsetEnv()` (env isolation,
needed by `config` and `database` tests). It must never become a fixture
factory, a mock registry, or a generic helper dumping ground. Production code
must never import it. Domain-specific fixtures belong with their own
module's tests, not in `testkit`.

### Configuration tests

Use `t.Setenv` (and `testkit.UnsetEnv` first, to strip ambient shell state) so
tests never depend on the developer's real environment. Cover defaults, valid
custom values, required-value failures, semantic validation (e.g. pool-size
relationships), and malformed input. Don't test `caarlos0/env`'s own parsing
internals. Never use `t.Parallel()` in a test that calls `t.Setenv` or
otherwise touches shared/process-wide state.

### Time and concurrency

Never use `time.Sleep` for synchronization — use a deterministic primitive
when concurrency is actually under test. Don't introduce a global `Clock`
abstraction speculatively; if production logic becomes genuinely
time-dependent, add the smallest seam needed at that point.

### Database testing

Don't mock `pgxpool.Pool`, `pgx.Rows`, or wire-protocol behavior to simulate a
repository test — that isn't testing anything real. Config parsing (`LoadConfig`)
is unit-testable without a database. `go test ./...` must not require Docker or
Postgres. The first real repository should get a deliberately designed,
clearly separated integration-test lifecycle against real Postgres — not a
speculative one added now.

### Fx testing

Don't use Fx in ordinary unit tests — construct dependencies directly.
Reach for `fxtest` only once module composition itself is complex enough to be
worth protecting (many modules, grouped providers, workers, non-trivial
lifecycle hooks) — today's wiring doesn't meet that bar.

### Fuzzing and benchmarks

Add a `FuzzXxx` only for a real arbitrary-input boundary (parsers, decoders,
protocol input, custom serialization) — not because Go supports fuzzing. Add a
`BenchmarkXxx` only to answer an actual performance question, never for a
trivial getter/setter.

### Required commands

These must always pass:

```text
go build ./...
go vet ./...
gofmt -l .
go test ./...
go test -race ./...
```

`make test-cover` (coverage profile) is available; there is no mandatory
coverage threshold.

---

## 19. Dependency Policy

Prefer the Go standard library when it provides a strong common abstraction.

Examples:

```text
http.Handler
context.Context
log/slog
errors
```

Use focused third-party libraries where they solve a real problem better than maintaining custom infrastructure.

Current examples:

```text
Uber Fx           dependency injection/lifecycle
pgx               PostgreSQL
caarlos0/env      typed environment configuration
httpsnoop         safe HTTP response metrics
golang-migrate    schema migrations
```

Do not wrap third-party dependencies merely for theoretical replaceability.

Create abstractions around meaningful architectural/business boundaries.

### Licensing

This repository's own code is Apache-2.0 (`LICENSE`). Dependencies keep
their own licenses.

- **Imported modules** are covered by the dependency license report. After
  changing dependencies, run `make licenses` and commit
  `THIRD_PARTY_LICENSES.txt`. `make licenses-check` (CI) fails on a
  license outside the allowlist in `make/licenses.mk`, on an unidentified
  license, or on a stale report. A failing license is a decision for a
  human. Never widen the allowlist, add an `--ignore`, or drop the
  dependency just to make the check pass. Strong copyleft (GPL, AGPL) is
  never allowed by default.
- **Copied third-party source** is anything taken from elsewhere and put
  into this repository instead of imported: Go files, Lua scripts,
  templates, vendored or generated code. It must keep its original
  copyright and license notice, either as the file header or as a
  `LICENSE` file next to it. Never remove or rewrite upstream license
  headers. Never paste a third-party implementation into our own files
  as if it were ours. Keep copied code in its own file with its notice,
  and check that its license is in the allowlist.

---

## 20. General Engineering Rules

Prefer explicit code over clever abstractions.

Do not create infrastructure before a real requirement exists.

Avoid:

```text
BaseService
BaseRepository
GenericCRUD
God configuration structs
Service locator
Global mutable state
ORM schema auto-sync
HTTP-specific domain errors
```

Add abstractions when they protect a real boundary, not because a design pattern exists.

Keep constructors small and dependency-specific.

Keep business logic out of transport and infrastructure packages.

The authoritative simulation/game logic must remain deterministic and independent of presentation or AI providers.

Prefer handwritten production files below ~350–400 lines. This is a
review threshold, not a hard limit — split by responsibility, use
case, or domain concept, never by arbitrary line count. Generated
files, migrations, and static schemas/tables are exempt. Avoid a
large `service.go`, `handler.go`, or `repository.go` accumulating
unrelated behavior; one file per use case/concept, as already done
under `application/`.

---

## 21. Authentication & Tokens

Short-lived, signed access tokens (e.g. JWT) are never persisted in
PostgreSQL or Redis. They are validated statelessly on each request
(signature, issuer, expiry); revocation is accepted to lag until the
token's own short expiry rather than solved with a blacklist.

Long-lived credential tokens (e.g. refresh tokens) are opaque, cryptographically
random values. The raw value is returned to the client exactly once and
never stored; only its hash is persisted, and it rotates on every use.

Authentication middleware that extracts a caller's identity from a
token is business-module-specific and lives in that module's
`transport/http/`, not in `platform/httpserver/middleware/` — the
platform layer stays technology-level (request IDs, logging, recovery,
CORS), while "who is making this request" is a business concern.

Refresh-token rotation (or any "read current value, then replace it"
credential update) must be a compare-and-swap, not a read followed by
an unconditional write. A separate read-then-write lets two concurrent
requests presenting the same credential both pass validation and then
both write — the second silently clobbers the first's result. Make the
write itself conditional on the value not having changed since it was
read (a single `UPDATE ... WHERE current_value = $expected`, or an
explicit transaction with row locking), so PostgreSQL's own row-level
locking serializes concurrent attempts and only one can ever succeed.
An initial unconditional read beforehand is still fine — even good, for
giving a precise error in the non-racing case — as long as it is
understood to be advisory, not the actual protection.

More generally: any state transition where a security-sensitive
resource is created or invalidated (session creation, token rotation)
must be safe under concurrent execution, and must order operations so
that a fallible step (generating a token, calling out to a signer)
never happens *after* a DB write it can't roll back. Prefer: do
everything fallible and non-persistent first, mutate the database last.
That way a failure after the mutation is impossible, and the caller
never receives a "success" for a change that didn't fully happen (or a
credential is invalidated without a replacement ever having been
issued).

Integration tests that create or drop a database (e.g. a disposable
scratch database for repository tests) must guard those destructive
statements: never derive the target name from arbitrary input (a DSN's
path, an env var) and pass it straight to `CREATE`/`DROP DATABASE`.
Generate the name yourself with a fixed, recognizable prefix, reject
Postgres's own built-in database names outright, and verify the guard
with its own non-integration-tagged unit test so it's checked even
without Docker available. This exists so a mistaken admin DSN pointed
at a real environment can never result in data loss.

---

## 22. HTTP Routing

Routes carry HTTP method and path as separate fields:

```go
type Route struct {
    Method  string
    Path    string
    Handler http.Handler
}
```

Feature modules build routes through typed constructors —
`httpserver.GET/POST/PUT/PATCH/DELETE(path, handler)` — and never
concatenate a method and path into ServeMux's `"METHOD /path"` pattern
string themselves. Only `platform/httpserver`'s router performs that
conversion; it is a private implementation detail nowhere else may
depend on. This keeps feature modules free of net/http.ServeMux's own
pattern syntax and leaves room to change the underlying router without
touching every module.

The router validates registered routes (known method, path starting
with `/`, non-nil handler, no duplicate method+path pair) and returns
an error rather than panicking, so a bad route registration fails
application startup with a clear message.

---

## 23. Request Body Handling

JSON request decoding is production-safe by default: a bounded body
size (`http.MaxBytesReader`), required `Content-Type: application/json`
(with or without `; charset=utf-8`), unknown fields rejected, and
exactly one JSON value required (an empty body, a second concatenated
object, and trailing garbage after the first value are all rejected).
Parser/decoder errors are never returned to the client directly — only
stable `apperror` codes are.

This lives in `internal/platform/httpserver/request` because it isn't
specific to any one module. A narrowly-named package under
`platform/httpserver` is the right home for this kind of
cross-cutting-but-still-HTTP-specific behavior; it is not a general
`utils` dumping ground, and it must stay scoped to request parsing.

The ordinary JSON body limit is deliberately small and is not meant to
accommodate file/media uploads — those get their own dedicated
upload/object-storage flow with its own limit, not a raised version of
this one.

---

## 24. Client IP Resolution

A request's client IP is never read naively from `X-Forwarded-For`,
`X-Real-IP`, or `Forwarded`. The default source of truth is the direct
TCP peer (`RemoteAddr`). Forwarded-header values are only honored when
the immediate peer is an explicitly configured trusted proxy
(`HTTP_TRUSTED_PROXIES`, IPs/CIDRs); an `X-Forwarded-For` chain is
walked from the trusted side inward, and any malformed entry discards
the whole header rather than partially trusting it. Without configured
trusted proxies, a spoofed forwarded header has no effect.

This lives in `platform/httpserver` (`clientip.Resolver`), not in a
single business module, because the resolved address is a candidate
input for several unrelated future concerns (rate limiting, auth
auditing, abuse detection) — it is infrastructure, not one module's
business logic.

It is resolved exactly once per request: `middleware.ClientIP` (right
after `RequestID`) stores the result in the context, and the access log,
the generic and module rate limiters, and handlers all read
`requestctx.ClientIP(ctx)`. Nothing else calls the resolver, so no two
consumers can disagree about who the client is. Handlers convert it to
the application's `*string` input at the transport edge (nil when
unknown).

---

## 25. OpenAPI Contract

The REST API contract is OpenAPI-first: `docs/api/openapi.yaml` is
hand-written and is the source of truth. Go types are generated from
it (`oapi-codegen`, pinned version, `make openapi-generate`) — never
the reverse. Do not generate OpenAPI from handler comments, struct
tags, or domain/application types, and do not scatter documentation
annotations through handlers or DTOs to compensate.

Generated types live in a transport-owned location
(`internal/api/openapi/`), are never hand-edited, and never leak past
the HTTP transport layer — application, domain, and repository
interfaces stay generated-type-free so the same use cases remain
callable from a future non-REST transport (e.g. GraphQL) without
change. A transport handler maps a generated request type into an
application input, and an application output into a generated response
type; if a generated model is awkward for a particular use case, add an
explicit mapping function rather than reshaping application/domain
types around it.

`make openapi-check` regenerates and fails the build if that changes
any committed file, catching a spec/generated-code drift a developer
forgot to commit.

Documented paths must match runtime exactly, prefix included: the spec
lists `/api/v1/auth/login`, not `/auth/login` with the version hidden
in `servers`. A reader of the contract should be able to read a full
path off it. See section 30 for how the URL major relates to
`info.version`.

---


## 26. Go Style and Tooling

Style priority is, in order: official Go conventions and `gofmt`, Go Code
Review Comments, the Uber Go Style Guide where Go itself is not prescriptive,
and this repository's documented architecture rules. Do not copy broad external
style guides into this repository; document only local decisions.

Formatting is mechanical. Run `make fmt` to apply pinned `goimports` formatting
with `github.com/t4l3x/go-webapp-template` as the local import prefix. Run
`make fmt-check` in CI or before review to check formatting without changing
files. Do not manually align Go indentation with spaces, do not adopt `golines`,
and do not introduce stricter-than-standard formatting such as `gofumpt` without
a concrete repository benefit.

Use approximately 100 characters as a human readability guideline, not a hard
linter limit. Break lines where the structure becomes clearer, and leave short
expressions alone when wrapping would make them harder to read.

Static analysis is intentionally useful rather than maximal. Run `make lint` for
the pinned golangci-lint configuration and `make lint-fix` for safe automatic
fixes. The enabled set covers correctness and common footguns (`errcheck`,
`govet`, `staticcheck`, `ineffassign`, `unused`, `revive`, `errorlint`,
`noctx`, `bodyclose`, `nilerr`, `nolintlint`, `unconvert`, `misspell`,
`gosec`) while avoiding subjective rules such as hard line length, function
length, file length, variable-name length, mandatory blank-line rules, and
blanket exported-comment requirements. Any remaining `//nolint` must name the
specific linter and explain the exception when it is not obvious.

Developer tools are pinned in `tools/go.mod` and invoked through
`go tool -modfile=tools/go.mod ...` from the Makefile, so their dependency tree
stays out of the application module. `make tidy` and `make tidy-check` cover both
the application module and the tool module.

Generated code remains generator-owned. In particular,
`internal/api/openapi/types.gen.go` must not be hand-edited for style or lint
findings. Regenerate it from `docs/api/openapi.yaml` instead.

Handwritten PostgreSQL should use uppercase SQL keywords, lowercase snake_case
identifiers, explicit production column lists, no `SELECT *`, one major clause
per line, and one column per line when a list is long. Runtime values must use
pgx parameters or `NamedArgs`; schema identifiers must never be interpolated from
user input. Short embedded single-statement SQL may keep positional parameters
and omit a semicolon. Migration `.sql` files use semicolons and the same readable
keyword/identifier style.

Comments should explain invariants, security reasons, concurrency behavior, or
non-obvious constraints. Avoid comments that narrate the next line of code.
Exported API comments are useful when the symbol forms a real package contract;
do not add verbose comments solely to satisfy a style rule.

## 27. Go Version Policy

This is a greenfield project: track the latest stable Go release
rather than pinning to an old one for compatibility's sake. When
upgrading, update `go.mod`, both dev/prod Docker images, and any
version-specific documentation together in the same change, and run
the full build/test suite to catch real compatibility breaks rather
than assuming a version bump is free.

---

## 28. Asynchronous Work & the Outbox

Important asynchronous work (anything a client's success response
implicitly promises will happen — e.g. "an email will be sent") must
never be handed off to a detached goroutine from inside a request
handler or use case. A bare `go doSomething()` with no supervision can
silently drop work on a panic, on process restart, or simply because
nothing ever waits for or checks its result. If the work matters, it
must be durable before the request returns successfully.

The current durable mechanism for this is a PostgreSQL outbox
(`internal/platform/outbox`), not a message broker — introduce
Kafka/RabbitMQ/CDC only once an actual operational requirement
(throughput, cross-service fan-out, a need PostgreSQL genuinely can't
meet) justifies the added moving parts, not preemptively.

The pattern:

- The business write and the outbox event insert **must** commit in
  the same transaction. Never do `create record; COMMIT; insert
  outbox` — a crash between those two statements loses the event
  forever. Prefer a narrow port expressing the atomic requirement
  (e.g. `RegistrationStore`) over a generic transaction-runner
  abstraction.
- A separate poller (`outbox.Runner`, run from `cmd/worker`) claims due
  events with `SELECT ... FOR UPDATE SKIP LOCKED`, so multiple worker
  processes can safely share one table — this is the intended way to
  scale processing, not fanning out goroutines within one process.
- Claiming sets a lease (`available_at = now() + ClaimLease`), not just
  an attempts counter. Without a lease, a claimed-but-not-yet-finished
  event stays selectable by another claimer immediately, which is
  actual duplicate concurrent processing, not just an at-least-once
  retry — the lease is what actually prevents that.
- The composed worker validates `BatchSize * (MAIL_TIMEOUT + 5s) < ClaimLease`.
  The reserve covers outcome writes. Each handler receives a context deadline
  within its share of the lease and its remaining claim lifetime; I/O must
  honor that deadline. Keep practical headroom for database reads and rendering.
- Delivery permits duplicates. A crash between finishing work and recording
  its outcome can invoke the same handler again. Handlers must tolerate this;
  SMTP delivery may repeat even when retries preserve the same credential.
- Outcome writes require the current attempt and unfinished state. Expired
  claims at the attempt ceiling are recovered to an explicit terminal failure.
  Invalid payloads can fail permanently without exhausting retries.
- A handler must not regenerate a secret/credential differently on
  every retry attempt. Prefer deriving it deterministically from
  non-secret fields already in the payload (e.g. an HMAC over an id and
  an expiry) over generating it once and threading the literal secret
  through the payload/table — this also means the outbox never holds
  bearer-capable material at rest, not even transiently.
- A poison event (failing past a configured attempt ceiling) transitions
  to an explicit terminal state (`failed_at`) rather than just being
  excluded from claim by an attempts/ceiling coincidence — it must stay
  queryable (id, type, attempts, last_error) for operational visibility
  rather than becoming reachable only through raw SQL and
  `attempts >= N` reasoning.
- Event payloads are minimal, versionable, plain data — never a
  domain entity serialized directly — and are owned by the module that
  produces/consumes them, not a shared `events` package.
- Event types are named `<module>.<event_name>.v<version>` — e.g.
  `identity.email_verification_requested.v1`,
  `identity.password_reset_requested.v1`. The type string is persisted
  and outlives the deploy that wrote it, so once shipped its value is
  frozen. A payload change that already-enqueued rows couldn't survive
  gets its own `.v2` constant and `...V2` payload struct, registered
  alongside the v1 handler for as long as v1 rows can exist — never an
  edit to the existing type or shape. Keep the Go identifiers
  versioned too, so v1 and v2 can coexist without a rename.
- Types are matched exactly at dispatch. An event whose type has no
  registered handler is failed, retried, and eventually marked
  terminally failed — never silently dropped, and never decoded as if
  it were a different type. This is what makes a mid-rollout deploy
  ordering safe in either direction.
- None of this warrants an event framework: no registry abstraction,
  no envelope/codec layer, no shared `events` package. A constant, a
  payload struct, and a handler registration per event.

---

## 29. Platform Mail

`internal/platform/mail` is a narrow `Sender` interface (one message
shape, one send operation), not a generic notification framework.
`SMTPSender` is the current implementation, built on
`wneessen/go-mail` — that library's types stay inside this one file
(`smtp.go`); identity/application/domain never import it, and never
will need to know it exists. It works unchanged against any SMTP
server — a local Mailpit in development, a real provider in production
— purely through config (host/port/credentials/`MAIL_TLS_POLICY`);
never branch application or transport code on which environment it's
running in. A provider-native adapter (SES, Postmark) would be a new
type in this same package implementing `Sender`, wired in
`module.go` — not a change anywhere above it.

No implementation may ever log a message's body/token or the
configured credentials. A `Sender` must not retry internally — outbox's
retry policy is the only retry policy; a sender that also retries makes
the effective backoff impossible to reason about. Bound the whole send
operation with a timeout kept well under `platform/outbox`'s
`ClaimLease` (see section 27) — that relationship is the caller's most
important invariant to keep straight when tuning either config.

---

## 30. Shared Validation

`internal/validation` holds syntactic, business-neutral normalization
shared across modules — currently `NormalizeEmail` and
`NormalizeE164Phone`.

A rule may live there only if it is genuinely project-wide and carries
no policy. The test is whether the rule would read identically to a
module that knows nothing about the one it came from:

```text
canonicalizing an email, rejecting the display-name form   -> validation
E.164 syntax                                               -> validation

password minimum/maximum length      -> identity/application
account status rules                 -> identity/application
verification TTL, login policy       -> identity/application
whether a phone is required          -> identity/application
whether an email must be verified    -> identity/application
```

The package uses the standard library, returns ordinary errors, and
must not import `apperror` or know about HTTP, a database, or any
module. Translating a plain validation failure into a stable
application error code (`invalid_email`, `invalid_phone`) stays in the
application layer that owns the code.

Keep files concept-specific — `email.go`/`email_test.go`,
`phone.go`/`phone_test.go`. `validation/common.go`, `helpers.go`, or
`utils.go` are never acceptable here; the same goes for a module's own
`application/`, where a policy gets a named file
(`password_policy.go`), not a catch-all. Business-use-case tests stay
with their use case — `internal/validation`'s tests cover syntax only.

A rule used by exactly one module, and not clearly project-wide, stays
in that module until a second real consumer appears.

---

## 31. API Versioning

Public business REST endpoints carry a major version in the URL:

```text
/api/v{major}/...
```

Operational endpoints stay unversioned:

```text
/health
/ready
```

They are infrastructure, not part of the public contract — monitoring
and orchestration probe a stable path that must not move when the API's
compatibility boundary does.

The major version is a **compatibility boundary only**. It changes for
a breaking change and nothing else:

```text
stays v1                          requires v2
────────────────────────────      ──────────────────────────────────
a new endpoint                    removing or renaming a field
a new optional response field     changing a field's type
a new optional query parameter    changing existing field semantics
                                  restructuring a request/response
```

The URL major and the OpenAPI document's `info.version` are different
things: `info.version` moves for any published revision, including
backward-compatible ones, while `/api/v1` stays put across many of
them. A v2, when it arrives, is added alongside v1 rather than
replacing it in place.

Versioning is a **transport concern**. `domain`, `application`,
repositories, and infrastructure never gain a v1/v2 notion — both
transports call the same use cases wherever semantics are compatible:

```text
HTTP v1 ─┐
         ├── application use cases
HTTP v2 ─┘
```

A version that needs genuinely different behavior expresses that in its
own transport mapping, or in a new use case — never by branching a use
case on which HTTP version called it.

Do not create version directories before a second version exists.
Identity's handlers stay in `transport/http/`; `transport/http/v1/`
appears the day `v2/` does, not before. The same applies to the spec
and generated types — one `docs/api/openapi.yaml` and one
`internal/api/openapi/` package today, splitting only when a real v2
lands:

```text
docs/api/                     internal/api/openapi/
├── v1/openapi.yaml           ├── v1/
└── v2/openapi.yaml           └── v2/
```

HATEOAS is not part of this API's strategy. Clients construct URLs from
the documented contract; responses carry data, not link relations.

---

## 32. Localization

Who translates what:

```text
frontend UI text              -> frontend
API error codes               -> never localized
backend-generated human text  -> backend (this section)
```

The API's contract is the stable machine-readable `code`
(`invalid_credentials`, `invalid_email`); a client maps it to whatever
text it wants to show. Do not translate REST responses server-side off
`Accept-Language` — if that ever becomes desirable, it is its own
architectural decision, not a side effect of this package existing.
Domain and repository errors are never localized either.

Backend localization covers content this backend authors *and*
delivers: email subjects and bodies today; SMS, push, and
server-generated notifications later.

The split of responsibility:

```text
platform/localization   the engine — parsing, matching, fallback, templating
modules/<m>/translations that module's wording
```

Wording belongs to the module that owns the feature, not to a global
catalog every team edits. A module embeds its own `*.toml` files with
`//go:embed` — so a deployed binary carries its catalogs and never
depends on the working directory — and registers them by providing a
`localization.Catalog` into the `localization_catalogs` Fx group. That
group is the entire registration mechanism; `platform/localization`
imports no business module, and there is no plugin framework.

Message IDs are `<module>.<feature>.<message>`:

```text
identity.email_verification.subject
identity.email_verification.body
```

They are contracts, not English sentences: the ID stays put when the
wording changes. Template variables are explicit and named
(`VerificationURL`, `ExpiresAt`); an unsupplied one is a hard error, not
a `<no value>` hole in a sent email.

Locale resolution is deterministic:

```text
requested locale -> base language -> configured default
lv-LV            -> lv            -> en
```

An unknown, unsupported, or malformed locale falls back — not knowing a
recipient's language is normal, not a failure. Having nothing to say is
a failure: a message that resolves to nothing returns an error so the
outbox can retry and alert, because a blank email cannot be un-sent.

Locale must be explicit at the point of use. A worker has no HTTP
request, so it never reads `Accept-Language`; it uses a locale it was
given or falls back. Never infer one from an email domain, phone
country code, IP, or timezone. Do not add a persisted `preferred_locale`
until a real feature needs it.

The third-party library (`go-i18n`) is confined to
`platform/localization/goi18n.go`. Everything else depends on
`Localizer`, `Message`, and `Catalog`.

Not in scope, and not to be added speculatively: a generic notification
framework, database-backed or CMS-managed translations, machine
translation, or localization middleware with no consumer.

---

## 33. Rate Limiting

Abuse protection is layered; each layer does only what it is placed to
do well:

```text
CDN / WAF / API gateway        coarse DDoS and IP reputation, before traffic reaches Go
Go global HTTP limiter         generic per-IP allowance for every /api request
module endpoint limiters       per-IP limits on sensitive endpoints
                               (identity: register, login, refresh, verify-email, resend)
application/domain rules       login risk rules (identity: failed logins per account+IP,
                               5 / 15 min, before password hashing) and per-account
                               business limits (identity: resend cooldown + 24h cap)
```

### Login abuse protection

Login gets two independent layers, deliberately not one limiter:

```text
HTTP (transport)       15 login requests / minute / IP         ratelimit.Policy
Application (security) 5 failed attempts / account+IP / 15 min LoginRiskEvaluator
Future                 account-wide progressive delay/challenge for distributed failures
```

`LoginService` consults a `LoginRiskEvaluator` (Strategy) before the
password hash is verified and reports successful logins to it. The only
rule today is `AccountIPFailureRule`, backed by a `LoginFailureCounter`
port whose Redis adapter keys on `HMAC-SHA256(AUTH_ABUSE_KEY_SECRET,
normalized email)` plus the client IP — no raw address ever reaches
Redis. Each attempt reserves a slot atomically *before* verification, so
a burst of concurrent guesses cannot race past the threshold; a success
deletes the counter, so only failures accumulate. The window is fixed at
the first attempt. A refusal is the same `rate_limit_exceeded` 429 as the
IP limit, so it reveals neither which limit fired nor whether the account
exists. The counter fails open like the HTTP limiters.

It never locks an account: only the offending account+IP pair is
throttled, so the owner signing in from anywhere else is unaffected.
Account-wide protection against distributed guessing is future work and
must not be a hard block — it should combine progressive delay or a
challenge with trusted-device recognition.

Operational notes:

- **It is a hard deny for that pair.** After 5 failures, even the right
  password from that IP is refused until the window ends. Users behind a
  shared address (offices, hotels, carrier-grade NAT) share the pair, so
  one person's typos can block a colleague on the same account. That is
  acceptable for a starter; a real product may prefer progressive delay
  or a challenge at this layer instead of a deny — a new `RiskAction`,
  not a new limiter.
- **Unknown addresses follow the same path.** The bucket is reserved for
  the normalized supplied address whether or not an account exists, and
  every attempt that names an account — unknown, password-less, wrong
  password, disabled — costs one password verification (a throwaway hash
  where there is no real one) and returns the same `invalid_credentials`.
  "Account disabled" is only reported after the correct password. None
  of these paths may become cheaper or observably different.
- **Fail-open must be loud, not a log storm.** If Redis is down, the
  counter allows the attempt. Every such check increments an OTel counter
  — `auth_abuse_protection_unavailable` (and, for the HTTP limiters,
  `rate_limit_protection_unavailable`; Prometheus shows both as
  `*_total`) — while logs are limited to one Error on the transition, an
  Error summary at most once a minute with the failure count, and one
  Info on recovery (`observability.ProtectionSignal`). Alert on the
  counters' rate or the transition log. While they fire, login
  brute-force protection and the distributed rate limits are both off —
  edge/WAF protection is the remaining defense, so production must have
  one.
- **The dummy hash is precomputed.** `NewLoginService` hashes a
  throwaway password once at startup through the configured hasher, so
  it always has the current Argon2 parameters; requests only *verify*
  against it. Changing the parameters needs no extra step.

Invariants — keep these when changing anything here:

1. **Authentication never depends on observability.** `ProtectionSignal`
   and security events sit beside the request path, not in it: counter
   updates are in-memory (the OTel SDK exports asynchronously), no
   network I/O happens on a request, and an exporter or collector
   failure can never turn a login into a 500 or delay it. Instrument
   creation may fail startup; recording never fails a request.
2. **Metric attributes stay bounded.** Low-cardinality values such as
   `scope=identity.login.account_ip` only — never IP, email, user ID, a
   Redis key or any other per-subject value as an OTel attribute.
3. **It is timing equalization, not constant time.** Every account path
   does exactly one Argon2 verification, which removes by far the
   largest observable difference; database, cache and network paths can
   still differ slightly. Don't describe or rely on it as constant-time.
4. **Redis down disables both** the distributed HTTP limits and
   account+IP protection. Failing open is the deliberate availability
   choice; the transition logs and counters exist to make it visible,
   and edge/WAF protection is what remains meanwhile.

Operations: alert on the protection *state* and outage duration (the
"unavailable" transition log, or a counter that keeps increasing over a
window), not on raw counter volume — ten thousand increments during one
Redis outage are one incident. A 0/1 "degraded" gauge would make
duration alerts simpler; add it when a real alerting setup needs it.

Scope: the starter's login-abuse layer is complete. The next change here
should be a genuinely new capability — trusted devices with
progressive account-wide friction, MFA/passkeys, or challenge handling —
not more abstraction or more variants of IP counting.
- **Rotating `AUTH_ABUSE_KEY_SECRET` resets all failure counters** (the
  keys are HMACs under it). That is expected, not data loss: at worst an
  attacker regains one window's allowance. Old keys expire on their own.
- **Keep the seam small.** One rule does not justify a rule registry,
  policy factory, decision engine or context builder. A second rule
  becomes a composite evaluator; anything more waits for real rules that
  need it. The same goes for `SecurityEvents`: one tiny method, one log
  consumer, until audit/metrics consumers exist.

Where future pieces go:

```text
Middleware                request ID, client IP, coarse rate limiting, auth, request-level bot protection
Ports & Adapters          CAPTCHA/challenge verifier, MFA provider, passkeys/WebAuthn,
                          breached-password checker, external risk intelligence
Evaluator rules (chain)   trusted-device recognition, account-wide progressive throttling,
                          login risk rules, suspicious-login checks
Events                    identity.login.failed / .throttled today; challenge_required,
                          risk_high, mfa.failed later
```

A second rule is added as a composite `LoginRiskEvaluator` in wiring
(run rules in order, keep the strictest decision), and each new
`RiskAction` (Delay, Challenge, RequireMFA) arrives with the rule that
returns it. Security outcomes are published through
`application.SecurityEvents`; alerting, audit logs and metrics subscribe
to that port (structured logs are the first consumer) instead of being
written into `LoginService` or the handler. Events carry user id, client
IP and an internal reason — never the email or password.

Trusted devices, when built: a cryptographically random, server-signed
device token in an `HttpOnly`, `Secure` cookie with an appropriate
`SameSite`; never the User-Agent as device identity; and recognition only
lowers friction (skips a throttle or challenge) — it is never an
authentication factor on its own.

Anything keyed by IP can be sidestepped by changing IP, so a rule that
must hold per user — "one verification email per minute for this
account" — belongs in the last layer. There it is computed from durable
business records under the account's row lock, in the same transaction
as the write, so neither a new address nor concurrent requests get
around it (see identity's `domain.ResendPolicy` and
`VerificationStore.Replace`). It throttles with an exact `Retry-After`;
it never locks the account. Account-wide rules of that kind apply only
to authenticated actions: an account-keyed block on login would let
anyone who knows an address deny that user their own logins — which is
why login's account rule is scoped to account+IP (above).

`ratelimit.Policy` is the reusable abstraction. A module lists its
endpoint policies in a plain struct in its transport package (identity's
`RateLimitPolicies`), filled from its own config — there is no
cross-module policy interface, provider, or registry.

Rate limiting is distributed, backed by Redis (`platform/redis`,
`go-redis`), so a limit is shared across every API process rather than
being per-instance. The GCRA algorithm comes from `redis_rate` and runs
as a Lua script inside Redis — never read-modify-write in Go, which
would let two processes both see the last free slot and both take it.

`redis_rate` types must not appear outside `platform/ratelimit`'s Redis
adapter; for HTTP limits, modules depend on `Policy`, `Key`, `Result`,
and `Limiter`. A module's own abuse counter (identity's login failures)
is an ordinary infrastructure adapter using go-redis behind an
application port, the way a repository uses pgx.

The split of responsibility:

```text
platform/ratelimit   mechanics: keys, validation, backend, failure policy
modules/<m>          policy: what this module's endpoints are worth
```

Generic per-IP protection for the whole API lives in platform config
(`RATELIMIT_GLOBAL_PER_MINUTE`). Endpoint limits live with the module
that owns the endpoint (identity's `AUTH_RATE_LIMIT_*`). Tuning per
environment means overriding a handful of variables, not adding one per
route: an endpoint only earns its own knob when it is worth abusing
specifically, and everything else is covered by the generic limit.
Defaults are conservative starting points — tune from real traffic
before treating them as load-bearing.

Client IP must come from `requestctx.ClientIP`, resolved once by
`middleware.ClientIP` through the trusted-proxy policy
(`platform/httpserver/clientip`). Never read `X-Forwarded-For` or
`X-Real-IP` in a limiter: a caller that can choose its own IP can choose an unused
bucket per request and is effectively unlimited.

Keys are built by `ratelimit`, never assembled by a handler. Only
per-IP keys exist today. IPs stay in the clear — they are not
credentials, are already in access logs, and staying greppable is
useful during an incident. A personal identifier (an email address) is
normalized (see `internal/validation`) and keyed-hashed before it
reaches Redis — a Redis instance is a far softer target than the
database. Identity's login failure counter does exactly that, with
HMAC-SHA256 under the dedicated `AUTH_ABUSE_KEY_SECRET`.

Operational endpoints (`/health`, `/ready`) are exempt. Rate-limiting a
liveness probe is an outage mode: the probe source starts getting 429s,
which reads as an unhealthy process and gets a healthy one restarted.

Middleware order is `RequestID → ClientIP → AccessLog → Recovery → CORS →
RateLimit → router`. Rejected requests are still given an id, a
resolved client address, and logged, a limiter panic is still recovered, a 429 still carries CORS
headers, preflight OPTIONS never spends allowance, and the router never
runs for a denied request.

A rejected request gets `429` with the standard error envelope
(`rate_limit_exceeded`) and a `Retry-After` header in whole seconds,
always rounded up — telling a client to retry sooner than its allowance
exists invites a rejection loop. Only `Retry-After` is sent; the
`RateLimit-*` headers are still an IETF draft and are deliberately not
adopted.

**Limiter failures fail open.** Redis unavailable means the request
proceeds; each such check increments `rate_limit_protection_unavailable`
(OTel counter, with the scope), and the outage is logged on transition,
as a once-a-minute summary, and on recovery — never per request, which
during an attack would be a log storm. A limiter is protective, not
an authorization boundary — making Redis able to reject all traffic
turns an optional dependency into a global single point of failure,
which is worse and far likelier than the abuse window while it is down.
Anything that must hold with Redis down belongs in an authorization
check. Failing open is never silent, and never logs the subject: that is
an IP or account hash, and it would put per-user identifiers into log
aggregation at request rate.

Redis holds ephemeral enforcement state with backend-managed expiry.
Never write limiter state to PostgreSQL, and never manage TTLs by hand —
`redis_rate`'s script owns them.

**Account lockout is a separate concern and is deliberately not
implemented.** These are three different things:

```text
rate limiting     protects traffic and resources
login throttling  slows authentication abuse
account lockout   persistent business/security state
```

Login is limited on two independent dimensions — client IP and
normalized account identifier — so that one source attacking many
accounts and many sources attacking one account are both covered. Both
are consumed on every attempt, before credentials are verified: that
way a refused attempt costs no password hashing (Argon2 is deliberately
expensive, which makes an unthrottled login endpoint a CPU exhaustion
vector), and the limiter never looks the account up, so an address that
has never registered behaves identically to one that has. Preserve that
property — a limit that behaves differently for real accounts is an
enumeration oracle.

The account dimension throttles and must never become lockout. GCRA
refills continuously, so an attacker hammering one address delays that
user by seconds; a fixed penalty window would hand any attacker a way to
deny service to any user they can name.

Do not add: a custom GCRA or copied Lua, a rules engine, a
database-backed limiter, CAPTCHA, device fingerprinting, abuse scoring,
or a Redis Cluster abstraction before an environment needs one.
