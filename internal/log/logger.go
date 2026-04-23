package log

import (
	"log/slog"
	"os"
)

// New returns a structured logger for the application.
func New() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
}
