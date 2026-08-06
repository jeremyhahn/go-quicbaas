//go:build mcp

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

package mcp

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// fakeMCPSealedProbe is a deterministic sealed-window probe for gate tests.
type fakeMCPSealedProbe struct{ sealed bool }

func (p *fakeMCPSealedProbe) IsSealed() bool { return p.sealed }

// newSealedGateTestServer builds an MCP server (no listeners started) with
// the sealed-window gate installed over the canonical allowlist.
func newSealedGateTestServer(t *testing.T, sealed bool) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	s := NewServer(nil, &Config{ServerName: "test", ServerVersion: "0", Transport: "http"}, log)
	require.NotNil(t, s)
	s.SetSealedWindowGate(&fakeMCPSealedProbe{sealed: sealed}, sealedwindow.MCPAllowlist())
	return s
}

// TestSealedWindowDeniesTool_SealedAdmitDenyTable is the table-driven
// admit/deny check for the MCP tool gate while sealed, driven from the
// single canonical derivation.
func TestSealedWindowDeniesTool_SealedAdmitDenyTable(t *testing.T) {
	s := newSealedGateTestServer(t, true)

	cases := []struct {
		tool   string
		denied bool
	}{
		{"qrdb_barrier_status", false},
		{"qrdb_barrier_initialize", false},
		{"qrdb_barrier_unseal", false},
		{"qrdb_barrier_shamir_unseal_share", false},
		{"qrdb_barrier_root_token", false},
		{"qrdb_ceremony_initialize", false},
		{"qrdb_ceremony_status", false},
		{"qrdb_ceremony_sign_csr", false},
		{"quicbaas_ceremony_list_officers", false},

		{"qrdb_barrier_seal", true},
		{"qrdb_barrier_enable_encryption", true},
		{"qrdb_credential_submit", true},
		{"qrdb_credential_list", true},
		{"qrdb_tenant_barrier_enable_encryption", true},
		{"quicbaas_kv_get", true},
		{"quicbaas_kms_generate_key", true},
		{"no_such_tool", true},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			assert.Equal(t, tc.denied, s.sealedWindowDeniesTool(tc.tool))
		})
	}
}

// TestSealedWindowDeniesTool_UnsealedPassThrough asserts the gate opens the
// moment the probe reports unsealed, and stays inert when unwired.
func TestSealedWindowDeniesTool_UnsealedPassThrough(t *testing.T) {
	s := newSealedGateTestServer(t, false)
	assert.False(t, s.sealedWindowDeniesTool("quicbaas_kv_get"))
	assert.False(t, s.sealedWindowDeniesTool("qrdb_credential_submit"))

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	unwired := NewServer(nil, &Config{Transport: "http"}, log)
	assert.False(t, unwired.sealedWindowDeniesTool("quicbaas_kv_get"))
}

// TestHandleToolsCall_SealedDeniesOffAllowlist asserts the tools/call
// dispatcher rejects an off-allowlist tool with the sealed envelope BEFORE
// authorization or execution, and that the response is a JSON-RPC error
// carrying the typed SealedWindowError message.
func TestHandleToolsCall_SealedDeniesOffAllowlist(t *testing.T) {
	s := newSealedGateTestServer(t, true)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "quicbaas_kv_get",
			"arguments": map[string]interface{}{},
		},
	}
	resp := s.handleToolsCall(context.Background(), req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error, "sealed off-allowlist tool must produce a JSON-RPC error")
	data, _ := resp.Error.Data.(string)
	assert.Contains(t, data, "barrier sealed")
	assert.Contains(t, data, "quicbaas_kv_get")
}

// TestSealedWindowError_Message pins the coarse sealed envelope wording.
func TestSealedWindowError_Message(t *testing.T) {
	err := &SealedWindowError{Tool: "quicbaas_kv_get"}
	assert.Equal(t,
		"barrier sealed: tool quicbaas_kv_get is not available until the barrier is unsealed",
		err.Error())
}

// TestStartHTTPTransport_BindFailureIsFatal asserts B2 (process-fatal
// transport construction): a listener bind failure surfaces synchronously
// from Start() as a typed *HTTPListenError so the daemon can abort startup,
// instead of being logged from a goroutine while the daemon continues
// without an MCP surface.
func TestStartHTTPTransport_BindFailureIsFatal(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)

	// Occupy a port, then point the MCP server at it.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = occupied.Close() })

	s := NewServer(nil, &Config{
		ServerName:    "test",
		ServerVersion: "0",
		Transport:     "http",
		HTTPAddr:      occupied.Addr().String(),
	}, log)

	err = s.Start()
	var listenErr *HTTPListenError
	require.ErrorAs(t, err, &listenErr, "bind failure must be a typed HTTPListenError, got %v", err)
	assert.Equal(t, occupied.Addr().String(), listenErr.Addr)
	assert.False(t, s.running.Load(), "server must not report running after a bind failure")
}
