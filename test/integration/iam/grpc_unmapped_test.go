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

package iam_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// grpcUnmappedMethodRefusal is the text of the gRPC method authorizer's
// refusal of a method with no permission mapping (UnmappedMethodError in
// pkg/api/grpc/authz_interceptor.go); the method name follows it.
const grpcUnmappedMethodRefusal = "no qrdb permission mapping for gRPC method"

// expectRoleRefusedOnGRPC runs the iam role command args and asserts the gRPC
// method authorizer refuses it when the context's transport goes through that
// authorizer (gRPC and the Unix socket). The role CRUD RPCs carry no gRPC
// permission mapping on purpose, so gRPC refuses them. It returns true when it
// asserted the refusal (the caller ends the test there) and false, without
// running the command, on every other transport, which keeps its success path.
func expectRoleRefusedOnGRPC(t *testing.T, c *iamCLITestContext, args ...string) bool {
	t.Helper()
	if c.protocol != "grpc" && c.protocol != "unix" {
		return false
	}
	stderr, err := c.runExpectError(args...)
	require.Error(t, err, "[%s] %v: an unmapped gRPC method must be refused: %s", c.protocol, args, stderr)
	require.True(t, strings.Contains(stderr, "PermissionDenied") || strings.Contains(stderr, "FORBIDDEN"),
		"[%s] %v should be refused with PermissionDenied or FORBIDDEN: %s", c.protocol, args, stderr)
	require.Contains(t, stderr, grpcUnmappedMethodRefusal,
		"[%s] %v should be refused for having no permission mapping: %s", c.protocol, args, stderr)
	return true
}
