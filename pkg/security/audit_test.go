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
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuditLogger(t *testing.T) {
	log := createTestLogger(t)

	tests := []struct {
		name    string
		config  *AuditConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "disabled audit logging",
			config: &AuditConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled without signing",
			config: &AuditConfig{
				Enabled:  true,
				SignLogs: false,
			},
			wantErr: false,
		},
		{
			name: "enabled with signing but no key",
			config: &AuditConfig{
				Enabled:  true,
				SignLogs: true,
			},
			wantErr: true,
		},
		{
			name: "enabled with signing and key",
			config: &AuditConfig{
				Enabled:  true,
				SignLogs: true,
				SignKey:  "test-signing-key",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al, err := NewAuditLogger(tt.config, log)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, al)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, al)
				if al != nil {
					al.Close()
				}
			}
		})
	}
}

func TestAuditLogger_Log(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled:  true,
		SignLogs: false,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	event := &AuditEvent{
		UserID:    "user-123",
		Username:  "alice",
		Action:    ActionRead,
		Resource:  "keys",
		Result:    ResultSuccess,
		IPAddress: "192.168.1.100",
		Details: map[string]string{
			"key": "/test/key",
		},
	}

	err = al.Log(context.Background(), event)
	assert.NoError(t, err)

	// Parse logged event
	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, event.UserID, logged.UserID)
	assert.Equal(t, event.Username, logged.Username)
	assert.Equal(t, event.Action, logged.Action)
	assert.Equal(t, event.Resource, logged.Resource)
	assert.Equal(t, event.Result, logged.Result)
	assert.NotEmpty(t, logged.EventID)
	assert.False(t, logged.Timestamp.IsZero())
}

func TestAuditLogger_LogWithSigning(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  "test-signing-key-12345",
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	event := &AuditEvent{
		UserID:    "user-123",
		Username:  "alice",
		Action:    ActionWrite,
		Resource:  "keys",
		Result:    ResultSuccess,
		IPAddress: "192.168.1.100",
	}

	err = al.Log(context.Background(), event)
	require.NoError(t, err)

	// Parse logged event
	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.NotEmpty(t, logged.Signature)

	// Verify signature
	assert.True(t, al.VerifySignature(&logged))

	// Tamper with event and verify it fails
	logged.Username = "bob"
	assert.False(t, al.VerifySignature(&logged))
}

func TestAuditLogger_LogAuthSuccess(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogAuthSuccess(context.Background(), "alice", "user-123", "192.168.1.100")
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, "alice", logged.Username)
	assert.Equal(t, ActionAuthenticate, logged.Action)
	assert.Equal(t, ResultSuccess, logged.Result)
}

func TestAuditLogger_LogAuthFailure(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogAuthFailure(context.Background(), "alice", "192.168.1.100", "invalid password")
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, "alice", logged.Username)
	assert.Equal(t, ActionAuthenticate, logged.Action)
	assert.Equal(t, ResultFailure, logged.Result)
	assert.Equal(t, "invalid password", logged.ErrorMsg)
}

func TestAuditLogger_LogAuthzDenied(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogAuthzDenied(context.Background(), "alice", "user-123", "cluster", "admin", "192.168.1.100")
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, "alice", logged.Username)
	assert.Equal(t, ActionAuthorize, logged.Action)
	assert.Equal(t, ResultDenied, logged.Result)
	assert.Equal(t, "cluster", logged.Resource)
}

func TestAuditLogger_LogDataAccess(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		success    bool
		wantResult string
	}{
		{name: "success", action: ActionRead, success: true, wantResult: ResultSuccess},
		{name: "failure", action: ActionWrite, success: false, wantResult: ResultFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al, err := NewAuditLogger(&AuditConfig{Enabled: true}, createTestLogger(t))
			require.NoError(t, err)
			defer al.Close()
			var buf bytes.Buffer
			al.writer = &buf

			err = al.LogDataAccess(context.Background(), "alice", "user-123", tt.action, "/test/key", "192.168.1.100", tt.success)
			require.NoError(t, err)

			var logged AuditEvent
			require.NoError(t, json.Unmarshal(buf.Bytes(), &logged))
			assert.Equal(t, "alice", logged.Username)
			assert.Equal(t, tt.action, logged.Action)
			assert.Equal(t, tt.wantResult, logged.Result)
			assert.Equal(t, "/test/key", logged.Details["key"])
		})
	}
}

func TestAuditLogger_LogClusterChange(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	details := map[string]string{
		"node_addr": "192.168.1.10:5000",
	}
	err = al.LogClusterChange(context.Background(), ActionClusterJoin, 1, details)
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, ActionClusterJoin, logged.Action)
	assert.Equal(t, "cluster", logged.Resource)
	assert.Equal(t, "1", logged.NodeID)
}

func TestAuditLogger_LogKeyRotation(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyRotation(context.Background(), 1, 2)
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, ActionKeyRotate, logged.Action)
	assert.Equal(t, "encryption", logged.Resource)
	assert.Equal(t, "1", logged.Details["old_version"])
	assert.Equal(t, "2", logged.Details["new_version"])
}

func TestAuditLogger_DisabledDoesNotLog(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled: false,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username: "alice",
		Action:   ActionRead,
		Resource: "keys",
		Result:   ResultSuccess,
	}

	err = al.Log(context.Background(), event)
	assert.NoError(t, err)
}

func TestAuditLogger_Flush(t *testing.T) {
	log := createTestLogger(t)

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled: true,
		File:    logFile,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username: "alice",
		Action:   ActionWrite,
		Resource: "keys",
		Result:   ResultSuccess,
	}

	err = al.Log(context.Background(), event)
	require.NoError(t, err)

	err = al.Flush()
	assert.NoError(t, err)

	// Verify file was written
	data, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestAuditLogger_ConcurrentWrites(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	// Concurrent writes
	const numGoroutines = 10
	const numEvents = 100

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numEvents; j++ {
				event := &AuditEvent{
					UserID:   "user-123",
					Username: "alice",
					Action:   ActionRead,
					Resource: "keys",
					Result:   ResultSuccess,
				}
				al.Log(context.Background(), event)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should have written all events without panics
	assert.True(t, buf.Len() > 0)
}

func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()
	id2 := generateEventID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "evt-")
}

// Tests for cross-tenant audit logging

func TestLogTenantSwitch(t *testing.T) {
	var buf bytes.Buffer
	al := &AuditLogger{
		writer: &buf,
		config: &AuditConfig{Enabled: true},
	}

	err := al.LogTenantSwitch(context.Background(),
		"admin-user",
		"user-123",
		"tenant-a",
		"tenant-b",
		"investigating support ticket #12345",
		"TICKET-12345",
		"192.168.1.100",
	)
	require.NoError(t, err)

	// Verify the event was logged
	var event AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
	require.NoError(t, err)

	assert.Equal(t, ActionTenantSwitch, event.Action)
	assert.Equal(t, "user-123", event.UserID)
	assert.Equal(t, "admin-user", event.Username)
	assert.Equal(t, "tenant", event.Resource)
	assert.Equal(t, ResultSuccess, event.Result)
	assert.Equal(t, "192.168.1.100", event.IPAddress)
	assert.Equal(t, "tenant-a", event.Details["original_tenant"])
	assert.Equal(t, "tenant-b", event.Details["target_tenant"])
	assert.Equal(t, "investigating support ticket #12345", event.Details["justification"])
	assert.Equal(t, "TICKET-12345", event.Details["assignment_id"])
}

func TestLogTenantSwitch_WithoutAssignmentID(t *testing.T) {
	var buf bytes.Buffer
	al := &AuditLogger{
		writer: &buf,
		config: &AuditConfig{Enabled: true},
	}

	err := al.LogTenantSwitch(context.Background(),
		"admin-user",
		"user-123",
		"tenant-a",
		"tenant-b",
		"justification",
		"", // no assignment ID
		"192.168.1.100",
	)
	require.NoError(t, err)

	var event AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
	require.NoError(t, err)

	// Assignment ID should not be in details when empty
	_, hasAssignmentID := event.Details["assignment_id"]
	assert.False(t, hasAssignmentID)
}

func TestLogCrossTenantRead(t *testing.T) {
	var buf bytes.Buffer
	al := &AuditLogger{
		writer: &buf,
		config: &AuditConfig{Enabled: true},
	}

	err := al.LogCrossTenantRead(context.Background(),
		"admin-user",
		"user-123",
		"tenant-a",
		"tenant-b",
		"keys/user-data",
		"support investigation",
		"192.168.1.100",
	)
	require.NoError(t, err)

	var event AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
	require.NoError(t, err)

	assert.Equal(t, ActionCrossTenantRead, event.Action)
	assert.Equal(t, "keys/user-data", event.Resource)
	assert.Equal(t, ResultSuccess, event.Result)
	assert.Equal(t, "tenant-a", event.Details["original_tenant"])
	assert.Equal(t, "tenant-b", event.Details["target_tenant"])
	assert.Equal(t, "support investigation", event.Details["justification"])
}

func TestLogCrossTenantWrite(t *testing.T) {
	var buf bytes.Buffer
	al := &AuditLogger{
		writer: &buf,
		config: &AuditConfig{Enabled: true},
	}

	err := al.LogCrossTenantWrite(context.Background(),
		"admin-user",
		"user-123",
		"tenant-a",
		"tenant-b",
		"keys/config",
		"migration operation",
		"192.168.1.100",
	)
	require.NoError(t, err)

	var event AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
	require.NoError(t, err)

	assert.Equal(t, ActionCrossTenantWrite, event.Action)
	assert.Equal(t, "keys/config", event.Resource)
	assert.Equal(t, ResultSuccess, event.Result)
}

func TestLogCrossTenantDelete(t *testing.T) {
	var buf bytes.Buffer
	al := &AuditLogger{
		writer: &buf,
		config: &AuditConfig{Enabled: true},
	}

	err := al.LogCrossTenantDelete(context.Background(),
		"admin-user",
		"user-123",
		"tenant-a",
		"tenant-b",
		"keys/old-data",
		"data cleanup",
		"192.168.1.100",
	)
	require.NoError(t, err)

	var event AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
	require.NoError(t, err)

	assert.Equal(t, ActionCrossTenantDelete, event.Action)
	assert.Equal(t, "keys/old-data", event.Resource)
	assert.Equal(t, ResultSuccess, event.Result)
}

func TestLogTenantAccessDenied(t *testing.T) {
	var buf bytes.Buffer
	al := &AuditLogger{
		writer: &buf,
		config: &AuditConfig{Enabled: true},
	}

	err := al.LogTenantAccessDenied(context.Background(),
		"regular-user",
		"user-456",
		"tenant-a",
		"tenant-b",
		"keys/secret-data",
		"192.168.1.100",
		"cross-tenant access not permitted",
	)
	require.NoError(t, err)

	var event AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
	require.NoError(t, err)

	assert.Equal(t, ActionTenantAccessDenied, event.Action)
	assert.Equal(t, "keys/secret-data", event.Resource)
	assert.Equal(t, ResultDenied, event.Result)
	assert.Equal(t, "cross-tenant access not permitted", event.ErrorMsg)
	assert.Equal(t, "tenant-a", event.Details["requesting_tenant"])
	assert.Equal(t, "tenant-b", event.Details["target_tenant"])
}

func TestLogTenantDEKOperation(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		success bool
		result  string
	}{
		{
			name:    "DEK create success",
			action:  ActionTenantDEKCreate,
			success: true,
			result:  ResultSuccess,
		},
		{
			name:    "DEK create failure",
			action:  ActionTenantDEKCreate,
			success: false,
			result:  ResultFailure,
		},
		{
			name:    "DEK rotate success",
			action:  ActionTenantDEKRotate,
			success: true,
			result:  ResultSuccess,
		},
		{
			name:    "DEK delete success",
			action:  ActionTenantDEKDelete,
			success: true,
			result:  ResultSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			al := &AuditLogger{
				writer: &buf,
				config: &AuditConfig{Enabled: true},
			}

			err := al.LogTenantDEKOperation(context.Background(),
				tt.action,
				"tenant-xyz",
				"admin-user",
				"user-123",
				"192.168.1.100",
				tt.success,
			)
			require.NoError(t, err)

			var event AuditEvent
			err = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event)
			require.NoError(t, err)

			assert.Equal(t, tt.action, event.Action)
			assert.Equal(t, "tenant.dek", event.Resource)
			assert.Equal(t, tt.result, event.Result)
			assert.Equal(t, "tenant-xyz", event.Details["tenant_id"])
		})
	}
}

func TestTenantAuditActionsConstants(t *testing.T) {
	// Verify the constant values are correct
	assert.Equal(t, "tenant.switch", ActionTenantSwitch)
	assert.Equal(t, "cross_tenant.read", ActionCrossTenantRead)
	assert.Equal(t, "cross_tenant.write", ActionCrossTenantWrite)
	assert.Equal(t, "cross_tenant.delete", ActionCrossTenantDelete)
	assert.Equal(t, "tenant.dek.create", ActionTenantDEKCreate)
	assert.Equal(t, "tenant.dek.rotate", ActionTenantDEKRotate)
	assert.Equal(t, "tenant.dek.delete", ActionTenantDEKDelete)
	assert.Equal(t, "tenant.access.denied", ActionTenantAccessDenied)
}

// SyslogExporter Tests

func TestNewSyslogExporter_NilConfig(t *testing.T) {
	log := createTestLogger(t)

	exporter, err := NewSyslogExporter(nil, log)
	assert.Nil(t, exporter)
	assert.Error(t, err)

	var syslogErr *SyslogExporterError
	assert.ErrorAs(t, err, &syslogErr)
	assert.Equal(t, "NewSyslogExporter", syslogErr.Op)
	assert.Contains(t, syslogErr.Reason, "config cannot be nil")
}

func TestNewSyslogExporter_DefaultsApplied(t *testing.T) {
	log := createTestLogger(t)

	// Create a UDP listener to accept the connection
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		// Tag and Hostname left empty to test defaults
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	// Verify defaults were applied
	assert.Equal(t, "quicbaas", cfg.Tag)
	assert.NotEmpty(t, cfg.Hostname)
}

func TestNewSyslogExporter_RemoteUDPConnection(t *testing.T) {
	log := createTestLogger(t)

	// Create a UDP listener
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test-app",
		Hostname: "test-host",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	assert.True(t, exporter.IsConnected())
	assert.True(t, exporter.isRemote)
}

func TestSyslogExporter_Export(t *testing.T) {
	log := createTestLogger(t)

	// Create a UDP listener to receive messages
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "quicbaas",
		Hostname: "test-host",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	// Create an audit event
	event := &AuditEvent{
		EventID:   "evt-12345",
		Timestamp: time.Now(),
		UserID:    "user-123",
		Username:  "alice",
		Action:    ActionRead,
		Resource:  "keys/test",
		Result:    ResultSuccess,
		IPAddress: "192.168.1.100",
		Details: map[string]string{
			"custom_field": "custom_value",
		},
	}

	// Start reading in background
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		listener.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := listener.ReadFrom(buf)
		if err == nil {
			done <- buf[:n]
		} else {
			done <- nil
		}
	}()

	// Export the event
	err = exporter.Export(event)
	require.NoError(t, err)

	// Wait for the message
	received := <-done
	require.NotNil(t, received, "Should have received syslog message")

	// Verify RFC 5424 format
	msg := string(received)
	assert.Contains(t, msg, "<")   // Priority
	assert.Contains(t, msg, ">1 ") // Version 1
	assert.Contains(t, msg, "test-host")
	assert.Contains(t, msg, "quicbaas")
	assert.Contains(t, msg, "evt-12345")
	assert.Contains(t, msg, "[audit@quicbaas")
	assert.Contains(t, msg, "action=\"read\"")
	assert.Contains(t, msg, "resource=\"keys/test\"")
	assert.Contains(t, msg, "result=\"success\"")
	assert.Contains(t, msg, "QuicBaaS Audit:")
}

func TestSyslogExporter_ExportNotConnected(t *testing.T) {
	log := createTestLogger(t)

	// Create a UDP listener
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	// Close and verify reconnection
	exporter.Close()
	assert.False(t, exporter.IsConnected())

	// Keep listener open for reconnection
	event := &AuditEvent{
		EventID:   "evt-test",
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// Should reconnect and export
	err = exporter.Export(event)
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())

	listener.Close()
	exporter.Close()
}

func TestSyslogExporter_MapEventToSeverity(t *testing.T) {
	log := createTestLogger(t)

	// Create a dummy exporter
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
		logger: log,
	}

	tests := []struct {
		name     string
		event    *AuditEvent
		expected SyslogSeverity
	}{
		{
			name: "success read operation",
			event: &AuditEvent{
				Action: ActionRead,
				Result: ResultSuccess,
			},
			expected: SyslogSeverityInformational,
		},
		{
			name: "success key rotation",
			event: &AuditEvent{
				Action: ActionKeyRotate,
				Result: ResultSuccess,
			},
			expected: SyslogSeverityNotice,
		},
		{
			name: "success user create",
			event: &AuditEvent{
				Action: ActionUserCreate,
				Result: ResultSuccess,
			},
			expected: SyslogSeverityNotice,
		},
		{
			name: "success config change",
			event: &AuditEvent{
				Action: ActionConfigChange,
				Result: ResultSuccess,
			},
			expected: SyslogSeverityNotice,
		},
		{
			name: "success tenant switch",
			event: &AuditEvent{
				Action: ActionTenantSwitch,
				Result: ResultSuccess,
			},
			expected: SyslogSeverityNotice,
		},
		{
			name: "failed authentication",
			event: &AuditEvent{
				Action: ActionAuthenticate,
				Result: ResultFailure,
			},
			expected: SyslogSeverityWarning,
		},
		{
			name: "failed general operation",
			event: &AuditEvent{
				Action: ActionRead,
				Result: ResultFailure,
			},
			expected: SyslogSeverityError,
		},
		{
			name: "denied authentication",
			event: &AuditEvent{
				Action: ActionAuthenticate,
				Result: ResultDenied,
			},
			expected: SyslogSeverityWarning,
		},
		{
			name: "denied tenant access",
			event: &AuditEvent{
				Action: ActionTenantAccessDenied,
				Result: ResultDenied,
			},
			expected: SyslogSeverityWarning,
		},
		{
			name: "denied general operation",
			event: &AuditEvent{
				Action: ActionRead,
				Result: ResultDenied,
			},
			expected: SyslogSeverityWarning,
		},
		{
			name: "unknown result",
			event: &AuditEvent{
				Action: ActionRead,
				Result: "unknown_result",
			},
			expected: SyslogSeverityInformational,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := exporter.mapEventToSeverity(tt.event)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestSyslogExporter_FormatRFC5424(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Facility: SyslogFacilityLocal0,
			Tag:      "quicbaas",
			Hostname: "test-host",
		},
		logger: log,
	}

	event := &AuditEvent{
		EventID:   "evt-12345",
		Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		UserID:    "user-123",
		Username:  "alice",
		Action:    ActionRead,
		Resource:  "keys/test",
		Result:    ResultSuccess,
		IPAddress: "192.168.1.100",
		NodeID:    "node-1",
		ShardID:   42,
	}

	message := "QuicBaaS Audit: action=read resource=keys/test result=success user=alice ip=192.168.1.100"
	formatted := exporter.formatRFC5424(event, SyslogSeverityInformational, message)

	// Verify RFC 5424 format components
	// Priority should be facility*8 + severity = 16*8 + 6 = 134
	assert.True(t, strings.HasPrefix(formatted, "<134>1 "), "Should have correct priority and version")
	assert.Contains(t, formatted, "2025-01-15T10:30:00")
	assert.Contains(t, formatted, "test-host")
	assert.Contains(t, formatted, "quicbaas")
	assert.Contains(t, formatted, "evt-12345")
	assert.Contains(t, formatted, "[audit@quicbaas")
	assert.Contains(t, formatted, "action=\"read\"")
	assert.Contains(t, formatted, "resource=\"keys/test\"")
	assert.Contains(t, formatted, "result=\"success\"")
	assert.Contains(t, formatted, "userID=\"user-123\"")
	assert.Contains(t, formatted, "username=\"alice\"")
	assert.Contains(t, formatted, "ipAddress=\"192.168.1.100\"")
	assert.Contains(t, formatted, "nodeID=\"node-1\"")
	assert.Contains(t, formatted, "shardID=\"42\"")
	assert.True(t, strings.HasSuffix(formatted, message+"\n"))
}

func TestSyslogExporter_FormatRFC5424_EmptyEventID(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Facility: SyslogFacilityLocal0,
			Tag:      "quicbaas",
			Hostname: "test-host",
		},
		logger: log,
	}

	event := &AuditEvent{
		EventID:   "", // Empty event ID
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	formatted := exporter.formatRFC5424(event, SyslogSeverityInformational, "test message")

	// Empty EventID should be replaced with "-"
	parts := strings.Fields(formatted)
	// Format: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID SD MSG
	// MSGID is at index 5 (0-based)
	assert.True(t, len(parts) > 5)
	assert.Equal(t, "-", parts[5], "Empty EventID should be NILVALUE '-'")
}

func TestSyslogExporter_BuildStructuredData(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
		logger: log,
	}

	event := &AuditEvent{
		UserID:    "user-123",
		Username:  "alice",
		Action:    ActionWrite,
		Resource:  "keys/data",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
		NodeID:    "node-2",
		ShardID:   10,
		ErrorMsg:  "",
		Details: map[string]string{
			"key": "/test/key",
		},
	}

	sd := exporter.buildStructuredData(event)

	assert.Contains(t, sd, "[audit@quicbaas")
	assert.Contains(t, sd, "action=\"write\"")
	assert.Contains(t, sd, "resource=\"keys/data\"")
	assert.Contains(t, sd, "result=\"success\"")
	assert.Contains(t, sd, "userID=\"user-123\"")
	assert.Contains(t, sd, "username=\"alice\"")
	assert.Contains(t, sd, "ipAddress=\"10.0.0.1\"")
	assert.Contains(t, sd, "nodeID=\"node-2\"")
	assert.Contains(t, sd, "shardID=\"10\"")
	assert.Contains(t, sd, "key=\"/test/key\"")
	assert.NotContains(t, sd, "errorMsg=") // Empty error should not be included
}

func TestSyslogExporter_EscapeSDParam(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escaping needed",
			input:    "simple value",
			expected: "simple value",
		},
		{
			name:     "escape backslash",
			input:    "path\\to\\file",
			expected: "path\\\\to\\\\file",
		},
		{
			name:     "escape quote",
			input:    `value with "quotes"`,
			expected: `value with \"quotes\"`,
		},
		{
			name:     "escape bracket",
			input:    "value with ]bracket",
			expected: `value with \]bracket`,
		},
		{
			name:     "escape all special chars",
			input:    `test\"]all`,
			expected: `test\\\"\]all`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeSDParam(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyslogExporter_FormatMessage(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
		logger: log,
	}

	tests := []struct {
		name        string
		event       *AuditEvent
		contains    []string
		notContains []string
	}{
		{
			name: "full event",
			event: &AuditEvent{
				Action:    ActionRead,
				Resource:  "keys/test",
				Result:    ResultSuccess,
				Username:  "alice",
				IPAddress: "192.168.1.100",
				ErrorMsg:  "",
			},
			contains: []string{
				"QuicBaaS Audit:",
				"action=read",
				"resource=keys/test",
				"result=success",
				"user=alice",
				"ip=192.168.1.100",
			},
		},
		{
			name: "with error message",
			event: &AuditEvent{
				Action:    ActionWrite,
				Resource:  "keys/data",
				Result:    ResultFailure,
				Username:  "bob",
				IPAddress: "10.0.0.1",
				ErrorMsg:  "permission denied",
			},
			contains: []string{
				"QuicBaaS Audit:",
				"action=write",
				"resource=keys/data",
				"result=failure",
				"user=bob",
				"ip=10.0.0.1",
				"error=permission denied",
			},
		},
		{
			name: "minimal event",
			event: &AuditEvent{
				Action:   ActionRead,
				Resource: "test",
				Result:   ResultSuccess,
			},
			contains: []string{
				"QuicBaaS Audit:",
				"action=read",
				"resource=test",
				"result=success",
			},
			notContains: []string{"user="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := exporter.formatMessage(tt.event)
			for _, expected := range tt.contains {
				assert.Contains(t, msg, expected)
			}
			for _, unexpected := range tt.notContains {
				assert.NotContains(t, msg, unexpected)
			}
		})
	}
}

func TestSyslogExporter_Close(t *testing.T) {
	log := createTestLogger(t)

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	assert.True(t, exporter.IsConnected())

	err = exporter.Close()
	assert.NoError(t, err)
	assert.False(t, exporter.IsConnected())
}

func TestSyslogExporter_Reconnect(t *testing.T) {
	log := createTestLogger(t)

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	// Close connection
	err = exporter.Close()
	require.NoError(t, err)
	assert.False(t, exporter.IsConnected())

	// Reconnect
	err = exporter.Reconnect()
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())

	exporter.Close()
}

func TestSyslogExporter_ConcurrentExport(t *testing.T) {
	log := createTestLogger(t)

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := &SyslogExporterConfig{
		Address:  listener.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	const numGoroutines = 10
	const numEvents = 50

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numEvents)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				event := &AuditEvent{
					EventID:   "evt-concurrent",
					Timestamp: time.Now(),
					Action:    ActionRead,
					Resource:  "test",
					Result:    ResultSuccess,
					Username:  "user",
				}
				if err := exporter.Export(event); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Should have no errors
	errorCount := 0
	for err := range errors {
		t.Logf("Concurrent export error: %v", err)
		errorCount++
	}
	assert.Zero(t, errorCount, "Should have no errors during concurrent export")
}

func TestSyslogFacilityConstants(t *testing.T) {
	assert.Equal(t, SyslogFacility(0), SyslogFacilityKern)
	assert.Equal(t, SyslogFacility(1), SyslogFacilityUser)
	assert.Equal(t, SyslogFacility(2), SyslogFacilityMail)
	assert.Equal(t, SyslogFacility(3), SyslogFacilityDaemon)
	assert.Equal(t, SyslogFacility(4), SyslogFacilityAuth)
	assert.Equal(t, SyslogFacility(5), SyslogFacilitySyslog)
	assert.Equal(t, SyslogFacility(6), SyslogFacilityLPR)
	assert.Equal(t, SyslogFacility(7), SyslogFacilityNews)
	assert.Equal(t, SyslogFacility(8), SyslogFacilityUUCP)
	assert.Equal(t, SyslogFacility(9), SyslogFacilityCron)
	assert.Equal(t, SyslogFacility(10), SyslogFacilityAuthPriv)
	assert.Equal(t, SyslogFacility(11), SyslogFacilityFTP)
	assert.Equal(t, SyslogFacility(16), SyslogFacilityLocal0)
	assert.Equal(t, SyslogFacility(17), SyslogFacilityLocal1)
	assert.Equal(t, SyslogFacility(18), SyslogFacilityLocal2)
	assert.Equal(t, SyslogFacility(19), SyslogFacilityLocal3)
	assert.Equal(t, SyslogFacility(20), SyslogFacilityLocal4)
	assert.Equal(t, SyslogFacility(21), SyslogFacilityLocal5)
	assert.Equal(t, SyslogFacility(22), SyslogFacilityLocal6)
	assert.Equal(t, SyslogFacility(23), SyslogFacilityLocal7)
}

func TestSyslogSeverityConstants(t *testing.T) {
	assert.Equal(t, SyslogSeverity(0), SyslogSeverityEmergency)
	assert.Equal(t, SyslogSeverity(1), SyslogSeverityAlert)
	assert.Equal(t, SyslogSeverity(2), SyslogSeverityCritical)
	assert.Equal(t, SyslogSeverity(3), SyslogSeverityError)
	assert.Equal(t, SyslogSeverity(4), SyslogSeverityWarning)
	assert.Equal(t, SyslogSeverity(5), SyslogSeverityNotice)
	assert.Equal(t, SyslogSeverity(6), SyslogSeverityInformational)
	assert.Equal(t, SyslogSeverity(7), SyslogSeverityDebug)
}

func TestSyslogExporterError_Error(t *testing.T) {
	err := &SyslogExporterError{
		Op:     "TestOperation",
		Reason: "test reason",
	}

	assert.Equal(t, "syslog exporter error: TestOperation: test reason", err.Error())
}

func TestSyslogConnectionError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *SyslogConnectionError
		expected string
	}{
		{
			name: "local connection error",
			err: &SyslogConnectionError{
				Network: "",
				Err:     net.UnknownNetworkError("test"),
			},
			expected: "failed to connect to local syslog: unknown network test",
		},
		{
			name: "remote connection error",
			err: &SyslogConnectionError{
				Address: "192.168.1.100:514",
				Network: "tcp",
				Err:     net.UnknownNetworkError("test"),
			},
			expected: "failed to connect to syslog at tcp://192.168.1.100:514: unknown network test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestSyslogConnectionError_Unwrap(t *testing.T) {
	originalErr := net.UnknownNetworkError("test")
	err := &SyslogConnectionError{
		Address: "test",
		Network: "tcp",
		Err:     originalErr,
	}

	assert.Equal(t, originalErr, err.Unwrap())
}

func TestSyslogWriteError_Error(t *testing.T) {
	originalErr := net.UnknownNetworkError("write failed")
	err := &SyslogWriteError{Err: originalErr}

	assert.Equal(t, "failed to write to syslog: unknown network write failed", err.Error())
}

func TestSyslogWriteError_Unwrap(t *testing.T) {
	originalErr := net.UnknownNetworkError("test")
	err := &SyslogWriteError{Err: originalErr}

	assert.Equal(t, originalErr, err.Unwrap())
}

func TestSyslogExporter_SendLocalNotConnected(t *testing.T) {
	exporter := &SyslogExporter{
		config:    &SyslogExporterConfig{},
		localConn: nil,
	}

	err := exporter.sendLocal(SyslogSeverityInformational, "test message")
	assert.Error(t, err)

	var syslogErr *SyslogExporterError
	assert.ErrorAs(t, err, &syslogErr)
	assert.Equal(t, "sendLocal", syslogErr.Op)
	assert.Contains(t, syslogErr.Reason, "local connection not established")
}

func TestSyslogExporter_SendRemoteNotConnected(t *testing.T) {
	exporter := &SyslogExporter{
		config:     &SyslogExporterConfig{},
		remoteConn: nil,
	}

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err := exporter.sendRemote(event, SyslogSeverityInformational, "test message")
	assert.Error(t, err)

	var syslogErr *SyslogExporterError
	assert.ErrorAs(t, err, &syslogErr)
	assert.Equal(t, "sendRemote", syslogErr.Op)
	assert.Contains(t, syslogErr.Reason, "remote connection not established")
}

func TestSyslogExporter_PriorityCalculation(t *testing.T) {
	log := createTestLogger(t)

	tests := []struct {
		name             string
		facility         SyslogFacility
		severity         SyslogSeverity
		expectedPriority int
	}{
		{
			name:             "kern emergency",
			facility:         SyslogFacilityKern,
			severity:         SyslogSeverityEmergency,
			expectedPriority: 0*8 + 0, // 0
		},
		{
			name:             "user info",
			facility:         SyslogFacilityUser,
			severity:         SyslogSeverityInformational,
			expectedPriority: 1*8 + 6, // 14
		},
		{
			name:             "local0 warning",
			facility:         SyslogFacilityLocal0,
			severity:         SyslogSeverityWarning,
			expectedPriority: 16*8 + 4, // 132
		},
		{
			name:             "auth error",
			facility:         SyslogFacilityAuth,
			severity:         SyslogSeverityError,
			expectedPriority: 4*8 + 3, // 35
		},
		{
			name:             "local7 debug",
			facility:         SyslogFacilityLocal7,
			severity:         SyslogSeverityDebug,
			expectedPriority: 23*8 + 7, // 191
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := &SyslogExporter{
				config: &SyslogExporterConfig{
					Facility: tt.facility,
					Hostname: "test",
					Tag:      "test",
				},
				logger: log,
			}

			event := &AuditEvent{
				EventID:   "test",
				Timestamp: time.Now(),
				Action:    ActionRead,
				Resource:  "test",
				Result:    ResultSuccess,
			}

			formatted := exporter.formatRFC5424(event, tt.severity, "test")
			expectedPrefix := "<" + string(rune('0'+tt.expectedPriority/100)) + string(rune('0'+(tt.expectedPriority%100)/10)) + string(rune('0'+tt.expectedPriority%10))
			if tt.expectedPriority < 10 {
				expectedPrefix = "<" + string(rune('0'+tt.expectedPriority))
			} else if tt.expectedPriority < 100 {
				expectedPrefix = "<" + string(rune('0'+tt.expectedPriority/10)) + string(rune('0'+tt.expectedPriority%10))
			}

			assert.True(t, strings.HasPrefix(formatted, expectedPrefix+">"),
				"Expected priority %d, got message: %s", tt.expectedPriority, formatted[:20])
		})
	}
}

// Test functions

// TestAuditLogger_PlaceholderMethods tests placeholder methods for future implementation
// TestAuditLogger_AdditionalOpenLogFileCases tests more openLogFile scenarios
// TestAuditLogger_SendToSyslogIntegration tests the sendToSyslog function
// TestAuditLogger_OpenLogFileWithSubdirectory tests file opening with nested dirs
func TestAuditLogger_OpenLogFileWithSubdirectory(t *testing.T) {
	logger := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "level1", "level2", "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
	}

	al := &AuditLogger{
		config:   cfg,
		logger:   logger,
		shutdown: make(chan struct{}),
	}

	// This should create the nested directory structure
	err := al.openLogFile()
	assert.NoError(t, err)
	assert.NotNil(t, al.file)
	al.file.Close()

	// Verify nested directories were created
	_, err = os.Stat(filepath.Dir(logFile))
	assert.NoError(t, err)
}

// TestAuditLogger_VerifySignatureVariants tests signature verification variants
func TestAuditLogger_VerifySignatureVariants(t *testing.T) {
	const signKey = "test-key-112bits!"

	tests := []struct {
		name     string
		signLogs bool
		// logFirst signs the event through Log before mutate runs.
		logFirst bool
		mutate   func(event *AuditEvent)
		// dropKey clears the signing key, as a logger built by other means.
		dropKey bool
		want    bool
	}{
		{
			name:     "signing disabled accepts any event",
			signLogs: false,
			mutate:   func(event *AuditEvent) { event.Signature = "" },
			want:     true,
		},
		{
			name:     "unsigned event with signing enabled fails closed",
			signLogs: true,
			mutate:   func(event *AuditEvent) { event.Signature = "" },
			want:     false,
		},
		{
			name:     "wrong signature",
			signLogs: true,
			mutate:   func(event *AuditEvent) { event.Signature = "invalid-signature" },
			want:     false,
		},
		{
			name:     "signature from Log verifies",
			signLogs: true,
			logFirst: true,
			want:     true,
		},
		{
			name:     "tampered field after signing",
			signLogs: true,
			logFirst: true,
			mutate:   func(event *AuditEvent) { event.Username = "attacker" },
			want:     false,
		},
		{
			name:     "missing signing key fails closed",
			signLogs: true,
			mutate:   func(event *AuditEvent) { event.Signature = "some-signature" },
			dropKey:  true,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AuditConfig{Enabled: true, SignLogs: tt.signLogs}
			if tt.signLogs {
				cfg.SignKey = signKey
			}

			al, err := NewAuditLogger(cfg, createTestLogger(t))
			require.NoError(t, err)
			defer al.Close()
			al.writer = &bytes.Buffer{}

			event := &AuditEvent{
				Timestamp: time.Now(),
				Username:  "test",
				Action:    ActionRead,
				Resource:  "test",
				Result:    ResultSuccess,
				IPAddress: "127.0.0.1",
			}
			if tt.logFirst {
				require.NoError(t, al.Log(context.Background(), event))
				require.NotEmpty(t, event.Signature)
			}
			if tt.mutate != nil {
				tt.mutate(event)
			}
			if tt.dropKey {
				al.signKey = nil
			}

			assert.Equal(t, tt.want, al.VerifySignature(event))
		})
	}
}

// ---------------------------------------------------------------------------
// compressFileAsync (audit.go:2014 -- 0.0% covered)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// compressFile (audit.go:2022 -- 62.9% covered)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// cleanupOldBackups (audit.go:2112 -- 0.0% covered)
// ---------------------------------------------------------------------------

func TestCleanupOldBackups(t *testing.T) {
	// Backups are listed oldest first; each gets a modification time one
	// hour after the previous one.
	backups := []string{
		"audit.log.20250101-000000",
		"audit.log.20250102-000000.gz",
		"audit.log.20250103-000000",
	}
	unrelated := []string{"audit.log", "audit.log.notatimestamp", "other.log.20250101-000000"}

	tests := []struct {
		name       string
		maxBackups int
		noFile     bool
		missingDir bool
		wantKept   []string
	}{
		{name: "zero max backups keeps everything", maxBackups: 0, wantKept: backups},
		{name: "no file configured keeps everything", maxBackups: 1, noFile: true, wantKept: backups},
		{name: "within the limit keeps everything", maxBackups: 3, wantKept: backups},
		{name: "prunes the oldest beyond the limit", maxBackups: 1, wantKept: backups[2:]},
		{name: "unreadable directory is logged and ignored", maxBackups: 1, missingDir: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			for i, name := range append(append([]string{}, backups...), unrelated...) {
				path := filepath.Join(dir, name)
				require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
				modTime := base.Add(time.Duration(i) * time.Hour)
				require.NoError(t, os.Chtimes(path, modTime, modTime))
			}

			cfg := &AuditConfig{Enabled: true, File: filepath.Join(dir, "audit.log"), MaxBackups: tt.maxBackups}
			if tt.noFile {
				cfg.File = ""
			}
			if tt.missingDir {
				cfg.File = filepath.Join(dir, "missing", "audit.log")
			}
			al := &AuditLogger{config: cfg, logger: createTestLogger(t)}

			al.cleanupOldBackups()

			for _, name := range unrelated {
				assert.FileExists(t, filepath.Join(dir, name), "non-backup files are never removed")
			}
			if tt.missingDir {
				return
			}
			for _, name := range backups {
				path := filepath.Join(dir, name)
				if slices.Contains(tt.wantKept, name) {
					assert.FileExists(t, path)
				} else {
					assert.NoFileExists(t, path)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sendLocal (audit.go:341 -- 11.8% covered)
// These tests need a real syslog.Writer mock via TCP listener
// ---------------------------------------------------------------------------

func TestSyslogExporter_SendRemoteWriteError(t *testing.T) {
	// Create a TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "quicbaas-test",
		Hostname: "testhost",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	// Close the listener to make the next write fail
	listener.Close()
	exporter.remoteConn.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-test-fail",
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err = exporter.sendRemote(event, SyslogSeverityInformational, "test message")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// connect (audit.go:283 -- 60.0% covered, remote branch)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// closeUnlocked (audit.go:559 -- 81.8% covered)
// ---------------------------------------------------------------------------

func TestSyslogExporter_CloseUnlockedWithBothConns(t *testing.T) {
	// Create TCP listener for remote
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
	}()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
		Hostname: "testhost",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())

	err = exporter.Close()
	require.NoError(t, err)
	assert.False(t, exporter.IsConnected())

	// Double close should be safe
	err = exporter.Close()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// rotateIfNeeded (audit.go:1970 -- 82.4% covered)
// ---------------------------------------------------------------------------

// TestRotateIfNeeded_AfterCloseStartsNothing pins the guarantee that makes
// Close's rotateWG.Wait sound.
//
// Wait only orders correctly if a positive Add on a zero counter
// happens-before it. The rotation loop commits to rotating the moment it
// selects ticker.C, and closing al.shutdown after that does not call it back --
// so without a barrier Close could Wait on a zero counter, return, and only
// then have rotateIfNeeded spawn the compression it was meant to wait for.
//
// The barrier is the closing flag, set by Close under mu and read here under
// mu. This asserts its observable consequence directly: once Close has
// returned, a rotation that would otherwise have fired starts no background
// work and creates no files. That is deterministic, unlike the interleaving
// itself.
func TestRotateIfNeeded_AfterCloseStartsNothing(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
		SignLogs:   false,
		Compress:   true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Past MaxSize, so rotateIfNeeded would rotate if it were allowed to.
	ctx := context.Background()
	bigData := make([]byte, 1024)
	for i := range bigData {
		bigData[i] = byte('A' + (i % 26))
	}
	for i := 0; i < 1200; i++ {
		require.NoError(t, al.Log(ctx, &AuditEvent{
			Username: "test",
			Action:   ActionWrite,
			Resource: string(bigData),
			Result:   ResultSuccess,
		}))
	}

	require.NoError(t, al.Close())

	// Hand the logger an OPEN file again before attempting the rotation.
	//
	// This is what makes the assertion mean something. In the interleaving
	// being guarded against, the rotation runs while the file is still open --
	// Close does not close it until after the Wait -- so it rotates
	// successfully and spawns its goroutines. Calling rotateIfNeeded on the
	// closed handle instead would fail early at Stat, and the test would pass
	// whether or not the closing flag existed: right answer, wrong reason.
	reopened, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	al.mu.Lock()
	al.file = reopened
	al.mu.Unlock()
	t.Cleanup(func() { _ = reopened.Close() })

	before, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	// The call the ticker goroutine would have made after Close returned. The
	// file is over MaxSize and open, so the closing flag is now the only thing
	// standing between this and a rename plus two background goroutines.
	al.mu.Lock()
	rotErr := al.rotateIfNeeded()
	al.mu.Unlock()
	require.NoError(t, rotErr, "a rotation after Close must be a no-op, not an error")

	after, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after),
		"rotation after Close must create no files: it would mean background work started with nobody waiting for it")
}

// ---------------------------------------------------------------------------
// VerifySignature (audit.go:1903 -- 83.3% covered)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// NewAuditLogger defaults (audit.go:1392 -- 97.8%)
// ---------------------------------------------------------------------------

func TestNewAuditLogger_NilConfig(t *testing.T) {
	log := createTestLogger(t)
	_, err := NewAuditLogger(nil, log)
	require.Error(t, err)
	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
	assert.Equal(t, ErrCodeAuditConfigNil, auditErr.Code)
}

func TestNewAuditLogger_DefaultValues(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		// Leave MaxSize, MaxBackups, BufferSize, FlushInterval as zero
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.Equal(t, int64(100), cfg.MaxSize)
	assert.Equal(t, 10, cfg.MaxBackups)
	assert.Equal(t, 1024, cfg.BufferSize)
	assert.Equal(t, 5*time.Second, cfg.FlushInterval)
}

// ---------------------------------------------------------------------------
// Log (audit.go:1508 -- 86.2% covered)
// ---------------------------------------------------------------------------

func TestLog_DisabledLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: false,
		File:    logFile,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.Log(context.Background(), &AuditEvent{
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	})
	require.NoError(t, err) // Should be no-op
}

// ---------------------------------------------------------------------------
// sendToSyslog (audit.go:2145 -- 73.3% covered)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Export (audit.go:321 -- 90% covered)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Error type coverage
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// NewSyslogExporter defaults (audit.go:251 -- 84.6%)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Log with signed events (audit.go:1508 -- covering sign + marshal paths)
// ---------------------------------------------------------------------------

func TestLog_WithSignedEvents(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		MaxSize:  100,
		SignLogs: true,
		SignKey:  "test-signing-key-for-hmac",
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	event := &AuditEvent{
		Username:  "admin",
		UserID:    "usr-123",
		Action:    ActionWrite,
		Resource:  "database/users",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
		NodeID:    "node-1",
		ShardID:   42,
		Details:   map[string]string{"operation": "insert"},
	}

	err = al.Log(ctx, event)
	require.NoError(t, err)

	// Event should have timestamp, event ID, and signature set
	assert.False(t, event.Timestamp.IsZero())
	assert.NotEmpty(t, event.EventID)
	assert.NotEmpty(t, event.Signature)
}

func TestLog_MultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		MaxSize:  100,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	for i := 0; i < 50; i++ {
		err := al.Log(ctx, &AuditEvent{
			Action:   ActionRead,
			Resource: "test",
			Result:   ResultSuccess,
		})
		require.NoError(t, err)
	}

	// Verify file was written
	info, err := os.Stat(logFile)
	require.NoError(t, err)
	assert.True(t, info.Size() > 0)
}

// ---------------------------------------------------------------------------
// openLogFile (audit.go:1917 -- 88.9%)
// ---------------------------------------------------------------------------

func TestNewAuditLogger_WithDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "subdir", "deep", "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.FileExists(t, logFile)
}

// ---------------------------------------------------------------------------
// sendLocal - cover all severity dispatch paths via a real local syslog mock
// We connect to a TCP listener and exercise the remote path since local syslog
// requires a real system syslog daemon. But we can also test sendLocal nil-conn
// error path and default severity branch.
// ---------------------------------------------------------------------------

func TestSyslogExporter_ConnectLocalUnixDialError(t *testing.T) {
	// Test the local unix dial path that fails with bad address
	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Network: "unix",
		Address: "/nonexistent/path/to/syslog.sock",
		Tag:     "test",
	}

	// NewSyslogExporter calls connect() internally, which should fail
	exporter, err := NewSyslogExporter(cfg, log)
	assert.Error(t, err)
	assert.Nil(t, exporter)
	var connErr *SyslogConnectionError
	assert.ErrorAs(t, err, &connErr)
}

// ---------------------------------------------------------------------------
// compressFile - cover gzWriter.Close error path and dstFile.Close error path
// We use a file with a lot of content to ensure copy path is exercised
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Log function: exercise syslog and syslog exporter paths together
// ---------------------------------------------------------------------------

func TestLog_WithSyslogAndSyslogExporter(t *testing.T) {
	// Start a TCP listener for syslog exporter
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

	// Create syslog exporter
	syslogExporter, syslogErr := NewSyslogExporter(&SyslogExporterConfig{
		Network: "tcp",
		Address: listener.Addr().String(),
		Tag:     "test-log",
	}, log)
	require.NoError(t, syslogErr)
	connErr := syslogExporter.connect()
	require.NoError(t, connErr)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:       true,
			Syslog:        true,
			SyslogAddr:    "127.0.0.1:0",
			FlushInterval: 1 * time.Second,
		},
		logger:         log,
		writer:         &bytes.Buffer{},
		syslogExporter: syslogExporter,
		shutdown:       make(chan struct{}),
	}
	defer close(al.shutdown)

	event := &AuditEvent{
		Action:   ActionWrite,
		Resource: "test-resource",
		Result:   ResultSuccess,
		UserID:   "user-syslog",
		Username: "sysloguser",
	}

	err = al.Log(context.Background(), event)
	assert.NoError(t, err)
	// Event ID should have been auto-generated
	assert.NotEmpty(t, event.EventID)
}

// ---------------------------------------------------------------------------
// closeUnlocked - cover both localConn and remoteConn error paths
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// NewAuditLogger - cover SyslogEnabled path that continues with warning
// ---------------------------------------------------------------------------

func TestNewAuditLogger_WithSignLogsNoKey(t *testing.T) {
	log := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  "", // Missing sign key
	}

	al, err := NewAuditLogger(cfg, log)
	assert.Error(t, err)
	assert.Nil(t, al)

	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
	assert.Equal(t, ErrCodeAuditSignKeyRequired, auditErr.Code)
}

// ---------------------------------------------------------------------------
// checkRotation - test that it returns immediately when File is empty
// ---------------------------------------------------------------------------

func TestCheckRotation_EmptyFilePath(t *testing.T) {
	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			File: "",
		},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	// checkRotation should return immediately since File is empty
	// We call it synchronously (not in a goroutine) to test the early return
	done := make(chan struct{})
	go func() {
		al.checkRotation()
		close(done)
	}()

	// It should return immediately because config.File is empty
	select {
	case <-done:
		// Good - returned immediately
	case <-time.After(2 * time.Second):
		close(al.shutdown) // Force stop
		t.Fatal("checkRotation did not return for empty file path")
	}
}

func TestCheckRotation_ShutdownSignal(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "rotation_test.log")

	err := os.WriteFile(logPath, []byte("test data"), 0o600)
	require.NoError(t, err)

	file, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, openErr)
	defer file.Close()

	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			File:    logPath,
			MaxSize: 100, // 100 MB - won't trigger rotation
		},
		logger:   log,
		file:     file,
		shutdown: make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		al.checkRotation()
		close(done)
	}()

	// Send shutdown signal - checkRotation should exit
	close(al.shutdown)

	select {
	case <-done:
		// Good
	case <-time.After(5 * time.Second):
		t.Fatal("checkRotation did not respond to shutdown")
	}
}

// ---------------------------------------------------------------------------
// rotateIfNeeded - cover the rotation path when file exceeds max size
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// sendToSyslog legacy path - cover the case where SyslogAddr is set
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// sendRemote - exercise all code paths with varying event fields
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// openLogFile - cover the MkdirAll error path (read-only parent)
// ---------------------------------------------------------------------------

// openLogFile does MkdirAll on the file's parent before opening it. A regular
// file standing in as a path component makes that MkdirAll return ENOTDIR for
// every uid; a mode-0500 parent only stops an unprivileged one
// (testing-guidelines.md, Fault Injection).
func TestOpenLogFile_ParentIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	err := os.WriteFile(readOnlyDir, []byte("blocking"), 0o644)
	require.NoError(t, err)

	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			File: filepath.Join(readOnlyDir, "subdir", "audit.log"),
		},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	err = al.openLogFile()
	assert.Error(t, err)

	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
}

// ---------------------------------------------------------------------------
// NewSyslogExporter - connect path testing with empty network
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// VerifySignature - cover the signEvent error path
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Log - cover sign event error path
// ---------------------------------------------------------------------------

func TestLog_SignEventError(t *testing.T) {
	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:       true,
			SignLogs:      true,
			FlushInterval: 1 * time.Second,
		},
		logger:   log,
		signKey:  nil, // no usable signing key
		writer:   &bytes.Buffer{},
		shutdown: make(chan struct{}),
	}
	defer close(al.shutdown)

	event := &AuditEvent{
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	err := al.Log(context.Background(), event)
	require.Error(t, err)

	var auditErr *AuditError
	require.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditEventSign, auditErr.Code)
	assert.Empty(t, event.Signature)
}

// ---------------------------------------------------------------------------
// compressFileAsync - cover error path that logs but doesn't return error
// ---------------------------------------------------------------------------

func TestLog_WriteError(t *testing.T) {
	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:       true,
			FlushInterval: 1 * time.Second,
		},
		logger:   log,
		writer:   &errorWriter{},
		shutdown: make(chan struct{}),
	}
	defer close(al.shutdown)

	event := &AuditEvent{
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	err := al.Log(context.Background(), event)
	assert.Error(t, err)

	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
	assert.Equal(t, ErrCodeAuditEventWrite, auditErr.Code)
}

func TestLog_WithSignLogs_Success(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	log := createTestLogger(t)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, SignLogs: true, SignKey: "test-sign-key-that-is-32-bytes!"},
		logger:   log,
		file:     f,
		writer:   f,
		signKey:  []byte("test-sign-key-that-is-32-bytes!"),
		shutdown: make(chan struct{}),
	}

	event := &AuditEvent{Action: "read", Resource: "test", Result: "success"}
	require.NoError(t, al.Log(context.Background(), event))
	assert.NotEmpty(t, event.Signature)
}

func TestAuditEvent_JSONWithSignature(t *testing.T) {
	event := &AuditEvent{
		EventID: "evt-json-sig", Action: "write", Resource: "db",
		Result: "success", Signature: "abc123", Timestamp: time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded AuditEvent
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, event.EventID, decoded.EventID)
	assert.Equal(t, event.Signature, decoded.Signature)
}

// TestSendToSyslog_LegacyEmptyAddr tests sendToSyslog when syslogExporter
// is nil and SyslogAddr is empty, covering the early return at audit.go:2155-2156.
// TestAuditLoggerClose_WithExporters tests Close when both SIEM and syslog
// exporters are set, covering the error handling paths at audit.go:2194-2205.
// TestAuditLoggerClose_NoFile tests Close when no file is open.
func TestAuditLoggerClose_NoFile(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		writer:   io.Discard,
		shutdown: make(chan struct{}),
	}
	err := al.Close()
	assert.NoError(t, err)
}

// TestOpenLogFile_OpenFileError tests openLogFile when os.OpenFile fails
// because the file path is a directory (MkdirAll succeeds but OpenFile fails),
// covering audit.go:1930-1936.
func TestOpenLogFile_OpenFileError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a directory at the exact path where the file should be opened
	filePath := filepath.Join(tmpDir, "audit.log")
	require.NoError(t, os.MkdirAll(filePath, 0o755))

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: filePath},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	err := al.openLogFile()
	assert.Error(t, err, "should fail when file path is a directory")
}

// TestOpenLogFile_Success tests openLogFile when both MkdirAll and OpenFile
// succeed, covering the success path at audit.go:1938-1943.
// TestLog_WithSyslogExporter tests Log when a syslog exporter is configured,
// covering the goroutine dispatch at audit.go:1573-1579.
func TestLog_WithSyslogExporter(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	log := createTestLogger(t)

	syslogExporter := &SyslogExporter{
		config:   &SyslogExporterConfig{Address: "127.0.0.1:0", Network: "tcp"},
		logger:   log,
		isRemote: true,
	}
	syslogExporter.connected.Store(false)

	al := &AuditLogger{
		config:         &AuditConfig{Enabled: true, File: logFile},
		logger:         log,
		file:           f,
		writer:         f,
		syslogExporter: syslogExporter,
		shutdown:       make(chan struct{}),
	}

	event := &AuditEvent{Action: "test", Resource: "r", Result: "success"}
	err = al.Log(context.Background(), event)
	assert.NoError(t, err, "Log should succeed even with exporter configured")

	f.Close()
}

// TestLog_WithLegacySyslog tests Log when the legacy Syslog config flag is true,
// covering the goroutine dispatch at audit.go:1559-1561.
func TestLog_WithLegacySyslog(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, Syslog: true, SyslogAddr: ""},
		logger:   log,
		file:     f,
		writer:   f,
		shutdown: make(chan struct{}),
	}

	event := &AuditEvent{Action: "test", Resource: "r", Result: "success"}
	err = al.Log(context.Background(), event)
	assert.NoError(t, err, "Log should succeed with legacy syslog configured")

	f.Close()
}

// TestAuditLogger_CleanupOldBackups_Extended tests the cleanupOldBackups function with more scenarios.
// TestAuditLogger_SendToSyslog tests the sendToSyslog function.
func TestAuditLogger_SendToSyslog(t *testing.T) {
	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
		Username:  "user",
		IPAddress: "1.2.3.4",
	}

	// listenTCP accepts one connection and sends what it reads on the channel.
	listenTCP := func(t *testing.T) (string, <-chan string) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { listener.Close() })
		received := make(chan string, 1)
		go func() {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			defer conn.Close()
			buf := make([]byte, 4096)
			n, readErr := conn.Read(buf)
			if readErr == nil {
				received <- string(buf[:n])
			}
		}()
		return listener.Addr().String(), received
	}

	// listenUDP sends the first datagram it reads on the channel.
	listenUDP := func(t *testing.T) (string, <-chan string) {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { pc.Close() })
		received := make(chan string, 1)
		go func() {
			buf := make([]byte, 4096)
			n, _, readErr := pc.ReadFrom(buf)
			if readErr == nil {
				received <- string(buf[:n])
			}
		}()
		return pc.LocalAddr().String(), received
	}

	tests := []struct {
		name  string
		setup func(t *testing.T, al *AuditLogger) <-chan string
		want  string
	}{
		{
			name: "exporter delivers the event",
			setup: func(t *testing.T, al *AuditLogger) <-chan string {
				addr, received := listenTCP(t)
				exporter, err := NewSyslogExporter(&SyslogExporterConfig{Address: addr, Network: "tcp", Facility: SyslogFacilityLocal0, Tag: "test"}, al.logger)
				require.NoError(t, err)
				t.Cleanup(func() { exporter.Close() })
				al.syslogExporter = exporter
				return received
			},
			want: ActionRead,
		},
		{
			name: "legacy address delivers over UDP",
			setup: func(t *testing.T, al *AuditLogger) <-chan string {
				addr, received := listenUDP(t)
				al.config.SyslogAddr = addr
				return received
			},
			want: "QuicBaaS Audit: action=read resource=data result=success user=user ip=1.2.3.4",
		},
		{
			name: "exporter error is logged, not raised",
			setup: func(t *testing.T, al *AuditLogger) <-chan string {
				exporter := &SyslogExporter{
					config:   &SyslogExporterConfig{Address: "127.0.0.1:0", Network: "tcp"},
					logger:   al.logger,
					isRemote: true,
				}
				al.syslogExporter = exporter
				return nil
			},
		},
		{
			name:  "no exporter and no address is a no-op",
			setup: func(t *testing.T, al *AuditLogger) <-chan string { return nil },
		},
		{
			name: "legacy dial error is logged, not raised",
			setup: func(t *testing.T, al *AuditLogger) <-chan string {
				al.config.SyslogAddr = "invalid-address:99999"
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al := &AuditLogger{
				config:   &AuditConfig{Enabled: true, Syslog: true},
				logger:   createTestLogger(t),
				shutdown: make(chan struct{}),
			}
			received := tt.setup(t, al)

			al.sendToSyslog(event)

			if received == nil {
				return
			}
			select {
			case msg := <-received:
				assert.Contains(t, msg, tt.want)
			case <-time.After(5 * time.Second):
				t.Fatal("syslog message was not delivered")
			}
		})
	}
}

// TestAuditLogger_RotateIfNeeded tests log rotation functionality.
func TestAuditLogger_RotateIfNeeded(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		maxSize  int64
		compress bool
		// noFile leaves the logger without an open file.
		noFile bool
		// closeFile closes the handle so Stat fails.
		closeFile bool
		// removeLog deletes the log path so the rename fails.
		removeLog   bool
		wantErr     bool
		wantCode    string
		wantBackup  string // glob suffix of the backup rotation must leave
		wantRotated bool
	}{
		{name: "no open file is a no-op", noFile: true, maxSize: 1},
		{name: "below max size does not rotate", size: 5, maxSize: 100},
		{name: "stat error on a closed file", size: 5, closeFile: true, wantErr: true},
		{name: "rename error when the log was removed", size: 2048, removeLog: true, wantErr: true, wantCode: ErrCodeAuditFileRotate},
		{name: "rotates past max size", size: 2 * 1024 * 1024, maxSize: 1, wantBackup: ".*", wantRotated: true},
		{name: "rotates and compresses the backup", size: 2 * 1024 * 1024, maxSize: 1, compress: true, wantBackup: ".*.gz", wantRotated: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			logFile := filepath.Join(tmpDir, "audit.log")
			require.NoError(t, os.WriteFile(logFile, bytes.Repeat([]byte("A"), tt.size), 0o600))

			al := &AuditLogger{
				config: &AuditConfig{
					Enabled:          true,
					File:             logFile,
					MaxSize:          tt.maxSize,
					MaxBackups:       5,
					Compress:         tt.compress,
					CompressionLevel: gzip.DefaultCompression,
				},
				logger:   createTestLogger(t),
				shutdown: make(chan struct{}),
			}
			if !tt.noFile {
				f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
				require.NoError(t, err)
				al.file, al.writer = f, f
			}
			if tt.closeFile {
				require.NoError(t, al.file.Close())
			}
			if tt.removeLog {
				require.NoError(t, os.Remove(logFile))
			}

			al.mu.Lock()
			err := al.rotateIfNeeded()
			al.mu.Unlock()
			// Compression and backup cleanup run on rotateWG.
			al.rotateWG.Wait()
			if al.file != nil && !tt.closeFile {
				t.Cleanup(func() { _ = al.file.Close() })
			}

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantCode != "" {
					var auditErr *AuditError
					require.ErrorAs(t, err, &auditErr)
					assert.Equal(t, tt.wantCode, auditErr.Code)
				}
				return
			}
			require.NoError(t, err)

			backups, err := filepath.Glob(logFile + ".*")
			require.NoError(t, err)
			if !tt.wantRotated {
				assert.Empty(t, backups, "no backup expected")
				return
			}

			want, err := filepath.Glob(logFile + tt.wantBackup)
			require.NoError(t, err)
			assert.Len(t, want, 1, "expected one backup matching %q", tt.wantBackup)
			assert.Len(t, backups, 1, "the uncompressed backup should not remain")

			info, err := al.file.Stat()
			require.NoError(t, err)
			assert.Zero(t, info.Size(), "rotation should reopen a fresh log file")
		})
	}
}

// TestAuditLogger_LogDataAccess_Failure tests data access logging with failure.
// TestAuditLogger_Log_WriteError tests log write error handling.
// TestKeyring_AutoRotationTimer tests automatic rotation timer setup.
func TestKeyring_AutoRotationTimer(t *testing.T) {
	log := createTestLogger(t)

	config := &EncryptionConfig{
		Enabled:     true,
		Algorithm:   AlgorithmAES256GCM,
		KeyProvider: "local",
		RotateDays:  1, // Enable auto rotation
	}

	kr, err := NewKeyring(config, log)
	require.NoError(t, err)

	// Timer should be set
	assert.NotNil(t, kr.rotationTimer)

	kr.Close()
}

// TestAuditLogger_OpenLogFile_CreateDir tests directory creation.
// ---------------------------------------------------------------------------
// audit.go -- sendLocal severity dispatch
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// audit.go -- LogKeyExport, LogKeyGenerate, LogKeyDeletion failure paths
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// audit.go -- compressFile success path
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// audit.go -- rotateIfNeeded
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// audit.go -- VerifySignature
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// audit.go -- Flush
// ---------------------------------------------------------------------------

func TestAuditLogger_FlushWithFileWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
	}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.Log(context.Background(), &AuditEvent{
		Action:   ActionRead,
		Resource: "data",
		Result:   ResultSuccess,
	})
	require.NoError(t, err)

	err = al.Flush()
	assert.NoError(t, err)
}

func TestAuditLogger_FlushWithNilFile(t *testing.T) {
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// No file set, just stdout writer.
	err = al.Flush()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// audit.go -- NewAuditLogger with SyslogEnabled
// ---------------------------------------------------------------------------

func TestNewAuditLogger_WithSyslogEnabled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			conn.Close()
		}
	}()

	cfg := &AuditConfig{
		Enabled:        true,
		SyslogEnabled:  true,
		SyslogAddress:  listener.Addr().String(),
		SyslogNetwork:  "tcp",
		SyslogFacility: SyslogFacilityLocal0,
		SyslogTag:      "quicbaas-test",
	}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.NotNil(t, al.GetSyslogExporter())
}

func TestNewAuditLogger_SyslogEnabledUnreachable(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:        true,
		SyslogEnabled:  true,
		SyslogAddress:  "127.0.0.1:1",
		SyslogNetwork:  "tcp",
		SyslogFacility: SyslogFacilityLocal0,
		SyslogTag:      "quicbaas-test",
	}
	log := createTestLogger(t)

	// Should not fail -- syslog is optional.
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Syslog exporter should be nil because connection failed.
	assert.Nil(t, al.GetSyslogExporter())
}

// ---------------------------------------------------------------------------
// audit.go -- LogsDataAccess and LogsBackupOperation
// ---------------------------------------------------------------------------

func TestAuditLogger_LogsBackupAndRestore(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogBackupOperation(context.Background(), "admin", "u-1", ActionBackup, map[string]string{"path": "/backups/1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionBackup)
	assert.Contains(t, buf.String(), ResultSuccess)
}

// ---------------------------------------------------------------------------
// AuditError tests
// ---------------------------------------------------------------------------

// Tests for LogKeyExport, LogKeyGenerate, LogKeyImport, LogKeyDeletion

func TestAuditLogger_LogKeyOperations(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		wantAction string
		log        func(al *AuditLogger, success bool) error
	}{
		{
			name:       "export",
			wantAction: ActionKeyExport,
			log: func(al *AuditLogger, success bool) error {
				return al.LogKeyExport(ctx, "alice", "user-123", "key-001", "AES-256-GCM", "wrapping-key", "RSA-OAEP", "192.168.1.100", success)
			},
		},
		{
			name:       "generate",
			wantAction: ActionKeyGenerate,
			log: func(al *AuditLogger, success bool) error {
				return al.LogKeyGenerate(ctx, "alice", "user-123", "key-001", "my-key", "AES-256-GCM", "192.168.1.100", success)
			},
		},
		{
			name:       "import",
			wantAction: ActionKeyImport,
			log: func(al *AuditLogger, success bool) error {
				return al.LogKeyImport(ctx, "alice", "user-123", "key-001", "imported-key", "AES-256-GCM", "192.168.1.100", success)
			},
		},
		{
			name:       "deletion",
			wantAction: ActionKeyDelete,
			log: func(al *AuditLogger, success bool) error {
				return al.LogKeyDeletion(ctx, "alice", "user-123", "key-001", "192.168.1.100", success)
			},
		},
	}

	for _, tt := range tests {
		for _, success := range []bool{true, false} {
			wantResult := ResultSuccess
			if !success {
				wantResult = ResultFailure
			}
			t.Run(tt.name+"/"+wantResult, func(t *testing.T) {
				al, err := NewAuditLogger(&AuditConfig{Enabled: true}, createTestLogger(t))
				require.NoError(t, err)
				defer al.Close()
				var buf bytes.Buffer
				al.writer = &buf

				require.NoError(t, tt.log(al, success))

				var logged AuditEvent
				require.NoError(t, json.Unmarshal(buf.Bytes(), &logged))
				assert.Equal(t, tt.wantAction, logged.Action)
				assert.Equal(t, "kms", logged.Resource)
				assert.Equal(t, wantResult, logged.Result)
				assert.Equal(t, "alice", logged.Username)
				assert.Equal(t, "key-001", logged.Details["key_id"])
			})
		}
	}
}

// Tests for GetSyslogExporter

func TestAuditLogger_GetSyslogExporter(t *testing.T) {
	log := createTestLogger(t)

	t.Run("without syslog exporter", func(t *testing.T) {
		config := &AuditConfig{
			Enabled: true,
		}

		al, err := NewAuditLogger(config, log)
		require.NoError(t, err)
		defer al.Close()

		exporter := al.GetSyslogExporter()
		assert.Nil(t, exporter)
	})
}

// Tests for sendLocal with all severity levels

// Tests for compressFile edge cases

// Tests for sendToSyslog edge cases

// Tests for VerifySignature edge cases

// TestAuditLogger_VerifySignature_StrippedSignatureFailsClosed is the #22
// regression: with signing on, removing the signature from a signed event
// must not make it verify, and a tampered field must still fail.
func TestAuditLogger_VerifySignature_StrippedSignatureFailsClosed(t *testing.T) {
	var buf bytes.Buffer
	al, err := NewAuditLogger(&AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  "test-key-112bits!",
	}, createTestLogger(t))
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	require.NoError(t, al.Log(context.Background(), &AuditEvent{
		Username:  "alice",
		Action:    ActionDelete,
		Resource:  "keys/root",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
	}))

	var signed AuditEvent
	require.NoError(t, json.Unmarshal(buf.Bytes(), &signed))
	require.NotEmpty(t, signed.Signature)
	require.True(t, al.VerifySignature(&signed))

	stripped := signed
	stripped.Signature = ""
	assert.False(t, al.VerifySignature(&stripped), "stripped signature must not verify")

	tampered := signed
	tampered.Result = ResultFailure
	assert.False(t, al.VerifySignature(&tampered), "tampered field must not verify")

	strippedAndTampered := tampered
	strippedAndTampered.Signature = ""
	assert.False(t, al.VerifySignature(&strippedAndTampered), "stripping must not hide tampering")
}

// Tests for NewAuditLogger with syslog enabled

// Tests for closeUnlocked with both connections

// Tests for connect with reconnection

// Tests for connect with unix socket

// Tests for sendRemote write error

// Tests for Export reconnection

// Tests for mapEventToSeverity with all result types

// Tests for AuditLogger.Log with syslog exporter

func TestAuditLogger_Log_WithSyslogExporterConfigured(t *testing.T) {
	log := createTestLogger(t)

	// Create UDP listener
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled:        true,
		SyslogEnabled:  true,
		SyslogAddress:  listener.LocalAddr().String(),
		SyslogNetwork:  "udp",
		SyslogFacility: SyslogFacilityLocal0,
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

	// Give time for async syslog send
	time.Sleep(50 * time.Millisecond)
}

// Tests for cleanupOldBackups - renamed

// Tests for checkRotation exit on shutdown

func TestAuditLogger_CheckRotation_OnShutdownSignal(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)

	// Close will signal shutdown
	al.Close()

	// Verify it's closed
	select {
	case <-al.shutdown:
		// Good, channel is closed
	default:
		t.Error("shutdown channel should be closed")
	}
}

// Tests for rotateIfNeeded with nil file

// Tests for buildStructuredData with error message

func TestSyslogExporter_BuildStructuredData_WithErrorMsgField(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
		logger: log,
	}

	event := &AuditEvent{
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultFailure,
		ErrorMsg: "permission denied",
	}

	sd := exporter.buildStructuredData(event)

	assert.Contains(t, sd, "[audit@quicbaas")
	assert.Contains(t, sd, "errorMsg=\"permission denied\"")
}

// Tests for AuditError and SIEMError

// Test compressFileAsync - renamed

// Test compressFileAsync with non-existent file - renamed

// ---------------------------------------------------------------------------
// Error type tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// SyslogExporter tests
// ---------------------------------------------------------------------------

func TestNewSyslogExporter_ConnectsViaTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "quicbaas-test",
		Hostname: "test-host",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())
	assert.True(t, exporter.isRemote)

	err = exporter.Close()
	assert.NoError(t, err)
	assert.False(t, exporter.IsConnected())
}

func TestSyslogExporter_ExportsRemoteEvent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	receivedCh := make(chan string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, readErr := conn.Read(buf)
		if readErr != nil {
			return
		}
		receivedCh <- string(buf[:n])
	}()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityAuth,
		Tag:      "quicbaas",
		Hostname: "node-1",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-123",
		Username:  "admin",
		UserID:    "uid-1",
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
		NodeID:    "node-1",
		ShardID:   42,
		Details:   map[string]string{"method": "jwt"},
	}

	err = exporter.Export(event)
	assert.NoError(t, err)

	select {
	case msg := <-receivedCh:
		assert.Contains(t, msg, "quicbaas")
		assert.Contains(t, msg, "evt-123")
		assert.Contains(t, msg, "audit@quicbaas")
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive syslog message in time")
	}
}

func TestSyslogExporter_ExportAfterDisconnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  addr,
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
		Hostname: "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	require.NoError(t, exporter.Close())
	require.NoError(t, listener.Close())

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}

	// The listener is gone, so the reconnect inside Export must fail.
	err = exporter.Export(event)
	var connErr *SyslogConnectionError
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, addr, connErr.Address)
	assert.False(t, exporter.IsConnected())
}

func TestSyslogExporter_ConnectionRefused(t *testing.T) {
	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  "127.0.0.1:1",
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.Error(t, err)
	assert.Nil(t, exporter)

	var connErr *SyslogConnectionError
	require.True(t, errors.As(err, &connErr))
	assert.Equal(t, "127.0.0.1:1", connErr.Address)
	assert.Equal(t, "tcp", connErr.Network)
}

// ---------------------------------------------------------------------------
// RFC 5424 formatting
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// AuditLogger helper methods
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// AuditLogger sendToSyslog
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// AuditLogger Close with exporters
// ---------------------------------------------------------------------------

func TestAuditLogger_CloseShutsBothExporters(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			conn.Close()
		}
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
	}

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	syslogCfg := &SyslogExporterConfig{
		Address: listener.Addr().String(),
		Network: "tcp",
		Tag:     "test",
	}
	syslogExp, err := NewSyslogExporter(syslogCfg, log)
	require.NoError(t, err)
	al.syslogExporter = syslogExp
	require.True(t, syslogExp.IsConnected())

	err = al.Close()
	assert.NoError(t, err)
	assert.False(t, syslogExp.IsConnected())
}

// ---------------------------------------------------------------------------
// compressFile error paths
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// NewAuditLogger configuration
// ---------------------------------------------------------------------------

// TestNewAuditLogger_RejectsShortSignKey ensures HMAC keys weaker than the
// FIPS 140-3 minimum of 112 bits are refused rather than silently accepted.
func TestNewAuditLogger_RejectsShortSignKey(t *testing.T) {
	log := createTestLogger(t)

	for _, signKey := range []string{"k", "short-key-13b"} {
		t.Run(signKey, func(t *testing.T) {
			require.Less(t, len(signKey), MinAuditSignKeyLen)

			cfg := &AuditConfig{
				Enabled:  true,
				SignLogs: true,
				SignKey:  signKey,
			}
			al, err := NewAuditLogger(cfg, log)
			require.Error(t, err)
			assert.Nil(t, al)

			var auditErr *AuditError
			require.True(t, errors.As(err, &auditErr))
			assert.Equal(t, ErrCodeAuditSignKeyTooShort, auditErr.Code)
		})
	}
}

// TestNewAuditLogger_AcceptsMinimumLengthSignKey pins the boundary: a key of
// exactly MinAuditSignKeyLen bytes is accepted and signs events.
func TestNewAuditLogger_AcceptsMinimumLengthSignKey(t *testing.T) {
	signKey := strings.Repeat("k", MinAuditSignKeyLen)

	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  signKey,
	}
	al, err := NewAuditLogger(cfg, createTestLogger(t))
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}
	sig, err := al.signEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, sig)

	event.Signature = sig
	assert.True(t, al.VerifySignature(event))
}

func TestNewAuditLogger_DisabledIsNoop(t *testing.T) {
	cfg := &AuditConfig{Enabled: false}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.Log(context.Background(), &AuditEvent{Action: ActionRead})
	assert.NoError(t, err)
}

func TestNewAuditLogger_SetsDefaultCompression(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &AuditConfig{
		Enabled:          true,
		File:             filepath.Join(tmpDir, "audit.log"),
		Compress:         true,
		CompressionLevel: 0,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.Equal(t, CompressionLevelDefault, al.config.CompressionLevel)
}

// ---------------------------------------------------------------------------
// rotateIfNeeded
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Log dispatches to SIEM and syslog
// ---------------------------------------------------------------------------

func TestAuditLogger_LogDispatchesToExporters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			buf := make([]byte, 4096)
			conn.Read(buf)
			conn.Close()
		}
	}()

	log := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
	}

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	syslogCfg := &SyslogExporterConfig{
		Address: listener.Addr().String(),
		Network: "tcp",
		Tag:     "test",
	}
	syslogExp, err := NewSyslogExporter(syslogCfg, log)
	require.NoError(t, err)
	al.syslogExporter = syslogExp

	var buf bytes.Buffer
	al.writer = &buf

	err = al.Log(context.Background(), &AuditEvent{
		Action:   ActionWrite,
		Resource: "data",
		Result:   ResultSuccess,
		Username: "admin",
	})
	require.NoError(t, err)
	assert.Greater(t, buf.Len(), 0)
}

// TestAuditLogger_LogBackupOperation tests backup operation logging.
// TestAuditLogger_OpenLogFileError tests file opening errors. The log path is
// a directory, so openLogFile's MkdirAll succeeds on the parent and the
// OpenFile under test fails with EISDIR -- for every uid. It used to point at
// /root/nonexistent/audit.log, which root simply creates, so the error never
// happened on a CI runner (testing-guidelines.md, Fault Injection).
func TestAuditLogger_OpenLogFileError(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.log")
	require.NoError(t, os.MkdirAll(logPath, 0o750))

	cfg := &AuditConfig{
		Enabled: true,
		File:    logPath,
	}

	log := createTestLogger(t)
	_, err := NewAuditLogger(cfg, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open audit log file")
}

// TestAuditLogger_LogWithMarshalError tests log with JSON marshal error.
// TestAuditLogger_LogWithSigningError tests log signing error path.
// TestAuditLogger_DataAccessFailure tests data access logging with failure.
// TestAuditLogger_FlushWithoutFile tests flush when no file is configured.
// TestAuditLogger_CleanupOldBackups tests backup cleanup.
// TestAuditLogger_LogSignatureGeneration tests signature generation.
func TestAuditLogger_LogSignatureGeneration(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  "test-signing-key",
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "testuser",
		Action:    ActionRead,
		Resource:  "test-resource",
		Result:    ResultSuccess,
		IPAddress: "192.168.1.1",
	}

	// Manually generate signature
	sig, err := al.signEvent(event)
	require.NoError(t, err)

	// Verify it's a valid hex string
	_, err = hex.DecodeString(sig)
	assert.NoError(t, err)

	// Verify it matches expected HMAC
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		event.Timestamp.Format(time.RFC3339Nano),
		event.Username,
		event.Action,
		event.Resource,
		event.Result,
		event.IPAddress)

	h := hmac.New(sha256.New, []byte(cfg.SignKey))
	h.Write([]byte(data))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	assert.Equal(t, expectedSig, sig)
}
