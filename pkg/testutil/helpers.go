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

package testutil

import (
	"errors"
	"os"
	"testing"
	"time"
)

// CreateTempDir creates a temporary directory for testing and registers
// a cleanup function to remove it when the test completes.
func CreateTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "quicbaas-test-*")
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir) //nolint:gosec // acceptable in test cleanup
	})
	return dir
}

// ErrConditionTimeout is returned when WaitForCondition times out.
var ErrConditionTimeout = errors.New("condition was not met within timeout")

// WaitForCondition polls a condition function at the given interval until it
// returns true or the timeout is reached. Returns nil if the condition is met,
// or ErrConditionTimeout if the timeout expires.
func WaitForCondition(timeout, interval time.Duration, condition func() bool) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if condition() {
			return nil
		}
		select {
		case <-deadline:
			return ErrConditionTimeout
		case <-ticker.C:
			// continue polling
		}
	}
}
