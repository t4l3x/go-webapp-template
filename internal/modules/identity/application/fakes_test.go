package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

// errBoom is a generic sentinel used to inject failures into fakes.
var errBoom = errors.New("boom")

// fakeUserRepository is an in-memory application.UserRepository used to
// exercise use cases without a real database.
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
		if user.Phone != nil && existing.Phone != nil && *existing.Phone == *user.Phone {
			return domain.ErrPhoneAlreadyExists
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
// It mirrors the atomicity contract of the real Postgres
// implementation: a duplicate email/phone leaves no trace of the
// rejected attempt (no user, no verification, no event recorded), and
// setting registerErr lets tests inject an unexpected failure.
type fakeRegistrationStore struct {
	mu            sync.Mutex
	users         map[uuid.UUID]*domain.User
	verifications map[uuid.UUID]*domain.EmailVerification
	events        []application.EmailVerificationRequestedV1

	registerErr error
}

func newFakeRegistrationStore() *fakeRegistrationStore {
	return &fakeRegistrationStore{
		users:         make(map[uuid.UUID]*domain.User),
		verifications: make(map[uuid.UUID]*domain.EmailVerification),
	}
}

func (s *fakeRegistrationStore) Register(
	_ context.Context,
	user *domain.User,
	verification *domain.EmailVerification,
	event application.EmailVerificationRequestedV1,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.registerErr != nil {
		return s.registerErr
	}

	for _, existing := range s.users {
		if strings.EqualFold(existing.Email, user.Email) {
			return domain.ErrEmailAlreadyExists
		}
		if user.Phone != nil && existing.Phone != nil && *existing.Phone == *user.Phone {
			return domain.ErrPhoneAlreadyExists
		}
	}

	storedUser := *user
	s.users[user.ID] = &storedUser

	storedVerification := *verification
	s.verifications[verification.ID] = &storedVerification

	s.events = append(s.events, event)

	return nil
}

func (s *fakeRegistrationStore) findUserByEmail(email string) (*domain.User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range s.users {
		if strings.EqualFold(user.Email, email) {
			return user, true
		}
	}

	return nil, false
}

func (s *fakeRegistrationStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.users)
}

// fakeSessionRepository is an in-memory application.SessionRepository.
type fakeSessionRepository struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*domain.Session
}

func newFakeSessionRepository() *fakeSessionRepository {
	return &fakeSessionRepository{sessions: make(map[uuid.UUID]*domain.Session)}
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

// RotateRefreshToken mirrors the production compare-and-swap contract:
// the whole check-then-mutate sequence runs while holding r.mu, so of
// any number of concurrent callers presenting the same current hash,
// exactly one observes a match and wins.
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

// count reports how many sessions currently exist, for tests asserting
// that a failed use case left no orphaned session behind.
func (r *fakeSessionRepository) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.sessions)
}

func (r *fakeSessionRepository) Revoke(_ context.Context, sessionID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}

	if session.RevokedAt == nil {
		now := time.Now().UTC()
		session.RevokedAt = &now
		session.UpdatedAt = now
	}

	return nil
}

// fakeHasher is a deterministic, non-cryptographic application.PasswordHasher
// stand-in so application tests don't pay Argon2's cost.
type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func (fakeHasher) Verify(password string, encodedHash string) (bool, error) {
	return encodedHash == "hashed:"+password, nil
}

// fakeTokenManager is a deterministic application.TokenManager stand-in.
// Setting generateAccessTokenErr/generateRefreshTokenErr lets tests
// inject a failure at a specific point in a use case's flow.
type fakeTokenManager struct {
	ttl time.Duration

	generateAccessTokenErr  error
	generateRefreshTokenErr error
}

func newFakeTokenManager(ttl time.Duration) *fakeTokenManager {
	return &fakeTokenManager{ttl: ttl}
}

func (m *fakeTokenManager) GenerateAccessToken(userID, sessionID uuid.UUID) (string, time.Time, error) {
	if m.generateAccessTokenErr != nil {
		return "", time.Time{}, m.generateAccessTokenErr
	}

	expiresAt := time.Now().UTC().Add(m.ttl)

	return "access:" + userID.String() + ":" + sessionID.String(), expiresAt, nil
}

func (m *fakeTokenManager) ParseAccessToken(string) (application.AccessTokenClaims, error) {
	return application.AccessTokenClaims{}, nil
}

func (m *fakeTokenManager) GenerateRefreshToken() (string, error) {
	if m.generateRefreshTokenErr != nil {
		return "", m.generateRefreshTokenErr
	}

	return "refresh-token-" + uuid.NewString(), nil
}

func (m *fakeTokenManager) HashRefreshToken(raw string) string {
	return "hash:" + raw
}
