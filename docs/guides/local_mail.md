# Local Email Verification (Mailpit)

Development uses [Mailpit](https://mailpit.axllent.org/) as the SMTP
server: `platform/mail.SMTPSender` (backed by
[`wneessen/go-mail`](https://github.com/wneessen/go-mail)) connects to
it exactly the way it would connect to a real provider — nothing in
the sender, or anywhere above `platform/mail`, knows Mailpit exists.
Nothing is ever delivered to a real inbox locally.

## Addresses

| From                       | Host        | Port |
|-----------------------------|-------------|------|
| Inside `docker compose`     | `mailpit`   | 1025 (SMTP), 8025 (web UI, mapped to host) |
| On the host (worker run via `go run`/`make run-worker`, not in Docker) | `localhost` | 1025 |

Mailpit's web UI, once the dev stack is up, is always at
**http://localhost:8025** regardless of which way the worker is
running — it's the container's exposed port either way.

## Running end to end

```sh
# Everything in Docker (api, worker, postgres, mailpit, ...):
make up && make migrate-up

# Or: infra in Docker, worker on the host. .env already says
# MAIL_HOST=localhost (Compose overrides it to mailpit only inside the
# worker container), so no change is needed — just stop the container
# worker first so only one worker is polling:
docker stop go-webapp-template-worker
make run-worker
```

Then:

1. Register a user:
   ```sh
   curl -X POST http://localhost:8080/api/v1/auth/register \
     -H 'Content-Type: application/json' \
     -d '{"email":"you@example.com","password":"supersecretpassword"}'
   ```
2. Make sure the worker is running (see above) — it polls the outbox
   and sends the verification email.
3. Open **http://localhost:8025** and open the message. The link is
   real — `SMTPSender` sent an actual SMTP message to Mailpit, which
   is displaying it exactly as delivered.
4. Submit the link's token to `POST /api/v1/auth/verify-email` as JSON
   `{"token":"<token from link>"}`. Success returns 204; reuse returns 400.
5. Sign in and call `GET /api/v1/auth/me` to see `email_verified: true`.
   Before verification, an authenticated `POST /api/v1/auth/resend-verification`
   (no body) requests a replacement and invalidates older links.

Both API and worker require the same `AUTH_EMAIL_VERIFICATION_SECRET`.
See [registration and verification](registration_and_verification.md) for
failure handling and the frontend contract.

## Config reference

See `.env.example`'s `MAIL_*` variables (`MAIL_HOST`, `MAIL_PORT`,
`MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_FROM`, `MAIL_TLS_POLICY`,
`MAIL_TIMEOUT`). `MAIL_TLS_POLICY` is one of `none` (Mailpit — no TLS
at all), `starttls` (upgrade after connecting in the clear, typically
port 587), or `implicit` (TLS from the first byte, typically port
465). Pointing these at a real provider's host/port/credentials/TLS
policy — Mailpit accepts unauthenticated, unencrypted connections, so
`MAIL_USERNAME`/`MAIL_PASSWORD` stay empty and `MAIL_TLS_POLICY=none`
locally — is the entire change needed to use this in a real
environment; nothing in application code changes.
