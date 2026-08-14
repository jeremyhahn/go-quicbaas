//go:build quic

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

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestErrorToStatus_PolicyDenials asserts every spawn-policy refusal answers
// StatusPermissionDenied on the QUIC wire. These previously fell through to
// StatusInternalError, which an SDK caller cannot tell apart from a crash.
func TestErrorToStatus_PolicyDenials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{
			name: "mount not allowed",
			err: &orchestration.MountNotAllowedError{
				Field:    orchestration.MountFieldWorkDir,
				TenantID: "tenant-a",
				Source:   "/etc/shadow",
				Reason:   "outside every allowed mount root",
			},
		},
		{
			name: "sandbox mode violation",
			err: &orchestration.SandboxModeViolationError{
				Requested: orchestration.SandboxModeNone,
				Policy:    orchestration.SandboxPolicyRequired,
				Reason:    "raw exec forbidden under this policy",
			},
		},
		{
			name: "trust level violation",
			err: &orchestration.TrustLevelViolationError{
				Requested: orchestration.TrustLevelPinned,
				Policy:    orchestration.TrustPolicyAttested,
				Reason:    "below the operator's integrity floor",
			},
		},
		{
			name: "sandbox network not allowed",
			err: &orchestration.SandboxNetworkNotAllowedError{
				Requested: "host",
				Effective: orchestration.SandboxNetworkDefault,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ErrorToStatus(tc.err); got != StatusPermissionDenied {
				t.Fatalf("bare %T: got status %d want StatusPermissionDenied (%d)",
					tc.err, got, StatusPermissionDenied)
			}
			wrapped := fmt.Errorf("spawn session: %w", tc.err)
			if got := ErrorToStatus(wrapped); got != StatusPermissionDenied {
				t.Fatalf("wrapped %T: got status %d want StatusPermissionDenied (%d)",
					tc.err, got, StatusPermissionDenied)
			}
		})
	}
}

// TestErrorToStatus_PolicyDenialResponseKeepsMessage asserts the handler's
// error response carries the policy's actionable text, not just the code.
func TestErrorToStatus_PolicyDenialResponseKeepsMessage(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	resp := h.errorResponse(&orchestration.MountNotAllowedError{
		Field:    orchestration.MountFieldWorkDir,
		TenantID: "tenant-a",
		Source:   "/etc/shadow",
		Reason:   "outside every allowed mount root",
	})
	if resp.Status != StatusPermissionDenied {
		t.Fatalf("status: got %d want %d", resp.Status, StatusPermissionDenied)
	}
	if payload := string(resp.Payload); payload == "" {
		t.Fatal("payload must carry the denial message")
	}
}

// TestErrorToStatus_UnrelatedStaysInternal keeps the new branch from
// reclassifying real faults as policy denials.
func TestErrorToStatus_UnrelatedStaysInternal(t *testing.T) {
	t.Parallel()

	if got := ErrorToStatus(errors.New("disk on fire")); got != StatusInternalError {
		t.Fatalf("got status %d want StatusInternalError (%d)", got, StatusInternalError)
	}
}
