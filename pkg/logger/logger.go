// Package logger создаёт единый структурированный логгер для всех сервисов.
package logger

import (
	"log/slog"
	"os"
)

// New возвращает JSON-логгер с полем service.
func New(service string) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h).With("service", service)
}
