//go:build quic && !iam

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

package quic

import "testing"

// --- Content moved from coverage_iam_stub_test.go ---

// =============================================================================
// IAM Service Stub Tests - Target 0% coverage functions
// These functions are no-ops when IAM is not enabled.
// =============================================================================

func TestSetIAMService_Stub(t *testing.T) {
	t.Parallel()
	h := createTestHandler()

	// setIAMService is a no-op when IAM is disabled
	h.setIAMService("dummy-service")

	// No panic or error should occur
}

func TestGetIAMService_Stub(t *testing.T) {
	t.Parallel()
	h := createTestHandler()

	// getIAMService returns nil when IAM is disabled
	result := h.getIAMService()
	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}
}

func TestClearIAMService_Stub(t *testing.T) {
	t.Parallel()
	h := createTestHandler()

	// clearIAMService is a no-op when IAM is disabled
	h.clearIAMService()

	// No panic or error should occur
}

func TestSetMFAService_Stub(t *testing.T) {
	t.Parallel()
	h := createTestHandler()

	// setMFAService is a no-op when IAM is disabled
	h.setMFAService("dummy-mfa-service")

	// No panic or error should occur
}

func TestGetMFAService_Stub(t *testing.T) {
	t.Parallel()
	h := createTestHandler()

	// getMFAService returns nil when IAM is disabled
	result := h.getMFAService()
	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}
}

func TestClearMFAService_Stub(t *testing.T) {
	t.Parallel()
	h := createTestHandler()

	// clearMFAService is a no-op when IAM is disabled
	h.clearMFAService()

	// No panic or error should occur
}
