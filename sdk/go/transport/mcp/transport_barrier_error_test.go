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

// transport_barrier_error_test.go pins how a native go-qrdb barrier tool
// refusal reaches the SDK caller: the node's tools put the error code in the
// nested {"error": {"code": N, ...}} envelope, and the caller sees that code,
// not an internal error.

package mcp

import (
	"context"
	"testing"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestBarrierToolError_NativeEnvelopeKeepsCode(t *testing.T) {
	const tool = "qrdb_barrier_status"
	for _, tc := range []struct {
		name    string
		code    int
		status  string
		message string
		want    sdkerrors.ErrorCode
	}{
		{"permission denied", MCPPermissionDenied, "PERMISSION_DENIED", "permission denied", sdkerrors.CodeForbidden},
		{"unavailable", MCPServiceUnavailable, "UNAVAILABLE", "barrier is sealed", sdkerrors.CodeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:  tool,
				result:    map[string]interface{}{"error": tc.message},
				toolError: true,
				toolErrorStructured: map[string]interface{}{"error": map[string]interface{}{
					"code": tc.code, "status": tc.status, "message": tc.message, "tool": tool,
				}},
			})
			defer cleanup()

			_, err := tr.BarrierStatus(context.Background())
			if err == nil {
				t.Fatal("BarrierStatus: err = nil, want a tool error")
			}
			if got := sdkerrors.Code(err); got != tc.want {
				t.Fatalf("BarrierStatus: code = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}
