package logger

import (
	"log/slog"
	"os"
)

// New returns a structured JSON logger via stdlib log/slog — no external
// logging dependency needed.
func New() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler)
}
