# Targets run one at a time even when make is invoked with -j, because some
# depend on the previous one having finished (start the database, then migrate).
.NOTPARALLEL:
.DEFAULT_GOAL := help

-include .env

HTTP_PORT ?= 8080
DATABASE_URL ?= postgres://betledger:betledger@localhost:5432/betledger?sslmode=disable
OIDC_ISSUER_URL ?= http://localhost:8081/realms/betledger
OIDC_AUDIENCE ?= betledger-api
AWS_REGION ?= us-east-1
AWS_ENDPOINT_URL ?= http://localhost:4566
AWS_ACCESS_KEY_ID ?= test
AWS_SECRET_ACCESS_KEY ?= test
CLIENT ?= provider-a
export HTTP_PORT DATABASE_URL OIDC_ISSUER_URL OIDC_AUDIENCE AWS_REGION AWS_ENDPOINT_URL AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY

.PHONY: help dev run workers infra-up token db-up db-down db-reset migrate-up migrate-down migrate-status \
	test test-race test-integration vet fmt generate check

help: ## List the available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "} {printf "  %-18s %s\n", $$1, $$2}'

dev: infra-up migrate-up run ## Start the infrastructure, apply migrations and run the API

run: ## Run the HTTP API
	go run ./cmd/api

workers: ## Run the background workers
	go run ./cmd/workers

infra-up: ## Start PostgreSQL, Keycloak and LocalStack and wait until they are healthy
	docker compose up -d --wait postgres keycloak localstack

token: ## Print an access token for a realm client (CLIENT=provider-a by default)
	@curl -sf $(OIDC_ISSUER_URL)/protocol/openid-connect/token \
		-d grant_type=client_credentials -d client_id=$(CLIENT) -d client_secret=$(CLIENT)-secret \
		| sed -E 's/.*"access_token":"([^"]+)".*/\1/'

db-up: ## Start PostgreSQL and wait until it is healthy
	docker compose up -d --wait postgres

db-down: ## Stop the containers, keeping the database data
	docker compose down

db-reset: ## Stop the containers and delete the database data
	docker compose down --volumes

migrate-up: ## Apply pending migrations
	go run ./cmd/migrate up

migrate-down: ## Revert the last applied migration
	go run ./cmd/migrate down

migrate-status: ## Show which migrations are applied
	go run ./cmd/migrate status

test: ## Run unit tests
	go test ./...

test-race: ## Run unit tests with the race detector
	go test -race ./...

test-integration: ## Run unit and integration tests against real containers (requires Docker)
	go test -race -tags=integration ./...

vet: ## Run go vet, including the integration tests
	go vet ./...
	go vet -tags=integration ./...

fmt: ## Format the code
	gofmt -w .

generate: ## Regenerate the sqlc code from migrations and queries
	go tool sqlc generate

check: vet test-race ## Run the checks expected before committing
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)
	go tool sqlc diff
