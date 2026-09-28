MIGRATIONS_DIR := db/migrations

MIGRATE_VERSION := v4.19.1

MIGRATE := go run \
	-tags 'postgres' \
	github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)


.PHONY: migrate-create migrate-up migrate-down migrate-version migrate-test

migrate-create:
	@test -n "$(name)" || \
		(echo "usage: make migrate-create name=auth_create_users" && exit 1)
	@mkdir -p $(MIGRATIONS_DIR)
	$(MIGRATE) create \
		-ext sql \
		-dir $(MIGRATIONS_DIR) \
		-format 20060102150405 \
		$(name)

# migrate-up/down/version run golang-migrate on the host, against DB_DSN
# from .env — the host-side address (localhost:5432) of the dev Postgres
# started by `make up`. The containers see a different DB_DSN only
# because Compose overrides it; nothing here needs to know about that.
# The application never migrates on startup.
migrate-up:
	@$(ENV_LOAD); \
	: "$${DB_DSN:?DB_DSN is required: cp .env.example .env}"; \
	$(MIGRATE) \
		-path $(MIGRATIONS_DIR) \
		-database "$${DB_DSN}" \
		up

migrate-down:
	@$(ENV_LOAD); \
	: "$${DB_DSN:?DB_DSN is required: cp .env.example .env}"; \
	$(MIGRATE) \
		-path $(MIGRATIONS_DIR) \
		-database "$${DB_DSN}" \
		down 1

migrate-version:
	@$(ENV_LOAD); \
	: "$${DB_DSN:?DB_DSN is required: cp .env.example .env}"; \
	$(MIGRATE) \
		-path $(MIGRATIONS_DIR) \
		-database "$${DB_DSN}" \
		version

# migrate-test applies the full up chain and then the full down chain
# against a disposable database on the isolated test Postgres instance,
# verifying migrations are both forward-applicable and reversible.
migrate-test:
	@$(ENV_LOAD); \
	: "$${DB_DSN_TEST_ADMIN:?DB_DSN_TEST_ADMIN is required: cp .env.example .env}"; \
	set -e; \
	$(COMPOSE_TEST) up -d --wait; \
	db="app_test_migrate_$$$$"; \
	case "$$db" in \
		app_test_migrate_*) ;; \
		*) echo "refusing to operate on unsafe database name: $$db" >&2; exit 1 ;; \
	esac; \
	base="$${DB_DSN_TEST_ADMIN%%\?*}"; \
	base="$${base%/*}"; \
	query=""; \
	case "$$DB_DSN_TEST_ADMIN" in \
		*\?*) query="?$${DB_DSN_TEST_ADMIN#*\?}" ;; \
	esac; \
	test_dsn="$$base/$$db$$query"; \
	echo "creating database $$db"; \
	$(COMPOSE_TEST) exec -T postgres psql -U postgres -d postgres -c "CREATE DATABASE $$db"; \
	trap '$(COMPOSE_TEST) exec -T postgres psql -U postgres -d postgres -c "DROP DATABASE IF EXISTS $$db WITH (FORCE)"; $(COMPOSE_TEST) down' EXIT; \
	echo "applying up migrations"; \
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$$test_dsn" up; \
	echo "reversing down migrations"; \
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$$test_dsn" down -all