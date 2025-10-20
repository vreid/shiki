.PHONY: help dev build test clean

help:
	@echo "Available commands:"
	@echo "  make dev       - Start all services for development"
	@echo "  make build     - Build all services"
	@echo "  make test      - Run all tests"
	@echo "  make clean     - Clean up data and containers"

dev:
	docker compose up --build

dev-detached:
	docker compose up -d --build

build:
	docker compose build

test:
	cd services/metadata && go test ./...
	cd services/processor && go test ./...
	cd services/voting && go test ./...
	cd services/search && npm test

clean:
	docker compose down -v
	rm -rf data/*

logs:
	docker compose logs -f

restart:
	docker compose restart

stop:
	docker compose stop

# individual service commands
processor-logs:
	docker compose logs -f processor