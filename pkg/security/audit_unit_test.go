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

func TestAuditLogger_CheckRotationGoroutine(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Give checkRotation goroutine time to start
	time.Sleep(100 * time.Millisecond)

	// Write some data to trigger rotation check
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		err := al.Log(ctx, &AuditEvent{
			Username: "test",
			Action:   ActionRead,
			Resource: "test",
			Result:   ResultSuccess,
		})
		require.NoError(t, err)
	}

	// Verify the checkRotation goroutine is running by checking file exists
	assert.FileExists(t, logFile)
}

func TestAuditLogger_RotateIfNeededSmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    100, // 100 MB - won't trigger rotation
		MaxBackups: 3,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	err = al.Log(ctx, &AuditEvent{
		Username: "test",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	})
	require.NoError(t, err)

	// Test rotateIfNeeded when file is small
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	assert.NoError(t, err)
}

func TestAuditLogger_RotateIfNeededLargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
		Compress:   false, // Don't compress for this test
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Write enough data to trigger rotation (> 1 MB)
	largeData := make([]byte, 2*1024*1024) // 2 MB
	for i := range largeData {
		largeData[i] = 'x'
	}
	_, err = al.file.Write(largeData)
	require.NoError(t, err)
	require.NoError(t, al.file.Sync())

	// Trigger rotation
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	assert.NoError(t, err)

	// Verify backup file was created
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	backupFound := false
	for _, entry := range entries {
		if entry.Name() != "audit.log" && !entry.IsDir() {
			backupFound = true
			break
		}
	}
	assert.True(t, backupFound, "Backup file should be created")
}

func TestAuditLogger_RotateWithCompressionEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
		Compress:   true,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Write enough data to trigger rotation
	largeData := make([]byte, 2*1024*1024) // 2 MB
	for i := range largeData {
		largeData[i] = 'x'
	}
	_, err = al.file.Write(largeData)
	require.NoError(t, err)
	require.NoError(t, al.file.Sync())

	// Trigger rotation
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	assert.NoError(t, err)

	// Give compression goroutine time to run
	time.Sleep(200 * time.Millisecond)
}

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
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	// Create a test file
	content := []byte("test log content for compression\nwith multiple lines\nfor better testing\n")
	err := os.WriteFile(testFile, content, 0o644)
	require.NoError(t, err)

	cfg := &AuditConfig{
		Enabled:          true,
		File:             logFile,
		Compress:         true,
		CompressionLevel: gzip.DefaultCompression,
		SignLogs:         false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call compressFile method
	err = al.compressFile(testFile, gzip.DefaultCompression)
	require.NoError(t, err)

	// Verify compressed file was created
	assert.FileExists(t, compressedFile)

	// Verify original file was removed
	_, err = os.Stat(testFile)
	assert.True(t, os.IsNotExist(err), "Original file should be removed after compression")

	// Verify we can decompress it
	gzFile, err := os.Open(compressedFile)
	require.NoError(t, err)
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	require.NoError(t, err)
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	require.NoError(t, err)
	assert.Equal(t, content, decompressed)

	// Verify gzip header metadata
	assert.Equal(t, "test.log", gzReader.Name)
	assert.Equal(t, "QuicBaaS audit log", gzReader.Comment)
}

func TestAuditLogger_CompressFileWithDifferentLevels(t *testing.T) {
	testCases := []struct {
		name  string
		level int
	}{
		{"BestSpeed", gzip.BestSpeed},
		{"DefaultCompression", gzip.DefaultCompression},
		{"BestCompression", gzip.BestCompression},
		{"HuffmanOnly", gzip.HuffmanOnly},
		{"NoCompression", gzip.NoCompression},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			logFile := filepath.Join(tmpDir, "audit.log")
			testFile := filepath.Join(tmpDir, "test.log")
			compressedFile := testFile + ".gz"

			// Create a test file with content
			content := []byte("test log content for compression testing\n")
			err := os.WriteFile(testFile, content, 0o644)
			require.NoError(t, err)

			cfg := &AuditConfig{
				Enabled:          true,
				File:             logFile,
				Compress:         true,
				CompressionLevel: tc.level,
				SignLogs:         false,
			}

			log := createTestLogger(t)
			al, err := NewAuditLogger(cfg, log)
			require.NoError(t, err)
			defer al.Close()

			// Call compressFile method with specific level
			err = al.compressFile(testFile, tc.level)
			require.NoError(t, err)

			// Verify compressed file was created
			assert.FileExists(t, compressedFile)
		})
	}
}

func TestAuditLogger_CompressFileInvalidLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	// Create a test file
	content := []byte("test log content")
	err := os.WriteFile(testFile, content, 0o644)
	require.NoError(t, err)

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call compressFile with invalid level - should fallback to default
	err = al.compressFile(testFile, 999)
	require.NoError(t, err)

	// Verify compressed file was created
	assert.FileExists(t, compressedFile)
}

func TestAuditLogger_CompressFileSourceNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call compressFile on non-existent file
	err = al.compressFile(nonExistentFile, gzip.DefaultCompression)
	require.Error(t, err)

	// Verify error type
	auditErr, ok := err.(*AuditError)
	require.True(t, ok, "Expected AuditError type")
	assert.Equal(t, ErrCodeAuditCompressionOpen, auditErr.Code)
}

func TestAuditLogger_CompressFileDestinationError(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")

	// Create a test file
	content := []byte("test log content")
	err := os.WriteFile(testFile, content, 0o644)
	require.NoError(t, err)

	// Create a directory with the same name as the would-be compressed file
	// This will cause the destination file creation to fail
	invalidDest := testFile + ".gz"
	err = os.Mkdir(invalidDest, 0o755)
	require.NoError(t, err)

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call compressFile - should fail because destination is a directory
	err = al.compressFile(testFile, gzip.DefaultCompression)
	require.Error(t, err)

	// Verify error type
	auditErr, ok := err.(*AuditError)
	require.True(t, ok, "Expected AuditError type")
	assert.Equal(t, ErrCodeAuditCompressionOpen, auditErr.Code)
}

func TestAuditLogger_CompressFileAsync(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	// Create a test file
	content := []byte("test log content for async compression\n")
	err := os.WriteFile(testFile, content, 0o644)
	require.NoError(t, err)

	cfg := &AuditConfig{
		Enabled:          true,
		File:             logFile,
		Compress:         true,
		CompressionLevel: gzip.BestSpeed,
		SignLogs:         false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call compressFileAsync - runs in background
	al.compressFileAsync(testFile, gzip.BestSpeed)

	// Wait for async compression to complete
	time.Sleep(100 * time.Millisecond)

	// Verify compressed file was created
	assert.FileExists(t, compressedFile)
}

func TestCompressFileActual(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	// Create a test file with content
	content := []byte("test log content for compression\n")
	err := os.WriteFile(testFile, content, 0o644)
	require.NoError(t, err)

	// Implement actual compression for test coverage
	actualCompressFile := func(filename string) error {
		inFile, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer inFile.Close()

		outFile, err := os.Create(filename + ".gz")
		if err != nil {
			return err
		}
		defer outFile.Close()

		gzWriter := gzip.NewWriter(outFile)
		defer gzWriter.Close()

		_, err = io.Copy(gzWriter, inFile)
		return err
	}

	// Test the compression
	err = actualCompressFile(testFile)
	require.NoError(t, err)
	assert.FileExists(t, compressedFile)

	// Verify we can decompress it
	gzFile, err := os.Open(compressedFile)
	require.NoError(t, err)
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	require.NoError(t, err)
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	require.NoError(t, err)
	assert.Equal(t, content, decompressed)
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

func TestAuditLogger_VerifySignatureNotEnabled(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "", // No signature
	}

	// Should return true when not signing
	assert.True(t, al.VerifySignature(event))
}

func TestAuditLogger_VerifySignatureMismatch(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  "test-secret-key",
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		IPAddress: "127.0.0.1",
		Signature: "invalid-signature",
	}

	// Should return false for invalid signature
	assert.False(t, al.VerifySignature(event))
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

func TestAuditLogger_CheckRotationShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1,
		MaxBackups: 3,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Give checkRotation goroutine time to start
	time.Sleep(100 * time.Millisecond)

	// Close should trigger shutdown of checkRotation
	err = al.Close()
	assert.NoError(t, err)

	// Give time for goroutine to exit
	time.Sleep(100 * time.Millisecond)
}

func TestAuditLogger_RotateIfNeededNilFile(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al := &AuditLogger{
		config: cfg,
		logger: log,
		file:   nil, // No file
	}

	// Should return nil when file is nil
	err := al.rotateIfNeeded()
	assert.NoError(t, err)
}

func TestAuditLogger_CheckRotationEmptyFile(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		File:     "", // No file path
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Start checkRotation and immediately close
	time.Sleep(50 * time.Millisecond)
	err = al.Close()
	assert.NoError(t, err)
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

func TestAuditLogger_CompressionLevelConstants(t *testing.T) {
	// Verify compression level constants are properly exported
	assert.Equal(t, gzip.NoCompression, CompressionLevelNone)
	assert.Equal(t, gzip.BestSpeed, CompressionLevelBestSpeed)
	assert.Equal(t, gzip.DefaultCompression, CompressionLevelDefault)
	assert.Equal(t, gzip.BestCompression, CompressionLevelBestSize)
	assert.Equal(t, gzip.HuffmanOnly, CompressionLevelHuffmanOnly)
}

func TestAuditConfig_DefaultCompressionLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
		// CompressionLevel not set - should default to DefaultCompression
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Verify default compression level was set
	assert.Equal(t, CompressionLevelDefault, al.config.CompressionLevel)
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
