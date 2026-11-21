//go:build raft

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

package server

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestTransportType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		tt       TransportType
		expected string
	}{
		{"embedded", TransportEmbedded, "embedded"},
		{"grpc", TransportGRPC, "grpc"},
		{"rest", TransportREST, "rest"},
		{"quic", TransportQUIC, "quic"},
		{"mcp", TransportMCP, "mcp"},
		{"unix", TransportUnix, "unix"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.tt) != tc.expected {
				t.Errorf("TransportType %q = %q, want %q", tc.name, string(tc.tt), tc.expected)
			}
		})
	}
}

func TestTransportFactory_ValidateNetworkConfig_GRPCMissingAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportGRPC,
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportGRPC)
	if err == nil {
		t.Fatal("expected error for gRPC with missing address, got nil")
	}
	if !errors.Is(err, ErrNetworkTransportMissingAddress) {
		t.Errorf("expected ErrNetworkTransportMissingAddress, got %v", err)
	}
	var typedErr *NetworkTransportMissingAddressError
	if !errors.As(err, &typedErr) {
		t.Errorf("expected NetworkTransportMissingAddressError, got %T", err)
	}
	if typedErr.Transport != "grpc" {
		t.Errorf("NetworkTransportMissingAddressError.Transport = %q, want %q", typedErr.Transport, "grpc")
	}
}

func TestTransportFactory_ValidateNetworkConfig_RESTMissingAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportREST,
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportREST)
	if err == nil {
		t.Fatal("expected error for REST with missing address, got nil")
	}
	if !errors.Is(err, ErrNetworkTransportMissingAddress) {
		t.Errorf("expected ErrNetworkTransportMissingAddress, got %v", err)
	}
}

func TestTransportFactory_ValidateNetworkConfig_QUICMissingAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportQUIC,
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportQUIC)
	if err == nil {
		t.Fatal("expected error for QUIC with missing address, got nil")
	}
	if !errors.Is(err, ErrNetworkTransportMissingAddress) {
		t.Errorf("expected ErrNetworkTransportMissingAddress, got %v", err)
	}
}

func TestTransportFactory_ValidateNetworkConfig_MCPMissingAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportMCP,
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportMCP)
	if err == nil {
		t.Fatal("expected error for MCP with missing address, got nil")
	}
	if !errors.Is(err, ErrNetworkTransportMissingAddress) {
		t.Errorf("expected ErrNetworkTransportMissingAddress, got %v", err)
	}
}

func TestTransportFactory_ValidateNetworkConfig_UnixMissingSocket(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportUnix,
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportUnix)
	if err == nil {
		t.Fatal("expected error for Unix with missing socket, got nil")
	}
	if !errors.Is(err, ErrUnixTransportMissingSocket) {
		t.Errorf("expected ErrUnixTransportMissingSocket, got %v", err)
	}
	var typedErr *UnixTransportMissingSocketError
	if !errors.As(err, &typedErr) {
		t.Errorf("expected UnixTransportMissingSocketError, got %T", err)
	}
}

func TestTransportFactory_ValidateNetworkConfig_GRPCWithAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportGRPC,
		Address:   "localhost:50051",
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportGRPC)
	if err != nil {
		t.Fatalf("unexpected error for gRPC with address: %v", err)
	}
}

func TestTransportFactory_ValidateNetworkConfig_UnixWithSocket(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport:      config.QRDBTransportUnix,
		UnixSocketPath: "/var/run/qrdb.sock",
	}
	factory := newTransportFactory(cfg, nil, nil)
	err := factory.validateNetworkConfig(TransportUnix)
	if err != nil {
		t.Fatalf("unexpected error for Unix with socket: %v", err)
	}
}

func TestTransportFactory_CreateNetworkClient_UnsupportedTransport(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: "unsupported",
		Address:   "localhost:1234",
	}
	factory := newTransportFactory(cfg, nil, nil)
	_, err := factory.createNetworkClient()
	if err == nil {
		t.Fatal("expected error for unsupported transport, got nil")
	}
	if !errors.Is(err, ErrUnsupportedTransport) {
		t.Errorf("expected ErrUnsupportedTransport, got %v", err)
	}
	var typedErr *UnsupportedTransportError
	if !errors.As(err, &typedErr) {
		t.Errorf("expected UnsupportedTransportError, got %T", err)
	}
	if typedErr.Transport != "unsupported" {
		t.Errorf("UnsupportedTransportError.Transport = %q, want %q", typedErr.Transport, "unsupported")
	}
}

func TestBuildTransportOptions_EmptyConfig(t *testing.T) {
	cfg := &config.QRDBConfig{}
	opts := buildTransportOptions(cfg)
	if len(opts) != 0 {
		t.Errorf("expected 0 options for empty config, got %d", len(opts))
	}
}

func TestBuildTransportOptions_WithAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		Address: "localhost:9443",
	}
	opts := buildTransportOptions(cfg)
	if len(opts) != 1 {
		t.Errorf("expected 1 option for config with address, got %d", len(opts))
	}
}

func TestBuildTransportOptions_WithAddressAndTLS(t *testing.T) {
	cfg := &config.QRDBConfig{
		Address:     "localhost:9443",
		TLSEnabled:  true,
		TLSCertFile: "/cert.pem",
		TLSKeyFile:  "/key.pem",
		TLSCAFile:   "/ca.pem",
	}
	opts := buildTransportOptions(cfg)
	// Expect 2 options: WithAddress + WithTLS
	if len(opts) != 2 {
		t.Errorf("expected 2 options for config with address and TLS, got %d", len(opts))
	}
}

func TestBuildTransportOptions_TLSWithoutAddress(t *testing.T) {
	cfg := &config.QRDBConfig{
		TLSEnabled: true,
		TLSCAFile:  "/ca.pem",
	}
	opts := buildTransportOptions(cfg)
	// Only TLS option (no address option)
	if len(opts) != 1 {
		t.Errorf("expected 1 option for TLS without address, got %d", len(opts))
	}
}

func TestNetworkTransportHandlers_CoverAllTypes(t *testing.T) {
	expectedTypes := []TransportType{
		TransportGRPC,
		TransportREST,
		TransportQUIC,
		TransportMCP,
		TransportUnix,
	}

	for _, tt := range expectedTypes {
		t.Run(string(tt), func(t *testing.T) {
			_, ok := networkTransportHandlers[tt]
			if !ok {
				t.Errorf("networkTransportHandlers missing handler for %q", tt)
			}
		})
	}
}

func TestNetworkTransportHandlers_EmbeddedNotIncluded(t *testing.T) {
	_, ok := networkTransportHandlers[TransportEmbedded]
	if ok {
		t.Error("networkTransportHandlers should not contain embedded transport")
	}
}

// TestTransportFactory_CreateNetworkClient_GRPCSucceeds drives the full
// construction path of createNetworkClient for a supported (grpc) network
// transport: option building, transport constructor invocation, and SDK
// client creation. The grpc transport connects lazily, so client creation
// succeeds without a live server; this covers the success branch (and the
// buildTransportOptions/constructor/NewClient calls) that the unsupported-
// transport test cannot reach.
func TestTransportFactory_CreateNetworkClient_GRPCSucceeds(t *testing.T) {
	cfg := &config.QRDBConfig{
		Transport: config.QRDBTransportGRPC,
		Address:   "127.0.0.1:59999",
	}
	factory := newTransportFactory(cfg, nil, nil)
	client, err := factory.createNetworkClient()
	if err != nil {
		t.Fatalf("createNetworkClient(grpc) returned error: %v", err)
	}
	if client == nil {
		t.Fatal("createNetworkClient(grpc) returned nil client")
	}
	_ = client.Close()
}
