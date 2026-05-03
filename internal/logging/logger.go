package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"openlimit/internal/config"
)

func New(cfg config.LoggingConfig) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	writer := io.Writer(os.Stdout)
	if strings.EqualFold(cfg.Format, "text") {
		handler = slog.NewTextHandler(writer, opts)
	} else {
		handler = slog.NewJSONHandler(writer, opts)
	}

	return slog.New(handler)
}
