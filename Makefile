SHELL := /bin/bash
-include .env
export

GO_MODULES := lib-agent-assets ms-ai-platform ms-llm-gateway demo-mcp tools

.PHONY: up down db db-down dev-api dev-gateway dev-mcp dev-web test test-go test-web lint assets-validate gateway-check

up: db ## Start every service with live reload (Ctrl+C stops them; Postgres keeps running)
	go run ./tools/devrun

down: db-down ## Stop Postgres (services started by `make up` stop with Ctrl+C)

db: ## Start Postgres (docker compose, port 5433) and wait until it is ready
	docker compose -f deploy/docker-compose.yml up -d --wait postgres

db-down:
	docker compose -f deploy/docker-compose.yml down

dev-mcp: ## Run demo-mcp on :8081
	cd demo-mcp && go run .

dev-gateway: ## Run ms-llm-gateway on :8082 (OpenAI-compatible API in front of the providers in llms.yaml)
	cd ms-llm-gateway && go run ./cmd/gateway

dev-api: ## Run ms-ai-platform on :8080 (runs migrations on start)
	cd ms-ai-platform && go run ./cmd/api

dev-web: ## Run the Vite dev server on :5173 (proxies /api to :8080)
	cd web-ai-platform && npm run dev

test: test-go test-web

test-go:
	@for m in $(GO_MODULES); do echo "== $$m"; (cd $$m && go vet ./... && go test ./...) || exit 1; done

test-web:
	cd web-ai-platform && npm run lint && npm run test && npm run build

assets-validate:
	cd lib-agent-assets && go run ./cmd/cli validate

gateway-check: ## Validate the gateway's llms.yaml (LLM_CONFIG_PATH or the embedded file) and list its models
	cd ms-llm-gateway && go run ./cmd/gateway check --discover
