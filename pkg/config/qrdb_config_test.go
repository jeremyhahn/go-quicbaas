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

package config

import (
	"testing"
)

func TestDefaultQRDBConfig(t *testing.T) {
	cfg := DefaultQRDBConfig()
	if cfg.Transport != QRDBTransportEmbedded {
		t.Errorf("DefaultQRDBConfig().Transport = %q, want %q", cfg.Transport, QRDBTransportEmbedded)
	}
	if cfg.Address != "" {
		t.Errorf("DefaultQRDBConfig().Address = %q, want empty", cfg.Address)
	}
	if cfg.UnixSocketPath != "" {
		t.Errorf("DefaultQRDBConfig().UnixSocketPath = %q, want empty", cfg.UnixSocketPath)
	}
	if cfg.TLSEnabled {
		t.Error("DefaultQRDBConfig().TLSEnabled = true, want false")
	}
	if cfg.TLSCertFile != "" {
		t.Errorf("DefaultQRDBConfig().TLSCertFile = %q, want empty", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "" {
		t.Errorf("DefaultQRDBConfig().TLSKeyFile = %q, want empty", cfg.TLSKeyFile)
	}
	if cfg.TLSCAFile != "" {
		t.Errorf("DefaultQRDBConfig().TLSCAFile = %q, want empty", cfg.TLSCAFile)
	}
}

func TestQRDBConfig_IsEmbedded(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		want      bool
	}{
		{
			name:      "empty string defaults to embedded",
			transport: "",
			want:      true,
		},
		{
			name:      "explicit embedded",
			transport: QRDBTransportEmbedded,
			want:      true,
		},
		{
			name:      "grpc is not embedded",
			transport: QRDBTransportGRPC,
			want:      false,
		},
		{
			name:      "rest is not embedded",
			transport: QRDBTransportREST,
			want:      false,
		},
		{
			name:      "quic is not embedded",
			transport: QRDBTransportQUIC,
			want:      false,
		},
		{
			name:      "mcp is not embedded",
			transport: QRDBTransportMCP,
			want:      false,
		},
		{
			name:      "unix is not embedded",
			transport: QRDBTransportUnix,
			want:      false,
		},
		{
			name:      "unknown transport is not embedded",
			transport: "unknown",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &QRDBConfig{Transport: tt.transport}
			if got := cfg.IsEmbedded(); got != tt.want {
				t.Errorf("QRDBConfig{Transport: %q}.IsEmbedded() = %v, want %v", tt.transport, got, tt.want)
			}
		})
	}
}

func TestQRDBTransportConstants(t *testing.T) {
	// Verify transport constant values match expected strings.
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"embedded", QRDBTransportEmbedded, "embedded"},
		{"grpc", QRDBTransportGRPC, "grpc"},
		{"rest", QRDBTransportREST, "rest"},
		{"quic", QRDBTransportQUIC, "quic"},
		{"mcp", QRDBTransportMCP, "mcp"},
		{"unix", QRDBTransportUnix, "unix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("QRDBTransport constant %q = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}

func TestDefaultConfig_IncludesQRDB(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.QRDB.Transport != QRDBTransportEmbedded {
		t.Errorf("DefaultConfig().QRDB.Transport = %q, want %q", cfg.QRDB.Transport, QRDBTransportEmbedded)
	}
}

func TestTestConfig_IncludesQRDB(t *testing.T) {
	cfg := TestConfig()
	if cfg.QRDB.Transport != QRDBTransportEmbedded {
		t.Errorf("TestConfig().QRDB.Transport = %q, want %q", cfg.QRDB.Transport, QRDBTransportEmbedded)
	}
}

func TestProductionConfig_IncludesQRDB(t *testing.T) {
	cfg := ProductionConfig()
	// Production inherits from DefaultConfig, so QRDB should be embedded.
	if cfg.QRDB.Transport != QRDBTransportEmbedded {
		t.Errorf("ProductionConfig().QRDB.Transport = %q, want %q", cfg.QRDB.Transport, QRDBTransportEmbedded)
	}
}

func TestDevelopmentConfig_IncludesQRDB(t *testing.T) {
	cfg := DevelopmentConfig()
	if cfg.QRDB.Transport != QRDBTransportEmbedded {
		t.Errorf("DevelopmentConfig().QRDB.Transport = %q, want %q", cfg.QRDB.Transport, QRDBTransportEmbedded)
	}
}

func TestEmbeddedConfig_IncludesQRDB(t *testing.T) {
	cfg := EmbeddedConfig()
	if cfg.QRDB.Transport != QRDBTransportEmbedded {
		t.Errorf("EmbeddedConfig().QRDB.Transport = %q, want %q", cfg.QRDB.Transport, QRDBTransportEmbedded)
	}
}

func TestClone_IncludesQRDB(t *testing.T) {
	cfg := &Config{
		QRDB: QRDBConfig{
			Transport:      QRDBTransportGRPC,
			Address:        "localhost:50051",
			TLSEnabled:     true,
			TLSCertFile:    "/path/to/cert",
			TLSKeyFile:     "/path/to/key",
			TLSCAFile:      "/path/to/ca",
			UnixSocketPath: "/var/run/qrdb.sock",
		},
	}

	clone := cfg.Clone()

	if clone.QRDB.Transport != cfg.QRDB.Transport {
		t.Errorf("Clone().QRDB.Transport = %q, want %q", clone.QRDB.Transport, cfg.QRDB.Transport)
	}
	if clone.QRDB.Address != cfg.QRDB.Address {
		t.Errorf("Clone().QRDB.Address = %q, want %q", clone.QRDB.Address, cfg.QRDB.Address)
	}
	if clone.QRDB.TLSEnabled != cfg.QRDB.TLSEnabled {
		t.Errorf("Clone().QRDB.TLSEnabled = %v, want %v", clone.QRDB.TLSEnabled, cfg.QRDB.TLSEnabled)
	}
	if clone.QRDB.TLSCertFile != cfg.QRDB.TLSCertFile {
		t.Errorf("Clone().QRDB.TLSCertFile = %q, want %q", clone.QRDB.TLSCertFile, cfg.QRDB.TLSCertFile)
	}
	if clone.QRDB.TLSKeyFile != cfg.QRDB.TLSKeyFile {
		t.Errorf("Clone().QRDB.TLSKeyFile = %q, want %q", clone.QRDB.TLSKeyFile, cfg.QRDB.TLSKeyFile)
	}
	if clone.QRDB.TLSCAFile != cfg.QRDB.TLSCAFile {
		t.Errorf("Clone().QRDB.TLSCAFile = %q, want %q", clone.QRDB.TLSCAFile, cfg.QRDB.TLSCAFile)
	}
	if clone.QRDB.UnixSocketPath != cfg.QRDB.UnixSocketPath {
		t.Errorf("Clone().QRDB.UnixSocketPath = %q, want %q", clone.QRDB.UnixSocketPath, cfg.QRDB.UnixSocketPath)
	}

	// Verify independence: mutating clone does not affect original.
	clone.QRDB.Transport = QRDBTransportREST
	if cfg.QRDB.Transport != QRDBTransportGRPC {
		t.Error("Clone mutation affected original config")
	}
}

func TestQRDBConfig_NetworkTransportWithTLS(t *testing.T) {
	cfg := QRDBConfig{
		Transport:   QRDBTransportGRPC,
		Address:     "localhost:50051",
		TLSEnabled:  true,
		TLSCertFile: "/path/to/cert.pem",
		TLSKeyFile:  "/path/to/key.pem",
		TLSCAFile:   "/path/to/ca.pem",
	}

	if cfg.IsEmbedded() {
		t.Error("gRPC transport should not be embedded")
	}
	if !cfg.TLSEnabled {
		t.Error("TLS should be enabled")
	}
}

func TestQRDBConfig_UnixTransport(t *testing.T) {
	cfg := QRDBConfig{
		Transport:      QRDBTransportUnix,
		UnixSocketPath: "/var/run/qrdb.sock",
	}

	if cfg.IsEmbedded() {
		t.Error("unix transport should not be embedded")
	}
	if cfg.UnixSocketPath != "/var/run/qrdb.sock" {
		t.Errorf("UnixSocketPath = %q, want %q", cfg.UnixSocketPath, "/var/run/qrdb.sock")
	}
}
