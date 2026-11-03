SHELL := /bin/bash
-include .env
export

GO_MODULES := agent-assets ms-ai-platform demo-mcp

.PHONY: db db-down dev-api dev-mcp dev-web test test-go test-web lint assets-validate assets-export

db: ## Start Postgres (docker compose, port 5433)
	docker compose -f deploy/docker-compose.yml up -d postgres

db-down:
	docker compose -f deploy/docker-compose.yml down

dev-mcp: ## Run demo-mcp on :8081
	cd demo-mcp && go run .

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
	cd agent-assets && go run ./cmd/cli validate

assets-export:
	cd agent-assets && go run ./cmd/cli export
