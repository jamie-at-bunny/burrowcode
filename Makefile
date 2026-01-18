.PHONY: build up down dev logs tidy health clean generate

# Production-like Docker commands
build:
	docker-compose build

up:
	docker-compose up

up-d:
	docker-compose up -d

down:
	docker-compose down

# Development with hot-reload
dev:
	docker-compose -f docker-compose.dev.yml up --build

dev-d:
	docker-compose -f docker-compose.dev.yml up --build -d

dev-down:
	docker-compose -f docker-compose.dev.yml down

# Go module maintenance
tidy:
	cd api && go mod tidy
	cd worker && go mod tidy
	cd webhooks && go mod tidy

# Generate API code from OpenAPI spec (runs in Docker, no local tools needed)
generate:
	docker run --rm -v $(PWD)/api:/app -w /app golang:1.25-alpine sh -c "go install github.com/ogen-go/ogen/cmd/ogen@v1.8.1 && go generate ./..."

# Logs
logs:
	docker-compose logs -f

logs-api:
	docker-compose logs -f api

logs-worker:
	docker-compose logs -f worker

logs-webhooks:
	docker-compose logs -f webhooks

# Health checks
health:
	@echo "API:"
	@curl -s http://localhost:8080/health | jq . || echo "API not responding"
	@echo "\nWebhooks:"
	@curl -s http://localhost:8081/health | jq . || echo "Webhooks not responding"

# Cleanup
clean:
	docker-compose down -v --rmi local
	docker-compose -f docker-compose.dev.yml down -v --rmi local 2>/dev/null || true
	rm -rf api/oas api/tmp

# Redis CLI
redis-cli:
	docker-compose exec redis redis-cli
