package testkit

import (
	"os"
	"testing"
)

// UnsetEnv clears the given environment variables for the duration of the
// test, restoring their original values (if any) on cleanup. Use it before
// asserting on config defaults so tests don't depend on the developer's
// real shell environment.
func UnsetEnv(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}

		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset env %s: %v", key, err)
		}

		t.Cleanup(func() {
			_ = os.Setenv(key, value)
		})
	}
}
