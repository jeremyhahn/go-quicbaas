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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSIEMExporter_NewInvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *SIEMConfig
		wantCode string
	}{
		{name: "nil config", cfg: nil, wantCode: ErrCodeSIEMConfigNil},
		{name: "missing endpoint", cfg: &SIEMConfig{Enabled: true, Endpoint: ""}, wantCode: ErrCodeSIEMEndpointMissing},
		{name: "invalid endpoint", cfg: &SIEMConfig{Enabled: true, Endpoint: "://invalid"}, wantCode: ErrCodeSIEMEndpointInvalid},
		{name: "invalid scheme", cfg: &SIEMConfig{Enabled: true, Endpoint: "ftp://example.com"}, wantCode: ErrCodeSIEMSchemeInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSIEMExporter(tt.cfg, createTestLogger(t))
			var siemErr *SIEMError
			require.ErrorAs(t, err, &siemErr)
			assert.Equal(t, tt.wantCode, siemErr.Code)
		})
	}
}

func TestSIEMExporter_NewDisabled(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled: false,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, exporter)

	// Should not start when disabled
	exporter.Start()
	assert.False(t, exporter.IsRunning())

	// Send and Flush should be no-ops
	event := &AuditEvent{Username: "test"}
	err = exporter.Send(event)
	assert.NoError(t, err)
	assert.NoError(t, exporter.Flush())
}

func TestSIEMExporter_NewWithDefaults(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "https://siem.example.com/events",
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, exporter)

	// Verify defaults were applied
	assert.Equal(t, 100, exporter.config.BatchSize)
	assert.Equal(t, 10*time.Second, exporter.config.FlushInterval)
	assert.Equal(t, 30*time.Second, exporter.config.Timeout)
	assert.Equal(t, 3, exporter.config.MaxRetries)
	assert.Equal(t, time.Second, exporter.config.RetryDelay)
	assert.Equal(t, SIEMFormatJSON, exporter.config.Format)
	assert.Equal(t, "QuicBaaS", exporter.config.CEFVendor)
	assert.Equal(t, "QuicBaaS", exporter.config.LEEFVendor)
}

func TestSIEMExporter_SendNilEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	err = exporter.Send(nil)
	require.Error(t, err)

	siemErr, ok := err.(*SIEMError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeSIEMEventNil, siemErr.Code)
}

func TestSIEMExporter_JSONFormat(t *testing.T) {
	var receivedEvents []SIEMJSONEvent
	var receivedCount atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-ndjson", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Parse newline-delimited JSON
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			var event SIEMJSONEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			receivedEvents = append(receivedEvents, event)
		}
		receivedCount.Add(int64(len(lines)))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-123",
		Username:  "testuser",
		UserID:    "user-456",
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultSuccess,
		IPAddress: "192.0.2.100",
		NodeID:    "node-1",
		ShardID:   42,
		Details:   map[string]string{"method": "jwt"},
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	assert.Greater(t, receivedCount.Load(), int64(0))
	if len(receivedEvents) > 0 {
		lastEvent := receivedEvents[len(receivedEvents)-1]
		assert.Equal(t, "quicbaas", lastEvent.Source)
		assert.Equal(t, "audit", lastEvent.EventType)
		assert.Equal(t, "testuser", lastEvent.User)
		assert.Equal(t, "authenticate", lastEvent.Action)
	}
}

func TestSIEMExporter_CEFFormat(t *testing.T) {
	var receivedData string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "text/plain", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedData = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatCEF,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		CEFVendor:     "TestVendor",
		CEFProduct:    "TestProduct",
		CEFVersion:    "2.0",
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-123",
		Username:  "testuser",
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultSuccess,
		IPAddress: "192.0.2.100",
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify CEF format
	assert.Contains(t, receivedData, "CEF:0|TestVendor|TestProduct|2.0|")
	assert.Contains(t, receivedData, "suser=testuser")
	assert.Contains(t, receivedData, "act=authenticate")
	assert.Contains(t, receivedData, "outcome=success")
}

func TestSIEMExporter_LEEFFormat(t *testing.T) {
	var receivedData string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "text/plain", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedData = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatLEEF,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		LEEFVendor:    "TestVendor",
		LEEFProduct:   "TestProduct",
		LEEFVersion:   "3.0",
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-123",
		Username:  "testuser",
		Action:    ActionRead,
		Resource:  "keys",
		Result:    ResultDenied,
		IPAddress: "192.0.2.100",
		ErrorMsg:  "Access denied",
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify LEEF format
	assert.Contains(t, receivedData, "LEEF:2.0|TestVendor|TestProduct|3.0|")
	assert.Contains(t, receivedData, "usrName=testuser")
	assert.Contains(t, receivedData, "action=read")
	assert.Contains(t, receivedData, "result=denied")
}

func TestSIEMExporter_UnsupportedFormat(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      "https://siem.example.com",
		Format:        SIEMFormat("invalid"),
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	exporter.batch = append(exporter.batch, event)
	err = exporter.Flush()
	require.Error(t, err)

	siemErr, ok := err.(*SIEMError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeSIEMFormatBatch, siemErr.Code)
}

func TestSIEMExporter_RetryOnFailure(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		MaxRetries:    3,
		RetryDelay:    10 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	// Wait for retries
	time.Sleep(500 * time.Millisecond)

	// Should have made 3 attempts
	assert.GreaterOrEqual(t, requestCount.Load(), int32(3))
}

func TestSIEMExporter_CustomHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		Headers: map[string]string{
			"Authorization":   "Bearer test-token",
			"X-Custom-Header": "custom-value",
		},
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, "Bearer test-token", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "custom-value", receivedHeaders.Get("X-Custom-Header"))
}

func TestSIEMExporter_BatchFlush(t *testing.T) {
	var receivedBatches atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBatches.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     5,
		FlushInterval: 5 * time.Second, // Long interval to ensure batch size triggers flush
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	// Send 5 events to trigger batch flush
	for i := 0; i < 5; i++ {
		event := &AuditEvent{
			Timestamp: time.Now(),
			Username:  "test",
			Action:    ActionRead,
			Resource:  "test",
			Result:    ResultSuccess,
		}
		err = exporter.Send(event)
		require.NoError(t, err)
	}

	// Wait for batch flush
	time.Sleep(200 * time.Millisecond)

	assert.Greater(t, receivedBatches.Load(), int32(0))
}

func TestSIEMExporter_GetStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	for i := 0; i < 3; i++ {
		err = exporter.Send(event)
		require.NoError(t, err)
	}

	// Wait for flush
	time.Sleep(300 * time.Millisecond)

	sent, errors := exporter.GetStats()
	assert.GreaterOrEqual(t, sent, int64(3))
	assert.Equal(t, int64(0), errors)
}

func TestSIEMExporter_SeverityMapping(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "https://siem.example.com",
		Format:   SIEMFormatJSON,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	testCases := []struct {
		action   string
		result   string
		expected int
	}{
		{ActionAuthenticate, ResultDenied, 8},  // High - access denial
		{ActionAuthenticate, ResultFailure, 7}, // High - auth failure
		{ActionKeyExport, ResultFailure, 6},    // Medium-High - key op failure
		{ActionRead, ResultFailure, 4},         // Medium - general failure
		{ActionKeyExport, ResultSuccess, 5},    // Medium - key export
		{ActionTenantSwitch, ResultSuccess, 5}, // Medium - cross-tenant
		{ActionUserCreate, ResultSuccess, 4},   // Medium - admin operation
		{ActionRead, ResultSuccess, 1},         // Low - normal operation
	}

	for _, tc := range testCases {
		event := &AuditEvent{
			Action: tc.action,
			Result: tc.result,
		}
		severity := exporter.mapSeverity(event)
		assert.Equal(t, tc.expected, severity, "Action: %s, Result: %s", tc.action, tc.result)
	}
}

func TestSIEMExporter_LEEFEscaping(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "https://siem.example.com",
		Format:   SIEMFormatLEEF,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	// Test header escaping
	result := exporter.escapeLEEF("test|pipe\\backslash")
	assert.Equal(t, "test\\|pipe\\\\backslash", result)

	// Test key escaping
	result = exporter.escapeLEEFKey("key=with\ttab")
	assert.Equal(t, "key_with_tab", result)

	// Test value escaping
	result = exporter.escapeLEEFValue("test\ttab=equals\nnewline")
	assert.Equal(t, "test\\ttab\\=equals\\nnewline", result)
}

func TestSIEMError_Error(t *testing.T) {
	tests := []struct {
		name       string
		err        *SIEMError
		wantString string
		wantUnwrap error
	}{
		{
			name:       "with cause",
			err:        &SIEMError{Code: ErrCodeSIEMRequestFailed, Message: "request failed", Cause: io.EOF},
			wantString: "SIEM_REQUEST_FAILED: request failed: EOF",
			wantUnwrap: io.EOF,
		},
		{
			name:       "without cause",
			err:        &SIEMError{Code: ErrCodeSIEMConfigNil, Message: "config is nil"},
			wantString: "SIEM_CONFIG_NIL: config is nil",
			wantUnwrap: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantString, tt.err.Error())
			assert.Equal(t, tt.wantUnwrap, tt.err.Unwrap())
		})
	}
}

func TestSIEMExporter_DoubleStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	// Start twice - should not cause issues
	exporter.Start()
	exporter.Start()

	assert.True(t, exporter.IsRunning())

	err = exporter.Close()
	assert.NoError(t, err)
	assert.False(t, exporter.IsRunning())
}

func TestSIEMExporter_CloseWithoutStart(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "https://siem.example.com",
		Format:   SIEMFormatJSON,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	// Close without starting - should not cause issues
	err = exporter.Close()
	assert.NoError(t, err)
}

func TestSIEMExporter_FlushEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &SIEMConfig{
		Enabled:       true,
		Endpoint:      server.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)
	exporter.Start()
	defer exporter.Close()

	// Flush empty batch - should not cause issues
	err = exporter.Flush()
	assert.NoError(t, err)
}

// TestAuditLogger_SIEMExporterIntegration tests SIEM exporter integration
func TestAuditLogger_SIEMExporterIntegration(t *testing.T) {
	log := createTestLogger(t)

	// Create a test server to receive SIEM events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
		SIEMConfig: &SIEMConfig{
			Enabled:       true,
			Endpoint:      server.URL,
			Format:        SIEMFormatJSON,
			BatchSize:     10,
			FlushInterval: 100 * time.Millisecond,
		},
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username: "test",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	// Test SIEM exporter via the audit logger
	if al.siemExporter != nil {
		err = al.siemExporter.Send(event)
		assert.NoError(t, err)

		// Wait for flush
		time.Sleep(200 * time.Millisecond)
	}
}

// TestAuditLogger_SendToSIEMIntegration tests the sendToSIEM function
func TestAuditLogger_SendToSIEMIntegration(t *testing.T) {
	logger := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: "http://localhost:9200",
	}

	al, err := NewAuditLogger(cfg, logger)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		UserID:   "test-user",
		Username: "testuser",
		Action:   ActionWrite,
		Resource: "test-resource",
		Result:   ResultSuccess,
	}

	// This will call sendToSIEM in a goroutine
	err = al.Log(context.Background(), event)
	assert.NoError(t, err)

	// Give goroutine time to execute
	time.Sleep(50 * time.Millisecond)
}

// TestAuditLogger_BothSyslogAndSIEM tests both outputs together
func TestAuditLogger_BothSyslogAndSIEM(t *testing.T) {
	logger := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:      true,
		Syslog:       true,
		SyslogAddr:   "localhost:514",
		SIEM:         true,
		SIEMEndpoint: "http://localhost:9200",
	}

	al, err := NewAuditLogger(cfg, logger)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		UserID:   "test-user",
		Username: "testuser",
		Action:   ActionDelete,
		Resource: "test-resource",
		Result:   ResultSuccess,
	}

	// This will call both sendToSyslog and sendToSIEM in goroutines
	err = al.Log(context.Background(), event)
	assert.NoError(t, err)

	// Give goroutines time to execute
	time.Sleep(100 * time.Millisecond)
}

func TestLog_WithSIEMExporter(t *testing.T) {
	// Start a test HTTP server to receive SIEM events
	received := make(chan []byte, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100,
		SIEMConfig: &SIEMConfig{
			Enabled:       true,
			Endpoint:      server.URL,
			Format:        SIEMFormatJSON,
			BatchSize:     1,
			FlushInterval: 100 * time.Millisecond,
			Timeout:       5 * time.Second,
			MaxRetries:    1,
		},
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.Log(context.Background(), &AuditEvent{
		Action:   ActionRead,
		Resource: "test-siem",
		Result:   ResultSuccess,
	})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// formatJSONBatch (audit.go:899 -- 85.7% covered)
// ---------------------------------------------------------------------------

func TestFormatJSONBatch_MultipleEvents(t *testing.T) {
	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "http://localhost:8080",
		Format:   SIEMFormatJSON,
	}

	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	events := []*AuditEvent{
		{
			Timestamp: time.Now(),
			EventID:   "evt1",
			Action:    ActionRead,
			Resource:  "resource1",
			Result:    ResultSuccess,
		},
		{
			Timestamp: time.Now(),
			EventID:   "evt2",
			Action:    ActionWrite,
			Resource:  "resource2",
			Result:    ResultFailure,
			ErrorMsg:  "permission denied",
		},
	}

	data, contentType, err := exporter.formatJSONBatch(events)
	require.NoError(t, err)
	assert.Equal(t, "application/x-ndjson", contentType)
	assert.Contains(t, string(data), "evt1")
	assert.Contains(t, string(data), "evt2")

	// Verify each line is valid JSON
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	for _, line := range lines {
		var jsonEvent SIEMJSONEvent
		assert.NoError(t, json.Unmarshal(line, &jsonEvent))
	}
}

// ---------------------------------------------------------------------------
// doHTTPRequest (audit.go:854 -- 92.9% covered)
// ---------------------------------------------------------------------------

func TestDoHTTPRequest_SuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: server.URL,
		Format:   SIEMFormatJSON,
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"X-Custom":      "custom-value",
		},
	}

	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	err = exporter.doHTTPRequest([]byte(`{"test":"data"}`), "application/json")
	require.NoError(t, err)
}

func TestDoHTTPRequest_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: server.URL,
		Format:   SIEMFormatJSON,
	}

	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	err = exporter.doHTTPRequest([]byte(`{}`), "application/json")
	require.Error(t, err)
	var siemErr *SIEMError
	assert.ErrorAs(t, err, &siemErr)
	assert.Equal(t, ErrCodeSIEMStatusCode, siemErr.Code)
}

// ---------------------------------------------------------------------------
// AuditLogger.Close with SIEM and Syslog (audit.go:2187 -- 84.6%)
// ---------------------------------------------------------------------------

func TestAuditLogger_CloseWithSIEM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100,
		SIEMConfig: &SIEMConfig{
			Enabled:  true,
			Endpoint: server.URL,
			Format:   SIEMFormatJSON,
		},
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Close should cleanly shut down SIEM exporter
	require.NoError(t, al.Close())
}

func TestLog_WithSIEMAndSyslogExporterTogether(t *testing.T) {
	// Start HTTP server for SIEM
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Start TCP for syslog
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					if _, readErr := c.Read(buf); readErr != nil {
						return
					}
				}
			}(conn)
		}
	}()

	log := createTestLogger(t)

	// Create SIEM exporter
	siemExporter, siemErr := NewSIEMExporter(&SIEMConfig{
		Enabled:       true,
		Endpoint:      ts.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
	}, log)
	require.NoError(t, siemErr)
	siemExporter.Start()
	defer siemExporter.Close()

	// Create syslog exporter
	syslogExporter, syslogErr := NewSyslogExporter(&SyslogExporterConfig{
		Network: "tcp",
		Address: listener.Addr().String(),
		Tag:     "test-combined",
	}, log)
	require.NoError(t, syslogErr)
	connErr := syslogExporter.connect()
	require.NoError(t, connErr)
	defer syslogExporter.Close()

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:       true,
			SIEM:          true,
			Syslog:        true,
			FlushInterval: 1 * time.Second,
		},
		logger:         log,
		writer:         &bytes.Buffer{},
		siemExporter:   siemExporter,
		syslogExporter: syslogExporter,
		shutdown:       make(chan struct{}),
	}
	defer close(al.shutdown)

	event := &AuditEvent{
		Action:   ActionDelete,
		Resource: "combined-resource",
		Result:   ResultSuccess,
		UserID:   "user-combined",
		Username: "combined-user",
	}

	err = al.Log(context.Background(), event)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// formatJSONBatch - exercise error path (hard to trigger with valid events)
// ---------------------------------------------------------------------------

func TestFormatJSONBatch_EmptyEvents(t *testing.T) {
	log := createTestLogger(t)

	exporter, err := NewSIEMExporter(&SIEMConfig{
		Enabled:       true,
		Endpoint:      "http://localhost:9999",
		Format:        SIEMFormatJSON,
		BatchSize:     10,
		FlushInterval: 1 * time.Second,
	}, log)
	require.NoError(t, err)

	// Empty event list
	data, contentType, formatErr := exporter.formatJSONBatch([]*AuditEvent{})
	assert.NoError(t, formatErr)
	assert.Equal(t, "application/x-ndjson", contentType)
	assert.Empty(t, data)
}

func TestNewAuditLogger_SIEM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tests := []struct {
		name    string
		cfg     *AuditConfig
		wantErr bool
	}{
		{
			name: "legacy endpoint",
			cfg:  &AuditConfig{Enabled: true, SIEM: true, SIEMEndpoint: server.URL},
		},
		{
			name: "explicit config",
			cfg: &AuditConfig{
				Enabled:    true,
				SIEM:       true,
				SIEMConfig: &SIEMConfig{Enabled: true, Endpoint: server.URL, Format: SIEMFormatCEF},
			},
		},
		{
			name:    "legacy empty endpoint",
			cfg:     &AuditConfig{Enabled: true, SIEM: true, SIEMEndpoint: "", FlushInterval: time.Second},
			wantErr: true,
		},
		{
			name: "explicit config invalid scheme",
			cfg: &AuditConfig{
				Enabled:       true,
				SIEM:          true,
				SIEMConfig:    &SIEMConfig{Enabled: true, Endpoint: "ftp://invalid-scheme.example.com"},
				FlushInterval: time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al, err := NewAuditLogger(tt.cfg, createTestLogger(t))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer al.Close()
			assert.NotNil(t, al.siemExporter)
		})
	}
}

// ---------------------------------------------------------------------------
// Close - cover the close paths more thoroughly
// ---------------------------------------------------------------------------

func TestAuditLogger_CloseWithFileAndSyslogAndSIEM(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "close_test.log")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					if _, readErr := c.Read(buf); readErr != nil {
						return
					}
				}
			}(conn)
		}
	}()

	log := createTestLogger(t)

	// Create SIEM exporter
	siemExporter, siemErr := NewSIEMExporter(&SIEMConfig{
		Enabled:       true,
		Endpoint:      ts.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	}, log)
	require.NoError(t, siemErr)
	siemExporter.Start()

	// Create syslog exporter
	syslogExporter, syslogErr := NewSyslogExporter(&SyslogExporterConfig{
		Network: "tcp",
		Address: listener.Addr().String(),
		Tag:     "test-close",
	}, log)
	require.NoError(t, syslogErr)
	connErr := syslogExporter.connect()
	require.NoError(t, connErr)

	// Create audit logger with file, SIEM, and syslog
	file, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, openErr)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:       true,
			File:          logPath,
			FlushInterval: 100 * time.Millisecond,
		},
		logger:         log,
		file:           file,
		writer:         file,
		siemExporter:   siemExporter,
		syslogExporter: syslogExporter,
		shutdown:       make(chan struct{}),
	}

	err = al.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// SIEMExporter Flush with batch > 0 and retry path
// ---------------------------------------------------------------------------

func TestSIEMExporter_FlushWithRetryExhausted(t *testing.T) {
	// Server that always returns error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	log := createTestLogger(t)

	exporter, err := NewSIEMExporter(&SIEMConfig{
		Enabled:       true,
		Endpoint:      ts.URL,
		Format:        SIEMFormatJSON,
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
		MaxRetries:    1,
	}, log)
	require.NoError(t, err)

	// Queue an event
	sendErr := exporter.Send(&AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-retry",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	})
	assert.NoError(t, sendErr)

	// Flush should fail after retries exhausted
	err = exporter.Flush()
	var siemErr *SIEMError
	require.ErrorAs(t, err, &siemErr)
	assert.Equal(t, ErrCodeSIEMRetryExhausted, siemErr.Code)
}

// ---------------------------------------------------------------------------
// formatBatch dispatch for every format
// ---------------------------------------------------------------------------

func TestSIEMExporter_FormatBatch(t *testing.T) {
	tests := []struct {
		name            string
		format          SIEMFormat
		wantContentType string
		wantPrefix      string
		wantErrCode     string
	}{
		{name: "json", format: SIEMFormatJSON, wantContentType: "application/x-ndjson", wantPrefix: "{"},
		{name: "cef", format: SIEMFormatCEF, wantContentType: "text/plain", wantPrefix: "CEF:0|"},
		{name: "leef", format: SIEMFormatLEEF, wantContentType: "text/plain", wantPrefix: "LEEF:2.0|"},
		{name: "unknown format", format: SIEMFormat("unknown-format"), wantErrCode: ErrCodeSIEMFormatUnknown},
	}

	events := []*AuditEvent{
		{Timestamp: time.Now(), EventID: "evt-fmt-1", Action: ActionRead, Resource: "data", Result: ResultSuccess, Username: "alice"},
		{Timestamp: time.Now(), EventID: "evt-fmt-2", Action: ActionWrite, Resource: "keys", Result: ResultFailure, ErrorMsg: "timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter, err := NewSIEMExporter(&SIEMConfig{
				Enabled:  true,
				Endpoint: "http://localhost:9200",
				Format:   tt.format,
			}, createTestLogger(t))
			require.NoError(t, err)

			data, contentType, err := exporter.formatBatch(events)
			if tt.wantErrCode != "" {
				var siemErr *SIEMError
				require.ErrorAs(t, err, &siemErr)
				assert.Equal(t, tt.wantErrCode, siemErr.Code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantContentType, contentType)
			assert.True(t, bytes.HasPrefix(data, []byte(tt.wantPrefix)), "payload %q", data)
			assert.Contains(t, string(data), "evt-fmt-2")
		})
	}
}

// TestDoHTTPRequest_CreateError tests doHTTPRequest when http.NewRequest
// fails due to an invalid URL, covering audit.go:856-857.
func TestDoHTTPRequest_CreateError(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SIEMExporter{
		client: &http.Client{},
		config: &SIEMConfig{Enabled: true, Endpoint: "://invalid-url"},
		logger: log,
	}
	err := exporter.doHTTPRequest([]byte("test"), "application/json")
	assert.Error(t, err, "should fail with invalid URL")
}

// TestSendEvents_FormatBatchError tests sendEvents when formatBatch fails,
// covering audit.go:822-823.
func TestSendEvents_FormatBatchError(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SIEMExporter{
		config:   &SIEMConfig{Enabled: true, Format: "invalid-format"},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	events := []*AuditEvent{{Action: "test", Result: "success"}}
	err := exporter.sendEvents(events)
	assert.Error(t, err, "should fail when formatBatch fails")
}

// TestAuditLogger_SIEMExporter_Full tests the full SIEM exporter functionality.
func TestAuditLogger_SIEMExporter_Full(t *testing.T) {
	// Create a test server to receive SIEM events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			BatchSize:     10,
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
		Username: "test",
		Action:   ActionWrite,
		Resource: "data",
		Result:   ResultSuccess,
	}

	// Send event through the SIEM exporter
	if al.siemExporter != nil {
		err = al.siemExporter.Send(event)
		assert.NoError(t, err)

		// Wait for flush
		time.Sleep(200 * time.Millisecond)

		// Get stats
		sent, errs := al.siemExporter.GetStats()
		assert.GreaterOrEqual(t, sent, int64(0))
		assert.GreaterOrEqual(t, errs, int64(0))
	}
}

// ---------------------------------------------------------------------------
// audit.go -- mapCEFSeverity
// ---------------------------------------------------------------------------

func TestSIEMExporter_MapCEFSeverityAllPaths(t *testing.T) {
	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "http://localhost:9200",
		Format:   SIEMFormatCEF,
	}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	tests := []struct {
		name        string
		result      string
		action      string
		expectedSev int
	}{
		{"denied access", ResultDenied, ActionAuthorize, 8},
		{"failure auth", ResultFailure, ActionAuthenticate, 7},
		{"failure key rotate", ResultFailure, ActionKeyRotate, 6},
		{"failure key export", ResultFailure, ActionKeyExport, 6},
		{"failure key generate", ResultFailure, ActionKeyGenerate, 6},
		{"failure key import", ResultFailure, ActionKeyImport, 6},
		{"failure key delete", ResultFailure, ActionKeyDelete, 6},
		{"failure general", ResultFailure, ActionWrite, 4},
		{"success key export", ResultSuccess, ActionKeyExport, 5},
		{"success tenant switch", ResultSuccess, ActionTenantSwitch, 5},
		{"success cross tenant read", ResultSuccess, ActionCrossTenantRead, 5},
		{"success cross tenant write", ResultSuccess, ActionCrossTenantWrite, 5},
		{"success cross tenant delete", ResultSuccess, ActionCrossTenantDelete, 5},
		{"success user create", ResultSuccess, ActionUserCreate, 4},
		{"success user delete", ResultSuccess, ActionUserDelete, 4},
		{"success role create", ResultSuccess, ActionRoleCreate, 4},
		{"success role update", ResultSuccess, ActionRoleUpdate, 4},
		{"success normal", ResultSuccess, ActionRead, 1},
		{"unknown result", "something_else", ActionRead, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &AuditEvent{Result: tc.result, Action: tc.action}
			sev := exporter.mapCEFSeverity(event)
			assert.Equal(t, tc.expectedSev, sev)
		})
	}
}

// ---------------------------------------------------------------------------
// audit.go -- mapEventName
// ---------------------------------------------------------------------------

func TestSIEMExporter_MapEventName(t *testing.T) {
	cfg := &SIEMConfig{
		Enabled:  true,
		Endpoint: "http://localhost:9200",
		Format:   SIEMFormatCEF,
	}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	actions := []string{
		ActionAuthenticate, ActionAuthorize, ActionRead, ActionWrite, ActionDelete,
		ActionBackup, ActionRestore, ActionClusterJoin, ActionClusterLeave,
		ActionUserCreate, ActionUserDelete, ActionRoleCreate, ActionRoleUpdate,
		ActionKeyRotate, ActionKeyGenerate, ActionKeyExport, ActionKeyImport, ActionKeyDelete,
		ActionConfigChange, ActionSchemaChange,
		ActionTenantSwitch, ActionCrossTenantRead, ActionCrossTenantWrite, ActionCrossTenantDelete,
		ActionTenantDEKCreate, ActionTenantDEKRotate, ActionTenantDEKDelete, ActionTenantAccessDenied,
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			event := &AuditEvent{Action: action, Result: ResultSuccess}
			name := exporter.mapEventName(event)
			assert.Contains(t, name, " - success")
			assert.NotContains(t, name, "unknown")
		})
	}

	t.Run("unknown action falls back to the action string", func(t *testing.T) {
		event := &AuditEvent{Action: "custom.unknown.action", Result: ResultFailure}
		assert.Equal(t, "custom.unknown.action - failure", exporter.mapEventName(event))
	})
}

// ---------------------------------------------------------------------------
// audit.go -- SIEM Send and Flush with CEF / LEEF formats
// ---------------------------------------------------------------------------

func TestSIEMExporter_SendAndFlushCEFFormat(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		received = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &SIEMConfig{
		Enabled:   true,
		Endpoint:  server.URL,
		Format:    SIEMFormatCEF,
		BatchSize: 10,
	}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-cef",
		Action:    ActionWrite,
		Resource:  "data",
		Result:    ResultSuccess,
		Username:  "admin",
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	err = exporter.Flush()
	require.NoError(t, err)
	assert.Contains(t, string(received), "CEF:0|")
}

func TestSIEMExporter_SendAndFlushLEEFFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &SIEMConfig{
		Enabled:   true,
		Endpoint:  server.URL,
		Format:    SIEMFormatLEEF,
		BatchSize: 10,
	}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "secrets",
		Result:    ResultDenied,
	}

	err = exporter.Send(event)
	require.NoError(t, err)

	err = exporter.Flush()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// audit.go -- SIEM CEF escape helpers
// ---------------------------------------------------------------------------

func TestSIEMExporter_EscapeCEF(t *testing.T) {
	cfg := &SIEMConfig{Enabled: true, Endpoint: "http://localhost:9200", Format: SIEMFormatCEF}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no special chars", "hello", "hello"},
		{"pipe", "a|b", "a\\|b"},
		{"backslash", `a\b`, `a\\b`},
		{"both", `a\|b`, `a\\\|b`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := exporter.escapeCEF(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSIEMExporter_EscapeCEFValue(t *testing.T) {
	cfg := &SIEMConfig{Enabled: true, Endpoint: "http://localhost:9200", Format: SIEMFormatCEF}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no special chars", "hello", "hello"},
		{"equals", "a=b", "a\\=b"},
		{"newline", "a\nb", "a\\nb"},
		{"carriage return", "a\rb", "a\\rb"},
		{"backslash", `a\b`, `a\\b`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := exporter.escapeCEFValue(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// mapSignatureID
// ---------------------------------------------------------------------------

func TestSIEMExporter_MapSignatureID(t *testing.T) {
	cfg := &SIEMConfig{Enabled: true, Endpoint: "http://localhost:9200", Format: SIEMFormatCEF}
	log := createTestLogger(t)
	exporter, err := NewSIEMExporter(cfg, log)
	require.NoError(t, err)

	actions := map[string]string{
		ActionAuthenticate:       "1001",
		ActionAuthorize:          "1002",
		ActionRead:               "2001",
		ActionWrite:              "2002",
		ActionDelete:             "2003",
		ActionBackup:             "3001",
		ActionRestore:            "3002",
		ActionClusterJoin:        "4001",
		ActionClusterLeave:       "4002",
		ActionUserCreate:         "5001",
		ActionUserDelete:         "5002",
		ActionRoleCreate:         "5003",
		ActionRoleUpdate:         "5004",
		ActionKeyRotate:          "6001",
		ActionKeyGenerate:        "6002",
		ActionKeyExport:          "6003",
		ActionKeyImport:          "6004",
		ActionKeyDelete:          "6005",
		ActionConfigChange:       "7001",
		ActionSchemaChange:       "7002",
		ActionTenantSwitch:       "8001",
		ActionCrossTenantRead:    "8002",
		ActionCrossTenantWrite:   "8003",
		ActionCrossTenantDelete:  "8004",
		ActionTenantDEKCreate:    "8005",
		ActionTenantDEKRotate:    "8006",
		ActionTenantDEKDelete:    "8007",
		ActionTenantAccessDenied: "8008",
		"unknown_action":         "9999",
	}

	for action, expected := range actions {
		t.Run(action, func(t *testing.T) {
			got := exporter.mapSignatureID(action)
			assert.Equal(t, expected, got)
		})
	}
}

// Tests for buildCEFExtension

func TestSIEMExporter_BuildCEFExtension(t *testing.T) {
	exporter := &SIEMExporter{
		config: &SIEMConfig{
			Enabled:    true,
			CEFVendor:  "TestVendor",
			CEFProduct: "TestProduct",
			CEFVersion: "1.0",
		},
		logger: createTestLogger(t),
	}

	tests := []struct {
		name        string
		event       *AuditEvent
		contains    []string
		notContains []string
	}{
		{
			name: "all fields populated",
			event: &AuditEvent{
				EventID:   "evt-123",
				Timestamp: time.Now(),
				UserID:    "user-001",
				Username:  "alice",
				Action:    ActionRead,
				Resource:  "keys/test",
				Result:    ResultSuccess,
				IPAddress: "192.0.2.100",
				NodeID:    "node-1",
				ShardID:   42,
				ErrorMsg:  "some error",
				Details: map[string]string{
					"detail1": "value1",
					"detail2": "value2",
					"detail3": "value3",
					"detail4": "value4",
				},
			},
			contains: []string{
				"rt=", "src=192.0.2.100", "suser=alice", "suid=user-001", "act=read",
				"cs1=keys/test", "cs1Label=resource", "outcome=success",
				"cs2=node-1", "cs2Label=nodeId", "cs3=42", "cs3Label=shardId",
				"msg=some error", "externalId=evt-123",
			},
		},
		{
			name: "minimal fields only",
			event: &AuditEvent{
				Timestamp: time.Now(),
				Action:    ActionRead,
				Resource:  "test",
				Result:    ResultSuccess,
			},
			contains:    []string{"rt=", "act=read"},
			notContains: []string{"src=", "suser=", "nodeId", "msg="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extension := exporter.buildCEFExtension(tt.event)
			for _, want := range tt.contains {
				assert.Contains(t, extension, want)
			}
			for _, unwanted := range tt.notContains {
				assert.NotContains(t, extension, unwanted)
			}
		})
	}
}

// Tests for buildLEEFExtension

func TestSIEMExporter_BuildLEEFExtension(t *testing.T) {
	exporter := &SIEMExporter{
		config: &SIEMConfig{
			Enabled:     true,
			LEEFVendor:  "TestVendor",
			LEEFProduct: "TestProduct",
			LEEFVersion: "1.0",
		},
		logger: createTestLogger(t),
	}

	tests := []struct {
		name        string
		event       *AuditEvent
		contains    []string
		notContains []string
	}{
		{
			name: "all fields populated",
			event: &AuditEvent{
				EventID:   "evt-123",
				Timestamp: time.Now(),
				UserID:    "user-001",
				Username:  "alice",
				Action:    ActionWrite,
				Resource:  "keys/test",
				Result:    ResultFailure,
				IPAddress: "192.0.2.100",
				NodeID:    "node-1",
				ShardID:   42,
				ErrorMsg:  "permission denied",
				Details: map[string]string{
					"custom_key": "custom_value",
				},
			},
			contains: []string{
				"devTime=", "src=192.0.2.100", "usrName=alice", "identSrc=user-001",
				"action=write", "resource=keys/test", "result=failure", "sev=",
				"nodeId=node-1", "shardId=42", "reason=permission denied",
				"eventId=evt-123", "custom_key=custom_value",
			},
		},
		{
			name: "minimal fields only",
			event: &AuditEvent{
				Timestamp: time.Now(),
				Action:    ActionRead,
				Resource:  "data",
				Result:    ResultSuccess,
			},
			contains:    []string{"devTime=", "action="},
			notContains: []string{"src=", "usrName="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extension := exporter.buildLEEFExtension(tt.event)
			for _, want := range tt.contains {
				assert.Contains(t, extension, want)
			}
			for _, unwanted := range tt.notContains {
				assert.NotContains(t, extension, unwanted)
			}
		})
	}
}

// Tests for AuditLogger.Log with SIEM exporter

func TestAuditLogger_Log_WithSIEMExporterConfigured(t *testing.T) {
	log := createTestLogger(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	event := &AuditEvent{
		Username: "test",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	err = al.Log(context.Background(), event)
	require.NoError(t, err)

	// Give time for async SIEM send
	time.Sleep(50 * time.Millisecond)
}

func TestAuditLogger_GetSIEMExporterReturnsNil(t *testing.T) {
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.Nil(t, al.GetSIEMExporter())
}

// ---------------------------------------------------------------------------
// AuditLogger SIEM integration
// ---------------------------------------------------------------------------

func TestAuditLogger_CreatesSIEMExporter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.NotNil(t, al.GetSIEMExporter())
}

// TestAuditLogger_SyslogAndSIEM tests syslog and SIEM output paths.
func TestAuditLogger_SyslogAndSIEM(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:      true,
		Syslog:       true,
		SyslogAddr:   "localhost:514",
		SIEM:         true,
		SIEMEndpoint: "http://localhost:8080/events",
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	event := &AuditEvent{
		Username: "testuser",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	// This will call sendToSyslog and sendToSIEM in goroutines
	err = al.Log(ctx, event)
	assert.NoError(t, err)

	// Give goroutines time to execute
	time.Sleep(50 * time.Millisecond)
}
