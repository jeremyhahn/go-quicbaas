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

package mcp

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestMapErrorToJSONRPCError_PolicyDenials asserts every spawn-policy refusal
// answers OperationNotAllowed — the forbidden-but-authenticated code on this
// transport, which is where dberrors.ErrForbidden already lands — carrying the
// policy's own message. These previously collapsed into InternalError /
// "Tool execution failed", which told an agent nothing it could act on.
func TestMapErrorToJSONRPCError_PolicyDenials(t *testing.T) {
	server, _ := newTestServer(t)

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
			code, msg := server.mapErrorToJSONRPCError(tc.err)
			if code != OperationNotAllowed {
				t.Fatalf("code: got %d want OperationNotAllowed (%d)", code, OperationNotAllowed)
			}
			if !strings.Contains(msg, tc.wantInMsg) {
				t.Errorf("message %q lost %q", msg, tc.wantInMsg)
			}

			wrappedCode, _ := server.mapErrorToJSONRPCError(fmt.Errorf("spawn session: %w", tc.err))
			if wrappedCode != OperationNotAllowed {
				t.Fatalf("wrapped code: got %d want OperationNotAllowed (%d)",
					wrappedCode, OperationNotAllowed)
			}
		})
	}
}

// TestMapErrorToJSONRPCError_PolicyDenialIsNotAnAuthChallenge pins the code
// choice. PermissionDenied on this transport answers with an OAuth
// insufficient_scope challenge, which would send a client off to acquire a
// broader token for a denial that no token can lift.
func TestMapErrorToJSONRPCError_PolicyDenialIsNotAnAuthChallenge(t *testing.T) {
	server, _ := newTestServer(t)

	code, _ := server.mapErrorToJSONRPCError(&orchestration.MountNotAllowedError{
		Field:  orchestration.MountFieldWorkDir,
		Source: "/etc",
		Reason: "outside every allowed mount root",
	})
	if code == PermissionDenied {
		t.Fatal("a policy denial must not be reported as insufficient OAuth scope")
	}
	if code == InternalError {
		t.Fatal("a policy denial must not be reported as a server fault")
	}
}

// TestMapErrorToJSONRPCError_UnrelatedStaysInternal keeps the new branch from
// reclassifying real faults.
func TestMapErrorToJSONRPCError_UnrelatedStaysInternal(t *testing.T) {
	server, _ := newTestServer(t)

	code, _ := server.mapErrorToJSONRPCError(errors.New("disk on fire"))
	if code != InternalError {
		t.Fatalf("code: got %d want InternalError (%d)", code, InternalError)
	}
}
