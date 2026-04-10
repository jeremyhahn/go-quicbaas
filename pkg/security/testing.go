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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// createTestLogger is a helper function to create a test logger.
func createTestLogger(t testing.TB) *logger.Logger {
	config := logger.LogConfig{
		Level:     "error", // Reduce noise in tests
		Format:    "json",
		Output:    "stdout",
		Component: "security-test",
	}

	log, err := logger.New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	return log
}
