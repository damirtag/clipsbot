module mellclipsbot

// NOTE: sandbox has no access to proxy.golang.org, so this could not be
// `go build`-verified here. Run `go mod tidy` locally after pulling the
// project; it will resolve exact versions and populate go.sum.
go 1.25.0

require (
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1
	github.com/jackc/pgx/v5 v5.7.1
	golang.org/x/image v0.44.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
