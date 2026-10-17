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

// transport_barrier_enable_encryption_test.go drives BarrierEnableEncryption
// through a real in-process MCP server and pins the argument names qrdb's
// qrdb_barrier_enable_encryption tool decodes.

package mcp

import (
	"context"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestBarrierEnableEncryption_EncodesQRDBToolArguments(t *testing.T) {
	tests := []struct {
		name     string
		req      *qrdbtransport.BarrierEnableEncryptionRequest
		wantTool string
		wantArgs map[string]interface{}
		// noArgs names arguments the server's tool does not read, which
		// the client must therefore not send.
		noArgs []string
		result map[string]interface{}
		want   qrdbtransport.BarrierEnableEncryptionResponse
	}{
		{
			name: "system barrier to passphrase",
			req: &qrdbtransport.BarrierEnableEncryptionRequest{
				Strategy:     "passphrase",
				Mechanism:    "in-place",
				Availability: "online",
				Passphrase:   []byte("pw"),
			},
			wantTool: "qrdb_barrier_enable_encryption",
			noArgs:   []string{"tenant_id", "strategy"},
			wantArgs: map[string]interface{}{
				"target_strategy":  "passphrase",
				"mechanism":        "in-place",
				"availability":     "online",
				"passphrase":       "pw",
				"shamir_threshold": float64(0),
				"shamir_total":     float64(0),
			},
			result: map[string]interface{}{"status": "migration_started", "scope": "system"},
			want:   qrdbtransport.BarrierEnableEncryptionResponse{Status: "migration_started", Scope: "system"},
		},
		{
			name: "tenant barrier to shamir",
			req: &qrdbtransport.BarrierEnableEncryptionRequest{
				TenantID:        "acme",
				Strategy:        "shamir",
				ShamirThreshold: 2,
				ShamirTotal:     3,
			},
			wantTool: "qrdb_tenant_barrier_enable_encryption",
			noArgs:   []string{"strategy"},
			wantArgs: map[string]interface{}{
				"tenant_id":        "acme",
				"target_strategy":  "shamir",
				"mechanism":        "",
				"availability":     "",
				"passphrase":       "",
				"shamir_threshold": float64(2),
				"shamir_total":     float64(3),
			},
			result: map[string]interface{}{"status": "migration_started", "scope": "tenant"},
			want:   qrdbtransport.BarrierEnableEncryptionResponse{Status: "migration_started", Scope: "tenant"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: tc.wantTool,
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					for key, want := range tc.wantArgs {
						if got := args[key]; got != want {
							t.Errorf("arg %q = %#v, want %#v", key, got, want)
						}
					}
					for _, key := range tc.noArgs {
						if got, sent := args[key]; sent {
							t.Errorf("arg %q = %#v sent, but %s does not read it", key, got, tc.wantTool)
						}
					}
				},
				result: tc.result,
			})
			defer cleanup()

			resp, err := tr.BarrierEnableEncryption(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("BarrierEnableEncryption: %v", err)
			}
			if *resp != tc.want {
				t.Errorf("response = %+v, want %+v", *resp, tc.want)
			}
		})
	}
}

func TestBarrierEnableEncryption_ServerRefusalIsTyped(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "qrdb_barrier_enable_encryption",
		result:        map[string]interface{}{"error": "migration provider unavailable"},
		toolError:     true,
		toolErrorCode: MCPUnimplemented,
	})
	defer cleanup()

	_, err := tr.BarrierEnableEncryption(context.Background(), &qrdbtransport.BarrierEnableEncryptionRequest{
		Strategy:   "passphrase",
		Passphrase: []byte("pw"),
	})
	if !errors.IsUnimplemented(err) {
		t.Fatalf("err = %v, want an Unimplemented error", err)
	}
}

func TestBarrierEnableEncryption_UndecodableResponseIsInternal(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "qrdb_barrier_enable_encryption",
		result:   map[string]interface{}{"status": 7},
	})
	defer cleanup()

	_, err := tr.BarrierEnableEncryption(context.Background(), &qrdbtransport.BarrierEnableEncryptionRequest{Strategy: "passphrase"})
	if errors.Code(err) != errors.CodeInternal {
		t.Fatalf("err = %v, want an Internal error", err)
	}
}

func TestBarrierEnableEncryption_NilRequestIsInvalidInput(t *testing.T) {
	tr := &Transport{}
	if _, err := tr.BarrierEnableEncryption(context.Background(), nil); !errors.IsInvalidInput(err) {
		t.Fatalf("err = %v, want an InvalidInput error", err)
	}
}
