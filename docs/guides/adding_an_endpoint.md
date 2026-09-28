# Adding a New REST Endpoint

This is the standard, predictable path for adding one new REST endpoint
(and the use case behind it) to a business module. It follows the same
contract-first flow already used by every existing identity endpoint —
see [`## 25. OpenAPI Contract`](../conventions/backend_conventions.md)
in the conventions doc for the underlying rules this guide walks
through in practice.

```text
OpenAPI spec
     ↓
generated types (make openapi-generate)
     ↓
application use case
     ↓
HTTP handler (decode → call use case → map → write)
     ↓
route registration
     ↓
tests
```

The existing `POST /api/v1/auth/register` endpoint already follows this exact
shape end to end:

```text
openapi.RegisterRequest
        ↓
application.RegisterInput
        ↓
RegisterService.Register()
        ↓
application.RegisterOutput
        ↓
newRegisterResponse()
        ↓
openapi.RegisterResponse
```

When in doubt about a step below, read the real `register.go` /
`transport/http/handler.go` / `transport/http/dto.go` files — they're
the canonical example, not this document.

The walkthrough below adds a hypothetical `POST /api/v1/auth/change-password`
to the identity module.

---

## 1. Define the contract in `docs/api/openapi.yaml`

The spec is the source of truth. Nothing about this endpoint exists
anywhere else until it exists here.

Add the path:

```yaml
/api/v1/auth/change-password:
  post:
    tags: [auth]
    summary: Change the authenticated user's password
    operationId: changePassword
    security:
      - bearerAuth: []
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/ChangePasswordRequest'
    responses:
      '204':
        description: Password changed successfully
      '401':
        $ref: '#/components/responses/Unauthorized'
      '413':
        $ref: '#/components/responses/PayloadTooLarge'
      '415':
        $ref: '#/components/responses/UnsupportedMediaType'
      '500':
        $ref: '#/components/responses/InternalServerError'
```

Add the request schema next to the other `*Request` schemas:

```yaml
ChangePasswordRequest:
  type: object
  required: [current_password, new_password]
  properties:
    current_password:
      type: string
    new_password:
      type: string
      minLength: 8
      maxLength: 256
```

A few things this repo's spec deliberately does *not* do, on purpose —
don't reintroduce them:

- No `format: email` on email fields. `oapi-codegen`'s generated type
  for that format has its own `UnmarshalJSON` that silently extracts a
  bare address out of a `"Display Name <addr>"` string via
  `mail.ParseAddress` — before application-layer validation ever runs.
  Plain `type: string` keeps validation entirely in the application
  layer, where it belongs. (See the comment at the top of `dto.go`.)
- No documentation annotations scattered elsewhere (handler comments,
  struct tags). Everything about the contract lives in this one file.

## 2. Generate Go types

```sh
make openapi-generate
```

This produces (in `internal/api/openapi/types.gen.go`, never hand-edited):

```go
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
```

You never write that struct by hand, and you never import
`internal/api/openapi` from anywhere except a module's
`transport/http` package — not application, not domain, not
infrastructure.

## 3. Add the application use case

New file: `internal/modules/identity/application/change_password.go`.
Application code knows nothing about OpenAPI, HTTP status codes, or
JSON — only domain-shaped input and output.

```go
package application

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

var errCurrentPasswordIncorrect = apperror.New(
	apperror.KindUnauthorized,
	"current_password_incorrect",
	"Current password is incorrect",
)

type ChangePasswordService struct {
	users  UserRepository
	hasher PasswordHasher
	cfg    RegisterConfig // reuse the existing password-length policy
}

func NewChangePasswordService(
	users UserRepository,
	hasher PasswordHasher,
	cfg RegisterConfig,
) *ChangePasswordService {
	return &ChangePasswordService{users: users, hasher: hasher, cfg: cfg}
}

type ChangePasswordInput struct {
	UserID          uuid.UUID
	CurrentPassword string
	NewPassword     string
}

func (s *ChangePasswordService) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	user, err := s.users.FindByID(ctx, in.UserID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return errUnauthorized
	}
	if err != nil {
		return apperror.Wrap(apperror.KindInternal, "user_lookup_failed", "Failed to change password", err)
	}

	if user.PasswordHash == nil {
		return errCurrentPasswordIncorrect
	}

	ok, err := s.hasher.Verify(in.CurrentPassword, *user.PasswordHash)
	if err != nil {
		return apperror.Wrap(apperror.KindInternal, "password_verify_failed", "Failed to change password", err)
	}
	if !ok {
		return errCurrentPasswordIncorrect
	}

	length := utf8.RuneCountInString(in.NewPassword)
	if length < s.cfg.PasswordMinLength || length > passwordMaxLength {
		return apperror.New(apperror.KindValidation, "invalid_password", "Password does not meet length requirements")
	}

	newHash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return apperror.Wrap(apperror.KindInternal, "password_hash_failed", "Failed to change password", err)
	}

	// UserRepository has no write path for an existing user's password
	// today — add one narrow method for exactly this (e.g.
	// UpdatePasswordHash(ctx, userID, newHash) error) rather than a
	// generic Update/Save. Repository interfaces are consumer-driven:
	// add a method only once a real use case needs it, which is now.
	if err := s.users.UpdatePasswordHash(ctx, in.UserID, newHash); err != nil {
		return apperror.Wrap(apperror.KindInternal, "password_update_failed", "Failed to change password", err)
	}

	return nil
}
```

Note the one real piece of new plumbing this endpoint needs beyond the
happy-path template: `UserRepository` gains `UpdatePasswordHash`, so
`infrastructure/postgres/user_repository.go` needs that method too,
and the interface in `application/ports.go` needs it added. Don't add
speculative repository methods for hypothetical future endpoints —
only ever add exactly what the use case in front of you requires.

## 4. Wire it through Fx

In `internal/modules/identity/wiring_http.go` (`HTTPModule`), add the
constructor next to the other use cases:

```go
application.NewRegisterService,
application.NewLoginService,
application.NewRefreshService,
application.NewLogoutService,
application.NewGetMeService,
application.NewChangePasswordService, // new
```

Fx resolves `*ChangePasswordService`'s dependencies (`UserRepository`,
`PasswordHasher`, `application.RegisterConfig`) automatically — no
other wiring change is needed here since they're already provided
for the other use cases.

## 5. Extend the handler

`Handler` and `NewHandler` in `transport/http/handler.go` take one
field/param per use case, so both need the new dependency added:

```go
type Handler struct {
	register       *application.RegisterService
	login          *application.LoginService
	refresh        *application.RefreshService
	logout         *application.LogoutService
	getMe          *application.GetMeService
	changePassword *application.ChangePasswordService // new
	clientIP       *clientip.Resolver
	responder      *response.Responder
}
```

(`NewHandler`'s parameter list and the struct literal it builds need
the matching addition — Fx passes the extra constructor argument
automatically once it's part of the signature.)

Then the handler itself, following the same
**decode → call use case → map → write** shape every other handler
uses:

```go
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	req, err := request.DecodeJSON[openapi.ChangePasswordRequest](w, r)
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		h.responder.Error(w, r, errUnauthorized)
		return
	}

	err = h.changePassword.ChangePassword(r.Context(), application.ChangePasswordInput{
		UserID:          principal.UserID,
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	})
	if err != nil {
		h.responder.Error(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

The boundary to keep in mind at every step:

```text
openapi.ChangePasswordRequest
            ↓
application.ChangePasswordInput
```

Generated types stop at the handler. They never travel past it.

## 6. Register the route

In `transport/http/routes.go`:

```go
return httpserver.Prefix(
	apiV1Prefix,

	httpserver.POST("/auth/register", http.HandlerFunc(handler.Register)),
	httpserver.POST("/auth/login", http.HandlerFunc(handler.Login)),
	httpserver.POST("/auth/refresh", http.HandlerFunc(handler.Refresh)),
	httpserver.POST("/auth/logout", authenticated(handler.Logout)),
	httpserver.GET("/auth/me", authenticated(handler.GetMe)),
	httpserver.POST("/auth/change-password", authenticated(handler.ChangePassword)), // new
)
```

Paths here are relative to the group's mount point — write
`/auth/change-password`, never `/api/v1/auth/change-password`. The
prefix is stated once, at the top. The spec, by contrast, lists the
full runtime path (`/api/v1/auth/change-password`), so the two must be
kept consistent by hand: adding an endpoint to a versioned group means
the spec path carries the prefix the route constructor does not.

Adding an endpoint is a backward-compatible change, so it stays on
`/api/v1` — only a breaking change earns a new major (see section 30 of
the conventions).

Use `authenticated(...)` for anything requiring `security: [bearerAuth]`
in the spec, a bare `http.HandlerFunc(...)` otherwise — keep the route
list's auth wrapping consistent with what the OpenAPI doc promises for
that operation.

## 7. Add a response mapper — only if there's a response body

`/api/v1/auth/change-password` returns `204 No Content`, so there's nothing to
map here. When an endpoint does return a body, add one small function
in `dto.go`, next to `newUserResponse` / `newTokenResponse` /
`newRefreshResponse`:

```go
func newSomethingResponse(out application.SomethingOutput) openapi.SomethingResponse {
	return openapi.SomethingResponse{
		Id: out.ID,
	}
}
```

and call it from the handler instead of building the response type
inline:

```go
response.JSON(w, http.StatusOK, newSomethingResponse(out))
```

If a generated response type has a field that's awkward to populate
directly from the application output (a derived value, a unit
conversion), that logic belongs in the mapper — e.g. the existing
`expiresIn(time.Time) int64` helper in `dto.go`, which turns an
absolute expiry into the `expires_in` seconds-from-now the contract
expects. That's an HTTP/API representation detail, not something
application/domain code should know about.

## 8. Tests

Match the coverage the existing endpoints have:

- **Application use-case test** (`change_password_test.go`, next to
  the use case): correct-current-password success, wrong-current-password
  rejection (`current_password_incorrect`), weak new password rejection,
  nil-password (OAuth-style) user rejection, repository failure paths.
- **HTTP handler test** (`handler_test.go` or a focused
  `change_password_test.go` in `transport/http`): full request → response
  round trip via `httptest`, using the shared fakes already in that
  package. Include a case that goes through the real `DecodeJSON` path,
  not just the application layer directly — the email-format regression
  documented in `dto.go`'s comment happened exactly because a
  transport-layer detail (a generated type's own `UnmarshalJSON`) wasn't
  exercised by an application-only test.
- **Auth/error cases**: missing/invalid bearer token → 401; wrong
  current password → 401 with the stable `current_password_incorrect`
  code, not a generic message; malformed/oversized/wrong-Content-Type
  body → the same `request` package behavior every other endpoint gets
  for free.
- **Repository integration test** if persistence changed (it did here —
  `UpdatePasswordHash`): a real-Postgres, build-tag-gated test in
  `infrastructure/postgres`, following the existing session/user
  repository integration test pattern (disposable database, safety
  guard before any destructive statement).

## 9. Verify the contract didn't drift

```sh
make openapi-check
```

This regenerates from the spec and fails if the committed generated
file doesn't match — i.e. it catches "edited the spec but forgot to
regenerate" or "hand-edited the generated file" before either reaches
review.

---

## Keep in mind

- Every module doing this the same way is what keeps
  `internal/api/openapi` safe to depend on from `transport/http` and
  nowhere else — the moment a generated type leaks into `application`
  or `domain`, a future non-REST transport (GraphQL, gRPC) stops being
  a transport-only project and becomes a business-logic rewrite.
- Don't add a repository method, a config field, or an abstraction
  ahead of the endpoint that actually needs it. `UpdatePasswordHash`
  above exists because this endpoint needs it — not because "we'll
  probably want to update users generically later."
- If a step here ever stops matching the real code (naming, wiring,
  file layout), trust the code and fix this doc, not the other way
  around.
