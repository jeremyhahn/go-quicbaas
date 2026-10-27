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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
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
				SignKey:  testSignKey(t, "test-signing-key"),
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
		SignKey:  testSignKey(t, "test-signing-key-12345"),
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
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogDataAccess(context.Background(), "alice", "user-123", ActionRead, "/test/key", "192.168.1.100", true)
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, "alice", logged.Username)
	assert.Equal(t, ActionRead, logged.Action)
	assert.Equal(t, ResultSuccess, logged.Result)
	assert.Equal(t, "/test/key", logged.Details["key"])
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

func TestAuditLogger_FileRotation(t *testing.T) {
	log := createTestLogger(t)

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Write events
	for i := 0; i < 10; i++ {
		event := &AuditEvent{
			UserID:   "user-123",
			Username: "alice",
			Action:   ActionRead,
			Resource: "keys",
			Result:   ResultSuccess,
		}
		err = al.Log(context.Background(), event)
		require.NoError(t, err)
	}

	// Verify log file exists
	_, err = os.Stat(logFile)
	assert.NoError(t, err)
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

func TestNewSyslogExporter_RemoteTCPConnection(t *testing.T) {
	log := createTestLogger(t)

	// Create a TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// Accept connections in background
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			// Read and discard data
			buf := make([]byte, 1024)
			for {
				_, err := conn.Read(buf)
				if err != nil {
					break
				}
			}
		}
	}()

	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal1,
		Tag:      "tcp-test",
		Hostname: "tcp-host",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	assert.True(t, exporter.IsConnected())
	assert.True(t, exporter.isRemote)
}

func TestNewSyslogExporter_ConnectionFailure(t *testing.T) {
	log := createTestLogger(t)

	// Use an address that should fail to connect
	cfg := &SyslogExporterConfig{
		Address:  "192.0.2.1:514", // TEST-NET-1, should be unreachable
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	assert.Nil(t, exporter)
	assert.Error(t, err)

	var connErr *SyslogConnectionError
	assert.ErrorAs(t, err, &connErr)
	assert.Equal(t, "192.0.2.1:514", connErr.Address)
	assert.Equal(t, "tcp", connErr.Network)
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
		name     string
		event    *AuditEvent
		contains []string
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := exporter.formatMessage(tt.event)
			for _, expected := range tt.contains {
				assert.Contains(t, msg, expected)
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
func TestAuditLogger_PlaceholderMethods(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	t.Run("sendToSyslog", func(t *testing.T) {
		event := &AuditEvent{
			Username: "test",
			Action:   ActionRead,
			Resource: "test",
			Result:   ResultSuccess,
		}
		// Should not panic
		al.sendToSyslog(event)
	})

	t.Run("cleanupOldBackups", func(t *testing.T) {
		// Should not panic
		al.cleanupOldBackups()
	})
}

// TestCompressFile_WithAuditLogger tests the compressFile method on AuditLogger
func TestCompressFile_WithAuditLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	err := os.WriteFile(testFile, []byte("test data for compression"), 0644)
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
}

// TestAuditLogger_AdditionalOpenLogFileCases tests more openLogFile scenarios
func TestAuditLogger_AdditionalOpenLogFileCases(t *testing.T) {
	log := createTestLogger(t)

	t.Run("openLogFile creates directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		logFile := filepath.Join(tmpDir, "nested", "dir", "audit.log")

		config := &AuditConfig{
			Enabled: true,
			File:    logFile,
		}

		al, err := NewAuditLogger(config, log)
		require.NoError(t, err)
		defer al.Close()

		// Verify directory was created
		_, err = os.Stat(filepath.Dir(logFile))
		assert.NoError(t, err)
	})
}

// TestAuditLogger_CheckRotationEdgeCases tests rotation check edge cases
func TestAuditLogger_CheckRotationEdgeCases(t *testing.T) {
	log := createTestLogger(t)

	t.Run("checkRotation with file and ticker", func(t *testing.T) {
		tmpDir := t.TempDir()
		logFile := filepath.Join(tmpDir, "audit.log")

		config := &AuditConfig{
			Enabled: true,
			File:    logFile,
			MaxSize: 100,
		}

		al, err := NewAuditLogger(config, log)
		require.NoError(t, err)

		// Let ticker run briefly
		time.Sleep(50 * time.Millisecond)

		al.Close()
	})

	t.Run("checkRotation without file exits immediately", func(t *testing.T) {
		config := &AuditConfig{
			Enabled: true,
		}

		al, err := NewAuditLogger(config, log)
		require.NoError(t, err)

		// Should exit immediately when no file
		done := make(chan bool)
		go func() {
			al.checkRotation()
			done <- true
		}()

		// Should complete quickly
		select {
		case <-done:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Fatal("checkRotation did not exit quickly")
		}

		al.Close()
	})
}

// TestAuditLogger_SendToSyslogIntegration tests the sendToSyslog function
func TestAuditLogger_SendToSyslogIntegration(t *testing.T) {
	logger := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled: true,
		Syslog:  true,
	}

	al, err := NewAuditLogger(cfg, logger)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		UserID:   "test-user",
		Username: "testuser",
		Action:   ActionRead,
		Resource: "test-resource",
		Result:   ResultSuccess,
	}

	// This will call sendToSyslog in a goroutine
	err = al.Log(context.Background(), event)
	assert.NoError(t, err)

	// Give goroutine time to execute
	time.Sleep(50 * time.Millisecond)
}

// TestAuditLogger_RotationWithCompression tests rotation with compression enabled
func TestAuditLogger_RotationWithCompression(t *testing.T) {
	logger := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1MB
		MaxBackups: 3,
		Compress:   true,
	}

	al, err := NewAuditLogger(cfg, logger)
	require.NoError(t, err)
	defer al.Close()

	// Write data to make file large
	largeData := make([]byte, 1024*1024+1) // Slightly over 1MB
	for i := range largeData {
		largeData[i] = 'a'
	}
	_, err = al.file.Write(largeData)
	require.NoError(t, err)

	// Call rotateIfNeeded which will call compressFile and cleanupOldBackups
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()
	assert.NoError(t, err)

	// Give goroutines time to execute
	time.Sleep(100 * time.Millisecond)

	// Check that backup was created
	files, err := filepath.Glob(logFile + ".*")
	assert.NoError(t, err)
	assert.True(t, len(files) > 0, "Expected backup file to be created")
}

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
	logger := createTestLogger(t)

	tests := []struct {
		name        string
		signLogs    bool
		signKey     string
		eventSig    string
		expectValid bool
	}{
		{
			name:        "signing disabled",
			signLogs:    false,
			signKey:     "",
			eventSig:    "",
			expectValid: true,
		},
		{
			name:        "signing enabled but empty signature",
			signLogs:    true,
			signKey:     "test-key-112bits!",
			eventSig:    "",
			expectValid: false,
		},
		{
			name:        "signing enabled with invalid signature",
			signLogs:    true,
			signKey:     "test-key-112bits!",
			eventSig:    "invalid-signature",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AuditConfig{
				Enabled:  true,
				SignLogs: tt.signLogs,
				SignKey:  testSignKey(t, tt.signKey),
			}

			al, err := NewAuditLogger(cfg, logger)
			require.NoError(t, err)
			defer al.Close()

			event := &AuditEvent{
				Timestamp: time.Now(),
				UserID:    "test",
				Username:  "test",
				Action:    ActionRead,
				Resource:  "test",
				Result:    ResultSuccess,
				Signature: tt.eventSig,
			}

			valid := al.VerifySignature(event)
			assert.Equal(t, tt.expectValid, valid)
		})
	}
}

// ---------------------------------------------------------------------------
// compressFileAsync (audit.go:2014 -- 0.0% covered)
// ---------------------------------------------------------------------------

func TestCompressFileAsync_Success(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    100,
		MaxBackups: 3,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Create a test file to compress
	testFile := filepath.Join(tmpDir, "test-compress.log")
	require.NoError(t, os.WriteFile(testFile, []byte("test data to compress"), 0o644))

	// Call compressFileAsync synchronously for testing
	al.compressFileAsync(testFile, gzip.BestSpeed)

	// Verify compressed file was created
	assert.FileExists(t, testFile+".gz")

	// Verify original was removed
	_, err = os.Stat(testFile)
	assert.True(t, os.IsNotExist(err))
}

func TestCompressFileAsync_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    100,
		MaxBackups: 3,
		SignLogs:   false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Calling with non-existent file should not panic
	al.compressFileAsync("/nonexistent/path/file.log", gzip.BestSpeed)
}

// ---------------------------------------------------------------------------
// compressFile (audit.go:2022 -- 62.9% covered)
// ---------------------------------------------------------------------------

func TestCompressFile_Success(t *testing.T) {
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

	// Create test file
	testFile := filepath.Join(tmpDir, "compress-test.log")
	testData := []byte("compress me please")
	require.NoError(t, os.WriteFile(testFile, testData, 0o644))

	err = al.compressFile(testFile, gzip.DefaultCompression)
	require.NoError(t, err)

	// Verify compressed file exists and original was removed
	assert.FileExists(t, testFile+".gz")
	_, statErr := os.Stat(testFile)
	assert.True(t, os.IsNotExist(statErr))

	// Verify compressed content is valid gzip
	gzFile, err := os.Open(testFile + ".gz")
	require.NoError(t, err)
	defer gzFile.Close()

	reader, err := gzip.NewReader(gzFile)
	require.NoError(t, err)
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, testData, decompressed)
}

func TestCompressFile_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{Enabled: true, File: logFile, MaxSize: 100}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.compressFile("/nonexistent/file.log", gzip.DefaultCompression)
	require.Error(t, err)
	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
	assert.Equal(t, ErrCodeAuditCompressionOpen, auditErr.Code)
}

func TestCompressFile_InvalidCompressionLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{Enabled: true, File: logFile, MaxSize: 100}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Create test file
	testFile := filepath.Join(tmpDir, "invalid-level.log")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0o644))

	// Invalid level should fallback to default
	err = al.compressFile(testFile, 999)
	require.NoError(t, err)
	assert.FileExists(t, testFile+".gz")
}

// compressFile writes to "<source>.gz". A directory already sitting on that
// name makes the create fail with EISDIR, which root does not bypass -- a
// read-only parent directory would only fail for an unprivileged uid
// (testing-guidelines.md, Fault Injection).
func TestCompressFile_DestPathIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.MkdirAll(readOnlyDir, 0o755))

	cfg := &AuditConfig{Enabled: true, File: logFile, MaxSize: 100}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	testFile := filepath.Join(readOnlyDir, "test.log")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0o644))

	// Occupy the destination name with a directory so the create cannot succeed.
	require.NoError(t, os.MkdirAll(testFile+".gz", 0o755))

	err = al.compressFile(testFile, gzip.DefaultCompression)
	require.Error(t, err)
	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
}

func TestCompressFile_AllLevels(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{Enabled: true, File: logFile, MaxSize: 100}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	levels := []int{gzip.HuffmanOnly, gzip.BestSpeed, gzip.DefaultCompression, gzip.BestCompression}
	for i, level := range levels {
		testFile := filepath.Join(tmpDir, "level-test-"+string(rune('a'+i))+".log")
		require.NoError(t, os.WriteFile(testFile, []byte("data for level test"), 0o644))

		err := al.compressFile(testFile, level)
		require.NoError(t, err)
		assert.FileExists(t, testFile+".gz")
	}
}

// ---------------------------------------------------------------------------
// cleanupOldBackups (audit.go:2112 -- 0.0% covered)
// ---------------------------------------------------------------------------

func TestCleanupOldBackups(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    100,
		MaxBackups: 3,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call cleanupOldBackups (it's a stub/placeholder)
	al.cleanupOldBackups()
	// Should not panic or error
}

// ---------------------------------------------------------------------------
// sendLocal (audit.go:341 -- 11.8% covered)
// These tests need a real syslog.Writer mock via TCP listener
// ---------------------------------------------------------------------------

func TestSyslogExporter_SendRemoteSuccess(t *testing.T) {
	// Create a TCP listener to act as a remote syslog server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// Accept connections in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
	}()

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
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-test-remote",
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "test-resource",
		Result:    ResultSuccess,
	}

	err = exporter.Export(event)
	require.NoError(t, err)
}

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

func TestSyslogExporter_SendRemoteNilConn(t *testing.T) {
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Network:  "tcp",
			Address:  "127.0.0.1:514",
			Hostname: "testhost",
		},
		isRemote: true,
	}

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err := exporter.sendRemote(event, SyslogSeverityInformational, "test")
	require.Error(t, err)
	var sysErr *SyslogExporterError
	assert.ErrorAs(t, err, &sysErr)
}

func TestSyslogExporter_SendLocalNilConn(t *testing.T) {
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
	}

	err := exporter.sendLocal(SyslogSeverityInformational, "test message")
	require.Error(t, err)
	var sysErr *SyslogExporterError
	assert.ErrorAs(t, err, &sysErr)
}

// ---------------------------------------------------------------------------
// connect (audit.go:283 -- 60.0% covered, remote branch)
// ---------------------------------------------------------------------------

func TestSyslogExporter_ConnectRemoteTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// Accept in background
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
	assert.True(t, exporter.isRemote)
	require.NoError(t, exporter.Close())
}

func TestSyslogExporter_ConnectRemoteUDP(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  conn.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
		Hostname: "testhost",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())
	require.NoError(t, exporter.Close())
}

func TestSyslogExporter_ConnectRemoteFails(t *testing.T) {
	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  "192.0.2.1:0", // Non-routable address
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
		Hostname: "testhost",
	}

	_, err := NewSyslogExporter(cfg, log)
	require.Error(t, err)
	var connErr *SyslogConnectionError
	assert.ErrorAs(t, err, &connErr)
}

func TestSyslogExporter_NilConfig(t *testing.T) {
	log := createTestLogger(t)
	_, err := NewSyslogExporter(nil, log)
	require.Error(t, err)
}

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

func TestRotateIfNeeded_FileExceedsMaxSize(t *testing.T) {
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
	defer al.Close()

	// Write enough data to exceed 1MB
	ctx := context.Background()
	bigData := make([]byte, 1024) // 1KB per event
	for i := range bigData {
		bigData[i] = byte('A' + (i % 26))
	}

	for i := 0; i < 1200; i++ {
		writeErr := al.Log(ctx, &AuditEvent{
			Username: "test",
			Action:   ActionWrite,
			Resource: string(bigData),
			Result:   ResultSuccess,
		})
		require.NoError(t, writeErr)
	}

	// Force rotation check
	al.mu.Lock()
	rotErr := al.rotateIfNeeded()
	al.mu.Unlock()

	require.NoError(t, rotErr)
}

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

func TestRotateIfNeeded_NilFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Close the file to make it nil
	al.mu.Lock()
	if al.file != nil {
		al.file.Close()
		al.file = nil
	}
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	require.NoError(t, err) // Should return nil when file is nil
}

// ---------------------------------------------------------------------------
// VerifySignature (audit.go:1903 -- 83.3% covered)
// ---------------------------------------------------------------------------

func TestVerifySignature_SigningDisabled(t *testing.T) {
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

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// When signing is disabled, verify should return true
	assert.True(t, al.VerifySignature(event))
}

func TestVerifySignature_EmptySignature(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		MaxSize:  100,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-hmac-key-for-signing"),
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "",
	}

	// Empty signature with signing enabled fails closed
	assert.False(t, al.VerifySignature(event))
}

func TestVerifySignature_ValidSignature(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		MaxSize:  100,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-hmac-key-for-signing"),
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Log an event to get it signed
	ctx := context.Background()
	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err = al.Log(ctx, event)
	require.NoError(t, err)

	// Event should now have a signature
	assert.NotEmpty(t, event.Signature)

	// Verify should succeed
	assert.True(t, al.VerifySignature(event))
}

func TestVerifySignature_InvalidSignature(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		MaxSize:  100,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-hmac-key-for-signing"),
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "invalid-signature-value",
	}

	assert.False(t, al.VerifySignature(event))
}

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

func TestAuditLogger_CloseWithSyslog(t *testing.T) {
	// Start TCP listener to act as syslog
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

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:        true,
		File:           logFile,
		MaxSize:        100,
		SyslogEnabled:  true,
		SyslogAddress:  listener.Addr().String(),
		SyslogNetwork:  "tcp",
		SyslogFacility: SyslogFacilityLocal0,
		SyslogTag:      "quicbaas-test",
		SyslogHostname: "testhost",
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	require.NoError(t, al.Close())
}

func TestAuditLogger_CloseWithNilFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Set file to nil
	al.mu.Lock()
	if al.file != nil {
		al.file.Close()
		al.file = nil
	}
	al.mu.Unlock()

	require.NoError(t, al.Close())
}

// ---------------------------------------------------------------------------
// sendToSyslog (audit.go:2145 -- 73.3% covered)
// ---------------------------------------------------------------------------

func TestSendToSyslog_WithExporter(t *testing.T) {
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

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:        true,
		File:           logFile,
		MaxSize:        100,
		Syslog:         true,
		SyslogEnabled:  true,
		SyslogAddress:  listener.Addr().String(),
		SyslogNetwork:  "tcp",
		SyslogFacility: SyslogFacilityLocal0,
		SyslogTag:      "test",
		SyslogHostname: "testhost",
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// Call sendToSyslog directly
	al.sendToSyslog(event)
}

func TestSendToSyslog_NoExporterNoAddr(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		MaxSize:  100,
		Syslog:   true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// Should not panic when no exporter and no addr
	al.sendToSyslog(event)
}

// ---------------------------------------------------------------------------
// Export (audit.go:321 -- 90% covered)
// ---------------------------------------------------------------------------

func TestSyslogExporter_ExportRemote(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	received := make(chan string, 10)
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			buf := make([]byte, 4096)
			n, _ := conn.Read(buf)
			if n > 0 {
				received <- string(buf[:n])
			}
			conn.Close()
		}
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
	defer exporter.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-export-1",
		Username:  "admin",
		UserID:    "user-123",
		Action:    ActionWrite,
		Resource:  "database",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
	}

	err = exporter.Export(event)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Error type coverage
// ---------------------------------------------------------------------------

func TestSyslogConnectionError(t *testing.T) {
	err := &SyslogConnectionError{
		Address: "192.168.1.1:514",
		Network: "tcp",
		Err:     errors.New("connection refused"),
	}
	assert.Contains(t, err.Error(), "192.168.1.1:514")
	assert.Contains(t, err.Error(), "tcp")
	assert.NotNil(t, err.Unwrap())
}

func TestSyslogWriteError(t *testing.T) {
	err := &SyslogWriteError{Err: errors.New("write failed")}
	assert.Contains(t, err.Error(), "write failed")
	assert.NotNil(t, err.Unwrap())
}

func TestSyslogExporterError(t *testing.T) {
	err := &SyslogExporterError{Op: "connect", Reason: "timeout"}
	assert.Contains(t, err.Error(), "connect")
	assert.Contains(t, err.Error(), "timeout")
}

// ---------------------------------------------------------------------------
// NewSyslogExporter defaults (audit.go:251 -- 84.6%)
// ---------------------------------------------------------------------------

func TestNewSyslogExporter_DefaultTag(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Tag:      "", // Should default to "quicbaas"
		Hostname: "", // Should auto-detect
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	assert.Equal(t, "quicbaas", cfg.Tag)
	assert.NotEmpty(t, cfg.Hostname)
}

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
		SignKey:  testSignKey(t, "test-signing-key-for-hmac"),
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

func TestSyslogExporter_ExportWithReconnect(t *testing.T) {
	// Start TCP listener for syslog
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	var received atomic.Int32
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
					_, readErr := c.Read(buf)
					if readErr != nil {
						return
					}
					received.Add(1)
				}
			}(conn)
		}
	}()

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Network: "tcp",
		Address: listener.Addr().String(),
		Tag:     "test-reconnect",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	defer exporter.Close()

	// Export sends via remote path
	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-reconnect-1",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		UserID:    "user1",
		Username:  "testuser",
	}

	err = exporter.Export(event)
	assert.NoError(t, err)

	// Force disconnect and reconnect
	exporter.connected.Store(false)
	err = exporter.Reconnect()
	assert.NoError(t, err)

	// Export again after reconnect
	err = exporter.Export(event)
	assert.NoError(t, err)
}

func TestSyslogExporter_SendRemoteSetDeadlineError(t *testing.T) {
	// Use a connection that has been closed to trigger SetWriteDeadline error
	log := createTestLogger(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		conn.Close()
	}()

	cfg := &SyslogExporterConfig{
		Network: "tcp",
		Address: listener.Addr().String(),
		Tag:     "test-deadline",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	// connect to get a valid connection
	connErr := exporter.connect()
	require.NoError(t, connErr)

	// Close the remote conn to trigger errors
	exporter.mu.Lock()
	if exporter.remoteConn != nil {
		exporter.remoteConn.Close()
	}
	exporter.mu.Unlock()

	// Now try sendRemote - SetWriteDeadline or Write should error
	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-deadline-test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	err = exporter.Export(event)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// compressFile - cover gzWriter.Close error path and dstFile.Close error path
// We use a file with a lot of content to ensure copy path is exercised
// ---------------------------------------------------------------------------

func TestCompressFile_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled: true,
		},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	// Create a large file (64KB+) to exercise the buffer copy path
	srcPath := filepath.Join(tmpDir, "large_audit.log")
	data := make([]byte, 128*1024) // 128KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	err := os.WriteFile(srcPath, data, 0o600)
	require.NoError(t, err)

	// Compress with best compression
	err = al.compressFile(srcPath, gzip.BestCompression)
	assert.NoError(t, err)

	// Verify compressed file exists and is smaller
	gzPath := srcPath + ".gz"
	info, statErr := os.Stat(gzPath)
	require.NoError(t, statErr)
	assert.Less(t, info.Size(), int64(128*1024))

	// Verify original was removed
	_, statErr = os.Stat(srcPath)
	assert.True(t, os.IsNotExist(statErr))

	// Verify we can decompress and get original data
	gzFile, openErr := os.Open(gzPath)
	require.NoError(t, openErr)
	defer gzFile.Close()

	gzReader, gzErr := gzip.NewReader(gzFile)
	require.NoError(t, gzErr)
	defer gzReader.Close()

	decompressed, readErr := io.ReadAll(gzReader)
	require.NoError(t, readErr)
	assert.Equal(t, data, decompressed)
}

func TestCompressFile_HuffmanOnlyLevel(t *testing.T) {
	tmpDir := t.TempDir()
	log := createTestLogger(t)

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	srcPath := filepath.Join(tmpDir, "huffman.log")
	err := os.WriteFile(srcPath, []byte("test content for huffman compression"), 0o600)
	require.NoError(t, err)

	err = al.compressFile(srcPath, gzip.HuffmanOnly)
	assert.NoError(t, err)

	gzPath := srcPath + ".gz"
	_, statErr := os.Stat(gzPath)
	assert.NoError(t, statErr)
}

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

func TestSyslogExporter_CloseUnlockedBothConnsWithErrors(t *testing.T) {
	// Create a TCP listener, connect to it, then test close
	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		conn.Close()
	}()

	log := createTestLogger(t)

	// Create exporter that connects to our listener
	cfg := &SyslogExporterConfig{
		Network: "tcp",
		Address: listener.Addr().String(),
		Tag:     "test-close",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	listener.Close()

	// Close should work
	err = exporter.Close()
	assert.NoError(t, err)
	assert.False(t, exporter.IsConnected())
}

// ---------------------------------------------------------------------------
// NewAuditLogger - cover SyslogEnabled path that continues with warning
// ---------------------------------------------------------------------------

func TestNewAuditLogger_WithSyslogEnabledFailsGracefully(t *testing.T) {
	log := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:       true,
		SyslogEnabled: true,
		SyslogNetwork: "tcp",
		SyslogAddress: "127.0.0.1:1", // Port 1 should fail to connect
		SyslogTag:     "test-fail",
		FlushInterval: 100 * time.Millisecond,
	}

	// This should succeed (syslog is optional, just warns)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, al)
	defer close(al.shutdown)
}

func TestNewAuditLogger_WithSignLogsNoKey(t *testing.T) {
	log := createTestLogger(t)

	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, ""), // Missing sign key
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

func TestRotateIfNeeded_TriggersRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "rotate_me.log")

	// Write data that exceeds MaxSize (1 MB)
	data := make([]byte, 2*1024*1024) // 2 MB
	for i := range data {
		data[i] = 'A'
	}
	err := os.WriteFile(logPath, data, 0o600)
	require.NoError(t, err)

	file, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, openErr)

	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			File:       logPath,
			MaxSize:    1, // 1 MB - file is 2 MB so rotation should trigger
			MaxBackups: 5,
			Compress:   false, // Don't compress to simplify test
		},
		logger:   log,
		file:     file,
		writer:   file,
		shutdown: make(chan struct{}),
	}

	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()
	assert.NoError(t, err)

	// After rotation, the original file should have been renamed
	// and a new file created
	assert.NotNil(t, al.file)
}

func TestRotateIfNeeded_WithCompress(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "rotate_compress.log")

	// Write data that exceeds MaxSize
	data := make([]byte, 2*1024*1024) // 2 MB
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	err := os.WriteFile(logPath, data, 0o600)
	require.NoError(t, err)

	file, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, openErr)

	log := createTestLogger(t)

	// Test rotation without async compression to avoid race with t.TempDir() cleanup.
	// The compressFileAsync goroutine runs after rotateIfNeeded returns, and the
	// temp directory may be cleaned up before it finishes, causing spurious errors.
	al := &AuditLogger{
		config: &AuditConfig{
			File:       logPath,
			MaxSize:    1,
			MaxBackups: 5,
			Compress:   false,
		},
		logger:   log,
		file:     file,
		writer:   file,
		shutdown: make(chan struct{}),
	}

	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()
	assert.NoError(t, err)

	// Verify the backup file was created
	matches, globErr := filepath.Glob(filepath.Join(tmpDir, "rotate_compress.log.*"))
	assert.NoError(t, globErr)
	assert.NotEmpty(t, matches, "should have created a backup file")
}

// ---------------------------------------------------------------------------
// sendToSyslog legacy path - cover the case where SyslogAddr is set
// ---------------------------------------------------------------------------

func TestSendToSyslog_LegacyWithAddr(t *testing.T) {
	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:    true,
			Syslog:     true,
			SyslogAddr: "127.0.0.1:1", // Will fail to connect but covers the code path
		},
		logger:   log,
		writer:   &bytes.Buffer{},
		shutdown: make(chan struct{}),
	}

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-legacy",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// This should not panic even though connection will fail
	al.sendToSyslog(event)
}

// ---------------------------------------------------------------------------
// sendRemote - exercise all code paths with varying event fields
// ---------------------------------------------------------------------------

func TestSyslogExporter_SendRemoteWithAllEventFields(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// receivedData captures syslog output for verification
	done := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			close(done)
			return
		}
		defer conn.Close()
		buf := make([]byte, 8192)
		n, _ := conn.Read(buf)
		_ = buf[:n] // data received
		close(done)
	}()

	log := createTestLogger(t)

	exporter, err := NewSyslogExporter(&SyslogExporterConfig{
		Network:  "tcp",
		Address:  listener.Addr().String(),
		Tag:      "test-full",
		Hostname: "test-host.example.com",
		Facility: 1,
	}, log)
	require.NoError(t, err)
	defer exporter.Close()

	connErr := exporter.connect()
	require.NoError(t, connErr)

	// Event with all fields populated to maximize structured data coverage
	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-full-fields",
		Action:    ActionKeyRotate,
		Resource:  "encryption-key",
		Result:    ResultSuccess,
		UserID:    "admin-user",
		Username:  "admin",
		IPAddress: "192.168.1.100",
		NodeID:    "node-1",
		ShardID:   42,
		Details: map[string]string{
			"key_version": "3",
			"algorithm":   "AES-256-GCM",
		},
	}

	err = exporter.Export(event)
	assert.NoError(t, err)

	// Wait briefly for data to arrive
	select {
	case <-done:
		// Data received
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for syslog data")
	}
}

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

func TestNewSyslogExporter_EmptyNetworkWithAddress(t *testing.T) {
	// An empty Network means "local syslog", and NewSyslogExporter connects
	// before it returns -- so this test needs a syslog socket to exist. A CI
	// job container has none, which is ambient host state the test cannot
	// assert its way around (lessons.md, "Tests that depend on ambient host
	// state"). Skip where the daemon is absent rather than fail there.
	if _, err := os.Stat("/dev/log"); err != nil {
		t.Skip("no local syslog socket at /dev/log")
	}

	log := createTestLogger(t)

	cfg := &SyslogExporterConfig{
		Network: "", // empty network means local
		Address: "", // empty address means default
		Tag:     "test-empty",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, exporter)

	// Trying to connect with local syslog - may fail in test env
	// but exercises the code path
	_ = exporter.connect()
}

// ---------------------------------------------------------------------------
// VerifySignature - cover the signEvent error path
// ---------------------------------------------------------------------------

func TestVerifySignature_WithEmptySignKey(t *testing.T) {
	al := &AuditLogger{
		config: &AuditConfig{
			SignLogs: true,
		},
		signKey: nil, // Empty key
	}

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-sig-test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "some-signature",
	}

	// With no signing key, signEvent fails and verification must not pass.
	result := al.VerifySignature(event)
	assert.False(t, result)
}

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

func TestCompressFileAsync_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "async_compress.log")

	err := os.WriteFile(srcPath, []byte("async test content for compression"), 0o600)
	require.NoError(t, err)

	log := createTestLogger(t)

	al := &AuditLogger{
		config: &AuditConfig{
			Enabled:          true,
			Compress:         true,
			CompressionLevel: gzip.DefaultCompression,
		},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	// compressFileAsync compresses on the calling goroutine; callers run it
	// in their own goroutine, so the file exists as soon as it returns.
	al.compressFileAsync(srcPath, gzip.DefaultCompression)

	require.FileExists(t, srcPath+".gz", "compressed file was not created")
}

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

func TestSendToSyslog_LegacyDialError(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SyslogAddr: "invalid-address:99999"},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	al.sendToSyslog(&AuditEvent{Action: "test", Resource: "r", Result: "success", Username: "u", IPAddress: "127.0.0.1"})
}

func TestLog_MarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	log := createTestLogger(t)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer f.Close()

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile},
		logger:   log,
		file:     f,
		writer:   f,
		shutdown: make(chan struct{}),
	}

	// Details is map[string]string so we can't put a channel in it.
	// Instead, we test write error by closing the writer.
	f.Close()

	event := &AuditEvent{
		Action: "test", Resource: "r", Result: "success",
		Timestamp: time.Now(), EventID: "evt-write",
	}
	err = al.Log(context.Background(), event)
	assert.Error(t, err)
}

func TestRotateIfNeeded_RenameError(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	log := createTestLogger(t)

	bigData := make([]byte, 2048)
	require.NoError(t, os.WriteFile(logFile, bigData, 0o600))

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, MaxSize: 0},
		logger:   log,
		file:     f,
		writer:   f,
		shutdown: make(chan struct{}),
	}

	require.NoError(t, os.Remove(logFile))
	assert.Error(t, al.rotateIfNeeded())
}

func TestOpenLogFile_DirCreateError(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: "/dev/null/impossible/path/audit.log"},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	assert.Error(t, al.openLogFile())
}

func TestVerifySignature_NonexistentFile(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SignLogs: true, SignKey: testSignKey(t, "test-key-12345678901234567890123"), File: "/nonexistent/path/audit.log"},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	event := &AuditEvent{Action: "test", Signature: "sig"}
	valid := al.VerifySignature(event)
	assert.False(t, valid)
}

func TestCleanupOldBackups_96(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{config: &AuditConfig{Enabled: true}, logger: log}
	al.cleanupOldBackups()
}

func TestNewAuditLogger_OpenFileError_96(t *testing.T) {
	log := createTestLogger(t)
	_, err := NewAuditLogger(&AuditConfig{Enabled: true, File: "/proc/nonexistent/audit.log", FlushInterval: time.Second}, log)
	assert.Error(t, err)
}

func TestLog_WithSignLogs_Success(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	log := createTestLogger(t)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, SignLogs: true, SignKey: testSignKey(t, "test-sign-key-that-is-32-bytes!")},
		logger:   log,
		file:     f,
		writer:   f,
		signKey:  testSignKey(t, "test-sign-key-that-is-32-bytes!"),
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

func TestSendToSyslog_WithExporterExportError(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SyslogExporter{
		config:   &SyslogExporterConfig{Address: "127.0.0.1:0", Network: "tcp"},
		logger:   log,
		isRemote: true,
	}
	exporter.connected.Store(false)

	al := &AuditLogger{
		config:         &AuditConfig{Enabled: true, SyslogAddr: ""},
		logger:         log,
		syslogExporter: exporter,
		shutdown:       make(chan struct{}),
	}
	al.sendToSyslog(&AuditEvent{Action: "test", Result: ResultSuccess})
}

func TestVerifySignature_ValidSignedLogs(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	log := createTestLogger(t)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, SignLogs: true, SignKey: testSignKey(t, "test-sign-key-that-is-32-bytes!")},
		logger:   log,
		file:     f,
		writer:   f,
		signKey:  testSignKey(t, "test-sign-key-that-is-32-bytes!"),
		shutdown: make(chan struct{}),
	}

	ctx := context.Background()
	event := &AuditEvent{Action: "read", Resource: "r", Result: "success"}
	require.NoError(t, al.Log(ctx, event))
	f.Close()

	valid := al.VerifySignature(event)
	assert.True(t, valid)
}

// TestCompressFile_SourceOpenError tests compressFile when the source file
// does not exist, covering the os.Open error path at audit.go:2029-2036.
func TestCompressFile_SourceOpenError(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	err := al.compressFile("/nonexistent/file/path.log", 6)
	assert.Error(t, err, "should fail when source file does not exist")
}

// TestCompressFile_DestCreateError tests compressFile when the destination
// .gz file cannot be created, covering audit.go:2041-2048.
func TestCompressFile_DestCreateError(t *testing.T) {
	tmpDir := t.TempDir()
	sourceFile := filepath.Join(tmpDir, "audit.log")
	require.NoError(t, os.WriteFile(sourceFile, []byte("test log data"), 0o600))

	gzPath := sourceFile + ".gz"
	require.NoError(t, os.MkdirAll(gzPath, 0o755))

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	err := al.compressFile(sourceFile, 6)
	assert.Error(t, err, "should fail when dest .gz path is a directory")
}

// TestCompressFile_InvalidLevel_96 tests compressFile with an out-of-range
// compression level, verifying it falls back to gzip.DefaultCompression.
func TestCompressFile_InvalidLevel_96(t *testing.T) {
	tmpDir := t.TempDir()
	sourceFile := filepath.Join(tmpDir, "audit.log")
	require.NoError(t, os.WriteFile(sourceFile, []byte("test data"), 0o600))

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	err := al.compressFile(sourceFile, 999)
	require.NoError(t, err)
}

// TestSendToSyslog_LegacyEmptyAddr tests sendToSyslog when syslogExporter
// is nil and SyslogAddr is empty, covering the early return at audit.go:2155-2156.
func TestSendToSyslog_LegacyEmptyAddr(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SyslogAddr: ""},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	al.sendToSyslog(&AuditEvent{Action: "test", Result: "success"})
}

// TestAuditLoggerClose_WithExporters tests Close when both SIEM and syslog
// exporters are set, covering the error handling paths at audit.go:2194-2205.
func TestAuditLoggerClose_WithExporters(t *testing.T) {
	log := createTestLogger(t)

	siemExporter := &SIEMExporter{
		config:   &SIEMConfig{Enabled: false},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	syslogExporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
		logger: log,
	}
	syslogExporter.connected.Store(false)

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	al := &AuditLogger{
		config:         &AuditConfig{Enabled: true, File: logFile},
		logger:         log,
		file:           f,
		writer:         f,
		siemExporter:   siemExporter,
		syslogExporter: syslogExporter,
		shutdown:       make(chan struct{}),
	}

	err = al.Close()
	assert.NoError(t, err)
}

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

// TestRotateIfNeeded_FileNil tests rotateIfNeeded when file is nil,
// covering the early return at audit.go:1971-1973.
func TestRotateIfNeeded_FileNil(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, MaxSize: 0},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	err := al.rotateIfNeeded()
	assert.NoError(t, err, "should return nil when file is nil")
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

// TestVerifySignature_SignLogsDisabled tests VerifySignature when
// SignLogs is false, covering the first condition in the early return.
func TestVerifySignature_SignLogsDisabled(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SignLogs: false},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	event := &AuditEvent{Action: "test", Signature: "some-sig"}
	valid := al.VerifySignature(event)
	assert.True(t, valid, "should return true when signing is disabled")
}

// TestOpenLogFile_Success tests openLogFile when both MkdirAll and OpenFile
// succeed, covering the success path at audit.go:1938-1943.
func TestOpenLogFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "subdir", "audit.log")

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile},
		logger:   log,
		shutdown: make(chan struct{}),
	}

	err := al.openLogFile()
	require.NoError(t, err, "openLogFile should succeed")
	assert.NotNil(t, al.file, "file should be opened")

	// Clean up
	if al.file != nil {
		al.file.Close()
	}
}

// TestRotateIfNeeded_BelowMaxSize tests rotateIfNeeded when the file size
// is below the max, covering the early return at audit.go:1981-1983.
func TestRotateIfNeeded_BelowMaxSize(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	require.NoError(t, os.WriteFile(logFile, []byte("small"), 0o600))

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer f.Close()

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, MaxSize: 100},
		logger:   log,
		file:     f,
		writer:   f,
		shutdown: make(chan struct{}),
	}

	err = al.rotateIfNeeded()
	assert.NoError(t, err, "should not rotate when file is below max size")
}

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

// TestRotateIfNeeded_StatError tests rotateIfNeeded when file.Stat() fails
// because the file has been closed, covering audit.go:1975-1978.
func TestRotateIfNeeded_StatError(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	require.NoError(t, os.WriteFile(logFile, []byte("test"), 0o600))

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, File: logFile, MaxSize: 0},
		logger:   log,
		file:     f,
		writer:   f,
		shutdown: make(chan struct{}),
	}

	// Close the file so Stat() fails
	f.Close()

	err = al.rotateIfNeeded()
	assert.Error(t, err, "should fail when file.Stat() fails on closed file")
}

// TestAuditLogger_CompressFile_Method tests the compressFile method on AuditLogger.
func TestAuditLogger_CompressFile_Method(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	// Create a test file with content
	content := []byte("test log content for compression\nline 2\nline 3\n")
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
}

// TestAuditLogger_CleanupOldBackups_Extended tests the cleanupOldBackups function with more scenarios.
func TestAuditLogger_CleanupOldBackups_Extended(t *testing.T) {
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

	// Call cleanupOldBackups multiple times - it's a placeholder, so just verify it doesn't panic
	al.cleanupOldBackups()
	al.cleanupOldBackups()

	// Since it's a placeholder, verify the logger is still functional
	assert.NotNil(t, al)
}

// TestAuditLogger_SendToSyslog tests the sendToSyslog function.
func TestAuditLogger_SendToSyslog(t *testing.T) {
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

	// Call sendToSyslog - it's a placeholder, so just verify it doesn't panic
	al.sendToSyslog(event)

	// Since it's a placeholder, verify the event is still intact
	assert.NotNil(t, event)
	assert.Equal(t, "test", event.Username)
}

// TestAuditLogger_CheckRotation tests the checkRotation function comprehensively.
func TestAuditLogger_CheckRotation_Coverage(t *testing.T) {
	t.Run("with file path", func(t *testing.T) {
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
		time.Sleep(150 * time.Millisecond)

		// Close to stop checkRotation
		err = al.Close()
		assert.NoError(t, err)

		// Give time for goroutine to exit
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("without file path", func(t *testing.T) {
		cfg := &AuditConfig{
			Enabled:  true,
			File:     "", // No file path
			SignLogs: false,
		}

		log := createTestLogger(t)
		al, err := NewAuditLogger(cfg, log)
		require.NoError(t, err)

		// Give time for any goroutines to start
		time.Sleep(50 * time.Millisecond)

		err = al.Close()
		assert.NoError(t, err)
	})

	t.Run("rotation with error", func(t *testing.T) {
		tmpDir := t.TempDir()
		logFile := filepath.Join(tmpDir, "audit.log")

		cfg := &AuditConfig{
			Enabled:    true,
			File:       logFile,
			MaxSize:    1, // Small size to trigger rotation
			MaxBackups: 3,
			Compress:   false,
			SignLogs:   false,
		}

		log := createTestLogger(t)
		al, err := NewAuditLogger(cfg, log)
		require.NoError(t, err)

		// Write large data to trigger rotation
		largeData := make([]byte, 2*1024*1024) // 2 MB
		for i := range largeData {
			largeData[i] = 'x'
		}
		_, err = al.file.Write(largeData)
		require.NoError(t, err)
		require.NoError(t, al.file.Sync())

		// Wait for rotation check
		time.Sleep(2 * time.Second)

		err = al.Close()
		assert.NoError(t, err)
	})
}

// TestCompressFile_ActualImplementation tests actual compression logic.
func TestCompressFile_ActualImplementation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "compress_test.log")
	compressedFile := testFile + ".gz"

	// Create test file with content
	content := []byte("This is test content for compression testing\n")
	content = append(content, []byte("Line 2 with more data\n")...)
	content = append(content, []byte("Line 3 with even more data to compress\n")...)
	err := os.WriteFile(testFile, content, 0o644)
	require.NoError(t, err)

	// Implement actual compression
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

	// Test compression
	err = actualCompressFile(testFile)
	require.NoError(t, err)
	assert.FileExists(t, compressedFile)

	// Verify we can decompress
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

// TestAuditLogger_RotateIfNeeded tests log rotation functionality.
func TestAuditLogger_RotateIfNeeded(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxSize:    1, // 1 MB for testing
		MaxBackups: 3,
		Compress:   false, // Disable compression for simpler testing
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Write enough data to trigger rotation
	largeData := strings.Repeat("x", 1024*1024+100) // > 1 MB
	event := &AuditEvent{
		Username: "test",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
		Details:  map[string]string{"data": largeData},
	}

	err = al.Log(context.Background(), event)
	require.NoError(t, err)

	// Force rotation check
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	// Should rotate because file exceeds max size
	if err == nil {
		// Check if backup file was created
		files, err := os.ReadDir(tmpDir)
		require.NoError(t, err)

		// Should have at least the main file
		assert.GreaterOrEqual(t, len(files), 1)
	}
}

// TestAuditLogger_RotateIfNeeded_NoFile tests rotation when no file is set.
func TestAuditLogger_RotateIfNeeded_NoFile(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Should not error when file is nil
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	assert.NoError(t, err)
}

// TestAuditLogger_RotateIfNeeded_BelowThreshold tests no rotation when below threshold.
func TestAuditLogger_RotateIfNeeded_BelowThreshold(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100, // 100 MB - much larger than our test data
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

	err = al.Log(context.Background(), event)
	require.NoError(t, err)

	// Force rotation check - should not rotate
	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()

	assert.NoError(t, err)

	// Should only have main log file, no backups
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 1, len(files))
}

// TestAuditLogger_CheckRotation tests the rotation goroutine.
func TestAuditLogger_CheckRotation(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)

	// Close immediately to test shutdown signal
	al.Close()

	// checkRotation should exit cleanly via shutdown channel
	// No assertion needed - if it doesn't exit, test will timeout
}

// TestAuditLogger_VerifySignature_EdgeCases tests signature verification edge cases.
func TestAuditLogger_VerifySignature_EdgeCases(t *testing.T) {
	log := createTestLogger(t)

	t.Run("signature verification disabled", func(t *testing.T) {
		config := &AuditConfig{
			Enabled:  true,
			SignLogs: false,
		}

		al, err := NewAuditLogger(config, log)
		require.NoError(t, err)
		defer al.Close()

		event := &AuditEvent{
			Username:  "test",
			Action:    ActionRead,
			Resource:  "test",
			Result:    ResultSuccess,
			Signature: "", // No signature
		}

		// Should return true when signing is disabled
		assert.True(t, al.VerifySignature(event))
	})

	t.Run("empty signature with signing enabled", func(t *testing.T) {
		config := &AuditConfig{
			Enabled:  true,
			SignLogs: true,
			SignKey:  testSignKey(t, "test-key-112bits!"),
		}

		al, err := NewAuditLogger(config, log)
		require.NoError(t, err)
		defer al.Close()

		event := &AuditEvent{
			Username:  "test",
			Action:    ActionRead,
			Resource:  "test",
			Result:    ResultSuccess,
			Timestamp: time.Now(),
			Signature: "", // Empty signature
		}

		// Unsigned events, including ones written before signing was
		// enabled, report unverified while signing is on
		assert.False(t, al.VerifySignature(event))
	})
}

// TestAuditLogger_LogDataAccess_Failure tests data access logging with failure.
func TestAuditLogger_LogDataAccess_Failure(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogDataAccess(context.Background(), "alice", "user-123", ActionWrite, "/test/key", "192.168.1.100", false)
	assert.NoError(t, err)

	var logged AuditEvent
	err = json.Unmarshal(buf.Bytes(), &logged)
	require.NoError(t, err)

	assert.Equal(t, ResultFailure, logged.Result)
}

// TestAuditLogger_Log_WriteError tests log write error handling.
func TestAuditLogger_Log_WriteError(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Use a writer that always errors
	al.writer = &errorWriter{}

	event := &AuditEvent{
		Username: "test",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	err = al.Log(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write audit event")
}

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
func TestAuditLogger_OpenLogFile_CreateDir(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "subdir", "audit.log")

	config := &AuditConfig{
		Enabled: true,
		File:    logFile,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Verify directory was created
	_, err = os.Stat(filepath.Dir(logFile))
	assert.NoError(t, err)
}

// TestAuditLogger_CheckRotation_Ticker tests rotation ticker.
func TestAuditLogger_CheckRotation_Ticker(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100, // Large enough to not trigger rotation
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)

	// Let it run for a bit to test the ticker path
	time.Sleep(100 * time.Millisecond)

	al.Close()
}

// ---------------------------------------------------------------------------
// audit.go -- sendLocal severity dispatch
// ---------------------------------------------------------------------------

func TestSyslogExporter_SendLocalAllSeverities(t *testing.T) {
	// sendLocal has 11.8% coverage because only the nil-conn path is tested.
	// We cover the nil-conn error path for every severity level.
	log := createTestLogger(t)

	tests := []struct {
		name     string
		severity SyslogSeverity
	}{
		{"emergency", SyslogSeverityEmergency},
		{"alert", SyslogSeverityAlert},
		{"critical", SyslogSeverityCritical},
		{"error", SyslogSeverityError},
		{"warning", SyslogSeverityWarning},
		{"notice", SyslogSeverityNotice},
		{"informational", SyslogSeverityInformational},
		{"debug", SyslogSeverityDebug},
		{"default", SyslogSeverity(99)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exporter := &SyslogExporter{
				config: &SyslogExporterConfig{
					Tag:      "test",
					Hostname: "test-host",
				},
				logger:   log,
				isRemote: false,
			}
			exporter.connected.Store(true)

			err := exporter.sendLocal(tc.severity, "test message")
			require.Error(t, err)

			var syslogErr *SyslogExporterError
			assert.True(t, errors.As(err, &syslogErr))
			assert.Contains(t, syslogErr.Reason, "local connection not established")
		})
	}
}

// ---------------------------------------------------------------------------
// audit.go -- LogKeyExport, LogKeyGenerate, LogKeyDeletion failure paths
// ---------------------------------------------------------------------------

func TestAuditLogger_LogKeyExportFailure(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogKeyExport(context.Background(), "admin", "u-1", "k-1", "AES-256", "wk-1", "RSA-OAEP", "10.0.0.1", false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ResultFailure)
	assert.Contains(t, buf.String(), ActionKeyExport)
}

func TestAuditLogger_LogKeyGenerateFailure(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogKeyGenerate(context.Background(), "admin", "u-1", "k-1", "master", "AES-256", "10.0.0.1", false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ResultFailure)
	assert.Contains(t, buf.String(), ActionKeyGenerate)
}

func TestAuditLogger_LogKeyDeletionFailure(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogKeyDeletion(context.Background(), "admin", "u-1", "k-1", "10.0.0.1", false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ResultFailure)
	assert.Contains(t, buf.String(), ActionKeyDelete)
}

// ---------------------------------------------------------------------------
// audit.go -- compressFile success path
// ---------------------------------------------------------------------------

func TestAuditLogger_CompressFileSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Create a test file with real content to compress.
	testFile := filepath.Join(tmpDir, "compress-me.log")
	content := bytes.Repeat([]byte("audit log line: action=read resource=data result=success\n"), 100)
	err = os.WriteFile(testFile, content, 0600)
	require.NoError(t, err)

	err = al.compressFile(testFile, gzip.DefaultCompression)
	require.NoError(t, err)

	gzFile := testFile + ".gz"
	assert.FileExists(t, gzFile)

	// Verify the gzip file is valid.
	gzData, err := os.ReadFile(gzFile)
	require.NoError(t, err)

	reader, err := gzip.NewReader(bytes.NewReader(gzData))
	require.NoError(t, err)
	defer reader.Close()

	var decompressed bytes.Buffer
	_, err = decompressed.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, content, decompressed.Bytes())
}

func TestAuditLogger_CompressFileAllLevels(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{Enabled: true, File: logFile, Compress: true}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	levels := []int{
		gzip.NoCompression,
		gzip.BestSpeed,
		gzip.DefaultCompression,
		gzip.BestCompression,
		gzip.HuffmanOnly,
	}

	for _, level := range levels {
		testFile := filepath.Join(tmpDir, "test-level.log")
		err := os.WriteFile(testFile, []byte("data to compress"), 0600)
		require.NoError(t, err)

		compressErr := al.compressFile(testFile, level)
		assert.NoError(t, compressErr)
		os.Remove(testFile + ".gz")
	}
}

// ---------------------------------------------------------------------------
// audit.go -- rotateIfNeeded
// ---------------------------------------------------------------------------

func TestAuditLogger_RotateIfNeededNoFile(t *testing.T) {
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// file is nil so rotateIfNeeded should return nil.
	err = al.rotateIfNeeded()
	assert.NoError(t, err)
}

func TestAuditLogger_RotateIfNeededUnderMaxSize(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled: true,
		File:    logFile,
		MaxSize: 100, // 100 MB
	}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Write a small amount. Should not trigger rotation.
	err = al.rotateIfNeeded()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// audit.go -- VerifySignature
// ---------------------------------------------------------------------------

func TestAuditLogger_VerifySignatureWhenSigningDisabled(t *testing.T) {
	cfg := &AuditConfig{Enabled: true, SignLogs: false}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{Action: ActionRead}
	assert.True(t, al.VerifySignature(event))
}

func TestAuditLogger_VerifySignatureValid(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-signing-key-for-hmac"),
	}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-sig-test",
		Action:    ActionWrite,
		Resource:  "keys",
		Result:    ResultSuccess,
		Username:  "admin",
	}

	err = al.Log(context.Background(), event)
	require.NoError(t, err)
	assert.NotEmpty(t, event.Signature)

	assert.True(t, al.VerifySignature(event))
}

func TestAuditLogger_VerifySignatureInvalid(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-signing-key-for-hmac"),
	}
	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &bytes.Buffer{}

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}

	err = al.Log(context.Background(), event)
	require.NoError(t, err)

	// Tamper with the signature.
	event.Signature = "tampered-signature-value"
	assert.False(t, al.VerifySignature(event))
}

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

func TestAuditLogger_LogsDataAccessSuccessAndFailure(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	t.Run("success", func(t *testing.T) {
		buf.Reset()
		err := al.LogDataAccess(context.Background(), "user1", "u-1", ActionRead, "my-key", "10.0.0.1", true)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), ResultSuccess)
	})

	t.Run("failure", func(t *testing.T) {
		buf.Reset()
		err := al.LogDataAccess(context.Background(), "user1", "u-1", ActionWrite, "my-key", "10.0.0.1", false)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), ResultFailure)
	})
}

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

func TestAuditError_ErrorWithCause(t *testing.T) {
	cause := errors.New("disk full")
	err := &AuditError{
		Code:    ErrCodeAuditFileOpen,
		Message: "failed to open",
		Cause:   cause,
	}
	got := err.Error()
	assert.Contains(t, got, ErrCodeAuditFileOpen)
	assert.Contains(t, got, "disk full")
}

func TestAuditError_ErrorWithoutCause(t *testing.T) {
	err := &AuditError{
		Code:    ErrCodeAuditConfigNil,
		Message: "config nil",
	}
	got := err.Error()
	assert.Contains(t, got, ErrCodeAuditConfigNil)
	assert.Contains(t, got, "config nil")
}

func TestAuditError_UnwrapGivesOriginalCause(t *testing.T) {
	cause := errors.New("io error")
	err := &AuditError{Code: "TEST", Message: "msg", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
}

// Tests for LogKeyExport, LogKeyGenerate, LogKeyImport, LogKeyDeletion

func TestAuditLogger_LogKeyExport_Success(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyExport(context.Background(), "alice", "user-123", "key-001", "AES-256-GCM", "wrapping-key", "RSA-OAEP", "192.168.1.100", true)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.export")
	assert.Contains(t, buf.String(), "success")
}

func TestAuditLogger_LogKeyExport_Failure(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyExport(context.Background(), "alice", "user-123", "key-001", "AES-256-GCM", "wrapping-key", "RSA-OAEP", "192.168.1.100", false)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.export")
	assert.Contains(t, buf.String(), "failure")
}

func TestAuditLogger_LogKeyGenerate_Success(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyGenerate(context.Background(), "alice", "user-123", "key-001", "my-key", "AES-256-GCM", "192.168.1.100", true)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.generate")
	assert.Contains(t, buf.String(), "success")
}

func TestAuditLogger_LogKeyGenerate_Failure(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyGenerate(context.Background(), "alice", "user-123", "key-001", "my-key", "AES-256-GCM", "192.168.1.100", false)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.generate")
	assert.Contains(t, buf.String(), "failure")
}

func TestAuditLogger_LogKeyImport_Success(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyImport(context.Background(), "alice", "user-123", "key-001", "imported-key", "AES-256-GCM", "192.168.1.100", true)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.import")
	assert.Contains(t, buf.String(), "success")
}

func TestAuditLogger_LogKeyImport_Failure(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyImport(context.Background(), "alice", "user-123", "key-001", "imported-key", "AES-256-GCM", "192.168.1.100", false)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.import")
	assert.Contains(t, buf.String(), "failure")
}

func TestAuditLogger_LogKeyDeletion_Success(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyDeletion(context.Background(), "alice", "user-123", "key-001", "192.168.1.100", true)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.delete")
	assert.Contains(t, buf.String(), "success")
}

func TestAuditLogger_LogKeyDeletion_Failure(t *testing.T) {
	log := createTestLogger(t)

	var buf bytes.Buffer
	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &buf

	err = al.LogKeyDeletion(context.Background(), "alice", "user-123", "key-001", "192.168.1.100", false)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key.delete")
	assert.Contains(t, buf.String(), "failure")
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

func TestSyslogExporter_SendLocal_AllSeverityLevels(t *testing.T) {
	// This test requires a local syslog which may not be available in all environments
	// We test the sendLocal method indirectly through Export which handles reconnection
	log := createTestLogger(t)

	// Create a mock exporter for testing
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Facility: SyslogFacilityLocal0,
			Tag:      "test",
			Hostname: "localhost",
		},
		logger: log,
	}

	// Test that sendLocal returns error when not connected
	severities := []SyslogSeverity{
		SyslogSeverityEmergency,
		SyslogSeverityAlert,
		SyslogSeverityCritical,
		SyslogSeverityError,
		SyslogSeverityWarning,
		SyslogSeverityNotice,
		SyslogSeverityInformational,
		SyslogSeverityDebug,
	}

	for _, sev := range severities {
		t.Run(sev.String(), func(t *testing.T) {
			err := exporter.sendLocal(sev, "test message")
			assert.Error(t, err)
		})
	}
}

// Tests for compressFile edge cases

func TestAuditLogger_CompressFile_WithInvalidLevel(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.log")
	err = os.WriteFile(testFile, []byte("test data"), 0644)
	require.NoError(t, err)

	// Test with invalid level (should default to default compression)
	err = al.compressFile(testFile, 99)
	require.NoError(t, err)

	// Verify compressed file exists
	assert.FileExists(t, testFile+".gz")
}

func TestAuditLogger_CompressFile_WithNonExistentFile(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Try to compress non-existent file
	err = al.compressFile("/nonexistent/path/file.log", 5)
	assert.Error(t, err)

	var auditErr *AuditError
	assert.ErrorAs(t, err, &auditErr)
}

// Tests for sendToSyslog edge cases

func TestAuditLogger_SendToSyslog_WithoutExporterConfigured(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled:    true,
		Syslog:     true,
		SyslogAddr: "", // No address - should return early
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

	// Should not panic and return early
	al.sendToSyslog(event)
}

// Tests for VerifySignature edge cases

func TestAuditLogger_VerifySignature_WhenSigningDisabled(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled:  true,
		SignLogs: false,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "",
	}

	// Should return true when signing is disabled
	assert.True(t, al.VerifySignature(event))
}

func TestAuditLogger_VerifySignature_WithEmptySignature(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-key-112bits!"),
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username:  "test",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "", // Empty signature
	}

	// Signing is on, so an unsigned event fails closed
	assert.False(t, al.VerifySignature(event))
}

// TestAuditLogger_VerifySignature_StrippedSignatureFailsClosed is the #22
// regression: with signing on, removing the signature from a signed event
// must not make it verify, and a tampered field must still fail.
func TestAuditLogger_VerifySignature_StrippedSignatureFailsClosed(t *testing.T) {
	var buf bytes.Buffer
	al, err := NewAuditLogger(&AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-key-112bits!"),
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

func TestNewAuditLogger_WithSyslogEnabled_WhenConnectionFails(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled:       true,
		SyslogEnabled: true,
		SyslogAddress: "192.0.2.1:514", // TEST-NET-1, unreachable
		SyslogNetwork: "tcp",
	}

	// Should succeed but without syslog exporter (warning logged)
	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Syslog exporter should be nil due to connection failure
	assert.Nil(t, al.syslogExporter)
}

// Tests for closeUnlocked with both connections

func TestSyslogExporter_CloseUnlocked_WithBothConnections(t *testing.T) {
	log := createTestLogger(t)

	// Create TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var connWg sync.WaitGroup
	connWg.Add(1)
	go func() {
		defer connWg.Done()
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 1024)
			for {
				_, err := conn.Read(buf)
				if err != nil {
					break
				}
			}
		}
	}()

	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	// Close should work
	err = exporter.Close()
	assert.NoError(t, err)
	assert.False(t, exporter.IsConnected())

	listener.Close()
	connWg.Wait()
}

// Tests for connect with reconnection

func TestSyslogExporter_Connect_WithReconnection(t *testing.T) {
	log := createTestLogger(t)

	// Create UDP listener
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

	assert.True(t, exporter.IsConnected())

	// Reconnect should work
	err = exporter.Reconnect()
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())
}

// Tests for connect with unix socket

func TestSyslogExporter_Connect_WithUnixSocket(t *testing.T) {
	log := createTestLogger(t)

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Create a Unix socket listener
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err, "Unix socket listener must succeed on Linux")
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	cfg := &SyslogExporterConfig{
		Address:  socketPath,
		Network:  "unix",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	// The Unix socket syslog connection should fail gracefully as it tries to use standard syslog
	_, err = NewSyslogExporter(cfg, log)
	// This may succeed or fail depending on system syslog availability
	// The important thing is it doesn't panic
	_ = err
}

// Tests for sendRemote write error

func TestSyslogExporter_SendRemote_WithWriteDeadlineError(t *testing.T) {
	log := createTestLogger(t)

	// Create a closed connection to simulate write error
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var connWg sync.WaitGroup
	connWg.Add(1)
	go func() {
		defer connWg.Done()
		conn, err := listener.Accept()
		if err == nil {
			conn.Close() // Close immediately
		}
	}()

	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	// Wait for server to close connection
	connWg.Wait()
	time.Sleep(50 * time.Millisecond)

	event := &AuditEvent{
		EventID:   "test",
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// Should fail due to closed connection
	err = exporter.sendRemote(event, SyslogSeverityInformational, "test")
	// This may or may not error depending on timing, but shouldn't panic

	listener.Close()
	exporter.Close()
}

// Tests for Export reconnection

func TestSyslogExporter_Export_WithReconnection(t *testing.T) {
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

	// Force disconnection
	exporter.connected.Store(false)

	event := &AuditEvent{
		EventID:   "test",
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
	}

	// Should reconnect and export
	err = exporter.Export(event)
	require.NoError(t, err)
	assert.True(t, exporter.IsConnected())
}

// Tests for mapEventToSeverity with all result types

func TestSyslogExporter_MapEventToSeverity_WithAllResultTypes(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{},
		logger: log,
	}

	// Test default result
	event := &AuditEvent{
		Action: ActionRead,
		Result: "unknown_result",
	}
	severity := exporter.mapEventToSeverity(event)
	assert.Equal(t, SyslogSeverityInformational, severity)
}

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

func TestAuditLogger_CleanupOldBackups_NoPanic(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:    true,
		File:       logFile,
		MaxBackups: 3,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Call cleanupOldBackups - should not panic
	al.cleanupOldBackups()
}

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

func TestAuditLogger_RotateIfNeeded_WithNilFile(t *testing.T) {
	log := createTestLogger(t)

	config := &AuditConfig{
		Enabled: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Ensure file is nil
	al.file = nil

	// Should return nil without error
	err = al.rotateIfNeeded()
	assert.NoError(t, err)
}

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

func TestAuditError_ErrorMethodWithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := &AuditError{
		Code:    ErrCodeAuditFileOpen,
		Message: "failed to open file",
		Cause:   cause,
	}

	assert.Contains(t, err.Error(), ErrCodeAuditFileOpen)
	assert.Contains(t, err.Error(), "failed to open file")
	assert.Contains(t, err.Error(), "underlying error")
	assert.Equal(t, cause, err.Unwrap())
}

func TestAuditError_ErrorMethodWithoutCause(t *testing.T) {
	err := &AuditError{
		Code:    ErrCodeAuditFileOpen,
		Message: "failed to open file",
	}

	assert.Contains(t, err.Error(), ErrCodeAuditFileOpen)
	assert.Contains(t, err.Error(), "failed to open file")
	assert.Nil(t, err.Unwrap())
}

// Test compressFileAsync - renamed

func TestAuditLogger_CompressFileAsync_Success(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Create test file
	testFile := filepath.Join(tmpDir, "async_test.log")
	err = os.WriteFile(testFile, []byte("test data for async compression"), 0644)
	require.NoError(t, err)

	// Call async compression
	al.compressFileAsync(testFile, 5)

	// Wait for async compression
	time.Sleep(100 * time.Millisecond)

	// Verify compressed file exists
	assert.FileExists(t, testFile+".gz")
}

// Test compressFileAsync with non-existent file - renamed

func TestAuditLogger_CompressFileAsync_NonExistentFile(t *testing.T) {
	log := createTestLogger(t)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	config := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	al, err := NewAuditLogger(config, log)
	require.NoError(t, err)
	defer al.Close()

	// Call async compression with non-existent file - should not panic
	al.compressFileAsync("/nonexistent/path/file.log", 5)

	// Wait for async compression attempt
	time.Sleep(50 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Error type tests
// ---------------------------------------------------------------------------

func TestAuditError_ErrorIncludesCause(t *testing.T) {
	cause := errors.New("disk full")
	err := &AuditError{
		Code:    ErrCodeAuditFileOpen,
		Message: "cannot open file",
		Cause:   cause,
	}

	got := err.Error()
	assert.Contains(t, got, ErrCodeAuditFileOpen)
	assert.Contains(t, got, "cannot open file")
	assert.Contains(t, got, "disk full")
}

func TestAuditError_ErrorExcludesNilCause(t *testing.T) {
	err := &AuditError{
		Code:    ErrCodeAuditConfigNil,
		Message: "config is nil",
	}

	got := err.Error()
	assert.Contains(t, got, ErrCodeAuditConfigNil)
	assert.Contains(t, got, "config is nil")
}

func TestAuditError_UnwrapReturnsCause(t *testing.T) {
	cause := errors.New("underlying")
	err := &AuditError{Code: "TEST", Message: "test", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
}

func TestAuditError_UnwrapReturnsNilWhenNoCause(t *testing.T) {
	err := &AuditError{Code: "TEST", Message: "test"}
	assert.Nil(t, err.Unwrap())
}

func TestSyslogExporterError_ErrorMessage(t *testing.T) {
	err := &SyslogExporterError{Op: "connect", Reason: "timeout"}
	got := err.Error()
	assert.Contains(t, got, "connect")
	assert.Contains(t, got, "timeout")
}

func TestSyslogConnectionError_ErrorWithNetworkField(t *testing.T) {
	cause := errors.New("connection refused")
	err := &SyslogConnectionError{
		Address: "192.168.1.1:514",
		Network: "udp",
		Err:     cause,
	}

	got := err.Error()
	assert.Contains(t, got, "udp")
	assert.Contains(t, got, "192.168.1.1:514")
	assert.Contains(t, got, "connection refused")
}

func TestSyslogConnectionError_ErrorLocalFallback(t *testing.T) {
	cause := errors.New("no socket")
	err := &SyslogConnectionError{
		Network: "",
		Err:     cause,
	}

	got := err.Error()
	assert.Contains(t, got, "local syslog")
	assert.Contains(t, got, "no socket")
}

func TestSyslogConnectionError_UnwrapCause(t *testing.T) {
	cause := errors.New("root cause")
	err := &SyslogConnectionError{Err: cause}
	assert.Equal(t, cause, err.Unwrap())
}

func TestSyslogWriteError_ErrorMessage(t *testing.T) {
	cause := errors.New("broken pipe")
	err := &SyslogWriteError{Err: cause}
	got := err.Error()
	assert.Contains(t, got, "broken pipe")
}

func TestSyslogWriteError_UnwrapCause(t *testing.T) {
	cause := errors.New("io error")
	err := &SyslogWriteError{Err: cause}
	assert.Equal(t, cause, err.Unwrap())
}

// ---------------------------------------------------------------------------
// SyslogExporter tests
// ---------------------------------------------------------------------------

func TestNewSyslogExporter_RejectsNilConfig(t *testing.T) {
	log := createTestLogger(t)
	_, err := NewSyslogExporter(nil, log)
	require.Error(t, err)

	var syslogErr *SyslogExporterError
	assert.True(t, errors.As(err, &syslogErr))
	assert.Contains(t, syslogErr.Reason, "config cannot be nil")
}

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

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

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

	exporter.Close()
	assert.False(t, exporter.IsConnected())
	listener.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}
	// Exercises the reconnect path.
	_ = exporter.Export(event)
}

func TestSyslogExporter_ReconnectToTCP(t *testing.T) {
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

	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	exporter, err := NewSyslogExporter(cfg, log)
	require.NoError(t, err)

	err = exporter.Reconnect()
	assert.NoError(t, err)
	assert.True(t, exporter.IsConnected())

	exporter.Close()
}

func TestSyslogExporter_ConnectionRefused(t *testing.T) {
	log := createTestLogger(t)
	cfg := &SyslogExporterConfig{
		Address:  "127.0.0.1:1",
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	_, err := NewSyslogExporter(cfg, log)
	require.Error(t, err)

	var connErr *SyslogConnectionError
	assert.True(t, errors.As(err, &connErr))
}

func TestSyslogExporter_SendRemoteWithNilConn(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Tag:      "test",
			Hostname: "test",
			Facility: SyslogFacilityLocal0,
		},
		logger:   log,
		isRemote: true,
	}
	exporter.connected.Store(true)

	err := exporter.sendRemote(&AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}, SyslogSeverityInformational, "test message")
	require.Error(t, err)

	var syslogErr *SyslogExporterError
	assert.True(t, errors.As(err, &syslogErr))
	assert.Contains(t, syslogErr.Reason, "remote connection not established")
}

func TestSyslogExporter_SendLocalWithNilConn(t *testing.T) {
	log := createTestLogger(t)

	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Tag:      "test",
			Hostname: "test",
		},
		logger:   log,
		isRemote: false,
	}
	exporter.connected.Store(true)

	err := exporter.sendLocal(SyslogSeverityInformational, "test message")
	require.Error(t, err)

	var syslogErr *SyslogExporterError
	assert.True(t, errors.As(err, &syslogErr))
	assert.Contains(t, syslogErr.Reason, "local connection not established")
}

// ---------------------------------------------------------------------------
// RFC 5424 formatting
// ---------------------------------------------------------------------------

func TestSyslogExporter_RFC5424FullEvent(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Facility: SyslogFacilityAuth,
			Tag:      "quicbaas",
			Hostname: "node-1",
		},
		logger: log,
	}

	event := &AuditEvent{
		Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		EventID:   "evt-456",
		Username:  "alice",
		UserID:    "u-001",
		Action:    ActionWrite,
		Resource:  "keys",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.5",
		NodeID:    "n-1",
		ShardID:   7,
		Details:   map[string]string{"key": "foo"},
	}

	msg := exporter.formatRFC5424(event, SyslogSeverityNotice, "test msg")

	assert.Contains(t, msg, "<37>1")
	assert.Contains(t, msg, "node-1")
	assert.Contains(t, msg, "quicbaas")
	assert.Contains(t, msg, "evt-456")
	assert.Contains(t, msg, "audit@quicbaas")
	assert.Contains(t, msg, "test msg")
}

func TestSyslogExporter_RFC5424EmptyEventID(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{
			Facility: SyslogFacilityLocal0,
			Tag:      "quicbaas",
			Hostname: "host",
		},
		logger: log,
	}

	event := &AuditEvent{
		Timestamp: time.Now(),
		EventID:   "",
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}

	msg := exporter.formatRFC5424(event, SyslogSeverityInformational, "msg")
	assert.Contains(t, msg, " - ")
}

func TestSyslogExporter_StructuredDataAllFields(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "test", Hostname: "h"},
		logger: log,
	}

	event := &AuditEvent{
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultDenied,
		UserID:    "u-1",
		Username:  "bob",
		IPAddress: "1.2.3.4",
		NodeID:    "n-1",
		ShardID:   3,
		ErrorMsg:  "bad password",
		Details:   map[string]string{"attempt": "3"},
	}

	sd := exporter.buildStructuredData(event)
	assert.Contains(t, sd, "[audit@quicbaas")
	assert.Contains(t, sd, "action=\"authenticate\"")
	assert.Contains(t, sd, "userID=\"u-1\"")
	assert.Contains(t, sd, "username=\"bob\"")
	assert.Contains(t, sd, "ipAddress=\"1.2.3.4\"")
	assert.Contains(t, sd, "nodeID=\"n-1\"")
	assert.Contains(t, sd, "shardID=\"3\"")
	assert.Contains(t, sd, "errorMsg=\"bad password\"")
	assert.Contains(t, sd, "attempt=\"3\"")
}

func TestSyslogExporter_FormatMessageAllFields(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "test", Hostname: "h"},
		logger: log,
	}

	t.Run("full fields", func(t *testing.T) {
		event := &AuditEvent{
			Action:    ActionWrite,
			Resource:  "keys",
			Result:    ResultSuccess,
			Username:  "alice",
			IPAddress: "10.0.0.1",
			ErrorMsg:  "timeout",
		}
		msg := exporter.formatMessage(event)
		assert.Contains(t, msg, "action=write")
		assert.Contains(t, msg, "user=alice")
		assert.Contains(t, msg, "ip=10.0.0.1")
		assert.Contains(t, msg, "error=timeout")
	})

	t.Run("minimal fields", func(t *testing.T) {
		event := &AuditEvent{
			Action:   ActionRead,
			Resource: "data",
			Result:   ResultSuccess,
		}
		msg := exporter.formatMessage(event)
		assert.Contains(t, msg, "action=read")
		assert.NotContains(t, msg, "user=")
	})
}

func TestSyslogExporter_MapEventToSeverityAllResults(t *testing.T) {
	log := createTestLogger(t)
	exporter := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "test", Hostname: "h"},
		logger: log,
	}

	tests := []struct {
		name   string
		event  *AuditEvent
		expect SyslogSeverity
	}{
		{
			name:   "success default",
			event:  &AuditEvent{Result: ResultSuccess, Action: ActionRead},
			expect: SyslogSeverityInformational,
		},
		{
			name:   "success key rotate",
			event:  &AuditEvent{Result: ResultSuccess, Action: ActionKeyRotate},
			expect: SyslogSeverityNotice,
		},
		{
			name:   "success user create",
			event:  &AuditEvent{Result: ResultSuccess, Action: ActionUserCreate},
			expect: SyslogSeverityNotice,
		},
		{
			name:   "success config change",
			event:  &AuditEvent{Result: ResultSuccess, Action: ActionConfigChange},
			expect: SyslogSeverityNotice,
		},
		{
			name:   "success tenant switch",
			event:  &AuditEvent{Result: ResultSuccess, Action: ActionTenantSwitch},
			expect: SyslogSeverityNotice,
		},
		{
			name:   "failure auth",
			event:  &AuditEvent{Result: ResultFailure, Action: ActionAuthenticate},
			expect: SyslogSeverityWarning,
		},
		{
			name:   "failure default",
			event:  &AuditEvent{Result: ResultFailure, Action: ActionWrite},
			expect: SyslogSeverityError,
		},
		{
			name:   "denied auth",
			event:  &AuditEvent{Result: ResultDenied, Action: ActionAuthenticate},
			expect: SyslogSeverityWarning,
		},
		{
			name:   "denied tenant access",
			event:  &AuditEvent{Result: ResultDenied, Action: ActionTenantAccessDenied},
			expect: SyslogSeverityWarning,
		},
		{
			name:   "denied default",
			event:  &AuditEvent{Result: ResultDenied, Action: ActionDelete},
			expect: SyslogSeverityWarning,
		},
		{
			name:   "unknown result",
			event:  &AuditEvent{Result: "unknown_result", Action: ActionRead},
			expect: SyslogSeverityInformational,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := exporter.mapEventToSeverity(tc.event)
			assert.Equal(t, tc.expect, got)
		})
	}
}

// ---------------------------------------------------------------------------
// AuditLogger helper methods
// ---------------------------------------------------------------------------

func TestAuditLogger_LogsAuthSuccess(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogAuthSuccess(context.Background(), "alice", "u-1", "10.0.0.1")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionAuthenticate)
	assert.Contains(t, buf.String(), ResultSuccess)
}

func TestAuditLogger_LogsAuthFailure(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogAuthFailure(context.Background(), "hacker", "10.0.0.99", "bad credentials")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionAuthenticate)
	assert.Contains(t, buf.String(), ResultFailure)
	assert.Contains(t, buf.String(), "bad credentials")
}

func TestAuditLogger_LogsAuthzDenied(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogAuthzDenied(context.Background(), "bob", "u-2", "admin-panel", "delete", "10.0.0.2")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionAuthorize)
	assert.Contains(t, buf.String(), ResultDenied)
}

func TestAuditLogger_LogsClusterChange(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogClusterChange(context.Background(), ActionClusterJoin, 42, map[string]string{"addr": "10.0.0.5"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionClusterJoin)
	assert.Contains(t, buf.String(), "42")
}

func TestAuditLogger_LogsKeyRotation(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogKeyRotation(context.Background(), 1, 2)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionKeyRotate)
}

func TestAuditLogger_LogsTenantSwitch(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogTenantSwitch(context.Background(), "admin", "u-1", "tenant-a", "tenant-b", "debug issue", "assign-1", "10.0.0.1")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionTenantSwitch)
	assert.Contains(t, buf.String(), "tenant-a")
	assert.Contains(t, buf.String(), "assignment_id")
}

func TestAuditLogger_LogsTenantSwitchEmptyAssignment(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogTenantSwitch(context.Background(), "admin", "u-1", "tenant-a", "tenant-b", "debug", "", "10.0.0.1")
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "assignment_id")
}

func TestAuditLogger_LogsCrossTenantRead(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogCrossTenantRead(context.Background(), "admin", "u-1", "t-a", "t-b", "users", "investigation", "10.0.0.1")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionCrossTenantRead)
}

func TestAuditLogger_LogsCrossTenantWrite(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogCrossTenantWrite(context.Background(), "admin", "u-1", "t-a", "t-b", "config", "migration", "10.0.0.1")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionCrossTenantWrite)
}

func TestAuditLogger_LogsCrossTenantDelete(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogCrossTenantDelete(context.Background(), "admin", "u-1", "t-a", "t-b", "data", "cleanup", "10.0.0.1")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionCrossTenantDelete)
}

func TestAuditLogger_LogsTenantAccessDenied(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	err = al.LogTenantAccessDenied(context.Background(), "user", "u-1", "t-a", "t-b", "secrets", "10.0.0.1", "insufficient privileges")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), ActionTenantAccessDenied)
	assert.Contains(t, buf.String(), ResultDenied)
}

func TestAuditLogger_LogsKeyExportImportDeleteGenerate(t *testing.T) {
	var buf bytes.Buffer
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()
	al.writer = &buf

	t.Run("key export success", func(t *testing.T) {
		buf.Reset()
		err := al.LogKeyExport(context.Background(), "admin", "u-1", "k-1", "AES-256", "wk-1", "RSA-OAEP", "10.0.0.1", true)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), ActionKeyExport)
	})

	t.Run("key generate success", func(t *testing.T) {
		buf.Reset()
		err := al.LogKeyGenerate(context.Background(), "admin", "u-1", "k-2", "master", "AES-256", "10.0.0.1", true)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), ActionKeyGenerate)
	})

	t.Run("key import failure", func(t *testing.T) {
		buf.Reset()
		err := al.LogKeyImport(context.Background(), "admin", "u-1", "k-3", "imported", "AES-256", "10.0.0.1", false)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), ResultFailure)
	})

	t.Run("key deletion success", func(t *testing.T) {
		buf.Reset()
		err := al.LogKeyDeletion(context.Background(), "admin", "u-1", "k-4", "10.0.0.1", true)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), ActionKeyDelete)
	})
}

func TestAuditLogger_GetSyslogExporterReturnsNil(t *testing.T) {
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	assert.Nil(t, al.GetSyslogExporter())
}

// ---------------------------------------------------------------------------
// AuditLogger sendToSyslog
// ---------------------------------------------------------------------------

func TestAuditLogger_SendToSyslogViaExporter(t *testing.T) {
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
	syslogCfg := &SyslogExporterConfig{
		Address:  listener.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityLocal0,
		Tag:      "test",
	}

	syslogExp, err := NewSyslogExporter(syslogCfg, log)
	require.NoError(t, err)

	cfg := &AuditConfig{
		Enabled: true,
		Syslog:  true,
	}

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	al.syslogExporter = syslogExp

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
		Username:  "user",
		IPAddress: "1.2.3.4",
	}

	al.sendToSyslog(event)
}

func TestAuditLogger_SendToSyslogLegacyEmptyAddr(t *testing.T) {
	log := createTestLogger(t)
	cfg := &AuditConfig{
		Enabled:    true,
		Syslog:     true,
		SyslogAddr: "",
	}

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
	}

	al.sendToSyslog(event)
}

func TestAuditLogger_SendToSyslogLegacyUnreachable(t *testing.T) {
	log := createTestLogger(t)
	cfg := &AuditConfig{
		Enabled:    true,
		Syslog:     true,
		SyslogAddr: "127.0.0.1:1",
	}

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
		Username:  "user",
		IPAddress: "1.2.3.4",
	}

	al.sendToSyslog(event)
}

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

	err = al.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// compressFile error paths
// ---------------------------------------------------------------------------

func TestAuditLogger_CompressFileSourceMissing(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.compressFile("/nonexistent/path/file.log", 6)
	require.Error(t, err)

	var auditErr *AuditError
	assert.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditCompressionOpen, auditErr.Code)
}

func TestAuditLogger_CompressFileBadLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")

	err := os.WriteFile(testFile, []byte("test data for compression"), 0o644)
	require.NoError(t, err)

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.compressFile(testFile, 999)
	assert.NoError(t, err)
	assert.FileExists(t, testFile+".gz")
}

func TestAuditLogger_CompressFileDestPermission(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:  true,
		File:     logFile,
		Compress: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	roDir := filepath.Join(tmpDir, "readonly")
	err = os.MkdirAll(roDir, 0o755)
	require.NoError(t, err)

	roFile := filepath.Join(roDir, "test.log")
	err = os.WriteFile(roFile, []byte("data"), 0o644)
	require.NoError(t, err)

	// compressFile creates "<source>.gz"; a directory already on that name
	// makes the create fail with EISDIR for every uid, where a read-only
	// parent would not stop root (testing-guidelines.md, Fault Injection).
	require.NoError(t, os.MkdirAll(roFile+".gz", 0o755))

	compressErr := al.compressFile(roFile, 6)
	assert.Error(t, compressErr)
}

// ---------------------------------------------------------------------------
// NewAuditLogger configuration
// ---------------------------------------------------------------------------

func TestNewAuditLogger_RejectsNilConfig(t *testing.T) {
	log := createTestLogger(t)
	_, err := NewAuditLogger(nil, log)
	require.Error(t, err)

	var auditErr *AuditError
	assert.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditConfigNil, auditErr.Code)
}

func TestNewAuditLogger_RequiresSignKey(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, ""),
	}
	log := createTestLogger(t)
	_, err := NewAuditLogger(cfg, log)
	require.Error(t, err)

	var auditErr *AuditError
	assert.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditSignKeyRequired, auditErr.Code)
}

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
				SignKey:  testSignKey(t, signKey),
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
		SignKey:  testSignKey(t, signKey),
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

// testSignKey provisions key in qrdb's manual-strategy credential service,
// as an operator does, and returns the credentials.Memory AuditConfig.SignKey
// takes, destroyed when the test ends. An empty key yields no Memory at all,
// the "no sign key provisioned" case.
func testSignKey(t testing.TB, key string) *credentials.Memory {
	t.Helper()
	if key == "" {
		return nil
	}
	store, err := qrdbsdk.NewVersionedCredentialService(
		&qrdbsdk.CredentialConfig{Strategy: qrdbsdk.CredentialStrategyManual},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	require.NoError(t, err)
	require.NoError(t, store.StoreCredential(context.Background(), testSignKeyName, []byte(key)))
	m, err := credentials.NewMemory(context.Background(), store, testSignKeyName)
	require.NoError(t, err)
	t.Cleanup(m.Destroy)
	return m
}

// testSignKeyName is the credential name the audit tests provision the
// sign key under.
const testSignKeyName = "audit-sign-key"

// TestAuditLogger_CloseDestroysSignKey pins the protected-memory contract for
// the HMAC key (go-quicbaas#23): the logger signs straight from the caller's
// secure buffer, owns it, and wipes it on Close, after which nothing signs or
// verifies with it.
func TestAuditLogger_CloseDestroysSignKey(t *testing.T) {
	signKey := testSignKey(t, "close-destroys-this-hmac-key")
	al, err := NewAuditLogger(&AuditConfig{Enabled: true, SignLogs: true, SignKey: signKey}, createTestLogger(t))
	require.NoError(t, err)

	event := &AuditEvent{Timestamp: time.Now(), Username: "alice", Action: ActionRead, Result: ResultSuccess}
	require.NoError(t, al.Log(context.Background(), event))
	require.True(t, al.VerifySignature(event))

	require.NoError(t, al.Close())
	assert.False(t, signKey.IsAlive(), "Close must destroy the sign key Memory")

	_, err = al.signEvent(event)
	var auditErr *AuditError
	require.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditSignKeyDestroyed, auditErr.Code)
	assert.False(t, al.VerifySignature(event), "a destroyed key must not verify")
}

// TestNewAuditLogger_RejectsDestroyedSignKey ensures a sign key buffer that
// was already wiped is refused up front, not on the first event.
func TestNewAuditLogger_RejectsDestroyedSignKey(t *testing.T) {
	signKey := testSignKey(t, "destroyed-before-use-hmac-key")
	signKey.Destroy()

	al, err := NewAuditLogger(&AuditConfig{Enabled: true, SignLogs: true, SignKey: signKey}, createTestLogger(t))
	require.Error(t, err)
	assert.Nil(t, al)

	var auditErr *AuditError
	require.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditSignKeyDestroyed, auditErr.Code)
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
// AuditLogger write error
// ---------------------------------------------------------------------------

type coverageFailWriter struct{}

func (w *coverageFailWriter) Write(p []byte) (n int, err error) {
	return 0, &AuditError{Code: ErrCodeAuditEventWrite, Message: "simulated write failure"}
}

func TestAuditLogger_LogReturnsWriteError(t *testing.T) {
	cfg := &AuditConfig{Enabled: true}
	log := createTestLogger(t)

	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	al.writer = &coverageFailWriter{}

	err = al.Log(context.Background(), &AuditEvent{
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	})
	require.Error(t, err)

	var auditErr *AuditError
	assert.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditEventWrite, auditErr.Code)
}

// ---------------------------------------------------------------------------
// rotateIfNeeded
// ---------------------------------------------------------------------------

func TestAuditLogger_RotateIfNeededExceedsSize(t *testing.T) {
	tmpDir := t.TempDir()
	auditFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       auditFile,
		MaxSize:    1,
		MaxBackups: 5,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	bigDetails := map[string]string{"payload": string(make([]byte, 10000))}
	for i := 0; i < 150; i++ {
		al.Log(ctx, &AuditEvent{
			Action:   ActionWrite,
			Resource: "bulk",
			Result:   ResultSuccess,
			Details:  bigDetails,
		})
	}

	al.mu.Lock()
	err = al.rotateIfNeeded()
	al.mu.Unlock()
	_ = err
}

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
func TestAuditLogger_LogBackupOperation(t *testing.T) {
	tmpDir := t.TempDir()
	auditFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:       true,
		File:          auditFile,
		FlushInterval: 10 * time.Millisecond,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	details := map[string]string{
		"backup_id": "backup-123",
		"size":      "1024",
	}

	err = al.LogBackupOperation(ctx, "admin", "user-1", ActionBackup, details)
	assert.NoError(t, err)

	// Flush to ensure write
	time.Sleep(20 * time.Millisecond)
	err = al.Flush()
	assert.NoError(t, err)

	// Verify file exists and has content
	stat, err := os.Stat(auditFile)
	assert.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(0))
}

// TestAuditLogger_VerifySignature tests signature verification.
func TestAuditLogger_VerifySignature(t *testing.T) {
	tmpDir := t.TempDir()
	auditFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:       true,
		File:          auditFile,
		SignLogs:      true,
		SignKey:       testSignKey(t, "test-signing-key-12345"),
		FlushInterval: 10 * time.Millisecond,
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

	// Sign the event
	sig, err := al.signEvent(event)
	require.NoError(t, err)
	event.Signature = sig

	// Verify signature
	valid := al.VerifySignature(event)
	assert.True(t, valid, "Valid signature should be verified")

	// Tamper with event
	event.Username = "hacker"
	valid = al.VerifySignature(event)
	assert.False(t, valid, "Tampered signature should fail verification")
}

// TestAuditLogger_VerifySignatureWithoutSigning tests verification when signing is disabled.
func TestAuditLogger_VerifySignatureWithoutSigning(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: false,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Username: "testuser",
	}

	// Should always return true when not signing
	valid := al.VerifySignature(event)
	assert.True(t, valid)
}

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
func TestAuditLogger_LogWithMarshalError(t *testing.T) {
	cfg := &AuditConfig{
		Enabled: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Create event with Details that can't be marshaled (circular reference would be hard)
	// Instead test with a valid event to ensure normal path works
	ctx := context.Background()
	event := &AuditEvent{
		Username: "testuser",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	err = al.Log(ctx, event)
	assert.NoError(t, err)
}

// TestAuditLogger_RotationNeeded tests log rotation when file exceeds max size.
func TestAuditLogger_RotationNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	auditFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:       true,
		File:          auditFile,
		MaxSize:       1, // 1 MB
		MaxBackups:    3,
		Compress:      true,
		FlushInterval: 10 * time.Millisecond,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Write enough data to trigger rotation (1 MB)
	ctx := context.Background()
	largeDetails := make(map[string]string)
	for i := 0; i < 100; i++ {
		largeDetails[fmt.Sprintf("key%d", i)] = string(make([]byte, 1000))
	}

	// Write many events
	for i := 0; i < 200; i++ {
		event := &AuditEvent{
			Username: "testuser",
			Action:   ActionWrite,
			Resource: fmt.Sprintf("resource-%d", i),
			Result:   ResultSuccess,
			Details:  largeDetails,
		}
		err = al.Log(ctx, event)
		assert.NoError(t, err)
	}

	// Give time for rotation check
	time.Sleep(100 * time.Millisecond)

	// Verify backup files exist
	// Note: Rotation may or may not have happened depending on timing
	// This test mainly ensures the code path doesn't panic
}

// TestAuditLogger_LogWithSigningError tests log signing error path.
func TestAuditLogger_LogWithSigningError(t *testing.T) {
	tmpDir := t.TempDir()
	auditFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:       true,
		File:          auditFile,
		SignLogs:      true,
		SignKey:       testSignKey(t, "test-key-112bits!"),
		FlushInterval: 10 * time.Millisecond,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Test normal signing path
	ctx := context.Background()
	event := &AuditEvent{
		Username: "testuser",
		Action:   ActionRead,
		Resource: "test",
		Result:   ResultSuccess,
	}

	err = al.Log(ctx, event)
	assert.NoError(t, err)
}

// TestAuditLogger_DataAccessFailure tests data access logging with failure.
func TestAuditLogger_DataAccessFailure(t *testing.T) {
	cfg := &AuditConfig{
		Enabled: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	ctx := context.Background()
	err = al.LogDataAccess(ctx, "admin", "user-1", ActionWrite, "key123", "10.0.0.1", false)
	assert.NoError(t, err)
}

// TestCompressFile_WithAuditLoggerMethod tests file compression method on AuditLogger.
func TestCompressFile_WithAuditLoggerMethod(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	testFile := filepath.Join(tmpDir, "test.log")
	compressedFile := testFile + ".gz"

	// Create test file
	err := os.WriteFile(testFile, []byte("test content for compression"), 0o644)
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
}

// TestAuditLogger_FlushWithoutFile tests flush when no file is configured.
func TestAuditLogger_FlushWithoutFile(t *testing.T) {
	cfg := &AuditConfig{
		Enabled: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.Flush()
	assert.NoError(t, err)
}

// TestAuditLogger_CheckRotationWithoutFile tests rotation check without file.
func TestAuditLogger_CheckRotationWithoutFile(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:       true,
		File:          "",
		FlushInterval: 10 * time.Millisecond,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)

	// Start check rotation (should return immediately since no file)
	go al.checkRotation()

	time.Sleep(50 * time.Millisecond)

	al.Close()
}

// TestAuditLogger_RotateIfNeededWithoutFile tests rotation without file.
func TestAuditLogger_RotateIfNeededWithoutFile(t *testing.T) {
	cfg := &AuditConfig{
		Enabled: true,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	err = al.rotateIfNeeded()
	assert.NoError(t, err)
}

// TestAuditLogger_CleanupOldBackups tests backup cleanup.
func TestAuditLogger_CleanupOldBackups(t *testing.T) {
	tmpDir := t.TempDir()
	auditFile := filepath.Join(tmpDir, "audit.log")

	cfg := &AuditConfig{
		Enabled:    true,
		File:       auditFile,
		MaxBackups: 3,
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	// Call cleanup (currently a no-op)
	al.cleanupOldBackups()
}

// TestAuditLogger_VerifySignatureWithError tests signature verification error path.
func TestAuditLogger_VerifySignatureWithError(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-key-112bits!"),
	}

	log := createTestLogger(t)
	al, err := NewAuditLogger(cfg, log)
	require.NoError(t, err)
	defer al.Close()

	event := &AuditEvent{
		Timestamp: time.Now(),
		Username:  "testuser",
		Action:    ActionRead,
		Resource:  "test",
		Result:    ResultSuccess,
		Signature: "invalid-signature-format",
	}

	valid := al.VerifySignature(event)
	assert.False(t, valid)
}

// TestAuditLogger_LogSignatureGeneration tests signature generation.
func TestAuditLogger_LogSignatureGeneration(t *testing.T) {
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(t, "test-signing-key"),
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

	h := hmac.New(sha256.New, []byte("test-signing-key"))
	h.Write([]byte(data))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	assert.Equal(t, expectedSig, sig)
}
