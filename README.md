# Go Webapp Template

A production-minded Go web application: a modular monolith with an HTTP
API process and a background worker, built on stdlib `net/http`, Uber Fx
(composition roots only), PostgreSQL, Redis and a transactional outbox.

What's included:

- **Identity module** — registration, login, refresh/logout sessions,
  email verification (signed, derived tokens; no bearer secrets at rest).
- **Transactional outbox** (`internal/platform/outbox`) — leased claims,
  backoff, terminal failure state; delivered by `cmd/worker`.
- **Distributed rate limiting** — Redis GCRA, fails open.
- **Contract-first OpenAPI** — `docs/api/openapi.yaml` generates the
  transport types.
- **Localization** of emails, structured logging, OpenTelemetry, request
  IDs, panic recovery, CORS, trusted-proxy client IP resolution.

## Start a new project

Every real project needs its own Go module path — don't keep this
template's module path and don't use relative imports.

**A. Recommended: GitHub template.** Click *Use this template* on GitHub,
clone the new repository, then:

```sh
./scripts/init-template.sh github.com/acme/payment-service payment-service
```

**B. Manual clone.** Clone or copy this repository, point Git at your own
repository (`git remote set-url origin <your-repo-url>`, or `rm -rf .git
&& git init`), then run the same script.

The script rewrites the module path and project-name placeholders, runs
`go mod tidy`, `gofmt`, `go build` and `go test`, and prints next steps.
The project name is optional (defaults to the module path's last
element). Details, including a `gonew` alternative:
[docs/guides/new_project_from_template.md](docs/guides/new_project_from_template.md).

## Quick start

Requires Go (see `go.mod`) and Docker.

```sh
cp .env.example .env      # then replace the change-me secrets
make up                   # Postgres, Redis, Mailpit, API, worker, tracing
make migrate-up
make help                 # every target
```

Before committing: `make check-full` and `make test-integration`.

## Layout

```
cmd/api, cmd/worker        one binary per process; composition in internal/bootstrap
internal/modules/<name>/   domain → application → infrastructure → transport
internal/platform/         shared mechanics (httpserver, outbox, mail, ratelimit, …)
db/migrations/             golang-migrate SQL migrations
docs/                      API contract, conventions, guides
```

## Documentation

- [AGENTS.md](AGENTS.md) — the short rule set
- [docs/conventions/backend_conventions.md](docs/conventions/backend_conventions.md) — full conventions
- [docs/guides/adding_an_endpoint.md](docs/guides/adding_an_endpoint.md)
- [docs/guides/registration_and_verification.md](docs/guides/registration_and_verification.md)
- [docs/guides/local_mail.md](docs/guides/local_mail.md)
