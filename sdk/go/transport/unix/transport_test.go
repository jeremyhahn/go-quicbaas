// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestNew_NilConfig(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNew_EmptyAddress(t *testing.T) {
	_, err := New(&transport.Config{})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNew_InvalidSocketPath_NotAbsolute(t *testing.T) {
	cfg := &transport.Config{
		Address: "relative/path.sock",
		Timeout: 100 * time.Millisecond,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for non-absolute socket path")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNew_InvalidSocketPath_TooLong(t *testing.T) {
	// Create a path longer than 107 bytes
	longPath := "/" + strings.Repeat("a", 110)
	cfg := &transport.Config{
		Address: longPath,
		Timeout: 100 * time.Millisecond,
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for socket path exceeding max length")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNew_ConnectionFails_NonexistentSocket(t *testing.T) {
	cfg := &transport.Config{
		Address: "/tmp/nonexistent-quicbaas-test.sock",
		Timeout: 100 * time.Millisecond,
	}
	_, err := New(cfg)
	// Connection should fail since socket doesn't exist
	if err == nil {
		t.Fatal("expected connection error for nonexistent socket")
	}
	// The error should be wrapped from the gRPC transport
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestNew_UnixPrefixedPath(t *testing.T) {
	cfg := &transport.Config{
		Address: "unix:///tmp/nonexistent-quicbaas-test.sock",
		Timeout: 100 * time.Millisecond,
	}
	_, err := New(cfg)
	// Will fail connection but should pass validation
	if err == nil {
		t.Fatal("expected connection error for nonexistent socket")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestNew_AbstractSocket(t *testing.T) {
	cfg := &transport.Config{
		Address: "@quicbaas-abstract-test",
		Timeout: 100 * time.Millisecond,
	}
	_, err := New(cfg)
	// Will fail connection but should pass validation for abstract socket
	if err == nil {
		t.Fatal("expected connection error")
	}
	// Should be a connection error, not validation error
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestNormalizeSocketPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "raw path",
			input:    "/var/run/quicbaas.sock",
			expected: "/var/run/quicbaas.sock",
		},
		{
			name:     "unix:// prefix",
			input:    "unix:///var/run/quicbaas.sock",
			expected: "/var/run/quicbaas.sock",
		},
		{
			name:     "unix: prefix (gRPC format)",
			input:    "unix:/var/run/quicbaas.sock",
			expected: "/var/run/quicbaas.sock",
		},
		{
			name:     "abstract socket",
			input:    "@quicbaas",
			expected: "@quicbaas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeSocketPath(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeSocketPath(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateSocketPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expectError bool
		errorCheck  func(error) bool
	}{
		{
			name:        "valid absolute path",
			path:        "/var/run/quicbaas.sock",
			expectError: false,
		},
		{
			name:        "valid abstract socket",
			path:        "@quicbaas",
			expectError: false,
		},
		{
			name:        "empty path",
			path:        "",
			expectError: true,
			errorCheck:  errors.IsInvalidInput,
		},
		{
			name:        "relative path",
			path:        "relative/path.sock",
			expectError: true,
			errorCheck:  errors.IsInvalidInput,
		},
		{
			name:        "path exceeds max length",
			path:        "/" + strings.Repeat("a", 110),
			expectError: true,
			errorCheck:  errors.IsInvalidInput,
		},
		{
			name:        "path at max length boundary",
			path:        "/" + strings.Repeat("a", 106), // 107 total
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSocketPath(tt.path)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errorCheck != nil && !tt.errorCheck(err) {
					t.Errorf("error check failed for: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestTransport_Request_Closed(t *testing.T) {
	tr := &Transport{}
	tr.closed.Store(true)

	_, err := tr.Request(context.Background(), "service", "method", nil)
	if err == nil {
		t.Fatal("expected error for closed transport")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestTransport_Request_NilGRPC(t *testing.T) {
	tr := &Transport{}
	// Not closed, but grpc is nil

	_, err := tr.Request(context.Background(), "service", "method", nil)
	if err == nil {
		t.Fatal("expected error for nil grpc transport")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestTransport_RequestStream_Closed(t *testing.T) {
	tr := &Transport{}
	tr.closed.Store(true)

	_, err := tr.RequestStream(context.Background(), "service", "method", nil)
	if err == nil {
		t.Fatal("expected error for closed transport")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestTransport_RequestStream_NilGRPC(t *testing.T) {
	tr := &Transport{}
	// Not closed, but grpc is nil

	_, err := tr.RequestStream(context.Background(), "service", "method", nil)
	if err == nil {
		t.Fatal("expected error for nil grpc transport")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestTransport_Conn_NilGRPC(t *testing.T) {
	tr := &Transport{}
	// Test that Conn returns nil when grpc is nil
	result := tr.Conn()
	if result != nil {
		t.Error("expected nil connection for nil grpc transport")
	}
}

func TestTransport_Close_AlreadyClosed(t *testing.T) {
	tr := &Transport{}
	tr.closed.Store(true)

	err := tr.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTransport_Close_NilGRPC(t *testing.T) {
	tr := &Transport{}
	// Close with nil grpc should not panic

	err := tr.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !tr.closed.Load() {
		t.Error("expected transport to be marked closed")
	}
}

func TestTransport_Healthy_Closed(t *testing.T) {
	tr := &Transport{}
	tr.closed.Store(true)

	if tr.Healthy() {
		t.Error("expected unhealthy for closed transport")
	}
}

func TestTransport_Healthy_NilGRPC(t *testing.T) {
	tr := &Transport{}
	// Not closed, but grpc is nil

	if tr.Healthy() {
		t.Error("expected unhealthy for nil grpc transport")
	}
}

func TestTransport_SocketPath(t *testing.T) {
	tr := &Transport{
		socketPath: "/var/run/test.sock",
	}

	if tr.SocketPath() != "/var/run/test.sock" {
		t.Errorf("expected /var/run/test.sock, got %s", tr.SocketPath())
	}
}

func TestTransport_SocketPath_Empty(t *testing.T) {
	tr := &Transport{}

	if tr.SocketPath() != "" {
		t.Errorf("expected empty socket path, got %s", tr.SocketPath())
	}
}

func TestFactory_Create_NilConfig(t *testing.T) {
	f := &Factory{}

	_, err := f.Create(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestFactory_Create_EmptyAddress(t *testing.T) {
	f := &Factory{}

	_, err := f.Create(&transport.Config{})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestFactory_Create_InvalidPath(t *testing.T) {
	f := &Factory{}

	_, err := f.Create(&transport.Config{
		Address: "relative/path.sock",
	})
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestFactory_Create_ConnectionFailure(t *testing.T) {
	f := &Factory{}

	_, err := f.Create(&transport.Config{
		Address: "/tmp/nonexistent-factory-test.sock",
		Timeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
	// Should be a connection error
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

// TestTransportInterface verifies that Transport implements transport.Transport.
func TestTransportInterface(t *testing.T) {
	var _ transport.Transport = (*Transport)(nil)
}

// TestFactoryInterface verifies that Factory implements transport.Factory.
func TestFactoryInterface(t *testing.T) {
	var _ transport.Factory = (*Factory)(nil)
}

func TestNew_GRPCFormatPrefix(t *testing.T) {
	// Test unix: prefix (gRPC style without double slash)
	cfg := &transport.Config{
		Address: "unix:/tmp/nonexistent-grpc-format.sock",
		Timeout: 100 * time.Millisecond,
	}
	_, err := New(cfg)
	// Will fail connection but should pass validation
	if err == nil {
		t.Fatal("expected connection error")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestValidateSocketPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expectError bool
	}{
		{
			name:        "single slash",
			path:        "/",
			expectError: false,
		},
		{
			name:        "single at sign",
			path:        "@",
			expectError: false,
		},
		{
			name:        "abstract with path",
			path:        "@/quicbaas/socket",
			expectError: false,
		},
		{
			name:        "path with spaces",
			path:        "/var/run/dragon db.sock",
			expectError: false,
		},
		{
			name:        "deep nested path",
			path:        "/var/run/quicbaas/cluster/node1/socket.sock",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSocketPath(tt.path)
			if tt.expectError && err == nil {
				t.Fatal("expected error")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNew_ConfigPreservation(t *testing.T) {
	// Verify that the original config is not modified
	originalAddress := "unix:///tmp/test.sock"
	cfg := &transport.Config{
		Address: originalAddress,
		Timeout: 100 * time.Millisecond,
	}

	_, _ = New(cfg)

	// Original config address should be unchanged
	if cfg.Address != originalAddress {
		t.Errorf("original config was modified: expected %q, got %q", originalAddress, cfg.Address)
	}
}
