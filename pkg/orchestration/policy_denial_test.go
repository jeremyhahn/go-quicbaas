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

package orchestration

import (
	"errors"
	"fmt"
	"testing"
)

// TestIsPolicyDenial_ClassifiesEveryPolicyAxis asserts the classifier every
// transport keys its 403 / PermissionDenied answer off recognizes all four
// spawn-policy axes, wrapped or bare. A miss here silently downgrades a
// transport back to an opaque 500.
func TestIsPolicyDenial_ClassifiesEveryPolicyAxis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{
			name: "mount not allowed",
			err:  &MountNotAllowedError{Field: MountFieldWorkDir, Source: "/etc", Reason: "outside every root"},
		},
		{
			name: "sandbox mode violation",
			err:  &SandboxModeViolationError{Requested: SandboxModeNone, Policy: SandboxPolicyRequired},
		},
		{
			name: "trust level violation",
			err:  &TrustLevelViolationError{Requested: TrustLevelPinned, Policy: TrustPolicyAttested},
		},
		{
			name: "sandbox network not allowed",
			err:  &SandboxNetworkNotAllowedError{Requested: "host", Effective: SandboxNetworkDefault},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !IsPolicyDenial(tc.err) {
				t.Fatalf("bare %T not classified as a policy denial", tc.err)
			}
			wrapped := fmt.Errorf("spawn session: %w", tc.err)
			if !IsPolicyDenial(wrapped) {
				t.Fatalf("wrapped %T not classified as a policy denial", tc.err)
			}
		})
	}
}

// TestIsPolicyDenial_RejectsUnrelatedErrors keeps the classifier from
// swallowing genuine server faults into a 403, which would hide real outages
// from operators.
func TestIsPolicyDenial_RejectsUnrelatedErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{name: "nil", err: nil},
		{name: "plain error", err: errors.New("disk on fire")},
		{name: "service unavailable", err: &AgentServiceError{Sentinel: ErrAgentServiceUnavailable, Message: "no store"}},
		{name: "invalid input", err: &InputValidationError{Field: "backend", Message: "required"}},
		{name: "session not found", err: &SessionNotFoundError{SessionID: "sess-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if IsPolicyDenial(tc.err) {
				t.Fatalf("%v must not be classified as a policy denial", tc.err)
			}
		})
	}
}
