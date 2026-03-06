package framework

import (
	"context"
	"log/slog"
	"os"
)

// LogHandler implements [flag.Value] to create an [slog.Handler] from a log
// level string. Register with flag.Var before calling flag.Parse. The default
// level is info with JSON output to stderr.
//
//	var logHandler framework.LogHandler
//	flag.Var(&logHandler, "log-level", "Log level (debug, info, warn, error).")
type LogHandler struct {
	handler slog.Handler
}

// String returns the default log level.
func (l *LogHandler) String() string { return "info" }

// Set parses the level string and creates a JSON handler writing to stderr.
func (l *LogHandler) Set(s string) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return err
	}
	l.handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return nil
}

// Enabled reports whether the handler handles records at the given level.
func (l *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return l.getHandler().Enabled(ctx, level)
}

// Handle handles the Record.
func (l *LogHandler) Handle(ctx context.Context, r slog.Record) error {
	return l.getHandler().Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes.
func (l *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return l.getHandler().WithAttrs(attrs)
}

// WithGroup returns a new handler with the given group name.
func (l *LogHandler) WithGroup(name string) slog.Handler {
	return l.getHandler().WithGroup(name)
}

func (l *LogHandler) getHandler() slog.Handler {
	if l.handler == nil {
		l.handler = slog.NewJSONHandler(os.Stderr, nil)
	}
	return l.handler
}
