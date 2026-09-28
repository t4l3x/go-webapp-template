.PHONY: up down rebuild logs sh ps debug

up:
	$(COMPOSE_DEV) up -d

down:
	$(COMPOSE_DEV) down

rebuild:
	$(COMPOSE_DEV) up -d --build

logs:
	$(COMPOSE_DEV) logs -f api

sh:
	$(COMPOSE_DEV) exec api sh

ps:
	$(COMPOSE_DEV) ps

debug:
	-$(COMPOSE_DEV) stop api
	-docker rm -f go-webapp-template-api-debug 2>/dev/null
	$(COMPOSE_DEV) run --rm \
		--name go-webapp-template-api-debug \
		--service-ports \
		api \
		dlv debug ./cmd/api \
		--headless \
		--listen=:2345 \
		--api-version=2 \
		--accept-multiclient \
		--build-flags=-buildvcs=false