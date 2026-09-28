.PHONY: openapi-generate openapi-validate openapi-check

OPENAPI_SPEC := docs/api/openapi.yaml
OPENAPI_CODEGEN_VERSION := v2.8.0
OPENAPI_CODEGEN := go run \
	github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OPENAPI_CODEGEN_VERSION)

# openapi-generate regenerates Go transport types from the OpenAPI
# contract. docs/api/openapi.yaml is the source of truth; the generated
# file must never be edited by hand.
openapi-generate:
	$(OPENAPI_CODEGEN) -config internal/api/openapi/codegen.config.yaml $(OPENAPI_SPEC)

# openapi-validate checks that the OpenAPI document itself is a valid
# OpenAPI 3.x specification.
openapi-validate:
	go run ./cmd/openapi-validate $(OPENAPI_SPEC)

# openapi-check regenerates the OpenAPI types and fails if that changes
# any tracked file, catching a spec/generated-code drift that a
# developer forgot to commit.
openapi-check: openapi-generate
	@git diff --exit-code -- internal/api/openapi || \
		(echo "generated OpenAPI types are stale: run 'make openapi-generate' and commit the result" >&2; exit 1)
