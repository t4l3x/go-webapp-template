// Package localization renders backend-owned, human-facing text in a
// requested locale. It exists for content this backend authors and
// delivers itself — email subjects and bodies today, SMS or push
// notifications later — not for the REST API's responses.
//
// It is deliberately not a translation layer for the HTTP surface. API
// errors carry a stable machine-readable code (invalid_credentials,
// invalid_email); a client maps that code to its own localized text.
// Nothing here knows about HTTP, Accept-Language, users, email, or any
// business module.
//
// Wording is owned by the module that writes it: modules embed their
// own message catalogs and contribute them as a Catalog (see the
// "localization_catalogs" Fx group in module.go). This package owns
// only the mechanics — parsing, BCP 47 matching, fallback, templating.
package localization

import (
	"errors"
	"io/fs"
)

var (
	// ErrMessageNotFound reports that a message ID resolved to nothing
	// in the requested locale and nothing in the default locale either.
	// It is deliberately an error rather than an empty string: a blank
	// subject or body is worse than a failed delivery, because outbox
	// retry/alerting can see a failure but cannot see a sent-but-empty
	// email.
	ErrMessageNotFound = errors.New("localization: message not found")

	// ErrEmptyMessage reports that a message was found but rendered to
	// nothing — an empty catalog entry, or a template that produced no
	// output. Same reasoning as above: never hand an empty string back
	// to a caller that is about to mail it.
	ErrEmptyMessage = errors.New("localization: message rendered empty")
)

// Message names a message to render and the data it interpolates.
type Message struct {
	// ID is a stable, semantic identifier following
	// <module>.<feature>.<message> — e.g.
	// "identity.email_verification.subject". It is a contract: it stays
	// put when the English wording changes, which is the whole reason
	// message IDs are not English sentences.
	ID string

	// Data supplies the template variables a catalog entry references
	// (e.g. {{.VerificationURL}}). Keys must match what the catalogs
	// use; a reference to a key that isn't here fails the render rather
	// than rendering blank.
	Data map[string]any

	// PluralCount selects which plural form to use, for messages that
	// declare one. Leave nil for a message with a single form.
	PluralCount any
}

// Localizer renders one message in the closest available locale.
//
// locale is a BCP 47 language tag ("en", "lv-LV"). Resolution is
// deterministic: an exact catalog match wins, then the base language of
// a regional tag, then the configured default locale. An empty,
// unknown, or malformed locale is not an error — it resolves to the
// default — because not knowing a recipient's language is a normal
// state, not a failure.
//
// What is an error is having nothing to say: if neither the requested
// nor the default locale yields usable text, Translate returns an error
// rather than an empty string.
type Localizer interface {
	Translate(locale string, msg Message) (string, error)
}

// Catalog is one module's contribution of translation files. A module
// embeds its own catalog and provides it into the
// "localization_catalogs" Fx group; the engine collects every
// contribution at startup.
//
// This is how wording stays owned by the module that writes it, without
// this package importing a single business module, and without a global
// catalog every module has to edit.
type Catalog struct {
	// Owner names the contributing module ("identity"). It carries no
	// behavior — it appears in startup errors so a malformed or
	// conflicting catalog names its owner instead of just a file path.
	Owner string

	// Files holds the module's "<locale>.toml" message files. It is an
	// fs.FS so a module can embed them with //go:embed: a deployed
	// binary then carries its own catalogs and never depends on the
	// working directory being right.
	Files fs.FS
}
