package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitLogger_CreatesLogDir(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")

	_, err := InitLogger(logDir, "info", true)
	if err != nil {
		t.Fatalf("InitLogger() error: %v", err)
	}

	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("log directory was not created")
	}
}

func TestInitLogger_FileOutput(t *testing.T) {
	logDir := t.TempDir()

	logger, err := InitLogger(logDir, "info", true)
	if err != nil {
		t.Fatalf("InitLogger() error: %v", err)
	}
	defer logger.Sync()

	logger.Info("test message")

	// Close logger to flush
	_ = logger.Sync()

	logFile := filepath.Join(logDir, "app.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("log file is empty")
	}
}

func TestInitLogger_Levels(t *testing.T) {
	tests := []struct {
		level string
	}{
		{"debug"},
		{"info"},
		{"warn"},
		{"error"},
		{"DEBUG"},
		{"INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logDir := t.TempDir()
			_, err := InitLogger(logDir, tt.level, true)
			if err != nil {
				t.Errorf("InitLogger(%q) error: %v", tt.level, err)
			}
		})
	}
}
