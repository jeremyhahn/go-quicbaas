// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build integration

package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// grpcUnmappedMethodRefusal is the text of the gRPC method authorizer's
// refusal of a method with no permission mapping (UnmappedMethodError in
// pkg/api/grpc/authz_interceptor.go); the method name follows it.
const grpcUnmappedMethodRefusal = "no qrdb permission mapping for gRPC method"

// expectGRPCUnmappedRefusal runs cmd and asserts the gRPC method authorizer
// refuses it when the runner's transport goes through that authorizer (gRPC
// and the Unix socket). The delegated-admin assignment RPCs carry no gRPC
// permission mapping on purpose: their handlers look objects up by ID with no
// tenant check, so gRPC refuses them. It returns true when it asserted the
// refusal (the caller ends the test there) and false, without running cmd, on
// every other transport, which keeps its success path.
func expectGRPCUnmappedRefusal(t *testing.T, runner *TestRunner, cmd CommandDef) bool {
	t.Helper()
	if runner.transport != TransportGRPC && runner.transport != TransportUnix {
		return false
	}
	stdout, stderr, err := runner.Run(cmd)
	combined := stdout + stderr
	require.Error(t, err, "[%s] %s: an unmapped gRPC method must be refused: stdout=%s stderr=%s",
		runner.transport, cmd.Name, stdout, stderr)
	require.True(t, strings.Contains(combined, "PermissionDenied") || strings.Contains(combined, "FORBIDDEN"),
		"[%s] %s should be refused with PermissionDenied or FORBIDDEN: %s", runner.transport, cmd.Name, combined)
	require.Contains(t, combined, grpcUnmappedMethodRefusal,
		"[%s] %s should be refused for having no permission mapping: %s", runner.transport, cmd.Name, combined)
	return true
}
