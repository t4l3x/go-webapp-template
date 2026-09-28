// Package translations holds identity's own message catalogs — the
// human-facing wording this module sends, in every locale it supports.
//
// It lives inside the module rather than in a global catalog so that
// wording is owned by the team that owns the feature, and so a future
// billing or simulation module adds its own catalog without touching
// identity's. The platform localization engine collects catalogs
// through Fx; it never imports this package (see identity's
// WorkerModule for the registration).
//
// The files are embedded, so a deployed worker binary carries its own
// translations and never depends on the process's working directory.
package translations

import "embed"

// Files holds every "<locale>.toml" catalog in this directory. Adding a
// locale is adding a file here — no code, no configuration, no
// environment variable.
//
//go:embed *.toml
var Files embed.FS
