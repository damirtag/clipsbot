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

# Create a new timestamped migration pair (up/down) with the given name.
migrate-new name:
    migrate create -ext sql -dir internal/repository/postgres/migrations -seq {{name}}

migrate-up:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" up

migrate-down:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" down

# Roll back exactly one migration (safer than migrate-down for prod)
migrate-down-one:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" down 1

# Show current migration version and dirty state
migrate-status:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" version

# Force the schema_migrations table to a specific version after manually
# fixing a "dirty" migration (use with care — verify the DB state first)
migrate-force version:
    migrate -path internal/repository/postgres/migrations -database "$DATABASE_URL" force {{version}}