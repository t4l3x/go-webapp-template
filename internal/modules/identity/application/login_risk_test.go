package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/apperror"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/application"
	"github.com/t4l3x/go-webapp-template/internal/modules/identity/domain"
)

// memoryFailureCounter mirrors the Redis adapter's contract (atomic
// reserve, delete on reset) in memory; the adapter itself is covered by
// the integration suite.
type memoryFailureCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func newMemoryFailureCounter() *memoryFailureCounter {
	return &memoryFailureCounter{counts: map[string]int{}}
}

func subject(email string, ip *string) string {
	if ip == nil {
		return email + "|unknown"
	}
	return email + "|" + *ip
}

func (c *memoryFailureCounter) Reserve(_ context.Context, email string, ip *string, window time.Duration) (int, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[subject(email, ip)]++
	return c.counts[subject(email, ip)], window
}

func (c *memoryFailureCounter) Reset(_ context.Context, email string, ip *string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.counts, subject(email, ip))
}

func ptr(s string) *string { return &s }

var failurePolicy = application.LoginFailureConfig{MaxFailures: 5, Window: 15 * time.Minute}

func evaluate(t *testing.T, rule *application.AccountIPFailureRule, email, ip string) application.RiskDecision {
	t.Helper()
	d, err := rule.Evaluate(context.Background(), application.LoginAttempt{Email: email, IPAddress: ptr(ip)})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	return d
}

func TestAccountIPFailureRule_ThrottlesOnlyTheFailingIP(t *testing.T) {
	rule := application.NewAccountIPFailureRule(newMemoryFailureCounter(), failurePolicy)

	for i := range failurePolicy.MaxFailures {
		if d := evaluate(t, rule, "user@example.com", "198.51.100.7"); d.Action != application.RiskAllow {
			t.Fatalf("attempt %d denied, want allowed up to the threshold", i+1)
		}
	}

	d := evaluate(t, rule, "user@example.com", "198.51.100.7")
	if d.Action != application.RiskDeny || d.RetryAfter != failurePolicy.Window {
		t.Fatalf("attempt past the threshold = %+v, want deny with the window's remaining time", d)
	}

	// The real user on their own IP is unaffected: no lockout.
	if d := evaluate(t, rule, "user@example.com", "203.0.113.9"); d.Action != application.RiskAllow {
		t.Fatalf("another IP for the same account was throttled")
	}
	// Nor is another account from the throttled IP.
	if d := evaluate(t, rule, "other@example.com", "198.51.100.7"); d.Action != application.RiskAllow {
		t.Fatalf("another account from the same IP was throttled")
	}
}

// Distributed guessing from many IPs does not share an account+IP bucket
// (account-wide protection is future work, by design not a hard block).
func TestAccountIPFailureRule_DistributedIPsDoNotShareBucket(t *testing.T) {
	rule := application.NewAccountIPFailureRule(newMemoryFailureCounter(), failurePolicy)

	for i := range 50 {
		if d := evaluate(t, rule, "user@example.com", fmt.Sprintf("198.51.100.%d", i)); d.Action != application.RiskAllow {
			t.Fatalf("IP #%d was throttled by other IPs' failures", i)
		}
	}
}

func TestAccountIPFailureRule_SuccessResets(t *testing.T) {
	rule := application.NewAccountIPFailureRule(newMemoryFailureCounter(), failurePolicy)
	attempt := application.LoginAttempt{Email: "user@example.com", IPAddress: ptr("198.51.100.7")}

	for range failurePolicy.MaxFailures - 1 {
		evaluate(t, rule, attempt.Email, *attempt.IPAddress)
	}
	rule.Succeeded(context.Background(), attempt)

	for i := range failurePolicy.MaxFailures {
		if d := evaluate(t, rule, attempt.Email, *attempt.IPAddress); d.Action != application.RiskAllow {
			t.Fatalf("attempt %d after a successful login denied; the success must reset the count", i+1)
		}
	}
}

// Every attempt reserves before the password is checked, so a burst of
// concurrent guesses gets exactly MaxFailures through.
func TestAccountIPFailureRule_ConcurrencyCannotBypassThreshold(t *testing.T) {
	rule := application.NewAccountIPFailureRule(newMemoryFailureCounter(), failurePolicy)

	var allowed atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			<-start
			d, err := rule.Evaluate(context.Background(), application.LoginAttempt{Email: "user@example.com", IPAddress: ptr("198.51.100.7")})
			if err == nil && d.Action == application.RiskAllow {
				allowed.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != int64(failurePolicy.MaxFailures) {
		t.Fatalf("allowed %d concurrent attempts, want exactly %d", got, failurePolicy.MaxFailures)
	}
}

// --- LoginService with the rule ---

type countingHasher struct {
	fakeHasher
	verifies atomic.Int64
	hashes   atomic.Int64
}

func (h *countingHasher) Hash(password string) (string, error) {
	h.hashes.Add(1)
	return h.fakeHasher.Hash(password)
}

func (h *countingHasher) Verify(password, encodedHash string) (bool, error) {
	h.verifies.Add(1)
	return h.fakeHasher.Verify(password, encodedHash)
}

type recordingEvents struct {
	mu     sync.Mutex
	events []application.SecurityEvent
}

func (r *recordingEvents) Publish(_ context.Context, e application.SecurityEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func TestLoginService_ThrottledBeforePasswordHashingAfterFailures(t *testing.T) {
	users := newFakeUserRepository()
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")
	hasher := &countingHasher{}
	events := &recordingEvents{}
	login := mustLogin(application.NewLoginService(users, newFakeSessionRepository(), hasher, newFakeTokenManager(15*time.Minute),
		application.NewAccountIPFailureRule(newMemoryFailureCounter(), failurePolicy), events,
		application.SessionConfig{RefreshTokenTTL: time.Hour}))

	wrong := application.LoginInput{Email: "User@Example.com", Password: "wrongpassword", IPAddress: ptr("198.51.100.7")}
	for range failurePolicy.MaxFailures {
		if _, err := login.Login(context.Background(), wrong); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
	verifiesBefore := hasher.verifies.Load()

	// Even the correct password is refused from this IP now — without
	// spending a hash on it.
	right := wrong
	right.Password = "supersecretpassword"
	_, err := login.Login(context.Background(), right)

	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperror.KindTooManyRequests || appErr.Code != "rate_limit_exceeded" {
		t.Fatalf("error = %v, want too_many_requests/rate_limit_exceeded (indistinguishable from the IP limit)", err)
	}
	var retry interface{ RetryAfter() time.Duration }
	if !errors.As(err, &retry) || retry.RetryAfter() <= 0 {
		t.Fatalf("throttled error does not carry a retry delay: %v", err)
	}
	if hasher.verifies.Load() != verifiesBefore {
		t.Fatal("password was verified for a throttled attempt")
	}

	// The owner from another IP still gets in: no lockout.
	right.IPAddress = ptr("203.0.113.9")
	if _, err := login.Login(context.Background(), right); err != nil {
		t.Fatalf("login from another IP = %v, want success", err)
	}

	var failed, throttled int
	for _, e := range events.events {
		switch e.Type {
		case application.SecurityEventLoginFailed:
			failed++
			if e.Reason != "wrong_password" || e.UserID == nil {
				t.Fatalf("failed event = %+v, want wrong_password with a user id", e)
			}
		case application.SecurityEventLoginThrottled:
			throttled++
		}
	}
	if failed != failurePolicy.MaxFailures || throttled != 1 {
		t.Fatalf("events: failed=%d throttled=%d, want %d and 1", failed, throttled, failurePolicy.MaxFailures)
	}
}

// TestLoginService_EveryAccountPathCostsOnePasswordCheck: unknown
// address, password-less account, wrong password and disabled account
// (without its password) all do exactly one password verification and
// return the same invalid_credentials — no response or timing difference
// that would reveal whether an address is registered, and no cheaper
// path for guessing unknown addresses.
func TestLoginService_EveryAccountPathCostsOnePasswordCheck(t *testing.T) {
	users := newFakeUserRepository()
	mustRegisterUser(t, users, "user@example.com", "supersecretpassword")
	disabled := mustRegisterUser(t, users, "disabled@example.com", "supersecretpassword")
	users.mu.Lock()
	users.users[disabled.ID].Status = domain.UserStatusDisabled
	users.mu.Unlock()
	if err := users.Create(context.Background(), domain.NewUser("oauth@example.com", nil, nil)); err != nil {
		t.Fatal(err)
	}

	hasher := &countingHasher{}
	login := mustLogin(application.NewLoginService(users, newFakeSessionRepository(), hasher, newFakeTokenManager(15*time.Minute),
		allowAllRisk{}, discardEvents{}, application.SessionConfig{RefreshTokenTTL: time.Hour}))

	for name, email := range map[string]string{
		"unknown account":            "missing@example.com",
		"account without a password": "oauth@example.com",
		"wrong password":             "user@example.com",
		"disabled, without password": "disabled@example.com",
	} {
		t.Run(name, func(t *testing.T) {
			before := hasher.verifies.Load()
			_, err := login.Login(context.Background(), application.LoginInput{Email: email, Password: "not-the-password"})

			assertInvalidCredentials(t, err)
			if n := hasher.verifies.Load() - before; n != 1 {
				t.Fatalf("password verifications = %d, want exactly 1", n)
			}
		})
	}

	// The dummy hash is made once, at construction — never per request,
	// which would cost a hash generation on top of the verification.
	if n := hasher.hashes.Load(); n != 1 {
		t.Fatalf("Hash called %d times, want exactly 1 (precomputed at construction)", n)
	}

	// Disabled is only revealed to someone who knows the password.
	_, err := login.Login(context.Background(), application.LoginInput{Email: "disabled@example.com", Password: "supersecretpassword"})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != "account_disabled" {
		t.Fatalf("disabled account with the right password = %v, want account_disabled", err)
	}
}

func TestLoginService_UnknownAccountCountsAndPublishesWithoutUserID(t *testing.T) {
	events := &recordingEvents{}
	login := mustLogin(application.NewLoginService(newFakeUserRepository(), newFakeSessionRepository(), fakeHasher{}, newFakeTokenManager(15*time.Minute),
		application.NewAccountIPFailureRule(newMemoryFailureCounter(), failurePolicy), events,
		application.SessionConfig{RefreshTokenTTL: time.Hour}))

	in := application.LoginInput{Email: "missing@example.com", Password: "whatever12345", IPAddress: ptr("198.51.100.7")}
	for range failurePolicy.MaxFailures {
		assertInvalidCredentials(t, func() error { _, err := login.Login(context.Background(), in); return err }())
	}
	if _, err := login.Login(context.Background(), in); err == nil {
		t.Fatal("unknown account attempts past the threshold were not throttled")
	}
	if e := events.events[0]; e.Reason != "unknown_account" || e.UserID != nil {
		t.Fatalf("event = %+v, want unknown_account without a user id", e)
	}
}
