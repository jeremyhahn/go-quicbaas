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

package grpc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUnixSocketAddress(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		expected bool
	}{
		{
			name:     "unix prefix",
			address:  "unix:///var/run/quicbaas.sock",
			expected: true,
		},
		{
			name:     "unix abstract prefix",
			address:  "unix-abstract://quicbaas",
			expected: true,
		},
		{
			name:     "bare unix socket path",
			address:  "/tmp/quicbaas.sock",
			expected: true,
		},
		{
			name:     "var run path",
			address:  "/var/run/quicbaas/quicbaas.sock",
			expected: true,
		},
		{
			name:     "tcp localhost",
			address:  "localhost:50051",
			expected: false,
		},
		{
			name:     "tcp ip",
			address:  "192.0.2.1:9090",
			expected: false,
		},
		{
			name:     "tcp with host",
			address:  "server.example.com:50051",
			expected: false,
		},
		{
			name:     "path without .sock extension",
			address:  "/tmp/quicbaas",
			expected: false,
		},
		{
			name:     "empty string",
			address:  "",
			expected: false,
		},
		{
			name:     "relative path with dot-slash",
			address:  "./quicbaas.sock",
			expected: true,
		},
		{
			name:     "relative path with parent directory",
			address:  "../path/quicbaas.sock",
			expected: true,
		},
		{
			name:     "bare filename with .sock",
			address:  "quicbaas.sock",
			expected: true,
		},
		{
			name:     "relative path without .sock extension",
			address:  "./quicbaas",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnixSocketAddress(tt.address)
			assert.Equal(t, tt.expected, result, "IsUnixSocketAddress(%q)", tt.address)
		})
	}
}

func TestNormalizeUnixSocketAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bare path to unix prefix",
			input:    "/tmp/quicbaas.sock",
			expected: "unix:///tmp/quicbaas.sock",
		},
		{
			name:     "already has unix prefix",
			input:    "unix:///var/run/quicbaas.sock",
			expected: "unix:///var/run/quicbaas.sock",
		},
		{
			name:     "already has unix-abstract prefix",
			input:    "unix-abstract://quicbaas",
			expected: "unix-abstract://quicbaas",
		},
		{
			name:     "tcp localhost unchanged",
			input:    "localhost:50051",
			expected: "localhost:50051",
		},
		{
			name:     "tcp ip unchanged",
			input:    "192.0.2.1:9090",
			expected: "192.0.2.1:9090",
		},
		{
			name:     "tcp hostname unchanged",
			input:    "server.example.com:50051",
			expected: "server.example.com:50051",
		},
		{
			name:     "deep path",
			input:    "/var/run/quicbaas/node1/quicbaas.sock",
			expected: "unix:///var/run/quicbaas/node1/quicbaas.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeUnixSocketAddress(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeUnixSocketAddress(%q)", tt.input)
		})
	}
}

func TestNormalizeUnixSocketAddress_RelativePaths(t *testing.T) {
	// Get current working directory for expected results
	cwd, err := os.Getwd()
	assert.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "relative path with dot-slash",
			input:    "./quicbaas.sock",
			expected: "unix://" + filepath.Join(cwd, "quicbaas.sock"),
		},
		{
			name:     "bare filename with .sock",
			input:    "quicbaas.sock",
			expected: "unix://" + filepath.Join(cwd, "quicbaas.sock"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeUnixSocketAddress(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeUnixSocketAddress(%q)", tt.input)
		})
	}
}

func TestUnixSocketConstants(t *testing.T) {
	// Verify constants are set correctly
	assert.Equal(t, "unix://", UnixPrefix)
	assert.Equal(t, "unix-abstract://", UnixAbstractPrefix)
}
