.PHONY: run server cli test fmt vet migrate-up migrate-down docker-up docker-down

run:
	go run ./cmd/agent-server

server:
	go run ./cmd/agent-server

cli:
	go run ./cmd/agent

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

migrate-up:
	goose -dir internal/db/migrations postgres "$$DATABASE_URL" up

migrate-down:
	goose -dir internal/db/migrations postgres "$$DATABASE_URL" down

docker-up:
	docker compose up -d

docker-down:
	docker compose down
