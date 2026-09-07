package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLogger(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, "")
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hidden")
	if output.Len() != 0 {
		t.Fatal("debug log emitted at default level")
	}
	logger.Info("server starting", "addr", "127.0.0.1:8080")
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["message"] != "server starting" || entry["level"] != "info" || entry["addr"] != "127.0.0.1:8080" {
		t.Fatalf("unexpected structured log: %v", entry)
	}
}

func TestLoggerLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "WARN"} {
		t.Run(level, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := newLogger(&output, level)
			if err != nil {
				t.Fatal(err)
			}
			logger.Debug("debug message")
			emitted := output.Len() > 0
			wantDebug := level == "debug" || level == "DEBUG"
			if emitted != wantDebug {
				t.Fatalf("debug emitted = %v, want %v", emitted, wantDebug)
			}
		})
	}
	for _, level := range []string{"invalid"} {
		if _, err := newLogger(&bytes.Buffer{}, level); err == nil {
			t.Fatalf("unsupported log level %q accepted", level)
		}
	}
}

func TestWarningThreshold(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, "warn")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hidden")
	if output.Len() != 0 {
		t.Fatal("info log emitted at warning level")
	}
	logger.Warn("recovered problem")
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["level"] != "warn" || entry["message"] != "recovered problem" {
		t.Fatalf("unexpected warning log: %v", entry)
	}
	output.Reset()
	logger.Error("failed operation")
	if output.Len() == 0 {
		t.Fatal("error log suppressed at warning level")
	}
}
