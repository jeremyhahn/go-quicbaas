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

package sealedwindow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// togglableProbe is a deterministic probe whose sealed state tests flip to
// exercise the seal / unseal / re-seal cycle against ONE predicate instance.
type togglableProbe struct{ sealedState bool }

func (p *togglableProbe) IsSealed() bool { return p.sealedState }

// recordingSink captures every audit emission for assertions.
type recordingSink struct {
	events [][2]string
}

func (s *recordingSink) SealedWindowAuthBypass(transport, operation string) {
	s.events = append(s.events, [2]string{transport, operation})
}

func newBypassFixture(sealed bool) (*AuthBypass, *togglableProbe, *recordingSink) {
	probe := &togglableProbe{sealedState: sealed}
	sink := &recordingSink{}
	return NewAuthBypass(probe, sink), probe, sink
}

// TestAuthBypass_GRPC is the per-transport contract table for the gRPC
// predicate: sealed+allowlisted bypasses (audited), sealed+other never
// bypasses, and unsealed NEVER bypasses even for allowlisted verbs.
func TestAuthBypass_GRPC(t *testing.T) {
	cases := []struct {
		name       string
		sealed     bool
		fullMethod string
		bypass     bool
	}{
		{"sealed qrdb.v1 unseal", true, "/qrdb.v1.QRDBService/BarrierUnseal", true},
		{"sealed quicbaas mirror unseal", true, "/quicbaas.barrier.v1.Barrier/BarrierUnseal", true},
		{"sealed status", true, "/qrdb.v1.QRDBService/BarrierStatus", true},
		{"sealed ceremony", true, "/qrdb.v1.QRDBService/CeremonyInitialize", true},
		{"sealed share-claim (IdentityGated)", true, "/qrdb.v1.QRDBService/BarrierShareClaimBegin", true},
		{"sealed ceremony mirror list-officers", true, "/quicbaas.ceremony.v1.Ceremony/CeremonyListOfficers", true},
		{"sealed quicbaas health", true, "/quicbaas.v1.QuicBaaS/Health", true},

		{"sealed KV get denied", true, "/quicbaas.v1.QuicBaaS/Get", false},
		{"sealed barrier SEAL excluded", true, "/qrdb.v1.QRDBService/BarrierSeal", false},
		{"sealed credential excluded", true, "/qrdb.v1.QRDBService/CredentialSubmit", false},

		// Service pinning: matching is on the FULL method path, so a bare tail
		// and a same-named method on another co-registered service both stay
		// sealed. "/quicbaas.tenant.v1.TenantBarrier/Status" is the live
		// collision — the per-tenant barrier surface is a documented
		// sealed-window exclusion yet shares the "Status" tail.
		{"sealed bare tail rejected", true, "BarrierUnseal", false},
		{"sealed tenant barrier status rejected", true, "/quicbaas.tenant.v1.TenantBarrier/Status", false},
		{"sealed data-plane health rejected", true, "/quicbaas.ollama.v1.OllamaService/Health", false},

		{"unsealed unseal never bypasses", false, "/qrdb.v1.QRDBService/BarrierUnseal", false},
		{"unsealed status never bypasses", false, "/qrdb.v1.QRDBService/BarrierStatus", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, sink := newBypassFixture(tc.sealed)
			assert.Equal(t, tc.bypass, b.BypassesGRPC(tc.fullMethod))
			if tc.bypass {
				require.Len(t, sink.events, 1, "bypass must be audited")
				assert.Equal(t, "grpc", sink.events[0][0])
				assert.Equal(t, tc.fullMethod, sink.events[0][1])
			} else {
				assert.Empty(t, sink.events, "non-bypass must not be audited")
			}
		})
	}
}

// TestAuthBypass_RESTPath pins the REST path-matching semantics (exact match
// unless the allowlist entry ends in "/", mirroring qrdb's middleware).
func TestAuthBypass_RESTPath(t *testing.T) {
	cases := []struct {
		name   string
		sealed bool
		path   string
		bypass bool
	}{
		{"sealed unseal wire mount", true, "/v1/barrier/unseal", true},
		{"sealed unseal api mirror", true, "/api/v1/barrier/unseal", true},
		{"sealed status", true, "/v1/barrier/status", true},
		{"sealed init prefix", true, "/api/v1/init/status", true},
		{"sealed bootstrap prefix", true, "/v1/bootstrap/trust", true},
		{"sealed officer claim prefix", true, "/v1/barrier/officers/alice/claim/begin", true},
		{"sealed cluster secret", true, "/api/v1/cluster/secret", true},
		{"sealed health", true, "/health", true},

		{"sealed kms denied", true, "/api/v1/kms/keys", false},
		{"sealed credentials excluded", true, "/v1/credentials", false},
		{"sealed exact-entry subpath not prefix", true, "/v1/barrier/unseal/extra", false},
		{"sealed tenant barrier excluded", true, "/api/v1/tenants/t1/barrier/unseal", false},

		{"unsealed unseal never bypasses", false, "/v1/barrier/unseal", false},
		{"unsealed init prefix never bypasses", false, "/api/v1/init/status", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, sink := newBypassFixture(tc.sealed)
			assert.Equal(t, tc.bypass, b.BypassesRESTPath(tc.path))
			if tc.bypass {
				require.Len(t, sink.events, 1)
				assert.Equal(t, "rest", sink.events[0][0])
				assert.Equal(t, tc.path, sink.events[0][1])
			} else {
				assert.Empty(t, sink.events)
			}
		})
	}
}

// TestAuthBypass_NativeQUIC pins the opcode + payload[0] sub-op derivation.
func TestAuthBypass_NativeQUIC(t *testing.T) {
	cases := []struct {
		name    string
		sealed  bool
		opcode  byte
		payload []byte
		bypass  bool
	}{
		{"sealed health no sub-op", true, opHealth, nil, true},
		{"sealed barrier status", true, opBarrier, []byte{subBarrierStatus}, true},
		{"sealed barrier unseal", true, opBarrier, []byte{subBarrierUnseal}, true},
		{"sealed ceremony initialize", true, opCeremony, []byte{subCeremonyInitialize}, true},

		// 0x04 is barrier seal — deliberately excluded from the allowlist.
		{"sealed barrier seal denied", true, opBarrier, []byte{0x04}, false},
		{"sealed barrier empty payload denied", true, opBarrier, nil, false},
		{"sealed KV opcode denied", true, 0x01, nil, false},

		{"unsealed unseal never bypasses", false, opBarrier, []byte{subBarrierUnseal}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, sink := newBypassFixture(tc.sealed)
			assert.Equal(t, tc.bypass, b.BypassesNativeQUIC(tc.opcode, tc.payload))
			if tc.bypass {
				require.Len(t, sink.events, 1)
				assert.Equal(t, "quic", sink.events[0][0])
			} else {
				assert.Empty(t, sink.events)
			}
		})
	}
}

// TestAuthBypass_QRDBQUIC covers the qrdb-quic ALPN bridge method names.
func TestAuthBypass_QRDBQUIC(t *testing.T) {
	cases := []struct {
		name   string
		sealed bool
		method string
		bypass bool
	}{
		{"sealed barrier.Unseal", true, "barrier.Unseal", true},
		{"sealed barrier.Status", true, "barrier.Status", true},
		{"sealed admin.Health", true, "admin.Health", true},
		{"sealed ceremony.SignCSR", true, "ceremony.SignCSR", true},

		{"sealed barrier.Seal excluded", true, "barrier.Seal", false},
		{"sealed credential.Submit excluded", true, "credential.Submit", false},
		{"sealed tenant barrier excluded", true, "tenant.BarrierUnseal", false},

		{"unsealed barrier.Unseal never bypasses", false, "barrier.Unseal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, sink := newBypassFixture(tc.sealed)
			assert.Equal(t, tc.bypass, b.BypassesQRDBQUIC(tc.method))
			if tc.bypass {
				require.Len(t, sink.events, 1)
				assert.Equal(t, "qrdb-quic", sink.events[0][0])
				assert.Equal(t, tc.method, sink.events[0][1])
			} else {
				assert.Empty(t, sink.events)
			}
		})
	}
}

// TestAuthBypass_MCPTool covers the MCP tool-name surface.
func TestAuthBypass_MCPTool(t *testing.T) {
	cases := []struct {
		name   string
		sealed bool
		tool   string
		bypass bool
	}{
		{"sealed barrier unseal", true, "qrdb_barrier_unseal", true},
		{"sealed barrier status", true, "qrdb_barrier_status", true},
		{"sealed ceremony initialize", true, "qrdb_ceremony_initialize", true},
		{"sealed quicbaas officer list", true, "quicbaas_ceremony_list_officers", true},

		{"sealed barrier seal excluded", true, "qrdb_barrier_seal", false},
		{"sealed KV tool denied", true, "quicbaas_kv_get", false},
		{"sealed unknown tool denied", true, "no_such_tool", false},

		{"unsealed unseal never bypasses", false, "qrdb_barrier_unseal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, sink := newBypassFixture(tc.sealed)
			assert.Equal(t, tc.bypass, b.BypassesMCPTool(tc.tool))
			if tc.bypass {
				require.Len(t, sink.events, 1)
				assert.Equal(t, "mcp", sink.events[0][0])
				assert.Equal(t, tc.tool, sink.events[0][1])
			} else {
				assert.Empty(t, sink.events)
			}
		})
	}
}

// TestAuthBypass_ResealCycle drives ONE predicate instance through
// unsealed -> sealed -> unsealed and asserts the bypass window opens and
// closes with the probe: the bypass is strictly sealed-only.
func TestAuthBypass_ResealCycle(t *testing.T) {
	b, probe, sink := newBypassFixture(false)

	// Unsealed: full enforcement, no bypass, no audit.
	assert.False(t, b.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.False(t, b.BypassesRESTPath("/v1/barrier/unseal"))
	assert.Empty(t, sink.events)

	// Re-seal: the window opens for allowlisted verbs only.
	probe.sealedState = true
	assert.True(t, b.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.False(t, b.BypassesGRPC("/quicbaas.v1.QuicBaaS/Get"))
	assert.Len(t, sink.events, 1)

	// Unseal again: the window closes immediately.
	probe.sealedState = false
	assert.False(t, b.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.Len(t, sink.events, 1, "no audit after the window closes")
}

// TestAuthBypass_NilSafety pins that a nil predicate, a nil probe, and a nil
// sink are all safe and fail closed (never bypass, never panic).
func TestAuthBypass_NilSafety(t *testing.T) {
	var nilBypass *AuthBypass
	assert.False(t, nilBypass.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.False(t, nilBypass.BypassesRESTPath("/v1/barrier/unseal"))
	assert.False(t, nilBypass.BypassesNativeQUIC(opBarrier, []byte{subBarrierUnseal}))
	assert.False(t, nilBypass.BypassesQRDBQUIC("barrier.Unseal"))
	assert.False(t, nilBypass.BypassesMCPTool("qrdb_barrier_unseal"))

	nilProbe := NewAuthBypass(nil, &recordingSink{})
	assert.False(t, nilProbe.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))

	// A nil sink must not panic on a bypass.
	nilSink := NewAuthBypass(&togglableProbe{sealedState: true}, nil)
	assert.True(t, nilSink.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
}

// TestNewLoggerAuditSink covers both the nil-logger contract and a real
// structured emission (no assertion on output — the emission must simply not
// panic and must accept the canonical field set).
func TestNewLoggerAuditSink(t *testing.T) {
	assert.Nil(t, NewLoggerAuditSink(nil), "nil logger must yield a nil (no-op) sink")

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	sink := NewLoggerAuditSink(log)
	require.NotNil(t, sink)
	sink.SealedWindowAuthBypass("grpc", "/qrdb.v1.QRDBService/BarrierUnseal")
}
