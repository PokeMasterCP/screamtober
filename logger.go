package main

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func newLogger(output io.Writer, levelName string) (*slog.Logger, error) {
	var level slog.Level
	switch strings.ToLower(levelName) {
	case "", "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 {
				switch attr.Key {
				case slog.MessageKey:
					attr.Key = "message"
				case slog.LevelKey:
					attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
				}
			}
			return attr
		},
	})), nil
}
