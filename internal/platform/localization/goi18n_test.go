package localization_test

import (
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
)

// These test our own behavior — the fallback chain, startup validation,
// and the never-return-empty rule — not go-i18n's parsing or plural
// internals, which are its own project's concern.

const (
	enCatalog = `
[example.greeting]
other = "Hello"

[example.welcome]
other = "Welcome, {{.Name}}"

[example.items]
one = "{{.PluralCount}} item"
other = "{{.PluralCount}} items"
`

	lvCatalog = `
[example.greeting]
other = "Sveiki"
`
)

func TestEngine_Translate_DefaultLocale(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got, err := engine.Translate("", localization.Message{ID: "example.greeting"})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "Hello" {
		t.Fatalf("Translate() = %q, want %q", got, "Hello")
	}
}

func TestEngine_Translate_ExactLocaleMatch(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got, err := engine.Translate("lv", localization.Message{ID: "example.greeting"})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "Sveiki" {
		t.Fatalf("Translate() = %q, want %q", got, "Sveiki")
	}
}

// TestEngine_Translate_RegionalFallsBackToBaseLanguage covers the
// middle link of the chain: a regional tag with no catalog of its own
// resolves to its base language rather than skipping straight to the
// default.
func TestEngine_Translate_RegionalFallsBackToBaseLanguage(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got, err := engine.Translate("lv-LV", localization.Message{ID: "example.greeting"})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "Sveiki" {
		t.Fatalf("Translate(%q) = %q, want the base-language catalog's %q", "lv-LV", got, "Sveiki")
	}
}

func TestEngine_Translate_UnsupportedLocaleFallsBackToDefault(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	tests := []struct {
		name   string
		locale string
	}{
		{"unsupported_language", "de"},
		{"unsupported_region", "de-AT"},
		{"malformed", "not-a-locale"},
		{"empty", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.Translate(tc.locale, localization.Message{ID: "example.greeting"})
			if err != nil {
				t.Fatalf("Translate(%q) error = %v, want a fallback rather than a failure", tc.locale, err)
			}
			if got != "Hello" {
				t.Fatalf("Translate(%q) = %q, want the default locale's %q", tc.locale, got, "Hello")
			}
		})
	}
}

// TestEngine_Translate_SupportedLocaleMissingOneMessage covers the
// partial-catalog case: lv exists but doesn't define this message, so
// the default locale supplies it. go-i18n reports that as an error
// alongside the usable text; treating it as a failure here would make
// every partially translated catalog unusable.
func TestEngine_Translate_SupportedLocaleMissingOneMessage(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got, err := engine.Translate("lv", localization.Message{ID: "example.welcome", Data: map[string]any{"Name": "Ada"}})
	if err != nil {
		t.Fatalf("Translate() error = %v, want a default-locale fallback", err)
	}
	if got != "Welcome, Ada" {
		t.Fatalf("Translate() = %q, want the default locale's %q", got, "Welcome, Ada")
	}
}

func TestEngine_Translate_TemplateData(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got, err := engine.Translate("en", localization.Message{
		ID:   "example.welcome",
		Data: map[string]any{"Name": "Ada"},
	})
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "Welcome, Ada" {
		t.Fatalf("Translate() = %q, want %q", got, "Welcome, Ada")
	}
}

func TestEngine_Translate_PluralCount(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	tests := []struct {
		name  string
		count int
		want  string
	}{
		{"singular", 1, "1 item"},
		{"plural", 3, "3 items"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.Translate("en", localization.Message{
				ID:          "example.items",
				PluralCount: tc.count,
			})
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Translate() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEngine_Translate_MissingMessageIsAnError is the rule that keeps a
// blank email from ever being sent: nothing anywhere defines this ID,
// so there is no usable text and the caller must find out.
func TestEngine_Translate_MissingMessageIsAnError(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got, err := engine.Translate("en", localization.Message{ID: "example.nonexistent"})
	if !errors.Is(err, localization.ErrMessageNotFound) {
		t.Fatalf("Translate() error = %v, want %v", err, localization.ErrMessageNotFound)
	}
	if got != "" {
		t.Fatalf("Translate() = %q, want empty alongside the error", got)
	}
}

func TestEngine_Translate_EmptyMessageIDIsAnError(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	if _, err := engine.Translate("en", localization.Message{}); err == nil {
		t.Fatalf("Translate() error = nil, want an error for an empty message ID")
	}
}

func TestEngine_Translate_MissingTemplateVariableIsAnError(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	// example.welcome references {{.Name}}; supplying nothing must fail
	// rather than render "Welcome, " with a hole in it.
	if _, err := engine.Translate("en", localization.Message{ID: "example.welcome"}); err == nil {
		t.Fatalf("Translate() error = nil, want an error when a template variable is missing")
	}
}

func TestEngine_SupportedLocales(t *testing.T) {
	engine := newEngine(t, "en", catalog(enCatalog, lvCatalog))

	got := engine.SupportedLocales()

	if len(got) != 2 || got[0] != "en" || got[1] != "lv" {
		t.Fatalf("SupportedLocales() = %v, want [en lv]", got)
	}
}

func TestNewEngine_RejectsMalformedCatalog(t *testing.T) {
	_, err := buildEngine(t, "en", localization.Catalog{
		Owner: "broken",
		Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte("this is not = valid = toml")}},
	})
	if err == nil {
		t.Fatalf("NewEngine() error = nil, want a startup failure for a malformed catalog")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error = %v, want it to name the owning catalog", err)
	}
}

// TestNewEngine_RejectsDuplicateMessage matters because go-i18n itself
// does not: its bundle silently overwrites a repeated message ID, so
// one module's wording would vanish with no signal at all.
func TestNewEngine_RejectsDuplicateMessage(t *testing.T) {
	_, err := buildEngine(t, "en",
		localization.Catalog{
			Owner: "first",
			Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte(enCatalog)}},
		},
		localization.Catalog{
			Owner: "second",
			Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte(enCatalog)}},
		},
	)
	if err == nil {
		t.Fatalf("NewEngine() error = nil, want a startup failure for a duplicate message")
	}
	for _, want := range []string{"example.greeting", "first", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %q", err, want)
		}
	}
}

// TestNewEngine_DuplicateErrorIsDeterministic guards against an error
// that names a different message on each restart: go-i18n hands back
// messages in map order, so the engine sorts before reporting.
func TestNewEngine_DuplicateErrorIsDeterministic(t *testing.T) {
	var first string

	for range 20 {
		_, err := buildEngine(t, "en",
			localization.Catalog{
				Owner: "a",
				Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte(enCatalog)}},
			},
			localization.Catalog{
				Owner: "b",
				Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte(enCatalog)}},
			},
		)
		if err == nil {
			t.Fatalf("NewEngine() error = nil, want a duplicate failure")
		}

		if first == "" {
			first = err.Error()

			continue
		}

		if err.Error() != first {
			t.Fatalf("duplicate error varies between runs:\nfirst: %s\nthen:  %s", first, err)
		}
	}
}

func TestNewEngine_RejectsCatalogWithoutDefaultLocale(t *testing.T) {
	_, err := buildEngine(t, "en", localization.Catalog{
		Owner: "lv-only",
		Files: fstest.MapFS{"lv.toml": &fstest.MapFile{Data: []byte(lvCatalog)}},
	})
	if err == nil {
		t.Fatalf("NewEngine() error = nil, want a failure when the default locale has no catalog")
	}
	if !strings.Contains(err.Error(), "default locale") {
		t.Fatalf("error = %v, want it to explain the default locale is missing", err)
	}
}

func TestNewEngine_RejectsInvalidCatalogFileName(t *testing.T) {
	_, err := buildEngine(t, "en",
		localization.Catalog{
			Owner: "valid",
			Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte(enCatalog)}},
		},
		localization.Catalog{
			Owner: "misnamed",
			Files: fstest.MapFS{"engish.toml": &fstest.MapFile{Data: []byte(lvCatalog)}},
		},
	)
	if err == nil {
		t.Fatalf("NewEngine() error = nil, want a failure for a file not named for a language tag")
	}
	if !strings.Contains(err.Error(), "engish.toml") {
		t.Fatalf("error = %v, want it to name the offending file", err)
	}
}

func TestNewEngine_RejectsEmptyCatalog(t *testing.T) {
	tests := []struct {
		name    string
		catalog localization.Catalog
	}{
		{"no_files", localization.Catalog{Owner: "empty", Files: fstest.MapFS{}}},
		{"no_owner", localization.Catalog{Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte(enCatalog)}}}},
		{"no_fs", localization.Catalog{Owner: "nil-fs"}},
		{
			"no_messages",
			localization.Catalog{
				Owner: "blank",
				Files: fstest.MapFS{"en.toml": &fstest.MapFile{Data: []byte("")}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildEngine(t, "en", tc.catalog); err == nil {
				t.Fatalf("NewEngine() error = nil, want a startup failure")
			}
		})
	}
}

func catalog(en, lv string) []localization.Catalog {
	return []localization.Catalog{{
		Owner: "example",
		Files: fstest.MapFS{
			"en.toml": &fstest.MapFile{Data: []byte(en)},
			"lv.toml": &fstest.MapFile{Data: []byte(lv)},
		},
	}}
}

func newEngine(t *testing.T, defaultLocale string, catalogs []localization.Catalog) *localization.Engine {
	t.Helper()

	engine, err := buildEngine(t, defaultLocale, catalogs...)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	return engine
}

func buildEngine(
	t *testing.T,
	defaultLocale string,
	catalogs ...localization.Catalog,
) (*localization.Engine, error) {
	t.Helper()

	return localization.NewEngine(
		localization.EngineParams{Catalogs: catalogs},
		localization.Config{DefaultLocale: defaultLocale},
		slog.New(slog.DiscardHandler),
	)
}

// assert at compile time that an embed.FS-style filesystem is all the
// Catalog contract requires.
var _ fs.FS = fstest.MapFS{}
