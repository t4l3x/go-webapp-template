package bootstrap_test

import (
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/bootstrap"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

// These are Fx wiring smoke tests, not integration tests: fx.New builds
// the full dependency graph (running every constructor and fx.Invoke)
// during construction, but defers actual connections/listeners to
// lifecycle hooks that only run on App.Start — never called here. So
// this catches a broken or missing provider without needing
// Docker/Postgres.
//
// That class of bug is what these exist for, and it got easier to
// introduce once each process composes its own set of module adapters:
// a provider left out of (or misplaced between) identity's core, HTTP,
// and worker compositions type-checks perfectly and only fails when the
// affected binary starts — which no test that constructs dependencies
// directly can see. Each process therefore gets its own test; a
// provider that only the worker needs is not exercised by the API's.
//
// Each process is also built with only the secrets it should load, so
// these double as least-privilege checks: a provider that pulls another
// process's secret into the graph fails its build.
//
// Per this project's Fx-testing convention, ordinary unit tests
// construct dependencies directly rather than going through Fx; this
// file and internal/modules/identity/wiring_test.go are the deliberate
// exceptions, justified by exactly the kind of wiring-only bug unit
// tests structurally cannot see.

const (
	testJWTSecret               = "test-jwt-secret-that-is-at-least-32-bytes-long"
	testEmailVerificationSecret = "test-email-verification-secret-32-bytes-plus"
)

// baseConfigEnv sets what both processes need and clears every identity
// secret, so each test opts in to exactly the secrets it grants.
func baseConfigEnv(t *testing.T) {
	t.Helper()

	testkit.UnsetEnv(t, "AUTH_JWT_SECRET", "AUTH_JWT_ISSUER", "AUTH_EMAIL_VERIFICATION_SECRET")
	t.Setenv("DB_DSN", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
}

func apiSecretsEnv(t *testing.T) {
	t.Helper()

	t.Setenv("AUTH_JWT_SECRET", testJWTSecret)
	t.Setenv("AUTH_JWT_ISSUER", "go-webapp-template")
}

func workerSecretsEnv(t *testing.T) {
	t.Helper()

	t.Setenv("AUTH_EMAIL_VERIFICATION_SECRET", testEmailVerificationSecret)
}

// TestNewAPI_BuildsWithVerificationAndJWTSecrets builds the API
// (identity.CoreModule + HTTPModule) with both keys required by its endpoints.
func TestNewAPI_BuildsWithVerificationAndJWTSecrets(t *testing.T) {
	baseConfigEnv(t)
	apiSecretsEnv(t)
	workerSecretsEnv(t)

	app := bootstrap.NewAPI()
	if err := app.Err(); err != nil {
		t.Fatalf("NewAPI() failed to build the dependency graph: %v", err)
	}
}

// TestNewAPI_RequiresJWTSecret keeps the test above honest: the API
// graph really does require the API's session configuration.
func TestNewAPI_RequiresJWTSecret(t *testing.T) {
	baseConfigEnv(t)
	workerSecretsEnv(t)

	app := bootstrap.NewAPI()
	if err := app.Err(); err == nil {
		t.Fatalf("NewAPI() error = nil, want a failure without AUTH_JWT_SECRET")
	}
}

// TestNewWorker_BuildsWithoutJWTSecret builds the worker
// (identity.CoreModule + WorkerModule) with only the email-verification
// secret. The worker composes CoreModule, so this is what proves
// verification signing no longer drags in the auth token manager or its
// JWT settings.
func TestNewWorker_BuildsWithoutJWTSecret(t *testing.T) {
	baseConfigEnv(t)
	workerSecretsEnv(t)

	app := bootstrap.NewWorker()
	if err := app.Err(); err != nil {
		t.Fatalf("NewWorker() failed to build the dependency graph: %v", err)
	}
}

// TestNewWorker_RequiresEmailVerificationSecret keeps the test above
// honest: the worker graph really does build the verification signer.
func TestNewWorker_RequiresEmailVerificationSecret(t *testing.T) {
	baseConfigEnv(t)
	apiSecretsEnv(t)

	app := bootstrap.NewWorker()
	if err := app.Err(); err == nil {
		t.Fatalf("NewWorker() error = nil, want a failure without AUTH_EMAIL_VERIFICATION_SECRET")
	}
}

func TestNewAPI_RequiresEmailVerificationSecret(t *testing.T) {
	baseConfigEnv(t)
	apiSecretsEnv(t)
	if err := bootstrap.NewAPI().Err(); err == nil {
		t.Fatal("API must require verification key")
	}
}

func TestNewAPIRejectsIdenticalIdentityKeys(t *testing.T) {
	baseConfigEnv(t)
	apiSecretsEnv(t)
	t.Setenv("AUTH_EMAIL_VERIFICATION_SECRET", testJWTSecret)
	if err := bootstrap.NewAPI().Err(); err == nil {
		t.Fatal("API accepted identical signing keys")
	}
}

func TestNewWorkerRejectsUnsafeDeliveryBudget(t *testing.T) {
	baseConfigEnv(t)
	workerSecretsEnv(t)
	t.Setenv("OUTBOX_CLAIM_LEASE", "100s")
	t.Setenv("OUTBOX_BATCH_SIZE", "10")
	t.Setenv("MAIL_TIMEOUT", "10s")
	if err := bootstrap.NewWorker().Err(); err == nil {
		t.Fatal("worker accepted unsafe delivery budget")
	}
}
