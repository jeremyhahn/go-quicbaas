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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// fakeMCPSealedProbe is a deterministic sealed-window probe for gate tests.
type fakeMCPSealedProbe struct{ sealed bool }

func (p *fakeMCPSealedProbe) IsSealed() bool { return p.sealed }

// newSealedGateTestServer builds an MCP tool-registry server with the
// sealed-window gate installed over the canonical allowlist.
func newSealedGateTestServer(t *testing.T, sealed bool) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	s := NewServer(nil, &Config{ServerName: "test", ServerVersion: "0"}, log)
	require.NotNil(t, s)
	s.SetSealedWindowGate(&fakeMCPSealedProbe{sealed: sealed}, &mcpCeremonyStateProbe{},
		sealedwindow.MCPAllowlist())
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
		// go-qrdb v0.50: a sealed node answers no status question but
		// barrier status and lists no holder to an anonymous caller.
		{"qrdb_ceremony_status", true},
		{"quicbaas_ceremony_list_officers", true},

		// The strict ceremony model (sealedwindow.StrictCeremonyExclusions)
		// withholds the ceremony verbs that MINT or DISTRIBUTE trust from
		// EVERY transport's sealed window, MCP included. They are driven
		// in-process by `quicbaasd init`, never by an unauthenticated MCP
		// client against a sealed daemon.
		{"qrdb_ceremony_initialize", true},
		{"qrdb_ceremony_sign_csr", true},

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
	unwired := NewServer(nil, &Config{}, log)
	assert.False(t, unwired.sealedWindowDeniesTool("quicbaas_kv_get"))
}

// TestInvokeTool_SealedDeniesOffAllowlist asserts the tool dispatcher rejects
// an off-allowlist tool with the typed SealedWindowError BEFORE authorization
// or execution runs.
func TestInvokeTool_SealedDeniesOffAllowlist(t *testing.T) {
	s := newSealedGateTestServer(t, true)

	result, err := s.InvokeTool(context.Background(), "quicbaas_kv_get", map[string]interface{}{})
	require.Nil(t, result)
	require.Error(t, err, "sealed off-allowlist tool must produce an error")

	var sealedErr *SealedWindowError
	require.ErrorAs(t, err, &sealedErr)
	assert.Equal(t, "quicbaas_kv_get", sealedErr.Tool)
	assert.Contains(t, err.Error(), "barrier sealed")
	assert.Contains(t, err.Error(), "quicbaas_kv_get")
}

// TestSealedWindowError_Message pins the coarse sealed envelope wording.
func TestSealedWindowError_Message(t *testing.T) {
	err := &SealedWindowError{Tool: "quicbaas_kv_get"}
	assert.Equal(t,
		"barrier sealed: tool quicbaas_kv_get is not available until the barrier is unsealed",
		err.Error())
}
