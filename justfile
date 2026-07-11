# 
set dotenv-load := true

run:
    go run ./cmd/bot

build:
    go build -o bin/bot ./cmd/bot

test:
    go test ./...

lint:
    golangci-lint run

fmt:
    gofmt -w .

migrate-up:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" up

migrate-down:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" down