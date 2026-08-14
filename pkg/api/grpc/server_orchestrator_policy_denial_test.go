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
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestConvertOrchestratorError_PolicyDenials asserts every spawn-policy
// refusal answers PermissionDenied. Falling through to the Internal case at
// the bottom of convertOrchestratorError told an SDK caller that the SERVER
// had failed, when in fact the operator's own policy had refused the request.
func TestConvertOrchestratorError_PolicyDenials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		err       error
		wantInMsg string
	}{
		{
			name: "mount not allowed",
			err: &orchestration.MountNotAllowedError{
				Field:    orchestration.MountFieldWorkDir,
				TenantID: "tenant-a",
				Source:   "/etc/shadow",
				Reason:   "outside every allowed mount root",
			},
			wantInMsg: "/etc/shadow",
		},
		{
			name: "extra mounts not allowed",
			err: &orchestration.MountNotAllowedError{
				Field:  orchestration.MountFieldExtraMounts,
				Source: "/var/run/docker.sock",
				Reason: "outside every allowed mount root",
			},
			wantInMsg: orchestration.MountFieldExtraMounts,
		},
		{
			name: "sandbox mode violation",
			err: &orchestration.SandboxModeViolationError{
				Requested: orchestration.SandboxModeNone,
				Policy:    orchestration.SandboxPolicyRequired,
				Reason:    "raw exec forbidden under this policy",
			},
			wantInMsg: "raw exec forbidden",
		},
		{
			name: "trust level violation",
			err: &orchestration.TrustLevelViolationError{
				Requested: orchestration.TrustLevelPinned,
				Policy:    orchestration.TrustPolicyAttested,
				Reason:    "below the operator's integrity floor",
			},
			wantInMsg: "integrity floor",
		},
		{
			name: "sandbox network not allowed",
			err: &orchestration.SandboxNetworkNotAllowedError{
				Requested: "host",
				Effective: orchestration.SandboxNetworkDefault,
			},
			wantInMsg: "host networking is an operator decision",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st, ok := status.FromError(convertOrchestratorError(tc.err))
			if !ok {
				t.Fatalf("expected a gRPC status for %T", tc.err)
			}
			if st.Code() != codes.PermissionDenied {
				t.Fatalf("code: got %v want PermissionDenied", st.Code())
			}
			if !strings.Contains(st.Message(), tc.wantInMsg) {
				t.Errorf("message %q lost %q", st.Message(), tc.wantInMsg)
			}
		})
	}
}

// TestConvertOrchestratorError_UnrelatedStaysInternal keeps the new branch from
// swallowing real faults: an unclassified error is still Internal, so a genuine
// outage is not reported to callers as a policy problem.
func TestConvertOrchestratorError_UnrelatedStaysInternal(t *testing.T) {
	t.Parallel()

	st, ok := status.FromError(convertOrchestratorError(errors.New("disk on fire")))
	if !ok {
		t.Fatal("expected a gRPC status")
	}
	if st.Code() != codes.Internal {
		t.Fatalf("code: got %v want Internal", st.Code())
	}
}
