//go:build mcp

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

// Package mcp provides test helpers for vector MCP tests.
package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// newTestLogger creates a test logger instance for testing.
func newTestLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return log
}
