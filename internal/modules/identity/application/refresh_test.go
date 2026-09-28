package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

type refreshFixture struct {
	refresh  *application.RefreshService
	users    *fakeUserRepository
	sessions *fakeSessionRepository
	tokens   *fakeTokenManager
}

func newRefreshFixture(ttl time.Duration) refreshFixture {
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	tokens := newFakeTokenManager(15 * time.Minute)

	refresh := application.NewRefreshService(
		sessions,
		users,
		tokens,
		application.SessionConfig{RefreshTokenTTL: ttl},
	)

	return refreshFixture{refresh: refresh, users: users, sessions: sessions, tokens: tokens}
}

func newActiveSession(t *testing.T, sessions *fakeSessionRepository, tokens *fakeTokenManager, userID uuid.UUID, ttl time.Duration) (*domain.Session, string) {
	t.Helper()

	rawToken, err := tokens.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	session := domain.NewSession(
		userID,
		tokens.HashRefreshToken(rawToken),
		time.Now().UTC().Add(ttl),
		nil,
		nil,
	)

	if err := sessions.Create(context.Background(), session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	return session, rawToken
}

func TestRefreshService_Refresh_Success(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")
	session, rawToken := newActiveSession(t, fx.sessions, fx.tokens, user.ID, time.Hour)

	out, err := fx.refresh.Refresh(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if out.AccessToken == "" {
		t.Fatalf("AccessToken is empty")
	}
	if out.RefreshToken == "" || out.RefreshToken == rawToken {
		t.Fatalf("RefreshToken = %q, want a newly rotated token", out.RefreshToken)
	}

	rotated, err := fx.sessions.FindByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if rotated.RefreshTokenHash == fx.tokens.HashRefreshToken(rawToken) {
		t.Fatalf("session refresh token hash was not rotated")
	}
	if rotated.LastSeenAt == nil {
		t.Fatalf("expected LastSeenAt to be set after refresh")
	}
}

func TestRefreshService_Refresh_UnknownToken(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)

	_, err := fx.refresh.Refresh(context.Background(), "does-not-exist")

	assertInvalidRefreshToken(t, err)
}

func TestRefreshService_Refresh_RevokedSession(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")
	session, rawToken := newActiveSession(t, fx.sessions, fx.tokens, user.ID, time.Hour)

	if err := fx.sessions.Revoke(context.Background(), session.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	_, err := fx.refresh.Refresh(context.Background(), rawToken)

	assertInvalidRefreshToken(t, err)
}

func TestRefreshService_Refresh_ExpiredSession(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")
	_, rawToken := newActiveSession(t, fx.sessions, fx.tokens, user.ID, -time.Hour)

	_, err := fx.refresh.Refresh(context.Background(), rawToken)

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Code != "session_expired" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "session_expired")
	}
}

func TestRefreshService_Refresh_DisabledUser(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")
	_, rawToken := newActiveSession(t, fx.sessions, fx.tokens, user.ID, time.Hour)

	fx.users.mu.Lock()
	fx.users.users[user.ID].Status = domain.UserStatusDisabled
	fx.users.mu.Unlock()

	_, err := fx.refresh.Refresh(context.Background(), rawToken)

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindForbidden {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindForbidden)
	}
	if appErr.Code != "account_disabled" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "account_disabled")
	}

	// A disabled user must not still be able to mint a fresh access
	// token by never refreshing (i.e. the old token must not have been
	// rotated as a side effect of the rejected attempt).
	if _, err := fx.sessions.FindByRefreshTokenHash(context.Background(), fx.tokens.HashRefreshToken(rawToken)); err != nil {
		t.Fatalf("expected the original session to remain in place, FindByRefreshTokenHash() error = %v", err)
	}
}

func TestRefreshService_Refresh_RotatesToken(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")
	_, rawToken := newActiveSession(t, fx.sessions, fx.tokens, user.ID, time.Hour)

	first, err := fx.refresh.Refresh(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}

	// The old (already rotated) token must no longer work.
	if _, err := fx.refresh.Refresh(context.Background(), rawToken); err == nil {
		t.Fatalf("expected error reusing a rotated refresh token")
	}

	// The newly issued token must work.
	if _, err := fx.refresh.Refresh(context.Background(), first.RefreshToken); err != nil {
		t.Fatalf("Refresh() with rotated token error = %v", err)
	}
}

func TestRefreshService_Refresh_AccessTokenFailureLeavesOldTokenUsable(t *testing.T) {
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	tokens := newFakeTokenManager(15 * time.Minute)

	refresh := application.NewRefreshService(sessions, users, tokens, application.SessionConfig{RefreshTokenTTL: time.Hour})

	user := mustRegisterUser(t, users, "user@example.com", "supersecretpassword")
	_, rawToken := newActiveSession(t, sessions, tokens, user.ID, time.Hour)

	tokens.generateAccessTokenErr = errBoom

	if _, err := refresh.Refresh(context.Background(), rawToken); err == nil {
		t.Fatalf("Refresh() error = nil, want error")
	}

	// Rotation must not have happened: the original token must still work.
	tokens.generateAccessTokenErr = nil

	if _, err := refresh.Refresh(context.Background(), rawToken); err != nil {
		t.Fatalf("Refresh() with the original token after a prior failure error = %v, want success", err)
	}
}

func TestRefreshService_Refresh_RefreshTokenFailureLeavesOldTokenUsable(t *testing.T) {
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	tokens := newFakeTokenManager(15 * time.Minute)

	refresh := application.NewRefreshService(sessions, users, tokens, application.SessionConfig{RefreshTokenTTL: time.Hour})

	user := mustRegisterUser(t, users, "user@example.com", "supersecretpassword")
	_, rawToken := newActiveSession(t, sessions, tokens, user.ID, time.Hour)

	tokens.generateRefreshTokenErr = errBoom

	if _, err := refresh.Refresh(context.Background(), rawToken); err == nil {
		t.Fatalf("Refresh() error = nil, want error")
	}

	tokens.generateRefreshTokenErr = nil

	if _, err := refresh.Refresh(context.Background(), rawToken); err != nil {
		t.Fatalf("Refresh() with the original token after a prior failure error = %v, want success", err)
	}
}

// TestRefreshService_Refresh_ConcurrentSameTokenOnlyOneSucceeds exercises
// the compare-and-swap contract under real goroutine concurrency (not
// just sequential calls): of two requests racing with the same refresh
// token, exactly one must succeed and the other must fail with
// invalid_refresh_token. Synchronization uses a barrier channel, never
// a sleep.
func TestRefreshService_Refresh_ConcurrentSameTokenOnlyOneSucceeds(t *testing.T) {
	fx := newRefreshFixture(720 * time.Hour)
	user := mustRegisterUser(t, fx.users, "user@example.com", "supersecretpassword")
	_, rawToken := newActiveSession(t, fx.sessions, fx.tokens, user.ID, time.Hour)

	const attempts = 2

	start := make(chan struct{})
	results := make(chan error, attempts)

	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := fx.refresh.Refresh(context.Background(), rawToken)
			results <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	successCount, failureCount := 0, 0
	for err := range results {
		if err == nil {
			successCount++
			continue
		}
		assertInvalidRefreshToken(t, err)
		failureCount++
	}

	if successCount != 1 || failureCount != 1 {
		t.Fatalf("successCount = %d, failureCount = %d, want exactly one of each", successCount, failureCount)
	}
}

func assertInvalidRefreshToken(t *testing.T, err error) {
	t.Helper()

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T", err)
	}
	if appErr.Kind != apperror.KindUnauthorized {
		t.Fatalf("Kind = %v, want %v", appErr.Kind, apperror.KindUnauthorized)
	}
	if appErr.Code != "invalid_refresh_token" {
		t.Fatalf("Code = %q, want %q", appErr.Code, "invalid_refresh_token")
	}
}
