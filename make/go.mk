.PHONY: run run-worker test test-race test-cover test-integration fmt fmt-check vet tidy tidy-check lint lint-fix vuln check check-full

GO_TOOL := go tool -modfile=tools/go.mod

run:
	@$(ENV_LOAD); \
	APP_SERVICE=api go run ./cmd/api

run-worker:
	@$(ENV_LOAD); \
	APP_SERVICE=worker go run ./cmd/worker

test:
	go test ./...

test-race:
	go test -race ./...

# test-integration runs build-tagged integration tests (the identity
# module's Postgres repository suite, the outbox runner, and the Redis
# rate limiter) against isolated, disposable Postgres and Redis
# instances. It never runs as part of `test`.
test-integration:
	@$(ENV_LOAD); \
	: "$${DB_DSN_TEST_ADMIN:?DB_DSN_TEST_ADMIN is required: cp .env.example .env}"; \
	: "$${REDIS_URL_TEST:?REDIS_URL_TEST is required: cp .env.example .env}"; \
	set -e; \
	$(COMPOSE_TEST) up -d --wait; \
	trap '$(COMPOSE_TEST) down' EXIT; \
	DB_DSN_TEST_ADMIN="$$DB_DSN_TEST_ADMIN" \
	REDIS_URL_TEST="$$REDIS_URL_TEST" \
	go test -tags=integration ./...

test-cover:
	go test -coverprofile=coverage.out ./...

fmt:
	$(GO_TOOL) goimports -w -local github.com/t4l3x/go-webapp-template .

fmt-check:
	$(GO_TOOL) golangci-lint fmt --diff

vet:
	go vet ./...

tidy:
	go mod tidy
	(cd tools && go mod tidy)

tidy-check:
	go mod tidy -diff
	(cd tools && go mod tidy -diff)

lint:
	$(GO_TOOL) golangci-lint run

lint-fix:
	$(GO_TOOL) golangci-lint run --fix

vuln:
	govulncheck ./...

check: fmt-check tidy-check vet test openapi-validate

check-full: check test-race lint vuln openapi-check