.PHONY: dev down test lint gen run

dev: ## Postgres + сервер в Docker
	docker compose -f infra/docker-compose.yml up --build

down:
	docker compose -f infra/docker-compose.yml down

run: ## Сервер локально (Postgres должен работать: docker compose -f infra/docker-compose.yml up -d postgres)
	cd server && DATABASE_URL="postgres://shoplist:shoplist@localhost:5432/shoplist?sslmode=disable" go run ./cmd/api

test:
	cd server && go test ./...

lint:
	cd server && go vet ./...

gen: ## Перегенерировать код из SQL (нужен sqlc)
	cd server && sqlc generate
