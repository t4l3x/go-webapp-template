//go:build integration

package postgres_test

// identityTestDBPrefix is the required prefix for any disposable
// database this package's integration tests create via
// testkit.NewPostgresTestDB. The safety guard behind that prefix lives
// in internal/testkit (verified there by its own non-integration-
// tagged unit test), not duplicated here.
const identityTestDBPrefix = "app_test_identity_"
