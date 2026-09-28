package httpserver

import (
	"fmt"
	"net/http"
	"strings"
)

// Route pairs an HTTP method and path with its handler. Method and Path
// stay separate concepts here — only the router itself (see router.go)
// knows how to fold them into net/http.ServeMux's "METHOD /path" pattern
// string. Feature modules construct routes through GET/POST/PUT/PATCH/
// DELETE below instead of building that string themselves.
type Route struct {
	Method  string
	Path    string
	Handler http.Handler
}

func GET(path string, handler http.Handler) Route {
	return Route{Method: http.MethodGet, Path: path, Handler: handler}
}

func POST(path string, handler http.Handler) Route {
	return Route{Method: http.MethodPost, Path: path, Handler: handler}
}

func PUT(path string, handler http.Handler) Route {
	return Route{Method: http.MethodPut, Path: path, Handler: handler}
}

func PATCH(path string, handler http.Handler) Route {
	return Route{Method: http.MethodPatch, Path: path, Handler: handler}
}

func DELETE(path string, handler http.Handler) Route {
	return Route{Method: http.MethodDelete, Path: path, Handler: handler}
}

// Prefix returns copies of routes with prefix prepended to each path,
// so a module states its mount point once instead of repeating it on
// every line — notably the `/api/v{major}` prefix every public business
// endpoint carries (see the API versioning policy in
// docs/conventions/backend_conventions.md). Operational endpoints
// (/health) are registered by the router itself and never go through
// here, which is what keeps them unversioned.
//
// The input routes are never modified: range yields a copy of each
// Route, and only that copy's Path is rewritten, so a caller's slice —
// or a shared package-level one — cannot be altered by prefixing it.
//
// It returns an error rather than panicking, for the same reason the
// router validates instead of panicking: a bad mount point should fail
// application startup with a message naming it. The three rejected
// cases are exactly those where plain concatenation would otherwise
// yield a path that silently never matches ("/api/v1//auth/login" from
// a trailing slash, "/api/v1auth/login" from a relative sub-path).
func Prefix(prefix string, routes ...Route) ([]Route, error) {
	if !strings.HasPrefix(prefix, "/") {
		return nil, fmt.Errorf("httpserver: route prefix %q must start with %q", prefix, "/")
	}

	if strings.HasSuffix(prefix, "/") {
		return nil, fmt.Errorf("httpserver: route prefix %q must not end with %q", prefix, "/")
	}

	prefixed := make([]Route, 0, len(routes))

	for _, route := range routes {
		if !strings.HasPrefix(route.Path, "/") {
			return nil, fmt.Errorf(
				"httpserver: route path %q under prefix %q must start with %q", route.Path, prefix, "/")
		}

		route.Path = prefix + route.Path

		prefixed = append(prefixed, route)
	}

	return prefixed, nil
}

// pattern renders the route in net/http.ServeMux's own pattern syntax.
// It is unexported: nothing outside this package may depend on this
// representation.
func (r Route) pattern() string {
	return r.Method + " " + r.Path
}
