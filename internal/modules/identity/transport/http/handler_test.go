package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
	identityhttp "github.com/t4l3x/go-webapp-template/internal/modules/identity/transport/http"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/requestctx"
	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver/response"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func newTestHandler(t *testing.T) (*identityhttp.Handler, *fakeUserRepository, *fakeTokenManager) {
	t.Helper()

	handler, users, _, tokens := newTestHandlerWithSessions(t)

	return handler, users, tokens
}

func newTestHandlerWithSessions(t *testing.T) (*identityhttp.Handler, *fakeUserRepository, *fakeSessionRepository, *fakeTokenManager) {
	t.Helper()

	logger, _ := testkit.NewLogger()
	responder := response.NewResponder(logger)

	users := newFakeUserRepository()
	registrations := newFakeRegistrationStore()
	sessions := newFakeSessionRepository()
	tokens := newFakeTokenManager()

	handler := identityhttp.NewHandler(
		application.NewRegisterService(registrations, fakeHasher{}, application.RegisterConfig{
			PasswordMinLength:    12,
			EmailVerificationTTL: 24 * time.Hour,
		}),
		mustLogin(application.NewLoginService(users, sessions, fakeHasher{}, tokens, allowAllRisk{}, discardEvents{}, application.SessionConfig{RefreshTokenTTL: time.Hour})),
		application.NewRefreshService(sessions, users, tokens, application.SessionConfig{RefreshTokenTTL: time.Hour}),
		application.NewLogoutService(sessions),
		application.NewGetMeService(users),
		nil, // verifyEmail: covered by verification_test.go
		nil, // resendVerification: covered by verification_test.go
		responder,
	)

	return handler, users, sessions, tokens
}

func TestHandler_Register_Created(t *testing.T) {
	handler, _, _ := newTestHandler(t)

	body := `{"email":"user@example.com","password":"supersecretpassword"}`
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/register", body)
	rec := httptest.NewRecorder()

	handler.Register(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.User.Email != "user@example.com" {
		t.Fatalf("User.Email = %q, want %q", got.User.Email, "user@example.com")
	}
}

// TestHandler_Register_DisplayNameEmailRejected exercises the full HTTP
// path (DecodeJSON -> application.Register), not just the application
// layer in isolation. A generated OpenAPI wire type for an email field
// can silently rewrite "Display Name <addr>" input to a bare address
// during JSON decoding (see dto.go) — an application-only test would
// never see that, since by the time application code runs the value
// has already been silently normalized away.
func TestHandler_Register_DisplayNameEmailRejected(t *testing.T) {
	handler, users, _ := newTestHandler(t)

	body := `{"email":"John Smith <john@example.com>","password":"supersecretpassword"}`
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/register", body)
	rec := httptest.NewRecorder()

	handler.Register(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	code := decodeErrorCode(t, rec.Body.Bytes())
	if code != "invalid_email" {
		t.Fatalf("error.code = %q, want %q", code, "invalid_email")
	}

	if _, err := users.FindByEmail(context.Background(), "john@example.com"); err == nil {
		t.Fatalf("expected no user to be created for a display-name email")
	}
}

func TestHandler_Register_DuplicateEmailConflict(t *testing.T) {
	handler, _, _ := newTestHandler(t)

	body := `{"email":"user@example.com","password":"supersecretpassword"}`

	first := newJSONRequest(http.MethodPost, "/api/v1/auth/register", body)
	handler.Register(httptest.NewRecorder(), first)

	second := newJSONRequest(http.MethodPost, "/api/v1/auth/register", body)
	rec := httptest.NewRecorder()
	handler.Register(rec, second)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}

	code := decodeErrorCode(t, rec.Body.Bytes())
	if code != "email_already_exists" {
		t.Fatalf("error.code = %q, want %q", code, "email_already_exists")
	}
}

func TestHandler_Login_Success(t *testing.T) {
	handler, users, _ := newTestHandler(t)
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	body := `{"email":"user@example.com","password":"supersecretpassword"}`
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/login", body)
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.AccessToken == "" || got.RefreshToken == "" {
		t.Fatalf("expected non-empty tokens, got %+v", got)
	}
}

// TestHandler_Login_RecordsContextClientIP pins that the session's IP
// is the one the platform ClientIP middleware resolved and stored — not
// a second resolution of RemoteAddr or of a forwarded header.
func TestHandler_Login_RecordsContextClientIP(t *testing.T) {
	handler, users, sessions, _ := newTestHandlerWithSessions(t)
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	body := `{"email":"user@example.com","password":"supersecretpassword"}`
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req = req.WithContext(requestctx.WithClientIP(req.Context(), netip.MustParseAddr("203.0.113.9")))
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []string
	for _, session := range sessions.all() {
		if session.IPAddress != nil {
			got = append(got, *session.IPAddress)
		}
	}
	if len(got) != 1 || got[0] != "203.0.113.9" {
		t.Fatalf("session IPs = %v, want [203.0.113.9]", got)
	}
}

func TestHandler_Login_UnresolvedClientIPIsNil(t *testing.T) {
	handler, users, sessions, _ := newTestHandlerWithSessions(t)
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	body := `{"email":"user@example.com","password":"supersecretpassword"}`
	rec := httptest.NewRecorder()

	handler.Login(rec, newJSONRequest(http.MethodPost, "/api/v1/auth/login", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	for _, session := range sessions.all() {
		if session.IPAddress != nil {
			t.Fatalf("IPAddress = %q, want nil when no client IP was resolved", *session.IPAddress)
		}
	}
}

func TestHandler_Login_InvalidCredentialsUnauthorized(t *testing.T) {
	handler, _, _ := newTestHandler(t)

	body := `{"email":"missing@example.com","password":"whatever12345"}`
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/login", body)
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandler_Refresh_Success(t *testing.T) {
	handler, users, _ := newTestHandler(t)
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	loginBody := `{"email":"user@example.com","password":"supersecretpassword"}`
	loginReq := newJSONRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRec := httptest.NewRecorder()
	handler.Login(loginRec, loginReq)

	var loginOut struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(loginRec.Body).Decode(&loginOut); err != nil {
		t.Fatalf("decode login body: %v", err)
	}

	refreshBody, err := json.Marshal(map[string]string{"refresh_token": loginOut.RefreshToken})
	if err != nil {
		t.Fatalf("marshal refresh body: %v", err)
	}

	refreshReq := newJSONRequest(http.MethodPost, "/api/v1/auth/refresh", string(refreshBody))
	refreshRec := httptest.NewRecorder()
	handler.Refresh(refreshRec, refreshReq)

	if refreshRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", refreshRec.Code, http.StatusOK, refreshRec.Body.String())
	}

	var refreshOut struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(refreshRec.Body).Decode(&refreshOut); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}
	if refreshOut.RefreshToken == loginOut.RefreshToken {
		t.Fatalf("refresh token was not rotated")
	}
}

func TestHandler_Refresh_InvalidToken(t *testing.T) {
	handler, _, _ := newTestHandler(t)

	body := `{"refresh_token":"does-not-exist"}`
	req := newJSONRequest(http.MethodPost, "/api/v1/auth/refresh", body)
	rec := httptest.NewRecorder()

	handler.Refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandler_Logout_WithoutPrincipalUnauthorized(t *testing.T) {
	handler, _, _ := newTestHandler(t)

	// Calling the handler directly, bypassing AuthMiddleware, must be
	// rejected rather than proceed with a zero-value session ID.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()

	handler.Logout(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d for a request without an authenticated principal", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandler_Logout_Authenticated_NoContent(t *testing.T) {
	handler, users, tokens := newTestHandler(t)
	user := mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	sessionID := uuid.New()
	tokens.claims = application.AccessTokenClaims{UserID: user.ID, SessionID: sessionID}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()

	mw := identityhttp.NewAuthMiddleware(tokens, mustResponder(t))
	mw.Authenticate(http.HandlerFunc(handler.Logout)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestHandler_GetMe_Success(t *testing.T) {
	handler, users, tokens := newTestHandler(t)
	user := mustRegisterUser(t, users, "user@example.com", "supersecretpassword")

	claims := application.AccessTokenClaims{UserID: user.ID, SessionID: uuid.New()}
	tokens.claims = claims

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()

	mw := identityhttp.NewAuthMiddleware(tokens, mustResponder(t))
	req.Header.Set("Authorization", "Bearer anything")

	mw.Authenticate(http.HandlerFunc(handler.GetMe)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Email != "user@example.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "user@example.com")
	}
}

// newJSONRequest builds a request with a JSON Content-Type, matching
// what request.DecodeJSON now requires of every JSON endpoint.
func newJSONRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	return req
}

func decodeErrorCode(t *testing.T, body []byte) string {
	t.Helper()

	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	return out.Error.Code
}

func mustRegisterUser(t *testing.T, users *fakeUserRepository, email, password string) *domain.User {
	t.Helper()

	hash, err := (fakeHasher{}).Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	user := domain.NewUser(email, nil, &hash)

	if err := users.Create(context.Background(), user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	return user
}

func mustResponder(t *testing.T) *response.Responder {
	t.Helper()

	logger, _ := testkit.NewLogger()

	return response.NewResponder(logger)
}

// --- fakes shared by handler and middleware tests ---

type fakeUserRepository struct {
	mu    sync.Mutex
	users map[uuid.UUID]*domain.User
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[uuid.UUID]*domain.User)}
}

func (r *fakeUserRepository) Create(_ context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.users {
		if strings.EqualFold(existing.Email, user.Email) {
			return domain.ErrEmailAlreadyExists
		}
	}

	stored := *user
	r.users[user.ID] = &stored

	return nil
}

func (r *fakeUserRepository) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, user := range r.users {
		if strings.EqualFold(user.Email, email) {
			stored := *user
			return &stored, nil
		}
	}

	return nil, domain.ErrUserNotFound
}

func (r *fakeUserRepository) FindByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.users[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}

	stored := *user

	return &stored, nil
}

// fakeRegistrationStore is an in-memory application.RegistrationStore.
type fakeRegistrationStore struct {
	mu    sync.Mutex
	users map[uuid.UUID]*domain.User
}

func newFakeRegistrationStore() *fakeRegistrationStore {
	return &fakeRegistrationStore{users: make(map[uuid.UUID]*domain.User)}
}

func (s *fakeRegistrationStore) Register(
	_ context.Context,
	user *domain.User,
	_ *domain.EmailVerification,
	_ application.EmailVerificationRequestedV1,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.users {
		if strings.EqualFold(existing.Email, user.Email) {
			return domain.ErrEmailAlreadyExists
		}
	}

	stored := *user
	s.users[user.ID] = &stored

	return nil
}

type fakeSessionRepository struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*domain.Session
}

func newFakeSessionRepository() *fakeSessionRepository {
	return &fakeSessionRepository{sessions: make(map[uuid.UUID]*domain.Session)}
}

func (r *fakeSessionRepository) all() []domain.Session {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]domain.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		out = append(out, *session)
	}

	return out
}

func (r *fakeSessionRepository) Create(_ context.Context, session *domain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored := *session
	r.sessions[session.ID] = &stored

	return nil
}

func (r *fakeSessionRepository) FindByRefreshTokenHash(_ context.Context, hash string) (*domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, session := range r.sessions {
		if session.RefreshTokenHash == hash {
			stored := *session
			return &stored, nil
		}
	}

	return nil, domain.ErrSessionNotFound
}

func (r *fakeSessionRepository) FindByID(_ context.Context, id uuid.UUID) (*domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[id]
	if !ok {
		return nil, domain.ErrSessionNotFound
	}

	stored := *session

	return &stored, nil
}

func (r *fakeSessionRepository) RotateRefreshToken(
	_ context.Context,
	sessionID uuid.UUID,
	currentRefreshTokenHash string,
	newRefreshTokenHash string,
	newExpiresAt time.Time,
) (*domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[sessionID]
	if !ok {
		return nil, domain.ErrSessionNotFound
	}

	now := time.Now().UTC()

	if session.RefreshTokenHash != currentRefreshTokenHash ||
		session.IsRevoked() ||
		session.IsExpired(now) {
		return nil, domain.ErrSessionNotFound
	}

	session.RefreshTokenHash = newRefreshTokenHash
	session.ExpiresAt = newExpiresAt
	session.LastSeenAt = &now
	session.UpdatedAt = now

	stored := *session

	return &stored, nil
}

func (r *fakeSessionRepository) Revoke(_ context.Context, sessionID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}

	now := time.Now().UTC()
	session.RevokedAt = &now
	session.UpdatedAt = now

	return nil
}

// mustLogin unwraps NewLoginService, whose only error is a failed
// dummy-hash precompute (impossible with the fake hashers here).
func mustLogin(svc *application.LoginService, err error) *application.LoginService {
	if err != nil {
		panic(err)
	}
	return svc
}

// allowAllRisk / discardEvents stand in for login abuse protection in
// tests that aren't about it.
type allowAllRisk struct{}

func (allowAllRisk) Evaluate(context.Context, application.LoginAttempt) (application.RiskDecision, error) {
	return application.RiskDecision{Action: application.RiskAllow}, nil
}
func (allowAllRisk) Succeeded(context.Context, application.LoginAttempt) {}

type discardEvents struct{}

func (discardEvents) Publish(context.Context, application.SecurityEvent) {}

// countingHasher records how often password verification (Argon2 in
// production) actually ran.
type countingHasher struct {
	fakeHasher
	verifies atomic.Int64
}

func (h *countingHasher) Verify(password, encodedHash string) (bool, error) {
	h.verifies.Add(1)
	return h.fakeHasher.Verify(password, encodedHash)
}

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func (fakeHasher) Verify(password string, encodedHash string) (bool, error) {
	return encodedHash == "hashed:"+password, nil
}

// fakeTokenManager is a deterministic application.TokenManager stand-in.
// When claims is set, ParseAccessToken returns it regardless of the
// input token, letting tests drive the authenticated principal
// directly without depending on a real JWT implementation.
type fakeTokenManager struct {
	claims application.AccessTokenClaims
}

func newFakeTokenManager() *fakeTokenManager {
	return &fakeTokenManager{}
}

func (m *fakeTokenManager) GenerateAccessToken(userID, sessionID uuid.UUID) (string, time.Time, error) {
	return "access:" + userID.String() + ":" + sessionID.String(), time.Now().UTC().Add(15 * time.Minute), nil
}

func (m *fakeTokenManager) ParseAccessToken(string) (application.AccessTokenClaims, error) {
	return m.claims, nil
}

func (m *fakeTokenManager) GenerateRefreshToken() (string, error) {
	return "refresh-token-" + uuid.NewString(), nil
}

func (m *fakeTokenManager) HashRefreshToken(raw string) string {
	return "hash:" + raw
}
