package httpserver_test

import (
	"net/http"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/httpserver"
)

func TestRouteConstructors_SetMethodAndPath(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name       string
		route      httpserver.Route
		wantMethod string
	}{
		{"get", httpserver.GET("/example", handler), http.MethodGet},
		{"post", httpserver.POST("/example", handler), http.MethodPost},
		{"put", httpserver.PUT("/example", handler), http.MethodPut},
		{"patch", httpserver.PATCH("/example", handler), http.MethodPatch},
		{"delete", httpserver.DELETE("/example", handler), http.MethodDelete},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.route.Method != tc.wantMethod {
				t.Fatalf("Method = %q, want %q", tc.route.Method, tc.wantMethod)
			}
			if tc.route.Path != "/example" {
				t.Fatalf("Path = %q, want %q", tc.route.Path, "/example")
			}
			if tc.route.Handler == nil {
				t.Fatalf("Handler = nil, want the provided handler")
			}
		})
	}
}

func TestPrefix_JoinsPathsAndPreservesMethodAndHandler(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	routes, err := httpserver.Prefix(
		"/api/v1",
		httpserver.POST("/resource", handler),
		httpserver.GET("/resource/{id}", handler),
	)
	if err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}

	if routes[0].Path != "/api/v1/resource" {
		t.Fatalf("routes[0].Path = %q, want %q", routes[0].Path, "/api/v1/resource")
	}
	if routes[0].Method != http.MethodPost {
		t.Fatalf("routes[0].Method = %q, want %q", routes[0].Method, http.MethodPost)
	}
	if routes[0].Handler == nil {
		t.Fatalf("routes[0].Handler = nil, want the provided handler")
	}

	if routes[1].Path != "/api/v1/resource/{id}" {
		t.Fatalf("routes[1].Path = %q, want %q", routes[1].Path, "/api/v1/resource/{id}")
	}
	if routes[1].Method != http.MethodGet {
		t.Fatalf("routes[1].Method = %q, want %q", routes[1].Method, http.MethodGet)
	}
}

// TestPrefix_DoesNotMutateInputRoutes guards the property that makes
// Prefix safe to apply to a slice someone else owns, or to apply twice:
// it rewrites copies, never the routes handed to it.
func TestPrefix_DoesNotMutateInputRoutes(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	original := []httpserver.Route{httpserver.GET("/resource", handler)}

	if _, err := httpserver.Prefix("/api/v1", original...); err != nil {
		t.Fatalf("Prefix() error = %v", err)
	}

	if original[0].Path != "/resource" {
		t.Fatalf("input route Path = %q, want it left as %q", original[0].Path, "/resource")
	}
}

func TestPrefix_RejectsInvalidPrefixOrPath(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name   string
		prefix string
		path   string
	}{
		// Without the leading slash, concatenation yields a path the
		// router would reject anyway — caught here, where the message
		// can name the prefix.
		{"prefix_missing_leading_slash", "api/v1", "/resource"},
		{"prefix_empty", "", "/resource"},
		// These two are the silent failures: "/api/v1//resource" and
		// "/api/v1resource" both look like valid paths to the router but
		// can never match the intended URL.
		{"prefix_trailing_slash", "/api/v1/", "/resource"},
		{"path_missing_leading_slash", "/api/v1", "resource"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			routes, err := httpserver.Prefix(tc.prefix, httpserver.GET(tc.path, handler))
			if err == nil {
				t.Fatalf("Prefix(%q, %q) = %v, want an error", tc.prefix, tc.path, routes)
			}
		})
	}
}
