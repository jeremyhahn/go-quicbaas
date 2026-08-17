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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
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

// newSealedBypassTestServer builds an MCP tool-registry server with the
// sealed-window gate armed, then swaps the bypass sink for the recording
// sink so audits are observable. NOTE: request-time authorization
// (bearer-token / scope enforcement) is no longer this package's concern —
// on the unified server, qrdb's MCP tool registry authorizes every call
// before InvokeTool ever runs (see module_support.go); this package only
// owns the sealed-window admit/deny + bypass-audit gate ahead of that.
func newSealedBypassTestServer(
	t *testing.T,
	probe *mcpTogglableProbe,
	operational sealedwindow.OperationalProbe,
	sink *mcpRecordingSink,
) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	s := NewServer(nil, &Config{ServerName: "test", ServerVersion: "0"}, log)
	require.NotNil(t, s)
	s.SetSealedWindowGate(probe, operational, sealedwindow.MCPAllowlist())

	// Install a recording sink in place of the logger sink (same probe, same
	// canonical derivation) so the tests can assert audit emission.
	s.sealedMu.Lock()
	s.sealedBypass = sealedwindow.NewAuthBypass(probe, operational, sink)
	s.sealedMu.Unlock()
	return s
}

// TestMCPSealedWindowAuthBypass_AdmitsAndAudits pins the per-tool bypass
// contract: sealed+allowlisted admits without a token and audits exactly
// once; sealed+non-allowlisted (or unsealed) never bypasses and never
// audits.
func TestMCPSealedWindowAuthBypass_AdmitsAndAudits(t *testing.T) {
	cases := []struct {
		name        string
		sealed      bool
		operational bool
		tool        string
		wantBypass  bool
	}{
		{
			name:       "sealed allowlisted unseal bypasses",
			sealed:     true,
			tool:       "qrdb_barrier_unseal",
			wantBypass: true,
		},
		{
			name:       "sealed allowlisted status bypasses",
			sealed:     true,
			tool:       "qrdb_barrier_status",
			wantBypass: true,
		},
		{
			name:       "sealed pre-operational ceremony status bypasses",
			sealed:     true,
			tool:       "qrdb_ceremony_status",
			wantBypass: true,
		},
		{
			// The ceremony status / claim tools are ScopePreOperational. On an
			// already-initialized (operational) sealed daemon the deny gate
			// still SERVES them, but the bypass exemption has expired.
			name:        "sealed operational ceremony status does not bypass",
			sealed:      true,
			operational: true,
			tool:        "qrdb_ceremony_status",
			wantBypass:  false,
		},
		{
			name:       "sealed non-allowlisted tool does not bypass",
			sealed:     true,
			tool:       "quicbaas_kv_get",
			wantBypass: false,
		},
		{
			name:       "unsealed allowlisted tool does not bypass",
			sealed:     false,
			tool:       "qrdb_barrier_unseal",
			wantBypass: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &mcpTogglableProbe{sealedState: tc.sealed}
			sink := &mcpRecordingSink{}
			s := newSealedBypassTestServer(t, probe, &mcpCeremonyStateProbe{operational: tc.operational}, sink)

			bypasses := s.sealedWindowBypassesTool(tc.tool)
			assert.Equal(t, tc.wantBypass, bypasses)

			if tc.wantBypass {
				require.Len(t, sink.events, 1)
				assert.Equal(t, "mcp", sink.events[0][0])
				assert.Equal(t, tc.tool, sink.events[0][1])
			} else {
				assert.Empty(t, sink.events)
			}
		})
	}
}

// TestMCPSealedWindowAuthBypass_ResealCycle drives ONE server through
// unsealed -> re-sealed -> unsealed and asserts the bypass window opens and
// closes with the probe.
func TestMCPSealedWindowAuthBypass_ResealCycle(t *testing.T) {
	probe := &mcpTogglableProbe{sealedState: false}
	sink := &mcpRecordingSink{}
	s := newSealedBypassTestServer(t, probe, &mcpCeremonyStateProbe{}, sink)

	// Unsealed: never bypasses.
	assert.False(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))
	assert.Empty(t, sink.events)

	// Runtime RE-SEAL: allowlisted tool bypasses and is audited.
	probe.sealedState = true
	assert.True(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))
	require.Len(t, sink.events, 1)
	assert.False(t, s.sealedWindowBypassesTool("quicbaas_kv_get"), "non-allowlisted stays denied even while sealed")

	// Unseal completes: bypass closes immediately, no further audit.
	probe.sealedState = false
	assert.False(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))
	assert.Len(t, sink.events, 1, "no further audit after the window closes")
}

// TestSealedWindowBypassesTool_NilSafety pins that a server without the gate
// installed never bypasses.
func TestSealedWindowBypassesTool_NilSafety(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	s := NewServer(nil, &Config{ServerName: "test", ServerVersion: "0"}, log)
	assert.False(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))

	// Installing with a nil probe also leaves the bypass uninstalled.
	s.SetSealedWindowGate(nil, nil, sealedwindow.MCPAllowlist())
	assert.False(t, s.sealedWindowBypassesTool("qrdb_barrier_unseal"))
}
