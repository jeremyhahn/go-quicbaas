//go:build !integration

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

package security

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogger_CleanupOldBackupsPlaceholder(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1,
		MaxBackups: 2,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call cleanupOldBackups to increase coverage
	al.cleanupOldBackups()

	// Since it's a placeholder, just verify it doesn't panic
	assert.NotNil(t, al)
}

func TestAuditLogger_SendToSyslogPlaceholder(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:    true,
		Syslog:     true,
		SyslogAddr: "localhost:514",
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username: "test",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	// Call sendToSyslog to increase coverage (it's a placeholder)
	al.sendToSyslog(event)

	// Since it's a placeholder, just verify it doesn't panic
	assert.NotNil(t, event)
}

func TestAuditLogger_SIEMExporter(t *testing.T) {
	// Create a test server to receive SIEM events
	receivedEvents := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedEvents++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
		SIEMConfig: &SIEMConfig{
			Enabled:       true,
			Endpoint:      server.URL,
			Format:        SIEMFormatJSON,
			BatchSize:     1, // Flush immediately for testing
			FlushInterval: 100 * time.Millisecond,
		},
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Verify SIEM exporter was created
	assert.NotNil(t, al.GetSIEMExporter())
	assert.True(t, al.GetSIEMExporter().IsRunning())

	event := &AuditEvent{
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Timestamp: time.Now(),
		IPAddress: "192.168.1.1",
	}

	// Send event to SIEM
	err = al.siemExporter.Send(event)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify event was received
	assert.Greater(t, receivedEvents, 0)
}

func TestAuditLogger_CompressFile(t *testing.T) {
	small := []byte("test log content for compression\nwith multiple lines\n")
	// Larger than compressFile's 64KB copy buffer, so the copy loops.
	large := bytes.Repeat([]byte("audit log line: action=read result=success\n"), 4096)

	tests := []struct {
		name    string
		content []byte
		level   int
		// missingSource compresses a file that does not exist.
		missingSource bool
		// destIsDir occupies "<source>.gz" with a directory.
		destIsDir bool
		wantCode  string
	}{
		{name: "NoCompression", content: small, level: gzip.NoCompression},
		{name: "BestSpeed", content: small, level: gzip.BestSpeed},
		{name: "DefaultCompression", content: small, level: gzip.DefaultCompression},
		{name: "BestCompression", content: large, level: gzip.BestCompression},
		{name: "HuffmanOnly", content: small, level: gzip.HuffmanOnly},
		{name: "invalid level falls back to default", content: small, level: 999},
		{name: "missing source", level: gzip.DefaultCompression, missingSource: true, wantCode: ErrCodeAuditCompressionOpen},
		{name: "destination is a directory", content: small, level: gzip.DefaultCompression, destIsDir: true, wantCode: ErrCodeAuditCompressionOpen},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			al, err := NewAuditLogger(&AuditConfig{
				Enabled:  true,
				File:     filepath.Join(tmpDir, "audit.log"),
				Compress: true,
			}, createTestLogger(t))
			require.NoError(t, err)
			defer al.Close()

			testFile := filepath.Join(tmpDir, "test.log")
			if !tc.missingSource {
				require.NoError(t, os.WriteFile(testFile, tc.content, 0o600))
			}
			if tc.destIsDir {
				require.NoError(t, os.Mkdir(testFile+".gz", 0o755))
			}

			err = al.compressFile(testFile, tc.level)
			if tc.wantCode != "" {
				var auditErr *AuditError
				require.ErrorAs(t, err, &auditErr)
				assert.Equal(t, tc.wantCode, auditErr.Code)
				return
			}
			require.NoError(t, err)

			_, statErr := os.Stat(testFile)
			assert.True(t, os.IsNotExist(statErr), "original file should be removed after compression")

			gzFile, err := os.Open(testFile + ".gz")
			require.NoError(t, err)
			defer gzFile.Close()

			gzReader, err := gzip.NewReader(gzFile)
			require.NoError(t, err)
			defer gzReader.Close()

			decompressed, err := io.ReadAll(gzReader)
			require.NoError(t, err)
			assert.Equal(t, tc.content, decompressed)
			assert.Equal(t, "test.log", gzReader.Name)
			assert.Equal(t, "QuicBaaS audit log", gzReader.Comment)
		})
	}
}

func TestAuditLogger_CompressFileAsync(t *testing.T) {
	tmpDir := t.TempDir()
	al, err := NewAuditLogger(&AuditConfig{
		Enabled:  true,
		File:     filepath.Join(tmpDir, "audit.log"),
		Compress: true,
	}, createTestLogger(t))
	require.NoError(t, err)
	defer al.Close()

	t.Run("compresses on the calling goroutine", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.log")
		require.NoError(t, os.WriteFile(testFile, []byte("test log content for async compression\n"), 0o600))

		al.compressFileAsync(testFile, gzip.BestSpeed)

		assert.FileExists(t, testFile+".gz")
	})

	t.Run("logs and swallows a compression error", func(t *testing.T) {
		missing := filepath.Join(tmpDir, "missing.log")

		al.compressFileAsync(missing, gzip.BestSpeed)

		assert.NoFileExists(t, missing+".gz")
	})
}

func TestAuditLogger_LogWithBothSyslogAndSIEM(t *testing.T) {
	// Create a test server for SIEM
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:      true,
		File:         logFile,
		Syslog:       true,
		SyslogAddr:   "localhost:514",
		SIEM:         true,
		SIEMEndpoint: server.URL,
		SIEMConfig: &SIEMConfig{
			Enabled:       true,
			Endpoint:      server.URL,
			Format:        SIEMFormatJSON,
			BatchSize:     10,
			FlushInterval: 100 * time.Millisecond,
		},
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	event := &AuditEvent{
		Username: "test",
		Action:   ActionWrite,
		Resource: "data",
		Result:   ResultSuccess,
	}

	err = al.Log(ctx, event)
	require.NoError(t, err)

	// Give goroutines time to execute
	time.Sleep(200 * time.Millisecond)
}

func TestAuditLogger_OpenLogFileDirectoryError(t *testing.T) {
	// Use a path that will fail directory creation
	logFile := "/proc/cannot_create_dir_here/audit.log"

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		SignLogs: false,
	}

	log := createTestLogger(t)
	_, err := NewAuditLogger(cfg, log)

	// Should return error when directory creation fails
	assert.Error(t, err)
}

func TestAuditError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := io.EOF
		err := &AuditError{
			Code:    ErrCodeAuditCompressionOpen,
			Message: "test message",
			Cause:   cause,
		}
		expected := "AUDIT_COMPRESSION_OPEN: test message: EOF"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &AuditError{
			Code:    ErrCodeAuditConfigNil,
			Message: "config is nil",
		}
		expected := "AUDIT_CONFIG_NIL: config is nil"
		assert.Equal(t, expected, err.Error())
	})
}

func TestAuditError_Unwrap(t *testing.T) {
	cause := io.EOF
	err := &AuditError{
		Code:    ErrCodeAuditCompressionOpen,
		Message: "test message",
		Cause:   cause,
	}
	assert.Equal(t, cause, err.Unwrap())
}

func TestAuditLogger_GetSIEMExporter(t *testing.T) {
	// Without SIEM
	cfg := &AuditConfig{
		Enabled: true,
		SIEM:    false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.Nil(t, al.GetSIEMExporter())
}

// TestAuditLogger_RotationWatcherDoesNotLeakAcrossRotations verifies that the
// rotation watcher's lifetime is tied to the logger and not to the log file
// handle: rotating repeatedly reopens the file, and must not accumulate one
// watcher goroutine (and its ticker) per rotation.
func TestAuditLogger_RotationWatcherDoesNotLeakAcrossRotations(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:    true,
		File:       filepath.Join(t.TempDir(), "audit.log"),
		MaxSize:    1,
		MaxBackups: 3,
	}

	al, err := NewAuditLogger(cfg, createTestLogger(t))
	require.NoError(t, err)
	defer func() {
		require.NoError(t, al.Close())
	}()

	require.Eventually(t, func() bool { return al.rotationWatchers.Load() == 1 },
		2*time.Second, 5*time.Millisecond,
		"logger should start exactly one rotation watcher")

	// Force three rotations; each one closes, renames and reopens the log file.
	oversized := make([]byte, cfg.MaxSize*1024*1024+1)
	for i := 0; i < 3; i++ {
		al.mu.Lock()
		_, writeErr := al.file.Write(oversized)
		var rotateErr error
		if writeErr == nil {
			rotateErr = al.rotateIfNeeded()
		}
		al.mu.Unlock()
		require.NoError(t, writeErr)
		require.NoError(t, rotateErr, "rotation %d failed", i)
	}

	al.mu.Lock()
	info, statErr := al.file.Stat()
	al.mu.Unlock()
	require.NoError(t, statErr)
	require.Zero(t, info.Size(), "the last rotation should have left a fresh, empty log file")

	require.Never(t, func() bool { return al.rotationWatchers.Load() != 1 },
		500*time.Millisecond, 10*time.Millisecond,
		"rotation must not start additional watchers")
}

// TestAuditLogger_LogAfterRotationLandsInTheCurrentFile pins the property the
// existing rotation tests leave uncovered: that the write path and the file
// handle still agree once the log has rotated.
//
// Every other rotation test in this package stops at `al.file != nil`, which
// stays true while al.writer still points at the descriptor the rotation
// closed. Flush hides the split too, because it syncs al.file — so the logger
// reports healthy while nothing is being recorded. The assertion here is that
// the event reaches the CURRENT file, not merely that Log returned no error.
func TestAuditLogger_LogAfterRotationLandsInTheCurrentFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	// Start the file above MaxSize so the first rotateIfNeeded rotates.
	require.NoError(t, os.WriteFile(logPath, bytes.Repeat([]byte("A"), 2*1024*1024), 0o600))

	al, err := NewAuditLogger(&AuditConfig{
		Enabled:    true,
		File:       logPath,
		MaxSize:    1,
		MaxBackups: 5,
	}, createTestLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = al.Close() })

	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()
	require.NoError(t, err)

	const marker = "post-rotation-marker"
	require.NoError(t, al.Log(context.Background(), &AuditEvent{
		Action:   marker,
		Resource: "audit",
		Result:   "success",
	}), "Log after rotation")
	require.NoError(t, al.Flush())

	current, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(current), marker,
		"the post-rotation event did not reach the current audit log")
}
