package localization

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nicksnyder/go-i18n/v2/i18n/template"
	"go.uber.org/fx"
	"golang.org/x/text/language"
)

// This file is the only place in the codebase that imports go-i18n.
// Everything above it depends on Localizer/Message/Catalog, so swapping
// the library out would be a change here and nowhere else.

// catalogFileExt is the message-file format. TOML reads well for
// multi-line email bodies and is what go-i18n's own tooling assumes;
// the format is taken from the file extension, so this constant and the
// unmarshaler registered below must stay in agreement.
const catalogFileExt = ".toml"

type EngineParams struct {
	fx.In

	Catalogs []Catalog `group:"localization_catalogs"`
}

// Engine is the go-i18n-backed Localizer.
//
// It is built once at startup and is read-only afterwards, which is
// what makes it safe to share across the worker's goroutines: go-i18n's
// Bundle is explicitly not safe to modify while localizers read from
// it, so nothing here mutates the bundle after construction.
type Engine struct {
	bundle *i18n.Bundle
	parser *template.TextParser

	defaultLocale string
	supported     []string
}

// strictTemplateParser makes an unsupplied template variable a hard
// error. go-i18n's default ("missingkey=default") renders the literal
// "<no value>" instead, which would put that text into a verification
// email's body where the link should be — a broken message that still
// counts as delivered. Template variables are part of a message's
// contract with the code that sends it, so a mismatch fails the send
// and lets outbox retry/alerting see it.
//
// The parser holds no Funcs, so go-i18n treats its parsed templates as
// cacheable and this single instance is safe to share.
var strictTemplateParser = &template.TextParser{Option: "missingkey=error"}

// NewEngine loads every registered catalog and validates the result.
//
// All validation happens here, at startup, on purpose: a malformed
// catalog, a file that isn't named for a real language, two modules
// claiming the same message ID, or a default locale with no messages
// are all configuration mistakes. Discovering any of them while
// processing an outbox event instead would mean a user's verification
// email fails to send for a reason that was knowable before the process
// accepted any work.
func NewEngine(params EngineParams, cfg Config, logger *slog.Logger) (*Engine, error) {
	defaultTag, err := language.Parse(cfg.DefaultLocale)
	if err != nil {
		return nil, fmt.Errorf("localization: parse default locale %q: %w", cfg.DefaultLocale, err)
	}

	bundle := i18n.NewBundle(defaultTag)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// owners maps a loaded (locale, message ID) pair to the catalog file
	// that defined it. go-i18n's own AddMessages silently overwrites a
	// duplicate ID, so without this a second definition would quietly
	// win and the losing module's wording would vanish at runtime.
	owners := make(map[string]string)
	loaded := make(map[language.Tag]struct{})

	for _, catalog := range params.Catalogs {
		if err := loadCatalog(bundle, catalog, owners, loaded); err != nil {
			return nil, err
		}
	}

	if _, ok := loaded[defaultTag]; !ok {
		return nil, fmt.Errorf(
			"localization: no catalog provides the default locale %q — it is the last fallback, so every message must exist in it",
			cfg.DefaultLocale)
	}

	supported := make([]string, 0, len(loaded))
	for tag := range loaded {
		supported = append(supported, tag.String())
	}

	slices.Sort(supported)

	logger.Info("localization catalogs loaded",
		"component", "localization",
		"default_locale", cfg.DefaultLocale,
		"supported_locales", supported,
		"catalogs", len(params.Catalogs),
	)

	return &Engine{
		bundle:        bundle,
		parser:        strictTemplateParser,
		defaultLocale: cfg.DefaultLocale,
		supported:     supported,
	}, nil
}

// SupportedLocales returns the locales that actually have a catalog,
// sorted. The registered catalogs are the authoritative source of this
// set — it is never configured separately.
func (e *Engine) SupportedLocales() []string {
	return slices.Clone(e.supported)
}

// Translate implements Localizer.
func (e *Engine) Translate(locale string, msg Message) (string, error) {
	if msg.ID == "" {
		return "", fmt.Errorf("localization: message ID must not be empty")
	}

	localizer := i18n.NewLocalizer(e.bundle, e.preferences(locale)...)

	config := &i18n.LocalizeConfig{
		MessageID:      msg.ID,
		PluralCount:    msg.PluralCount,
		TemplateParser: e.parser,
	}

	// Left nil when there is nothing to interpolate. A nil map assigned
	// to LocalizeConfig's `any` field is not a nil interface, and
	// go-i18n only injects PluralCount into the template data when it
	// sees a truly nil one — so passing an empty map through would
	// silently break "{{.PluralCount}}".
	if data := templateData(msg); data != nil {
		config.TemplateData = data
	}

	rendered, err := localizer.Localize(config)

	// go-i18n reports MessageNotFoundErr even when it successfully fell
	// back to the default language, returning the fallback text
	// alongside the error. That fallback is exactly the behavior this
	// package promises, so it is a success — but only when it actually
	// produced text. Treating the error as fatal would break the
	// fallback chain; ignoring it entirely would let a genuinely missing
	// message through as "".
	var notFound *i18n.MessageNotFoundErr

	if errors.As(err, &notFound) {
		if rendered == "" {
			return "", fmt.Errorf("%w: %q (requested locale %q)", ErrMessageNotFound, msg.ID, locale)
		}

		err = nil
	}

	if err != nil {
		// Deliberately reports the message ID and locale only. Data may
		// carry security-sensitive values (a verification URL embeds a
		// bearer token), and this error travels to the outbox runner's
		// error log.
		return "", fmt.Errorf("localization: render %q for locale %q: %w", msg.ID, locale, err)
	}

	if rendered == "" {
		return "", fmt.Errorf("%w: %q (requested locale %q)", ErrEmptyMessage, msg.ID, locale)
	}

	return rendered, nil
}

// preferences builds the match list handed to go-i18n, whose matcher
// does the BCP 47 work: an exact tag wins, a regional tag falls back to
// its base language ("lv-LV" matches an "lv" catalog). Appending the
// default locale makes the final link in the chain explicit rather than
// an artifact of which tag the bundle happens to hold first.
//
// A malformed tag is dropped by go-i18n's own parsing, which leaves the
// default — the intended outcome, since an unusable locale string from
// a caller should degrade to readable text, not fail a send.
// templateData returns the data a message's template should execute
// against, or nil when there is none. A message that declares plural
// forms can reference {{.PluralCount}} alongside its own variables, so
// the count is merged in rather than replacing the caller's data.
func templateData(msg Message) map[string]any {
	if msg.PluralCount == nil {
		if len(msg.Data) == 0 {
			return nil
		}

		return msg.Data
	}

	merged := make(map[string]any, len(msg.Data)+1)
	maps.Copy(merged, msg.Data)
	merged["PluralCount"] = msg.PluralCount

	return merged
}

func (e *Engine) preferences(locale string) []string {
	if locale == "" || locale == e.defaultLocale {
		return []string{e.defaultLocale}
	}

	return []string{locale, e.defaultLocale}
}

func loadCatalog(
	bundle *i18n.Bundle,
	catalog Catalog,
	owners map[string]string,
	loaded map[language.Tag]struct{},
) error {
	if catalog.Owner == "" {
		return fmt.Errorf("localization: a registered catalog has no Owner set")
	}

	if catalog.Files == nil {
		return fmt.Errorf("localization: catalog %q has no Files", catalog.Owner)
	}

	files := 0

	err := fs.WalkDir(catalog.Files, ".", func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(filePath, catalogFileExt) {
			return nil
		}

		files++

		return loadCatalogFile(bundle, catalog, filePath, owners, loaded)
	})
	if err != nil {
		return err
	}

	if files == 0 {
		return fmt.Errorf(
			"localization: catalog %q contributed no %s message files", catalog.Owner, catalogFileExt)
	}

	return nil
}

func loadCatalogFile(
	bundle *i18n.Bundle,
	catalog Catalog,
	filePath string,
	owners map[string]string,
	loaded map[language.Tag]struct{},
) error {
	data, err := fs.ReadFile(catalog.Files, filePath)
	if err != nil {
		return fmt.Errorf("localization: read catalog %q file %q: %w", catalog.Owner, filePath, err)
	}

	// go-i18n derives the locale from the file name, and maps an
	// unrecognized one to the undefined tag rather than failing. Checked
	// here so "engish.toml" is a startup error naming the file, not a
	// catalog that silently loads under a locale nothing will ever
	// match.
	locale := strings.TrimSuffix(path.Base(filePath), catalogFileExt)

	if _, err := language.Parse(locale); err != nil {
		return fmt.Errorf(
			"localization: catalog %q file %q is not named for a valid BCP 47 language tag: %w",
			catalog.Owner, filePath, err)
	}

	messageFile, err := bundle.ParseMessageFileBytes(data, filePath)
	if err != nil {
		return fmt.Errorf("localization: parse catalog %q file %q: %w", catalog.Owner, filePath, err)
	}

	if len(messageFile.Messages) == 0 {
		return fmt.Errorf("localization: catalog %q file %q defines no messages", catalog.Owner, filePath)
	}

	// go-i18n builds Messages by ranging a map, so its order varies run
	// to run. Sorted here so that a catalog with several conflicts
	// always reports the same one: a startup error that names a
	// different message each restart is much harder to act on.
	messages := slices.SortedFunc(
		slices.Values(messageFile.Messages),
		func(a, b *i18n.Message) int { return strings.Compare(a.ID, b.ID) },
	)

	for _, message := range messages {
		key := messageFile.Tag.String() + "|" + message.ID

		if previous, duplicate := owners[key]; duplicate {
			return fmt.Errorf(
				"localization: message %q for locale %q is defined twice: %s and %s (catalog %q)",
				message.ID, messageFile.Tag, previous, filePath, catalog.Owner)
		}

		owners[key] = catalog.Owner + ":" + filePath
	}

	loaded[messageFile.Tag] = struct{}{}

	return nil
}
