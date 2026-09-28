package testkit

import "testing"

// TestIsSafeTestDatabaseName runs under plain `go test ./...` (no build
// tag), independent of any real database, so the destructive-operation
// safety guard behind NewPostgresTestDB is verified even when
// Docker/Postgres isn't available.
func TestIsSafeTestDatabaseName(t *testing.T) {
	const prefix = "app_test_example_"

	tests := []struct {
		name     string
		dbName   string
		wantSafe bool
	}{
		{"valid_generated_name", prefix + "1234567890", true},
		{"missing_prefix", "some_other_database", false},
		{"empty", "", false},
		{"postgres_admin_db", "postgres", false},
		{"postgres_admin_db_uppercase", "POSTGRES", false},
		{"template0", "template0", false},
		{"template1", "template1", false},
		{"prefix_with_dangerous_looking_suffix_is_still_safe", prefix + "postgres", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSafeTestDatabaseName(prefix, tc.dbName); got != tc.wantSafe {
				t.Fatalf("isSafeTestDatabaseName(%q, %q) = %v, want %v", prefix, tc.dbName, got, tc.wantSafe)
			}
		})
	}
}

func TestIsSafeTestDatabaseName_EmptyPrefixNeverSafe(t *testing.T) {
	if isSafeTestDatabaseName("", "anything") {
		t.Fatalf("isSafeTestDatabaseName with an empty prefix must never be safe")
	}
}
