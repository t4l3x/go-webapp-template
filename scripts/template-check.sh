#!/usr/bin/env bash
#
# template-check.sh smoke-tests scripts/init-template.sh: it copies the
# repository (tracked + untracked-but-not-ignored files) into a
# temporary directory, initializes that copy as a new project, and
# checks that no template identity is left and that the result is tidy,
# vets and passes `go test ./...` (init-template.sh itself builds and
# tests; this adds the assertions). The working repository is never
# modified. Needs no Docker or database.
#
# Template-only: init-template.sh deletes this file from new projects.

set -euo pipefail

readonly TEMPLATE_MODULE="github.com/t4l3x/go-webapp-template"
readonly NEW_MODULE="github.com/acme/example-service"
readonly NEW_NAME="example-service"

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/template-check.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

fail() {
  echo "template-check: FAIL: $*" >&2
  exit 1
}

echo "==> copying template to $WORK"
(
  cd "$ROOT"
  git ls-files -z --cached --others --exclude-standard |
    while IFS= read -r -d '' f; do [ -e "$f" ] && printf '%s\0' "$f"; done |
    tar --null -T - -cf -
) | (cd "$WORK" && tar -xf -)

cd "$WORK"
git init -q # init-template.sh selects files through git ls-files

echo "==> init-template.sh $NEW_MODULE $NEW_NAME"
./scripts/init-template.sh "$NEW_MODULE" "$NEW_NAME"

echo "==> assertions"
# The template's own usage guide and the init script legitimately keep
# naming the template; everything else must not.
leftovers="$(grep -rIl --exclude-dir=.git \
  -e "$TEMPLATE_MODULE" -e go-webapp-template -e go_webapp_template -e 'Go Webapp Template' . |
  grep -v -e '^\./scripts/init-template\.sh$' -e '^\./docs/guides/new_project_from_template\.md$' || true)"
[ -z "$leftovers" ] || fail "template references remain in: $leftovers"

[ "$(awk '$1 == "module" { print $2; exit }' go.mod)" = "$NEW_MODULE" ] || fail "go.mod module is not $NEW_MODULE"
[ "$(awk '$1 == "module" { print $2; exit }' tools/go.mod)" = "$NEW_MODULE/tools" ] || fail "tools/go.mod module not rewritten"

expect() { # expect <file> <fixed string>
  grep -qF -- "$2" "$1" || fail "$1 does not contain: $2"
}
expect cmd/api/main.go "\"$NEW_MODULE/internal/bootstrap\""
expect internal/bootstrap/bootstrap.go "\"$NEW_MODULE/internal/modules/identity\""
expect .golangci.yml "- $NEW_MODULE"
expect make/go.mk "-local $NEW_MODULE"
expect Makefile "PROJECT := $NEW_NAME"
expect docker/docker-compose.dev.yml "container_name: $NEW_NAME-postgres"
expect docker/docker-compose.dev.yml "POSTGRES_DB: example_service"
expect docker/docker-compose.test.yml "name: $NEW_NAME-test"
expect .env.example "5432/example_service?"
expect .env.example "AUTH_JWT_ISSUER=$NEW_NAME"
expect .env.example "MAIL_FROM=no-reply@$NEW_NAME.local"
expect docs/api/openapi.yaml "title: Example Service API"
expect README.md "# Example Service"

! grep -q 'template:start' README.md Makefile || fail "template-only blocks not stripped"
[ ! -e scripts/template-check.sh ] && [ ! -e make/template.mk ] || fail "template-only files not removed"

echo "==> go mod tidy -diff"
go mod tidy -diff
(cd tools && go mod tidy -diff)

echo "==> go vet ./..."
go vet ./...

echo "==> make -n help (Makefile still parses)"
make -n help >/dev/null

echo "template-check: PASS"
