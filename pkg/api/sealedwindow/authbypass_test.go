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
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// togglableProbe is a deterministic probe whose sealed state tests flip to
// exercise the seal / unseal / re-seal cycle against ONE predicate instance.
type togglableProbe struct{ sealedState bool }

func (p *togglableProbe) IsSealed() bool { return p.sealedState }

// togglableOperational is a deterministic OperationalProbe whose verdict tests
// flip to drive ONE predicate instance through both ceremony states.
type togglableOperational struct{ operationalState bool }

func (p *togglableOperational) IsOperational() bool { return p.operationalState }

// unavailableOperational models a probe that cannot determine the ceremony
// state — an absent ceremony service, or a status read that failed. The
// OperationalProbe contract requires it to report operational so the
// unauthenticated surface NARROWS rather than widening.
type unavailableOperational struct{}

func (unavailableOperational) IsOperational() bool { return true }

// recordingSink captures every audit emission for assertions.
type recordingSink struct {
	events [][2]string
}

func (s *recordingSink) SealedWindowAuthBypass(transport, operation string) {
	s.events = append(s.events, [2]string{transport, operation})
}

// state names the two ceremony states every transport matrix is evaluated in.
type state int

const (
	// statePreOperational is a sealed daemon whose init ceremony is still
	// unfinished: the pre-operational exemptions apply.
	statePreOperational state = iota

	// stateOperational is a sealed daemon that has already completed its
	// ceremony — the daemon-restart / post-boot-reseal case. The
	// pre-operational exemptions have expired.
	stateOperational
)

// String names the state for subtest naming.
func (s state) String() string {
	if s == statePreOperational {
		return "pre-operational"
	}
	return "operational"
}

// newBypassFixture builds a predicate over togglable probes. sealed sets the
// barrier state; st sets the ceremony state.
func newBypassFixture(sealed bool, st state) (*AuthBypass, *togglableProbe, *togglableOperational, *recordingSink) {
	probe := &togglableProbe{sealedState: sealed}
	operational := &togglableOperational{operationalState: st == stateOperational}
	sink := &recordingSink{}
	return NewAuthBypass(probe, operational, sink), probe, operational, sink
}

// assertBypass asserts one predicate decision plus the audit contract: exactly
// one event with the expected transport/operation on an admit, no event at all
// on a deny.
func assertBypass(t *testing.T, sink *recordingSink, transport, operation string, want, got bool) {
	t.Helper()
	assert.Equal(t, want, got)
	if want {
		require.Len(t, sink.events, 1, "bypass must be audited")
		assert.Equal(t, transport, sink.events[0][0])
		assert.Equal(t, operation, sink.events[0][1])
		return
	}
	assert.Empty(t, sink.events, "non-bypass must not be audited")
}

// wantFor selects the expected decision for a state from a case's two columns.
func wantFor(st state, preOp, op bool) bool {
	if st == stateOperational {
		return op
	}
	return preOp
}

// -----------------------------------------------------------------------------
// Per-transport admit / deny matrices, evaluated in BOTH ceremony states.
//
// Every case pins the decision for a specific wire name in both sealed states
// plus the unsealed state, so the scope axis is asserted entry by entry rather
// than by set size:
//
//   - preOp: sealed + ceremony unfinished
//   - op:    sealed + ceremony complete (restart / post-boot re-seal)
//   - unsealed is always false — the bypass is strictly sealed-only.
// -----------------------------------------------------------------------------

// TestAuthBypass_GRPC is the per-transport contract table for the gRPC
// predicate.
func TestAuthBypass_GRPC(t *testing.T) {
	cases := []struct {
		name       string
		fullMethod string
		preOp      bool
		op         bool
	}{
		// ScopeAlways: the unseal / recovery surface. A daemon restarted after
		// a completed ceremony is operational AND sealed, and these are the
		// only way back to a usable data plane — they must be admitted in BOTH
		// states or a sealed daemon can never be unsealed.
		{"qrdb.v1 unseal", "/qrdb.v1.QRDBService/BarrierUnseal", true, true},
		{"quicbaas mirror unseal", "/quicbaas.barrier.v1.Barrier/BarrierUnseal", true, true},
		{"barrier status", "/qrdb.v1.QRDBService/BarrierStatus", true, true},
		{"shamir unseal-share", "/qrdb.v1.QRDBService/BarrierShamirUnsealShare", true, true},
		{"shamir unseal-shares", "/qrdb.v1.QRDBService/BarrierShamirUnsealShares", true, true},
		{"submit unseal-share", "/qrdb.v1.QRDBService/CeremonySubmitUnsealShare", true, true},
		{"recovery recover", "/qrdb.v1.QRDBService/BarrierRecoveryRecover", true, true},
		{"root token", "/qrdb.v1.QRDBService/BarrierRootToken", true, true},
		{"barrier share-claim begin", "/qrdb.v1.QRDBService/BarrierShareClaimBegin", true, true},
		{"barrier share-claim complete", "/qrdb.v1.QRDBService/BarrierShareClaimComplete", true, true},
		{"ceremony list-officers mirror", "/quicbaas.ceremony.v1.Ceremony/CeremonyListOfficers", true, true},
		{"quicbaas health", "/quicbaas.v1.QuicBaaS/Health", true, true},

		// ScopePreOperational: the self-authorizing ceremony claim handshakes
		// and the read-only status probe. Exempt only while the ceremony is
		// unfinished; on an already-initialized sealed daemon they require
		// authentication.
		{"ceremony status", "/qrdb.v1.QRDBService/CeremonyStatus", true, false},
		{"ceremony status mirror", "/quicbaas.ceremony.v1.Ceremony/CeremonyStatus", true, false},
		{"claim-cert begin", "/qrdb.v1.QRDBService/CeremonyClaimCertBegin", true, false},
		{"claim-cert begin mirror", "/quicbaas.ceremony.v1.Ceremony/CeremonyClaimCertBegin", true, false},
		{"claim-cert complete", "/qrdb.v1.QRDBService/CeremonyClaimCertComplete", true, false},
		{"officer-share begin", "/qrdb.v1.QRDBService/CeremonyBeginClaimShare", true, false},
		{"officer-share complete", "/qrdb.v1.QRDBService/CeremonyCompleteClaimShare", true, false},
		{"custodian claim-share begin", "/qrdb.v1.QRDBService/CeremonyClaimShareBegin", true, false},
		{"custodian claim-share complete", "/qrdb.v1.QRDBService/CeremonyClaimShareComplete", true, false},

		// Strict ceremony model: the verbs that MINT or SIGN credential
		// material never bypass authentication, in EITHER state. qrdb keeps
		// them on its own permissive allowlist; quicbaas deliberately does not
		// (see StrictCeremonyExclusions). They are reachable only from the
		// local CLI against a daemon with no listener up.
		{"ceremony initialize excluded", "/qrdb.v1.QRDBService/CeremonyInitialize", false, false},
		{"ceremony sign-csr excluded", "/qrdb.v1.QRDBService/CeremonySignCSR", false, false},
		{"ceremony enroll-ext-officer excluded", "/qrdb.v1.QRDBService/CeremonyEnrollExternalOfficer", false, false},
		{"ceremony mirror initialize excluded", "/quicbaas.ceremony.v1.Ceremony/CeremonyInitialize", false, false},

		{"KV get denied", "/quicbaas.v1.QuicBaaS/Get", false, false},
		{"barrier SEAL excluded", "/qrdb.v1.QRDBService/BarrierSeal", false, false},
		{"credential excluded", "/qrdb.v1.QRDBService/CredentialSubmit", false, false},

		// Service pinning: matching is on the FULL method path, so a bare tail
		// and a same-named method on another co-registered service both stay
		// sealed. "/quicbaas.tenant.v1.TenantBarrier/Status" is the live
		// collision — the per-tenant barrier surface is a documented
		// sealed-window exclusion yet shares the "Status" tail.
		{"bare tail rejected", "BarrierUnseal", false, false},
		{"tenant barrier status rejected", "/quicbaas.tenant.v1.TenantBarrier/Status", false, false},
		{"data-plane health rejected", "/quicbaas.ollama.v1.OllamaService/Health", false, false},
	}
	for _, tc := range cases {
		for _, st := range []state{statePreOperational, stateOperational} {
			want := wantFor(st, tc.preOp, tc.op)
			t.Run(tc.name+" sealed "+st.String(), func(t *testing.T) {
				b, _, _, sink := newBypassFixture(true, st)
				assertBypass(t, sink, "grpc", tc.fullMethod, want, b.BypassesGRPC(tc.fullMethod))
			})
		}
		t.Run(tc.name+" unsealed", func(t *testing.T) {
			b, _, _, sink := newBypassFixture(false, statePreOperational)
			assertBypass(t, sink, "grpc", tc.fullMethod, false, b.BypassesGRPC(tc.fullMethod))
		})
	}
}

// TestAuthBypass_RESTPath pins the REST path-matching semantics (exact match
// unless the allowlist entry ends in "/", mirroring qrdb's middleware) in both
// ceremony states.
func TestAuthBypass_RESTPath(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		preOp  bool
		op     bool
		method string // empty means POST
	}{
		// ScopeAlways.
		{"unseal wire mount", "/v1/barrier/unseal", true, true, ""},
		{"unseal api mirror", "/api/v1/barrier/unseal", true, true, ""},
		{"barrier status", "/v1/barrier/status", true, true, ""},
		{"shamir unseal-share", "/v1/barrier/shamir/unseal-share", true, true, ""},
		{"submit unseal-share", "/v1/barrier/unseal-share", true, true, ""},
		{"recovery recover", "/v1/barrier/recovery/recover", true, true, ""},
		{"officer claim prefix", "/v1/barrier/officers/alice/claim/begin", true, true, ""},
		{"officer list init mirror", "/api/v1/init/officers", true, true, ""},
		{"bootstrap prefix", "/v1/bootstrap/trust", true, true, ""},
		{"cluster secret", "/api/v1/cluster/secret", true, true, ""},
		{"health", "/health", true, true, ""},

		// ScopePreOperational.
		{"ceremony status", "/v1/ceremony/status", true, false, ""},
		{"init status mirror", "/api/v1/init/status", true, false, ""},
		{"claim-cert begin", "/v1/ceremony/claim-cert/begin", true, false, ""},
		{"claim-cert begin mirror", "/api/v1/init/claim-cert/begin", true, false, ""},
		{"claim-cert complete mirror", "/api/v1/init/claim-cert/complete", true, false, ""},
		{"officer-share begin", "/v1/ceremony/officer-share/begin", true, false, ""},
		{"officer-share complete mirror", "/api/v1/init/officer-share/complete", true, false, ""},
		{"custodian claim-share begin", "/v1/ceremony/claim-share/begin", true, false, ""},
		{"custodian claim-share complete", "/v1/ceremony/claim-share/complete", true, false, ""},

		// Strict exclusions and off-allowlist paths, denied in every state.
		{"ceremony initialize excluded", "/v1/ceremony/initialize", false, false, ""},
		{"init initialize mirror excluded", "/api/v1/init/initialize", false, false, ""},
		{"sign-csr excluded", "/v1/ceremony/sign-csr", false, false, ""},
		{"enroll-external-officer excluded", "/v1/init/enroll-external-officer", false, false, ""},
		{"kms denied", "/api/v1/kms/keys", false, false, ""},
		{"credentials excluded", "/v1/credentials", false, false, ""},
		{"exact-entry subpath not prefix", "/v1/barrier/unseal/extra", false, false, ""},

		// go-qrdb v0.25 method-qualified the officer registrations and withdrew
		// the barrier officer list, rekey and the recovery-key verbs.
		{"officer register", "/v1/barrier/officers", true, false, http.MethodPost},
		{"barrier officer list withdrawn", "/v1/barrier/officers", false, false, http.MethodGet},
		{"recovery officer register", "/v1/barrier/recovery/officers", true, false, http.MethodPost},
		{"recovery officer list not admitted", "/v1/barrier/recovery/officers", false, false, http.MethodGet},
		{"rekey withdrawn", "/v1/barrier/rekey", false, false, ""},
		{"recovery generate withdrawn", "/v1/barrier/recovery/generate", false, false, ""},
		{"recovery status withdrawn", "/v1/barrier/recovery/status", false, false, http.MethodGet},
		{"recovery delete withdrawn", "/v1/barrier/recovery", false, false, http.MethodDelete},
		{"tenant barrier excluded", "/api/v1/tenants/t1/barrier/unseal", false, false, ""},
	}
	for _, tc := range cases {
		method := tc.method
		if method == "" {
			method = http.MethodPost
		}
		for _, st := range []state{statePreOperational, stateOperational} {
			want := wantFor(st, tc.preOp, tc.op)
			t.Run(tc.name+" sealed "+st.String(), func(t *testing.T) {
				b, _, _, sink := newBypassFixture(true, st)
				assertBypass(t, sink, "rest", tc.path, want, b.BypassesRESTPath(method, tc.path))
			})
		}
		t.Run(tc.name+" unsealed", func(t *testing.T) {
			b, _, _, sink := newBypassFixture(false, statePreOperational)
			assertBypass(t, sink, "rest", tc.path, false, b.BypassesRESTPath(method, tc.path))
		})
	}
}

// TestAuthBypass_NativeQUIC pins the opcode + payload[0] sub-op derivation in
// both ceremony states.
func TestAuthBypass_NativeQUIC(t *testing.T) {
	cases := []struct {
		name    string
		opcode  byte
		payload []byte
		preOp   bool
		op      bool
	}{
		// ScopeAlways.
		{"health no sub-op", opHealth, nil, true, true},
		{"barrier status", opBarrier, []byte{subBarrierStatus}, true, true},
		{"barrier unseal", opBarrier, []byte{subBarrierUnseal}, true, true},
		{"shamir unseal-share", opBarrier, []byte{subBarrierShamirUnsealShare}, true, true},
		{"shamir unseal-shares", opBarrier, []byte{subBarrierShamirUnsealShares}, true, true},
		{"recovery recover", opBarrier, []byte{subBarrierRecoveryRecover}, true, true},
		{"root token", opBarrier, []byte{subBarrierRootToken}, true, true},
		{"ceremony list-officers", opCeremony, []byte{subCeremonyListOfficers}, true, true},

		// ScopePreOperational.
		{"ceremony status", opCeremony, []byte{subCeremonyStatus}, true, false},
		{"claim-cert begin", opCeremony, []byte{subCeremonyClaimCertBegin}, true, false},
		{"claim-cert complete", opCeremony, []byte{subCeremonyClaimCertComplete}, true, false},
		{"officer-share begin", opCeremony, []byte{subCeremonyBeginClaimShare}, true, false},
		{"officer-share complete", opCeremony, []byte{subCeremonyCompleteClaimShare}, true, false},

		// The trust-minting ceremony verbs are withheld by
		// StrictCeremonyExclusions on EVERY transport and in EVERY state: an
		// unauthenticated caller reaching initialize on a fresh daemon owns the
		// trust root.
		{"ceremony initialize excluded", opCeremony, []byte{subCeremonyInitialize}, false, false},
		{"ceremony sign-csr excluded", opCeremony, []byte{subCeremonySignCSR}, false, false},
		{"ceremony enroll-officer excluded", opCeremony, []byte{subCeremonyEnrollExternalOfficer}, false, false},

		// 0x04 is barrier seal — deliberately excluded from the allowlist.
		{"barrier seal denied", opBarrier, []byte{0x04}, false, false},
		{"barrier empty payload denied", opBarrier, nil, false, false},
		{"KV opcode denied", 0x01, nil, false, false},
	}
	for _, tc := range cases {
		for _, st := range []state{statePreOperational, stateOperational} {
			want := wantFor(st, tc.preOp, tc.op)
			t.Run(tc.name+" sealed "+st.String(), func(t *testing.T) {
				b, _, _, sink := newBypassFixture(true, st)
				got := b.BypassesNativeQUIC(tc.opcode, tc.payload)
				assert.Equal(t, want, got)
				if want {
					require.Len(t, sink.events, 1)
					assert.Equal(t, "quic", sink.events[0][0])
				} else {
					assert.Empty(t, sink.events)
				}
			})
		}
		t.Run(tc.name+" unsealed", func(t *testing.T) {
			b, _, _, sink := newBypassFixture(false, statePreOperational)
			assert.False(t, b.BypassesNativeQUIC(tc.opcode, tc.payload))
			assert.Empty(t, sink.events)
		})
	}
}

// TestAuthBypass_QRDBQUIC covers the qrdb-quic ALPN bridge method names in both
// ceremony states.
func TestAuthBypass_QRDBQUIC(t *testing.T) {
	cases := []struct {
		name   string
		method string
		preOp  bool
		op     bool
	}{
		// ScopeAlways.
		{"barrier.Unseal", "barrier.Unseal", true, true},
		{"barrier.Status", "barrier.Status", true, true},
		{"barrier.ShamirUnsealShare", "barrier.ShamirUnsealShare", true, true},
		{"barrier.ShamirUnsealShares", "barrier.ShamirUnsealShares", true, true},
		{"barrier.RecoveryRecover", "barrier.RecoveryRecover", true, true},
		{"barrier.ShareClaimBegin", "barrier.ShareClaimBegin", true, true},
		{"ceremony.SubmitUnsealShare", "ceremony.SubmitUnsealShare", true, true},
		{"admin.Health", "admin.Health", true, true},

		// ScopePreOperational.
		{"ceremony.Status", "ceremony.Status", true, false},
		{"ceremony.ClaimCertBegin", "ceremony.ClaimCertBegin", true, false},
		{"ceremony.ClaimCertComplete", "ceremony.ClaimCertComplete", true, false},
		{"ceremony.BeginClaimShare", "ceremony.BeginClaimShare", true, false},
		{"ceremony.CompleteClaimShare", "ceremony.CompleteClaimShare", true, false},
		{"ceremony.ClaimShareBegin", "ceremony.ClaimShareBegin", true, false},
		{"ceremony.ClaimShareComplete", "ceremony.ClaimShareComplete", true, false},

		// StrictCeremonyExclusions applies to the bridge too.
		{"ceremony.SignCSR excluded", "ceremony.SignCSR", false, false},
		{"ceremony.Initialize excluded", "ceremony.Initialize", false, false},
		{"ceremony.EnrollExternalOfficer excluded", "ceremony.EnrollExternalOfficer", false, false},

		{"barrier.Seal excluded", "barrier.Seal", false, false},
		{"credential.Submit excluded", "credential.Submit", false, false},
		{"tenant barrier excluded", "tenant.BarrierUnseal", false, false},
	}
	for _, tc := range cases {
		for _, st := range []state{statePreOperational, stateOperational} {
			want := wantFor(st, tc.preOp, tc.op)
			t.Run(tc.name+" sealed "+st.String(), func(t *testing.T) {
				b, _, _, sink := newBypassFixture(true, st)
				assertBypass(t, sink, "qrdb-quic", tc.method, want, b.BypassesQRDBQUIC(tc.method))
			})
		}
		t.Run(tc.name+" unsealed", func(t *testing.T) {
			b, _, _, sink := newBypassFixture(false, statePreOperational)
			assertBypass(t, sink, "qrdb-quic", tc.method, false, b.BypassesQRDBQUIC(tc.method))
		})
	}
}

// TestAuthBypass_MCPTool covers the MCP tool-name surface in both ceremony
// states.
func TestAuthBypass_MCPTool(t *testing.T) {
	cases := []struct {
		name  string
		tool  string
		preOp bool
		op    bool
	}{
		// ScopeAlways.
		{"barrier unseal", "qrdb_barrier_unseal", true, true},
		{"barrier status", "qrdb_barrier_status", true, true},
		{"shamir unseal-share", "qrdb_barrier_shamir_unseal_share", true, true},
		{"shamir unseal-shares", "qrdb_barrier_shamir_unseal_shares", true, true},
		{"recovery recover", "qrdb_barrier_recovery_recover", true, true},
		{"submit unseal-share", "qrdb_ceremony_submit_unseal_share", true, true},
		{"quicbaas officer list", "quicbaas_ceremony_list_officers", true, true},

		// ScopePreOperational.
		{"ceremony status", "qrdb_ceremony_status", true, false},
		{"claim-cert begin", "qrdb_ceremony_claim_cert_begin", true, false},
		{"claim-cert complete", "qrdb_ceremony_claim_cert_complete", true, false},
		{"begin claim-share", "qrdb_ceremony_begin_claim_share", true, false},
		{"complete claim-share", "qrdb_ceremony_complete_claim_share", true, false},
		{"custodian claim-share begin", "qrdb_ceremony_claim_share_begin", true, false},
		{"custodian claim-share complete", "qrdb_ceremony_claim_share_complete", true, false},

		// StrictCeremonyExclusions applies to MCP too.
		{"ceremony initialize excluded", "qrdb_ceremony_initialize", false, false},
		{"ceremony sign-csr excluded", "qrdb_ceremony_sign_csr", false, false},

		{"barrier seal excluded", "qrdb_barrier_seal", false, false},
		{"KV tool denied", "quicbaas_kv_get", false, false},
		{"unknown tool denied", "no_such_tool", false, false},
	}
	for _, tc := range cases {
		for _, st := range []state{statePreOperational, stateOperational} {
			want := wantFor(st, tc.preOp, tc.op)
			t.Run(tc.name+" sealed "+st.String(), func(t *testing.T) {
				b, _, _, sink := newBypassFixture(true, st)
				assertBypass(t, sink, "mcp", tc.tool, want, b.BypassesMCPTool(tc.tool))
			})
		}
		t.Run(tc.name+" unsealed", func(t *testing.T) {
			b, _, _, sink := newBypassFixture(false, statePreOperational)
			assertBypass(t, sink, "mcp", tc.tool, false, b.BypassesMCPTool(tc.tool))
		})
	}
}

// -----------------------------------------------------------------------------
// Derived, exhaustive scope coverage. The tables above pin the interesting
// entries by name; these tests assert the SAME rule over EVERY entry the
// canonical table derives, so a verb added to Table cannot slip through
// untested.
// -----------------------------------------------------------------------------

// TestAuthBypass_UnsealFlowSurvivesOperational is the anti-brick guard: every
// ScopeAlways entry on every transport must still bypass authentication on a
// sealed daemon whose ceremony has completed. That is the whole point of the
// sealed window — a daemon restarted after a completed ceremony is operational
// AND sealed, and a sealed IAM plane cannot validate the token the gate would
// otherwise demand. A failure here means a sealed daemon can never be unsealed.
func TestAuthBypass_UnsealFlowSurvivesOperational(t *testing.T) {
	b, _, _, _ := newBypassFixture(true, stateOperational)

	restPaths := RESTAllowlistScoped(ScopeAlways)
	require.NotEmpty(t, restPaths)
	for _, entry := range restPaths {
		method, probe := restProbe(entry)
		assert.Truef(t, b.BypassesRESTPath(method, probe),
			"operational+sealed daemon must still admit REST %s %q", method, probe)
	}

	grpcMethods := GRPCFullMethodAllowlistScoped(ScopeAlways)
	require.NotEmpty(t, grpcMethods)
	for _, method := range grpcMethods {
		assert.Truef(t, b.BypassesGRPC(method),
			"operational+sealed daemon must still admit gRPC %q", method)
	}

	nativeOps := QUICAllowlistScoped(ScopeAlways)
	require.NotEmpty(t, nativeOps)
	for op := range nativeOps {
		var payload []byte
		if op.SubOp != 0 {
			payload = []byte{op.SubOp}
		}
		assert.Truef(t, b.BypassesNativeQUIC(op.Opcode, payload),
			"operational+sealed daemon must still admit QUIC 0x%02X/0x%02X", op.Opcode, op.SubOp)
	}

	bridge := QRDBQUICAllowlistScoped(ScopeAlways)
	require.NotEmpty(t, bridge)
	for method := range bridge {
		assert.Truef(t, b.BypassesQRDBQUIC(method),
			"operational+sealed daemon must still admit qrdb-quic %q", method)
	}

	tools := MCPAllowlistScoped(ScopeAlways)
	require.NotEmpty(t, tools)
	for tool := range tools {
		assert.Truef(t, b.BypassesMCPTool(tool),
			"operational+sealed daemon must still admit MCP tool %q", tool)
	}
}

// TestAuthBypass_PreOperationalExemptionExpires asserts the converse over every
// derived ScopePreOperational entry: admitted while the ceremony is unfinished,
// DENIED once the daemon is operational. This is the defect being closed — the
// ceremony claim handshakes must not stay authentication-exempt on an
// already-initialized sealed daemon.
func TestAuthBypass_PreOperationalExemptionExpires(t *testing.T) {
	pre, _, _, _ := newBypassFixture(true, statePreOperational)
	post, _, _, _ := newBypassFixture(true, stateOperational)

	restPaths := RESTAllowlistScoped(ScopePreOperational)
	require.NotEmpty(t, restPaths)
	for _, entry := range restPaths {
		method, probe := restProbe(entry)
		assert.Truef(t, pre.BypassesRESTPath(method, probe), "pre-operational must admit REST %s %q", method, probe)
		assert.Falsef(t, post.BypassesRESTPath(method, probe), "operational must DENY REST %s %q", method, probe)
	}

	grpcMethods := GRPCFullMethodAllowlistScoped(ScopePreOperational)
	require.NotEmpty(t, grpcMethods)
	for _, method := range grpcMethods {
		assert.Truef(t, pre.BypassesGRPC(method), "pre-operational must admit gRPC %q", method)
		assert.Falsef(t, post.BypassesGRPC(method), "operational must DENY gRPC %q", method)
	}

	nativeOps := QUICAllowlistScoped(ScopePreOperational)
	require.NotEmpty(t, nativeOps)
	for op := range nativeOps {
		payload := []byte{op.SubOp}
		assert.Truef(t, pre.BypassesNativeQUIC(op.Opcode, payload),
			"pre-operational must admit QUIC 0x%02X/0x%02X", op.Opcode, op.SubOp)
		assert.Falsef(t, post.BypassesNativeQUIC(op.Opcode, payload),
			"operational must DENY QUIC 0x%02X/0x%02X", op.Opcode, op.SubOp)
	}

	bridge := QRDBQUICAllowlistScoped(ScopePreOperational)
	require.NotEmpty(t, bridge)
	for method := range bridge {
		assert.Truef(t, pre.BypassesQRDBQUIC(method), "pre-operational must admit qrdb-quic %q", method)
		assert.Falsef(t, post.BypassesQRDBQUIC(method), "operational must DENY qrdb-quic %q", method)
	}

	tools := MCPAllowlistScoped(ScopePreOperational)
	require.NotEmpty(t, tools)
	for tool := range tools {
		assert.Truef(t, pre.BypassesMCPTool(tool), "pre-operational must admit MCP tool %q", tool)
		assert.Falsef(t, post.BypassesMCPTool(tool), "operational must DENY MCP tool %q", tool)
	}
}

// TestAuthBypass_StrictExclusionsDeniedInEveryState is the regression guard for
// the strict ceremony model: a withheld verb is denied on every transport in
// BOTH ceremony states. The scope axis must never become a back door into the
// exclusion list.
func TestAuthBypass_StrictExclusionsDeniedInEveryState(t *testing.T) {
	require.NotEmpty(t, StrictCeremonyExclusions)

	for _, st := range []state{statePreOperational, stateOperational} {
		t.Run(st.String(), func(t *testing.T) {
			b, _, _, sink := newBypassFixture(true, st)

			for v, reason := range StrictCeremonyExclusions {
				names := Table[v]
				for _, entry := range names.REST {
					method, probe := restProbe(entry)
					assert.Falsef(t, b.BypassesRESTPath(method, probe),
						"REST %s %q must never bypass (%s)", method, probe, reason)
				}
				for _, tail := range names.GRPC {
					for _, service := range []string{
						"/qrdb.v1.QRDBService/",
						"/quicbaas.ceremony.v1.Ceremony/",
						"/quicbaas.barrier.v1.Barrier/",
					} {
						full := service + tail
						assert.Falsef(t, b.BypassesGRPC(full),
							"gRPC %q must never bypass (%s)", full, reason)
					}
				}
				for _, op := range names.QUIC {
					assert.Falsef(t, b.BypassesNativeQUIC(op.Opcode, []byte{op.SubOp}),
						"QUIC 0x%02X/0x%02X must never bypass (%s)", op.Opcode, op.SubOp, reason)
				}
				for _, method := range names.QRDBQUIC {
					assert.Falsef(t, b.BypassesQRDBQUIC(method),
						"qrdb-quic %q must never bypass (%s)", method, reason)
				}
				for _, tool := range names.MCP {
					assert.Falsef(t, b.BypassesMCPTool(tool),
						"MCP tool %q must never bypass (%s)", tool, reason)
				}
			}
			assert.Empty(t, sink.events, "an excluded verb must never be audited as a bypass")
		})
	}
}

// TestAuthBypass_ScopesDisjoint pins that no wire name belongs to BOTH scopes on
// any transport. The predicate resolves an overlap in favour of ScopeAlways
// (denying would brick the always-verb sharing the name), so an overlap would
// silently defeat the pre-operational gate for that name. Keeping the sets
// disjoint means the state decision is unambiguous for every entry.
func TestAuthBypass_ScopesDisjoint(t *testing.T) {
	restAlways := make(map[string]struct{})
	for _, p := range RESTAllowlistScoped(ScopeAlways) {
		restAlways[p] = struct{}{}
	}
	for _, p := range RESTAllowlistScoped(ScopePreOperational) {
		_, dup := restAlways[p]
		assert.Falsef(t, dup, "REST path %q is in both scopes", p)
	}

	grpcAlways := make(map[string]struct{})
	for _, m := range GRPCFullMethodAllowlistScoped(ScopeAlways) {
		grpcAlways[m] = struct{}{}
	}
	for _, m := range GRPCFullMethodAllowlistScoped(ScopePreOperational) {
		_, dup := grpcAlways[m]
		assert.Falsef(t, dup, "gRPC method %q is in both scopes", m)
	}

	nativeAlways := QUICAllowlistScoped(ScopeAlways)
	for op := range QUICAllowlistScoped(ScopePreOperational) {
		_, dup := nativeAlways[op]
		assert.Falsef(t, dup, "QUIC op 0x%02X/0x%02X is in both scopes", op.Opcode, op.SubOp)
	}

	bridgeAlways := QRDBQUICAllowlistScoped(ScopeAlways)
	for m := range QRDBQUICAllowlistScoped(ScopePreOperational) {
		_, dup := bridgeAlways[m]
		assert.Falsef(t, dup, "qrdb-quic method %q is in both scopes", m)
	}

	mcpAlways := MCPAllowlistScoped(ScopeAlways)
	for tool := range MCPAllowlistScoped(ScopePreOperational) {
		_, dup := mcpAlways[tool]
		assert.Falsef(t, dup, "MCP tool %q is in both scopes", tool)
	}
}

// TestAuthBypass_OperationalProbeFailsClosed pins the fail-closed contract: an
// absent probe and a probe that cannot determine the ceremony state both yield
// OPERATIONAL semantics — the unseal surface keeps working, the ceremony claim
// surface does not. An unavailable probe must never widen the unauthenticated
// surface.
func TestAuthBypass_OperationalProbeFailsClosed(t *testing.T) {
	cases := []struct {
		name        string
		operational OperationalProbe
	}{
		{"nil probe", nil},
		{"probe cannot determine state", unavailableOperational{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewAuthBypass(&togglableProbe{sealedState: true}, tc.operational, &recordingSink{})

			// ScopeAlways is unaffected — recovery must stay possible.
			assert.True(t, b.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
			assert.True(t, b.BypassesRESTPath(http.MethodPost, "/v1/barrier/unseal"))
			assert.True(t, b.BypassesNativeQUIC(opBarrier, []byte{subBarrierUnseal}))
			assert.True(t, b.BypassesQRDBQUIC("barrier.Unseal"))
			assert.True(t, b.BypassesMCPTool("qrdb_barrier_unseal"))

			// ScopePreOperational is withheld.
			assert.False(t, b.BypassesGRPC("/qrdb.v1.QRDBService/CeremonyClaimCertBegin"))
			assert.False(t, b.BypassesRESTPath(http.MethodPost, "/api/v1/init/claim-cert/begin"))
			assert.False(t, b.BypassesNativeQUIC(opCeremony, []byte{subCeremonyClaimCertBegin}))
			assert.False(t, b.BypassesQRDBQUIC("ceremony.ClaimCertBegin"))
			assert.False(t, b.BypassesMCPTool("qrdb_ceremony_claim_cert_begin"))
		})
	}
}

// TestAuthBypass_ResealCycle drives ONE predicate instance through
// unsealed -> sealed -> unsealed and asserts the bypass window opens and
// closes with the probe: the bypass is strictly sealed-only.
func TestAuthBypass_ResealCycle(t *testing.T) {
	b, probe, _, sink := newBypassFixture(false, statePreOperational)

	// Unsealed: full enforcement, no bypass, no audit.
	assert.False(t, b.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.False(t, b.BypassesRESTPath(http.MethodPost, "/v1/barrier/unseal"))
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

// TestAuthBypass_CeremonyCompletionNarrowsLiveInstance drives ONE predicate
// instance across the ceremony completion boundary: the pre-operational
// exemption disappears the moment the ceremony reaches operational, while the
// unseal surface is untouched. This is the live-daemon shape of the fix — the
// gate is installed once at boot and must react to the state change.
func TestAuthBypass_CeremonyCompletionNarrowsLiveInstance(t *testing.T) {
	b, _, operational, sink := newBypassFixture(true, statePreOperational)

	assert.True(t, b.BypassesRESTPath(http.MethodPost, "/api/v1/init/claim-cert/begin"))
	assert.True(t, b.BypassesRESTPath(http.MethodPost, "/v1/barrier/unseal"))
	assert.Len(t, sink.events, 2)

	operational.operationalState = true

	assert.False(t, b.BypassesRESTPath(http.MethodPost, "/api/v1/init/claim-cert/begin"),
		"ceremony claim must stop bypassing once operational")
	assert.True(t, b.BypassesRESTPath(http.MethodPost, "/v1/barrier/unseal"),
		"unseal must keep bypassing after the ceremony completes")
	assert.Len(t, sink.events, 3, "only the unseal admit is audited after completion")
}

// TestAuthBypass_NilSafety pins that a nil predicate, a nil sealed probe, and a
// nil sink are all safe and fail closed (never bypass, never panic).
func TestAuthBypass_NilSafety(t *testing.T) {
	var nilBypass *AuthBypass
	assert.False(t, nilBypass.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.False(t, nilBypass.BypassesRESTPath(http.MethodPost, "/v1/barrier/unseal"))
	assert.False(t, nilBypass.BypassesNativeQUIC(opBarrier, []byte{subBarrierUnseal}))
	assert.False(t, nilBypass.BypassesQRDBQUIC("barrier.Unseal"))
	assert.False(t, nilBypass.BypassesMCPTool("qrdb_barrier_unseal"))

	nilProbe := NewAuthBypass(nil, &togglableOperational{}, &recordingSink{})
	assert.False(t, nilProbe.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))

	// A nil sink must not panic on a bypass, in either scope.
	nilSink := NewAuthBypass(&togglableProbe{sealedState: true}, &togglableOperational{}, nil)
	assert.True(t, nilSink.BypassesGRPC("/qrdb.v1.QRDBService/BarrierUnseal"))
	assert.True(t, nilSink.BypassesRESTPath(http.MethodPost, "/api/v1/init/status"))
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

// restProbe turns an allowlist entry into a request that exercises it: the
// method it names (POST when it names none) and its path, with a
// representative sub-path for a prefix entry.
func restProbe(entry string) (method, path string) {
	method, path = http.MethodPost, entry
	if m, p, qualified := strings.Cut(entry, " "); qualified {
		method, path = m, p
	}
	if strings.HasSuffix(path, "/") {
		path += "example"
	}
	return method, path
}
