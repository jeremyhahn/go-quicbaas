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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// mcpTogglableProbe flips sealed state so one server instance can be driven
// through the full re-seal cycle.
type mcpTogglableProbe struct{ sealedState bool }

func (p *mcpTogglableProbe) IsSealed() bool { return p.sealedState }

// mcpCeremonyStateProbe reports the ceremony state that decides whether the
// pre-operational sealed-window exemptions still apply. operational=false is a
// daemon whose init ceremony is unfinished; operational=true is the
// already-initialized daemon (restart / post-boot re-seal).
type mcpCeremonyStateProbe struct{ operational bool }

func (p *mcpCeremonyStateProbe) IsOperational() bool { return p.operational }

// mcpRecordingSink captures the sealed-window bypass audit events.
type mcpRecordingSink struct{ events [][2]string }

func (s *mcpRecordingSink) SealedWindowAuthBypass(transport, operation string) {
	s.events = append(s.events, [2]string{transport, operation})
}

// countingMCPAuthorizer is a validator seam double with a call counter so
// tests can assert the sealed-window bypass never consults the validator.
type countingMCPAuthorizer struct {
	calls    int
	authzCtx *platformtoken.AuthorizedContext
	err      error
}

func (c *countingMCPAuthorizer) Authorize(_ context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.authzCtx, nil
}

// newSealedBypassTestServer builds an MCP server (no listeners) with the
// validator posture installed and the sealed-window gate + shared bypass
// armed, then swaps the bypass sink for the recording sink so audits are
// observable. Mirrors the production wiring order (lifecycle installs the
// validator, then the sealed gate).
func newSealedBypassTestServer(
	t *testing.T,
	probe *mcpTogglableProbe,
	operational sealedwindow.OperationalProbe,
	authorizer ValidatorAuthorizer,
	sink *mcpRecordingSink,
) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	s := NewServer(nil, &Config{ServerName: "test", ServerVersion: "0", Transport: "http"}, log)
	require.NotNil(t, s)
	s.SetValidatorAuthorizer(authorizer)
	s.SetSealedWindowGate(probe, operational, sealedwindow.MCPAllowlist())

	// Install a recording sink in place of the logger sink (same probe, same
	// canonical derivation) so the tests can assert audit emission.
	s.sealedMu.Lock()
	s.sealedBypass = sealedwindow.NewAuthBypass(probe, operational, sink)
	s.sealedMu.Unlock()
	return s
}

// callTool drives a full tools/call through handleRequest, exactly as the
// HTTP layer does, with the token (possibly empty) in the context.
func callTool(s *Server, token, tool string) *JSONRPCResponse {
	ctx := contextWithToken(context.Background(), token)
	return s.handleRequest(ctx, &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      tool,
			"arguments": map[string]interface{}{},
		},
	})
}

// TestMCPSealedWindowAuthBypass_Composition is the per-transport contract
// table for the MCP tool dispatch:
//
//	sealed+allowlisted            -> tool executes, no auth, audited
//	sealed+non-allowlisted        -> sealed error
//	unsealed+allowlisted no token -> AuthenticationRequired
//	unsealed+valid scope          -> tool executes (normal enforcement)
//
// The test server has no barrier facade, so an ADMITTED barrier tool returns
// the barrier-unavailable ServiceUnavailable error — proof the request passed
// BOTH the sealed gate and the authorization layer and reached execution.
func TestMCPSealedWindowAuthBypass_Composition(t *testing.T) {
	cases := []struct {
		name        string
		sealed      bool
		operational bool
		tool        string
		token       string
		wantCode    int
		wantSealed  bool
		wantAudit   int
		wantAuthz   int
		wantAuthErr bool
	}{
		{
			name:      "sealed allowlisted unauthenticated admitted to execution",
			sealed:    true,
			tool:      "qrdb_barrier_unseal",
			wantCode:  ServiceUnavailable, // barrier facade absent: execution reached
			wantAudit: 1,
		},
		{
			name:      "sealed allowlisted status admitted to execution",
			sealed:    true,
			tool:      "qrdb_barrier_status",
			wantCode:  ServiceUnavailable,
			wantAudit: 1,
		},
		{
			name:      "sealed pre-operational ceremony status admitted to execution",
			sealed:    true,
			tool:      "qrdb_ceremony_status",
			wantCode:  ServiceUnavailable, // ceremony facade absent: execution reached
			wantAudit: 1,
		},
		// Scope axis, end to end: the ceremony status / claim tools are
		// ScopePreOperational. On an already-initialized (operational) sealed
		// daemon the deny gate still SERVES them — they stay on its allowlist —
		// but the exemption has expired, so the authorization layer demands a
		// token and the unauthenticated call is refused.
		{
			name:        "sealed operational ceremony status requires token",
			sealed:      true,
			operational: true,
			tool:        "qrdb_ceremony_status",
			wantCode:    AuthenticationRequired,
			wantAuthErr: true,
		},
		{
			name:        "sealed operational claim-cert begin requires token",
			sealed:      true,
			operational: true,
			tool:        "qrdb_ceremony_claim_cert_begin",
			wantCode:    AuthenticationRequired,
			wantAuthErr: true,
		},
		{
			name:        "sealed operational unseal still admitted to execution",
			sealed:      true,
			operational: true,
			tool:        "qrdb_barrier_unseal",
			wantCode:    ServiceUnavailable,
			wantAudit:   1,
		},
		{
			name:       "sealed non-allowlisted sealed error",
			sealed:     true,
			tool:       "quicbaas_kv_get",
			wantCode:   InternalError,
			wantSealed: true,
		},
		{
			name:       "sealed barrier seal tool sealed error (deliberate exclusion)",
			sealed:     true,
			tool:       "qrdb_barrier_seal",
			wantCode:   InternalError,
			wantSealed: true,
		},
		{
			name:        "unsealed allowlisted tool without token AuthenticationRequired",
			sealed:      false,
			tool:        "qrdb_barrier_unseal",
			wantCode:    AuthenticationRequired,
			wantAuthErr: true,
		},
		{
			name:      "unsealed valid scope admitted to execution",
			sealed:    false,
			tool:      "qrdb_barrier_unseal",
			token:     "good",
			wantCode:  ServiceUnavailable,
			wantAuthz: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &mcpTogglableProbe{sealedState: tc.sealed}
			sink := &mcpRecordingSink{}
			authorizer := &countingMCPAuthorizer{
				authzCtx: &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "system"},
			}
			s := newSealedBypassTestServer(
				t, probe, &mcpCeremonyStateProbe{operational: tc.operational}, authorizer, sink)

			resp := callTool(s, tc.token, tc.tool)
			require.NotNil(t, resp.Error, "every case ends in a typed JSON-RPC error in this harness")
			assert.Equal(t, tc.wantCode, resp.Error.Code)
			if tc.wantSealed {
				assert.Contains(t, resp.Error.Data.(string), "barrier sealed")
			}
			if !tc.wantAuthErr {
				assert.NotEqual(t, AuthenticationRequired, resp.Error.Code, "must not demand authentication")
			}
			assert.Len(t, sink.events, tc.wantAudit, "audit emissions")
			if tc.wantAudit > 0 {
				assert.Equal(t, "mcp", sink.events[0][0])
				assert.Equal(t, tc.tool, sink.events[0][1])
			}
			assert.Equal(t, tc.wantAuthz, authorizer.calls, "validator consultations")
		})
	}
}

// TestMCPSealedWindowAuthBypass_ResealCycle drives ONE server through
// unsealed -> re-sealed -> unsealed and asserts the bypass window opens and
// closes with the probe.
func TestMCPSealedWindowAuthBypass_ResealCycle(t *testing.T) {
	probe := &mcpTogglableProbe{sealedState: false}
	sink := &mcpRecordingSink{}
	authorizer := &countingMCPAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{Subject: "u1"},
	}
	s := newSealedBypassTestServer(t, probe, &mcpCeremonyStateProbe{}, authorizer, sink)

	// Unsealed: no token -> AuthenticationRequired; valid token -> executes.
	resp := callTool(s, "", "qrdb_barrier_unseal")
	require.NotNil(t, resp.Error)
	assert.Equal(t, AuthenticationRequired, resp.Error.Code)
	resp = callTool(s, "good", "qrdb_barrier_unseal")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ServiceUnavailable, resp.Error.Code)

	// Runtime RE-SEAL: allowlisted tool admits unauthenticated, KV sealed.
	probe.sealedState = true
	resp = callTool(s, "", "qrdb_barrier_unseal")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ServiceUnavailable, resp.Error.Code, "sealed+allowlisted must reach execution unauthenticated")
	require.Len(t, sink.events, 1)
	resp = callTool(s, "good", "quicbaas_kv_get")
	require.NotNil(t, resp.Error)
	assert.True(t, strings.Contains(resp.Error.Data.(string), "barrier sealed"),
		"sealed+non-allowlisted stays sealed even with a token")

	// Unseal completes: enforcement returns immediately.
	probe.sealedState = false
	resp = callTool(s, "", "qrdb_barrier_unseal")
	require.NotNil(t, resp.Error)
	assert.Equal(t, AuthenticationRequired, resp.Error.Code, "bypass must close when unsealed")
	assert.Len(t, sink.events, 1, "no further audit after the window closes")
}

// TestSealedWindowBypassesTool_NilSafety pins that a server without the gate
// installed never bypasses.
func TestSealedWindowBypassesTool_NilSafety(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	s := NewServer(nil, &Config{ServerName: "test", ServerVersion: "0", Transport: "http"}, log)
	assert.False(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))

	// Installing with a nil probe also leaves the bypass uninstalled.
	s.SetSealedWindowGate(nil, nil, sealedwindow.MCPAllowlist())
	assert.False(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))
}
