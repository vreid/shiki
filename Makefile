.PHONY: help dev build test clean proto

help:
	@echo "Available commands:"
	@echo "  make dev       - Start all services for development"
	@echo "  make build     - Build all services"
	@echo "  make test      - Run all tests"
	@echo "  make proto     - Generate protobuf code"
	@echo "  make clean     - Clean up data and containers"

dev:
	docker compose up --build

dev-detached:
	docker compose up -d --build

build:
	docker compose build

test:
	cd services/metadata && go test ./...
	cd services/image-processor && go test ./...
	cd services/voting && go test ./...
	cd services/search && npm test

proto-lint:
	bunx buf lint

proto-breaking:
	bunx buf breaking --against '.git#branch=main'

proto-generate:
	bunx buf generate

proto: proto-lint proto-generate

proto-clean:
	rm -rf libs/go/proto/imageapp

proto-format:
	bunx buf format -w

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
image-logs:
	docker compose logs -f image-processor