.PHONY: run server cli test fmt vet swagger docs eval eval-safety eval-report ui-install ui-dev ui-build ui-test tui tui-test migrate-up migrate-down docker-up docker-down

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

swagger:
	$$(go env GOPATH)/bin/swag init --parseDependency --parseInternal -g cmd/agent-server/main.go -o docs
	cp docs/swagger.json docs/openapi.json

docs: swagger

eval:
	go run ./cmd/agent eval run

eval-safety:
	go run ./cmd/agent eval run --category safety

eval-report:
	go run ./cmd/agent eval report latest

ui-install:
	cd ui/web && npm install

ui-dev:
	cd ui/web && npm run dev

ui-build:
	cd ui/web && npm run build

ui-test:
	cd ui/web && npm test

tui:
	cd ui/tui && go run ./cmd/agent-tui

tui-test:
	cd ui/tui && go test ./...

migrate-up:
	goose -dir internal/db/migrations postgres "$$DATABASE_URL" up

migrate-down:
	goose -dir internal/db/migrations postgres "$$DATABASE_URL" down

docker-up:
	docker compose up -d

docker-down:
	docker compose down
