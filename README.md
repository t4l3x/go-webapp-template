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
- Localized emails, structured JSON logging, request IDs, panic recovery,
  CORS, trusted-proxy client IP resolution.

<!-- template:start -->
## Start a new project from this template

Every real project needs its own Go module path. Don't keep this
template's module path, and don't use relative imports.

1. On GitHub, click **Use this template → Create a new repository**.
2. Create the new repository (e.g. `acme/payment-service`).
3. Clone it:
   `git clone git@github.com:acme/payment-service.git && cd payment-service`
4. Initialize it:

   ```sh
   ./scripts/init-template.sh github.com/acme/payment-service payment-service
   ```

5. Copy the environment file: `cp .env.example .env`
6. Start the stack as described in [Local development](#local-development).

The **Use this template** button appears only once the template's owner
has enabled *Settings → General → Template repository* on GitHub. Without
it, clone the template instead, point `origin` at your own repository,
and run the same script. See
[docs/guides/new_project_from_template.md](docs/guides/new_project_from_template.md)
for what the script changes, the manual-clone flow, and `gonew`.
<!-- template:end -->

## Local development

Requires Go (version in `go.mod`), Docker with Compose v2, and `make`.

```sh
cp .env.example .env   # works as is: dev-only defaults, never commit .env
make up                # Postgres, Redis, Mailpit, API (hot reload), worker
make migrate-up        # apply db/migrations (never run automatically)
curl localhost:8080/health
```

| What | Where |
| --- | --- |
| API | http://localhost:8080 (`/api/v1/...`, `/health`) |
| Mailpit (sent email) | http://localhost:8025 |
| Postgres / Redis | `localhost:5432` / `localhost:6379` |

`make help` lists every target. `make run` / `make run-worker` run a
process on the host instead of in Docker (stop its container first:
`docker stop go-webapp-template-api`). The email-verification flow is
walked through in [docs/guides/local_mail.md](docs/guides/local_mail.md).

Before committing: `make check-full` and `make test-integration`
(integration tests use a separate, disposable Postgres and Redis on ports
5434 / 6380).

## Configuration contexts

The same variable holds a different address depending on where the
process runs. There is one variable per dependency (`DB_DSN`, not
`DB_DSN_HOST` / `DB_DSN_DOCKER`); what changes is who supplies the value:

| Context | Who runs there | `DB_DSN` host part | Value comes from |
| --- | --- | --- | --- |
| Host machine | `make run`, `make run-worker`, `make migrate-*` | `localhost:5432` (published port) | `.env` |
| Docker network | `api` / `worker` containers from `make up` | `postgres:5432` (service name) | `.env`, overridden in `docker/docker-compose.dev.yml` |
| Production | deployed processes, migration job | managed database endpoint | deployment config / secret store |

`REDIS_URL` and `MAIL_HOST` follow the same pattern. Integration tests use
their own `DB_DSN_TEST_ADMIN` / `REDIS_URL_TEST` so they can never touch
development data.

## Troubleshooting

**Leftover containers from an older project name.** Compose resources
are named after the project (`make up` uses `go-webapp-template`). If you
ran this code earlier under another name, its containers may still hold
ports 5432/6379/8080. Inspect and stop them without touching data:

```sh
docker ps -a --format '{{.Names}}\t{{.Status}}'
docker compose -p <old-project-name> down      # removes containers, keeps volumes
docker volume ls
```

`docker compose -p <old-project-name> down -v` **also deletes that
project's volumes, i.e. its local database data**. Only run it if you
want that data gone. `make down` never removes volumes.

**`missing .env`** — run `cp .env.example .env`.

## Layout

```
cmd/api, cmd/worker        one binary per process; composition in internal/bootstrap
internal/modules/<name>/   domain → application → infrastructure → transport
internal/platform/         shared mechanics (httpserver, outbox, mail, ratelimit, …)
db/migrations/             golang-migrate SQL migrations
docs/                      API contract, conventions, guides
```

## License

Copyright © 2026 t4l3x

Licensed under the Apache License 2.0. See [LICENSE](LICENSE).

Third-party dependencies keep their own licenses. They are listed with
their full texts in [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt),
which is generated by `make licenses` and shipped in the production image
under `/usr/share/licenses/go-webapp-template/`.

CI runs `make licenses-check`. It fails if a dependency's license is
outside the allowlist in `make/licenses.mk`, if a license can't be identified, or if
the committed report is stale. Code copied into this repository (rather
than imported) must keep its original copyright and license notice. See
the licensing rules in
[docs/conventions/backend_conventions.md](docs/conventions/backend_conventions.md#licensing).

## Documentation

- [AGENTS.md](AGENTS.md) — the short rule set
- [docs/conventions/backend_conventions.md](docs/conventions/backend_conventions.md) — full conventions
- [docs/guides/adding_an_endpoint.md](docs/guides/adding_an_endpoint.md)
- [docs/guides/registration_and_verification.md](docs/guides/registration_and_verification.md)
- [docs/guides/local_mail.md](docs/guides/local_mail.md)
