// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package logger

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "test.log")

	tests := []struct {
		name    string
		config  LogConfig
		wantErr bool
		check   func(t *testing.T, logger *Logger)
	}{
		{
			name:   "valid json stdout config",
			config: LogConfig{Level: "info", Format: "json", Output: "stdout"},
		},
		{
			name:   "valid json stderr config",
			config: LogConfig{Level: "warn", Format: "json", Output: "stderr"},
		},
		{
			name:   "valid console stderr config",
			config: LogConfig{Level: "debug", Format: "console", Output: "stderr"},
		},
		{
			name:   "valid console stdout config without color",
			config: LogConfig{Level: "debug", Format: "console", Output: "stdout", NoColor: true},
		},
		{
			name:   "valid config with component",
			config: LogConfig{Level: "info", Format: "json", Output: "stdout", Component: "test-component"},
		},
		{
			name:   "file output creates the file",
			config: LogConfig{Level: "info", Format: "json", Output: "file", File: logFile},
			check: func(t *testing.T, logger *Logger) {
				logger.Info("test message")
				if _, err := os.Stat(logFile); err != nil {
					t.Errorf("log file not created: %v", err)
				}
			},
		},
		{
			name:    "invalid level",
			config:  LogConfig{Level: "invalid", Format: "json", Output: "stdout"},
			wantErr: true,
		},
		{
			name:    "invalid format",
			config:  LogConfig{Level: "info", Format: "invalid", Output: "stdout"},
			wantErr: true,
		},
		{
			name:    "unknown output",
			config:  LogConfig{Level: "info", Format: "json", Output: "unknown"},
			wantErr: true,
		},
		{
			name:    "file output without file path",
			config:  LogConfig{Level: "info", Format: "json", Output: "file"},
			wantErr: true,
		},
		{
			name:    "file output in unwritable directory",
			config:  LogConfig{Level: "info", Format: "json", Output: "file", File: "/root/nonexistent/directory/test.log"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if logger == nil {
				t.Fatal("New() returned nil logger without error")
			}
			if tt.check != nil {
				tt.check(t, logger)
			}
		})
	}
}

func TestLogger_WithFields(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	fields := map[string]interface{}{
		"user_id":    123,
		"request_id": "req-456",
	}

	newLogger := logger.WithFields(fields)

	// Verify original logger is unchanged
	if len(logger.GetFields()) != 0 {
		t.Error("Original logger should not be modified")
	}

	// Verify new logger has fields
	gotFields := newLogger.GetFields()
	if len(gotFields) != len(fields) {
		t.Errorf("New logger has %d fields, want %d", len(gotFields), len(fields))
	}

	for k, v := range fields {
		if gotFields[k] != v {
			t.Errorf("Field %s = %v, want %v", k, gotFields[k], v)
		}
	}
}

func TestLogger_WithField(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	newLogger := logger.WithField("key", "value")

	if val, ok := newLogger.GetFields()["key"]; !ok || val != "value" {
		t.Errorf("Field key = %v, want value", val)
	}
}

func TestLogger_WithError(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	testErr := errors.New("test error")
	newLogger := logger.WithError(testErr)

	if newLogger == nil {
		t.Error("WithError() returned nil logger")
	}
}

func TestLogger_SetLevel(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	if err := logger.SetLevel("debug"); err != nil {
		t.Errorf("SetLevel() error = %v", err)
	}

	if logger.GetLevel() != "debug" {
		t.Errorf("GetLevel() = %v, want debug", logger.GetLevel())
	}

	if err := logger.SetLevel("invalid"); err == nil {
		t.Error("SetLevel() with invalid level should return error")
	}
}

func TestLogger_GetFields(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	fields := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	}

	newLogger := logger.WithFields(fields)
	gotFields := newLogger.GetFields()

	if len(gotFields) != len(fields) {
		t.Errorf("GetFields() returned %d fields, want %d", len(gotFields), len(fields))
	}

	for k, v := range fields {
		if gotFields[k] != v {
			t.Errorf("Field %s = %v, want %v", k, gotFields[k], v)
		}
	}

	// Verify returned map is a copy
	gotFields["key3"] = "value3"
	if len(newLogger.GetFields()) != len(fields) {
		t.Error("GetFields() should return a copy, not the original map")
	}
}

func TestLogger_Clone(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger = logger.WithField("key", "value")
	cloned := logger.Clone()

	if len(cloned.GetFields()) != len(logger.GetFields()) {
		t.Error("Cloned logger should have same fields")
	}

	// Verify it's a copy — mutate the cloned fields map and ensure original is unaffected
	clonedFields := cloned.GetFields()
	clonedFields["new_key"] = "new_value"
	if _, ok := logger.GetFields()["new_key"]; ok {
		t.Error("Modifying clone fields should not affect original")
	}
}

func TestLogger_ToContext(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := logger.ToContext(context.Background())
	retrieved := FromContext(ctx)

	if retrieved == nil {
		t.Error("FromContext() returned nil")
	}
}

func TestFromContext(t *testing.T) {
	t.Run("with logger in context", func(t *testing.T) {
		logger, err := New(LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		})
		if err != nil {
			t.Fatalf("Failed to create logger: %v", err)
		}

		ctx := logger.ToContext(context.Background())
		retrieved := FromContext(ctx)

		if retrieved == nil {
			t.Error("FromContext() returned nil")
		}
	})

	t.Run("without logger in context", func(t *testing.T) {
		ctx := context.Background()
		retrieved := FromContext(ctx)

		if retrieved == nil {
			t.Error("FromContext() should return default logger, not nil")
		}
	})

	t.Run("with nil context", func(t *testing.T) {
		retrieved := FromContext(nil)

		if retrieved == nil {
			t.Error("FromContext() should return default logger, not nil")
		}
	})
}

func TestLogConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  LogConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: LogConfig{
				Level:  "info",
				Format: "json",
				Output: "stdout",
			},
			wantErr: false,
		},
		{
			name: "invalid level",
			config: LogConfig{
				Level:  "invalid",
				Format: "json",
				Output: "stdout",
			},
			wantErr: true,
		},
		{
			name: "invalid format",
			config: LogConfig{
				Level:  "info",
				Format: "invalid",
				Output: "stdout",
			},
			wantErr: true,
		},
		{
			name: "invalid output",
			config: LogConfig{
				Level:  "info",
				Format: "json",
				Output: "invalid",
			},
			wantErr: true,
		},
		{
			name: "file output without path",
			config: LogConfig{
				Level:  "info",
				Format: "json",
				Output: "file",
			},
			wantErr: true,
		},
		{
			name: "negative max size",
			config: LogConfig{
				Level:   "info",
				Format:  "json",
				Output:  "stdout",
				MaxSize: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("LogConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLogConfig_WithDefaults(t *testing.T) {
	config := LogConfig{}
	config = config.WithDefaults()

	if config.Level != "info" {
		t.Errorf("Default level = %v, want info", config.Level)
	}
	if config.Format != "json" {
		t.Errorf("Default format = %v, want json", config.Format)
	}
	if config.Output != "stdout" {
		t.Errorf("Default output = %v, want stdout", config.Output)
	}
	if config.MaxSize == 0 {
		t.Error("Default max_size should be set")
	}
}

func TestLogConfig_Clone(t *testing.T) {
	original := LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stderr",
		File:   "test.log",
	}

	cloned := original.Clone()

	if cloned.Level != original.Level {
		t.Error("Cloned config should match original")
	}

	// Modify clone
	cloned.Level = "info"

	if original.Level == cloned.Level {
		t.Error("Modifying clone should not affect original")
	}
}

func TestDefaultLogConfig(t *testing.T) {
	config := Default()

	if config.Level == "" {
		t.Error("Default config should have level set")
	}

	if config.Format == "" {
		t.Error("Default config should have format set")
	}

	if config.Output == "" {
		t.Error("Default config should have output set")
	}

	// Validate default config
	if err := config.Validate(); err != nil {
		t.Errorf("Default config should be valid: %v", err)
	}
}

func TestContextHelpers(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := WithContext(context.Background(), logger)

	// Test WithFieldsContext
	ctx = WithFieldsContext(ctx, map[string]interface{}{
		"key1": "value1",
	})

	retrieved := FromContext(ctx)
	if len(retrieved.GetFields()) == 0 {
		t.Error("WithFieldsContext should add fields to logger")
	}

	// Test WithFieldContext
	ctx = WithFieldContext(ctx, "key2", "value2")
	retrieved = FromContext(ctx)
	if len(retrieved.GetFields()) < 2 {
		t.Error("WithFieldContext should add field to logger")
	}

	// Test WithErrorContext
	testErr := errors.New("test error")
	ctx = WithErrorContext(ctx, testErr)
	retrieved = FromContext(ctx)
	if retrieved == nil {
		t.Error("WithErrorContext should return valid context")
	}
}

func TestLogger_LogMethods(t *testing.T) {
	// Create logger - just ensure methods don't panic
	logger, err := New(LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// These should not panic
	logger.Debug("debug message")
	logger.Debugf("debug %s", "formatted")
	logger.Info("info message")
	logger.Infof("info %d", 123)
	logger.Warn("warn message")
	logger.Warnf("warn %v", true)
	logger.Error("error message")
	logger.Errorf("error %s %d", "test", 456)

	// Test context logging functions
	ctx := logger.ToContext(context.Background())
	DebugContext(ctx, "debug context")
	DebugfContext(ctx, "debug %s", "context formatted")
	InfoContext(ctx, "info context")
	InfofContext(ctx, "info %d", 123)
	WarnContext(ctx, "warn context")
	WarnfContext(ctx, "warn %v", true)
	ErrorContext(ctx, "error context")
	ErrorfContext(ctx, "error %s %d", "test", 456)
}

// =============================================================================
// Consolidated from logger_additional_test.go
// =============================================================================

// TestNew_FileOutputError tests the New function with an invalid file path.
// TestLogger_WithFieldChaining tests chaining WithField calls.
func TestLogger_WithFieldChaining(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Chain multiple WithField calls
	newLogger := logger.WithField("key1", "value1").WithField("key2", "value2")
	if newLogger == nil {
		t.Error("WithField chaining returned nil logger")
	}

	// Verify both fields are present
	fields := newLogger.GetFields()
	if len(fields) != 2 {
		t.Errorf("Expected 2 fields, got %d", len(fields))
	}
}

// TestLogger_Panic tests the Panic method.
func TestLogger_Panic(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Panic() should have panicked")
		}
	}()

	logger.Panic("panic test message")
}

// TestLogger_Panicf tests the Panicf method.
func TestLogger_Panicf(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Panicf() should have panicked")
		}
	}()

	logger.Panicf("panic test %s %d", "formatted", 123)
}

// TestLogger_Fatal tests the Fatal method using subprocess pattern.
func TestLogger_Fatal(t *testing.T) {
	if os.Getenv("BE_FATAL") == "1" {
		logger, err := New(LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		})
		if err != nil {
			panic(err)
		}
		logger.Fatal("fatal test message")
		return
	}

	// Run the test in a subprocess
	cmd := exec.Command(os.Args[0], "-test.run=TestLogger_Fatal")
	cmd.Env = append(os.Environ(), "BE_FATAL=1")
	err := cmd.Run()

	// Fatal should cause the process to exit with non-zero status
	var e *exec.ExitError
	if errors.As(err, &e) && !e.Success() {
		// Expected behavior - process exited with non-zero status
		return
	}

	t.Error("Fatal() should have caused process to exit with non-zero status")
}

// TestLogger_Fatalf tests the Fatalf method using subprocess pattern.
func TestLogger_Fatalf(t *testing.T) {
	if os.Getenv("BE_FATALF") == "1" {
		logger, err := New(LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		})
		if err != nil {
			panic(err)
		}
		logger.Fatalf("fatal test %s %d", "formatted", 456)
		return
	}

	// Run the test in a subprocess
	cmd := exec.Command(os.Args[0], "-test.run=TestLogger_Fatalf")
	cmd.Env = append(os.Environ(), "BE_FATALF=1")
	err := cmd.Run()

	// Fatalf should cause the process to exit with non-zero status
	var e *exec.ExitError
	if errors.As(err, &e) && !e.Success() {
		// Expected behavior - process exited with non-zero status
		return
	}

	t.Error("Fatalf() should have caused process to exit with non-zero status")
}

// TestWithContext_NilContext tests WithContext with a nil context.
func TestWithContext_NilContext(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// WithContext should create a background context if nil is passed
	ctx := WithContext(nil, logger)
	if ctx == nil {
		t.Error("WithContext() should return a valid context even when passed nil")
	}

	// Verify we can retrieve the logger
	retrieved := FromContext(ctx)
	if retrieved == nil {
		t.Error("Should be able to retrieve logger from context created with nil input")
	}
}

// TestWithContext_ValidContext tests WithContext with a valid context.
func TestWithContext_ValidContext(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create a context with some values
	ctx := context.Background()
	ctx = context.WithValue(ctx, "test_key", "test_value")

	// Add logger to context
	ctx = WithContext(ctx, logger)

	// Verify logger is in context
	retrieved := FromContext(ctx)
	if retrieved == nil {
		t.Error("Should be able to retrieve logger from context")
	}

	// Verify original context values are preserved
	if val := ctx.Value("test_key"); val != "test_value" {
		t.Error("WithContext should preserve existing context values")
	}
}

// TestNew_StderrOutput tests the New function with stderr output.
// TestLogger_AllLevels tests logger with all valid log levels.
func TestLogger_AllLevels(t *testing.T) {
	levels := []string{"trace", "debug", "info", "warn", "error"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			config := LogConfig{
				Level:  level,
				Format: "json",
				Output: "stdout",
			}

			logger, err := New(config)
			if err != nil {
				t.Errorf("New() with level %s error = %v", level, err)
			}
			if logger == nil {
				t.Errorf("New() with level %s returned nil", level)
			}

			// Verify the level was set correctly
			if logger.GetLevel() != level {
				t.Errorf("GetLevel() = %v, want %s", logger.GetLevel(), level)
			}
		})
	}
}

// TestNew_FileOutputWithDefaultFilename tests file output without specifying a filename.
// TestLogger_ConsoleFormatWithColor tests console format without NoColor flag.
func TestLogger_ConsoleFormatWithColor(t *testing.T) {
	config := LogConfig{
		Level:   "debug",
		Format:  "console",
		Output:  "stdout",
		NoColor: false, // Enable color
	}

	logger, err := New(config)
	if err != nil {
		t.Errorf("New() error = %v, want nil", err)
	}
	if logger == nil {
		t.Error("New() returned nil logger")
	}

	// Log a message to ensure it works
	logger.Info("colored console output")
}

// TestLogger_WithAttrs tests the strongly-typed slog attributes method.
func TestLogger_WithAttrs(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Logger) *Logger
		want  map[string]interface{}
	}{
		{
			name: "several attrs in one call",
			apply: func(l *Logger) *Logger {
				return l.WithAttrs(
					slog.String("service", "quicbaas"),
					slog.Int("port", 8080),
					slog.Bool("debug", true),
				)
			},
			want: map[string]interface{}{"service": "quicbaas", "port": int64(8080), "debug": true},
		},
		{
			name: "chained calls accumulate",
			apply: func(l *Logger) *Logger {
				return l.
					WithAttrs(slog.String("service", "quicbaas")).
					WithAttrs(slog.Int("version", 1)).
					WithAttrs(slog.Bool("production", false))
			},
			want: map[string]interface{}{"service": "quicbaas", "version": int64(1), "production": false},
		},
		{
			name:  "no attrs",
			apply: func(l *Logger) *Logger { return l.WithAttrs() },
			want:  map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(LogConfig{Level: "debug", Format: "json", Output: "stdout"})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			attrLogger := tt.apply(logger)
			if attrLogger == nil {
				t.Fatal("WithAttrs() returned nil logger")
			}

			fields := attrLogger.GetFields()
			if len(fields) != len(tt.want) {
				t.Errorf("WithAttrs() fields = %v, want %v", fields, tt.want)
			}
			for key, want := range tt.want {
				if fields[key] != want {
					t.Errorf("WithAttrs() %s = %v, want %v", key, fields[key], want)
				}
			}
		})
	}
}

func TestLogger_LevelAttrs(t *testing.T) {
	tests := []struct {
		level string
		log   func(*Logger)
	}{
		{"debug", func(l *Logger) {
			l.DebugAttrs("debug message", slog.String("component", "test"), slog.Int64("bytes", 1024))
		}},
		{"info", func(l *Logger) {
			l.InfoAttrs("test message", slog.String("key", "value"), slog.Duration("elapsed", time.Second))
		}},
		{"warn", func(l *Logger) {
			l.WarnAttrs("warning message", slog.String("reason", "test"), slog.Float64("threshold", 0.95))
		}},
		{"error", func(l *Logger) {
			l.ErrorAttrs("error message", slog.Any("details", map[string]string{"key": "value"}))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger, err := New(LogConfig{Level: tt.level, Format: "json", Output: "stdout"})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			tt.log(logger)
		})
	}
}

// TestNewAdapter tests the NewAdapter function.
func TestNewAdapter(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter := NewAdapter(logger)
	if adapter == nil {
		t.Error("NewAdapter() returned nil")
	}

	// Verify it implements interfaces.Logger
	_, ok := adapter.(*LoggerAdapter)
	if !ok {
		t.Error("NewAdapter() should return *LoggerAdapter")
	}
}

// TestNewAdapter_NilLogger tests NewAdapter with nil logger.
func TestNewAdapter_NilLogger(t *testing.T) {
	adapter := NewAdapter(nil)
	if adapter != nil {
		t.Error("NewAdapter(nil) should return nil")
	}
}

// TestLoggerAdapter_WithField tests the WithField method of LoggerAdapter.
func TestLoggerAdapter_WithField(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter := NewAdapter(logger)
	result := adapter.WithField("key", "value")

	if result == nil {
		t.Error("WithField() returned nil")
	}

	// Verify it's still a LoggerAdapter
	resultAdapter, ok := result.(*LoggerAdapter)
	if !ok {
		t.Error("WithField() should return *LoggerAdapter")
	}

	// Verify the field was added
	fields := resultAdapter.GetFields()
	if fields["key"] != "value" {
		t.Errorf("WithField() key = %v, want 'value'", fields["key"])
	}
}

// TestLoggerAdapter_WithFields tests the WithFields method of LoggerAdapter.
func TestLoggerAdapter_WithFields(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter := NewAdapter(logger)
	fields := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}
	result := adapter.WithFields(fields)

	if result == nil {
		t.Error("WithFields() returned nil")
	}

	// Verify it's still a LoggerAdapter
	resultAdapter, ok := result.(*LoggerAdapter)
	if !ok {
		t.Error("WithFields() should return *LoggerAdapter")
	}

	// Verify fields were added
	resultFields := resultAdapter.GetFields()
	if resultFields["key1"] != "value1" {
		t.Errorf("WithFields() key1 = %v, want 'value1'", resultFields["key1"])
	}
}

// TestLoggerAdapter_WithError tests the WithError method of LoggerAdapter.
func TestLoggerAdapter_WithError(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter := NewAdapter(logger)
	testErr := errors.New("test error")
	result := adapter.WithError(testErr)

	if result == nil {
		t.Error("WithError() returned nil")
	}

	// Verify it's still a LoggerAdapter
	_, ok := result.(*LoggerAdapter)
	if !ok {
		t.Error("WithError() should return *LoggerAdapter")
	}
}

// TestLoggerAdapter_Chaining tests method chaining on LoggerAdapter.
func TestLoggerAdapter_Chaining(t *testing.T) {
	logger, err := New(LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter := NewAdapter(logger)
	result := adapter.
		WithField("key1", "value1").
		WithField("key2", "value2").
		WithError(errors.New("test error"))

	if result == nil {
		t.Error("Chained operations returned nil")
	}

	// Verify it's still a LoggerAdapter
	resultAdapter, ok := result.(*LoggerAdapter)
	if !ok {
		t.Error("Chained operations should return *LoggerAdapter")
	}

	// Verify fields are present
	fields := resultAdapter.GetFields()
	if fields["key1"] != "value1" {
		t.Errorf("Chained key1 = %v, want 'value1'", fields["key1"])
	}
	if fields["key2"] != "value2" {
		t.Errorf("Chained key2 = %v, want 'value2'", fields["key2"])
	}
}
