#!/usr/bin/env bash
#
# init-template.sh turns a fresh copy of the Go web application template
# into a new project: it rewrites the Go module path and the project-name
# placeholders, then tidies, formats, builds and tests the result.
#
# Usage:
#   ./scripts/init-template.sh <new-module-path> [project-name]
#
# Example:
#   ./scripts/init-template.sh github.com/acme/payment-service payment-service
#
# project-name defaults to the module path's last element (ignoring a
# /vN major-version suffix). It is used, in these forms, for:
#   kebab   payment-service   Compose project, container names, JWT issuer, mail domain
#   snake   payment_service   Postgres database names
#   title   Payment Service   OpenAPI title, email copy, README title
#
# It also removes what only makes sense in the template itself: the
# <!-- template:start/end --> blocks in README.md, the "# template:start/
# end" blocks in the Makefile, and the template smoke test
# (scripts/template-check.sh, make/template.mk). Generic values — process
# names (api/worker), ports, TTLs, limits — are left alone.
#
# Portable across GNU and BSD userlands (Linux, macOS): no sed -i, no
# GNU-only flags, bash 3.2 compatible.

set -euo pipefail

readonly TEMPLATE_MODULE="github.com/t4l3x/go-webapp-template"
readonly TEMPLATE_KEBAB="go-webapp-template"
readonly TEMPLATE_SNAKE="go_webapp_template"
readonly TEMPLATE_TITLE="Go Webapp Template"

# Files describing the template itself; rewriting them would point the
# "how to use this template" instructions at the new project.
readonly SKIP_FILES="scripts/init-template.sh docs/guides/new_project_from_template.md"

# Template-only files, meaningless (and broken) once the placeholders
# they test for are gone.
readonly TEMPLATE_ONLY_FILES="scripts/template-check.sh make/template.mk"

# Files carrying template-only blocks between start/end marker lines.
readonly MARKED_FILES="README.md Makefile"

die() {
  echo "init-template: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 <new-module-path> [project-name]" >&2
  echo "example: $0 github.com/acme/payment-service payment-service" >&2
  exit 2
}

[ "$#" -ge 1 ] && [ "$#" -le 2 ] || usage

NEW_MODULE="$1"
PROJECT_NAME="${2:-}"

# --- validate the module path -------------------------------------------------

[ -n "$NEW_MODULE" ] || die "module path must not be empty"

# First element must be a domain-like host (it needs a dot to be
# fetchable), followed by at least one path element. The character set
# deliberately excludes anything special to sed replacements (& | \).
if ! printf '%s\n' "$NEW_MODULE" |
  grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(/[A-Za-z0-9_.~-]+)+$'; then
  die "module path '$NEW_MODULE' does not look like a module path (expected e.g. github.com/acme/payment-service)"
fi
case "$NEW_MODULE" in
  */. | */.. | */./* | */../*) die "module path '$NEW_MODULE' contains a '.' or '..' element" ;;
  github.com/*/* | gitlab.com/*/* | bitbucket.org/*/*) ;;
  github.com/* | gitlab.com/* | bitbucket.org/*) die "module path '$NEW_MODULE' needs both an owner and a repository" ;;
esac

[ "$NEW_MODULE" != "$TEMPLATE_MODULE" ] || die "module path is still the template default ($TEMPLATE_MODULE)"

# --- derive and validate the project name -------------------------------------

if [ -z "$PROJECT_NAME" ]; then
  last="${NEW_MODULE##*/}"
  if printf '%s\n' "$last" | grep -Eq '^v[0-9]+$'; then
    rest="${NEW_MODULE%/*}"
    last="${rest##*/}"
  fi
  PROJECT_NAME="$(printf '%s\n' "$last" | tr 'A-Z_.' 'a-z--')"
fi

[ -n "$PROJECT_NAME" ] || die "project name must not be empty"
if ! printf '%s\n' "$PROJECT_NAME" | grep -Eq '^[a-z][a-z0-9-]*[a-z0-9]$'; then
  die "project name '$PROJECT_NAME' must be lowercase letters, digits and '-', starting with a letter"
fi
case "$PROJECT_NAME" in
  *--*) die "project name '$PROJECT_NAME' must not contain '--'" ;;
esac
[ "$PROJECT_NAME" != "$TEMPLATE_KEBAB" ] || die "project name is still the template default ($TEMPLATE_KEBAB)"

PROJECT_SNAKE="$(printf '%s\n' "$PROJECT_NAME" | tr '-' '_')"
PROJECT_TITLE="$(printf '%s\n' "$PROJECT_NAME" | awk -F- '{
  for (i = 1; i <= NF; i++) $i = toupper(substr($i, 1, 1)) substr($i, 2)
  print
}' OFS=' ')"

# --- locate the repository ----------------------------------------------------

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

[ -f go.mod ] || die "go.mod not found in $ROOT"
command -v go >/dev/null 2>&1 || die "go is not installed or not on PATH"

# Replace whatever module go.mod currently declares, so the script also
# works after a tool such as gonew already rewrote the Go imports.
OLD_MODULE="$(awk '$1 == "module" { print $2; exit }' go.mod)"
[ -n "$OLD_MODULE" ] || die "could not read the module path from go.mod"

if [ "$OLD_MODULE" = "$NEW_MODULE" ] && ! grep -rIq --exclude-dir=.git -e "$TEMPLATE_KEBAB" -e "$TEMPLATE_SNAKE" . ; then
  die "nothing to do: module is already $NEW_MODULE and no template placeholders remain"
fi

# --- drop template-only content -----------------------------------------------

for file in $MARKED_FILES; do
  [ -f "$file" ] || continue
  grep -q 'template:start' "$file" || continue
  tmp="$(mktemp)"
  awk '/template:start/ { skip = 1; next } /template:end/ { skip = 0; next } !skip' "$file" >"$tmp"
  cat "$tmp" >"$file"
  rm -f "$tmp"
  echo "  stripped template-only block from $file"
done

for file in $TEMPLATE_ONLY_FILES; do
  if [ -f "$file" ]; then
    rm -f "$file"
    echo "  removed $file"
  fi
done

# --- rewrite files ------------------------------------------------------------

escape_regex() {
  # Escape BRE metacharacters; '|' is the sed delimiter used below.
  printf '%s\n' "$1" | sed -e 's/[][\.*^$|/]/\\&/g'
}

# The new module is written through a sentinel so a module path that
# happens to contain the template's project name is not rewritten again
# by the project-name substitutions.
SENTINEL="@@INIT_TEMPLATE_NEW_MODULE@@"
OLD_MODULE_RE="$(escape_regex "$OLD_MODULE")"

list_files() {
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    # Tracked plus untracked-but-not-ignored: skips .env, build output, IDE state.
    git ls-files -z --cached --others --exclude-standard
  else
    find . \( -name .git -o -name .idea -o -name .vscode -o -name vendor -o -name node_modules \
      -o -name tmp -o -name bin -o -name dist \) -prune -o -type f ! -name '.env' -print0
  fi
}

changed=0
while IFS= read -r -d '' file; do
  file="${file#./}"
  [ -f "$file" ] || continue
  case " $SKIP_FILES " in *" $file "*) continue ;; esac
  # Skip binaries (grep -I treats them as non-matching).
  grep -Iq . "$file" 2>/dev/null || continue
  grep -q -e "$OLD_MODULE" -e "$TEMPLATE_KEBAB" -e "$TEMPLATE_SNAKE" -e "$TEMPLATE_TITLE" "$file" || continue

  tmp="$(mktemp)"
  sed \
    -e "s|$OLD_MODULE_RE|$SENTINEL|g" \
    -e "s|$TEMPLATE_TITLE|$PROJECT_TITLE|g" \
    -e "s|$TEMPLATE_KEBAB|$PROJECT_NAME|g" \
    -e "s|$TEMPLATE_SNAKE|$PROJECT_SNAKE|g" \
    -e "s|$SENTINEL|$NEW_MODULE|g" \
    "$file" >"$tmp"
  if ! cmp -s "$file" "$tmp"; then
    # cat > keeps the original file's permissions (unlike mv).
    cat "$tmp" >"$file"
    echo "  updated $file"
    changed=$((changed + 1))
  fi
  rm -f "$tmp"
done < <(list_files)

[ "$changed" -gt 0 ] || die "no files contained template references; is this a template checkout?"

# --- tidy, format, verify -----------------------------------------------------

echo "==> go mod tidy"
go mod tidy
if [ -f tools/go.mod ]; then
  (cd tools && go mod tidy)
fi

echo "==> gofmt"
gofmt -w .
unformatted="$(gofmt -l .)"
[ -z "$unformatted" ] || die "gofmt reports unformatted files: $unformatted"

echo "==> go build ./..."
go build ./...

echo "==> go test ./..."
go test ./...

cat <<EOF

Template initialized.
  old module:   $OLD_MODULE
  new module:   $NEW_MODULE
  project name: $PROJECT_NAME (db: $PROJECT_SNAKE, title: "$PROJECT_TITLE")
  files changed: $changed

Next steps:
  1. Review the changes:           git diff
  2. Create your local env file:   cp .env.example .env   (dev defaults work as is)
  3. Start dependencies and run:   make up && make migrate-up
  4. Run the full checks:          make check-full && make test-integration
  5. Commit:                       git add -A && git commit -m "Initialize $PROJECT_NAME from template"

Still yours to customize (the script deliberately leaves these alone):
  - README.md intro and docs/api/openapi.yaml info.description / info.version
  - Email wording: internal/modules/identity/translations/en.toml
  - Sender address: MAIL_FROM (placeholder no-reply@$PROJECT_NAME.local in
    .env.example and internal/platform/mail/config.go) -> a domain you own
  - APP_PUBLIC_URL: your frontend's origin (verification links point there)
  - AUTH_JWT_SECRET / AUTH_EMAIL_VERIFICATION_SECRET: real random values in
    every non-local environment (the .env.example ones are dev-only)
  - A LICENSE file, if the project needs one
  - Optionally delete scripts/init-template.sh and docs/guides/new_project_from_template.md
EOF
