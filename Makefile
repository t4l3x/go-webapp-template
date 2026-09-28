SHELL := /bin/bash
.DEFAULT_GOAL := help

PROJECT := go-webapp-template

COMPOSE_DEV := docker compose -p $(PROJECT) -f docker/docker-compose.dev.yml
COMPOSE_TEST := docker compose -p $(PROJECT)-test -f docker/docker-compose.test.yml

ENV_LOAD := set -a; [ ! -f .env ] || . ./.env; set +a

include make/dev.mk
include make/go.mk
include make/db.mk
include make/docs.mk
# template:start
include make/template.mk
# template:end


.PHONY: help
help:
	@echo "$(PROJECT)"
	@echo ""
	@echo "Development:"
	@echo "  up                 Start development stack"
	@echo "  down               Stop development stack"
	@echo "  rebuild            Rebuild and start development stack"
	@echo "  logs               Follow API logs"
	@echo "  sh                 Open shell in API container"
	@echo "  ps                 Show containers"
	@echo "  debug              Run API with Delve"
	@echo ""
	@echo "Go:"
	@echo "  run                Run API locally"
	@echo "  run-worker         Run the outbox worker locally"
	@echo "  test               Run tests"
	@echo "  test-race          Run tests with race detector"
	@echo "  test-cover         Run tests with coverage profile"
	@echo "  test-integration   Run integration tests against isolated Postgres"
	@echo "  fmt                Format Go code"
	@echo "  fmt-check          Check formatting"
	@echo "  vet                Run go vet"
	@echo "  tidy               Run go mod tidy"
	@echo "  tidy-check         Check go.mod/go.sum cleanliness"
	@echo "  lint               Run golangci-lint"
	@echo "  lint-fix           Apply safe golangci-lint fixes"
	@echo "  vuln               Run govulncheck"
	@echo "  check              Run standard quality checks"
	@echo "  check-full         Run full quality checks"
	@echo ""
	@echo "Database:"
	@echo "  migrate-create     Create migration: make migrate-create name=auth_create_users"
	@echo "  migrate-up         Apply pending migrations"
	@echo "  migrate-down       Roll back one migration"
	@echo "  migrate-version    Show current schema version"
	@echo "  migrate-test       Test full migration chain on isolated Postgres"
	@echo ""
	@echo "OpenAPI:"
	@echo "  openapi-generate   Regenerate Go types from docs/api/openapi.yaml"
	@echo "  openapi-validate   Validate the OpenAPI document"
	@echo "  openapi-check      Fail if generated OpenAPI types are stale"
# template:start
	@echo ""
	@echo "Template:"
	@echo "  template-check     Smoke-test scripts/init-template.sh on a temporary copy"
# template:end