//go:build quic

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

package quic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// quicTogglableProbe flips sealed state so one composed gate + handler pair
// can be driven through the full re-seal cycle.
type quicTogglableProbe struct{ sealedState bool }

func (p *quicTogglableProbe) IsSealed() bool { return p.sealedState }

// quicCeremonyStateProbe reports the ceremony state that decides whether the
// pre-operational sealed-window exemptions still apply. operational=false is a
// daemon whose init ceremony is unfinished; operational=true is the
// already-initialized daemon (restart / post-boot re-seal).
type quicCeremonyStateProbe struct{ operational bool }

func (p *quicCeremonyStateProbe) IsOperational() bool { return p.operational }

// quicRecordingSink captures the sealed-window bypass audit events.
type quicRecordingSink struct{ events [][2]string }

func (s *quicRecordingSink) SealedWindowAuthBypass(transport, operation string) {
	s.events = append(s.events, [2]string{transport, operation})
}

// countingQUICAuthorizer wraps fakeValidatorAuthorizer with a call counter so
// tests can assert the sealed-window bypass never consults the validator.
type countingQUICAuthorizer struct {
	fakeValidatorAuthorizer
	calls int
}

func (c *countingQUICAuthorizer) Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	c.calls++
	return c.fakeValidatorAuthorizer.Authorize(ctx, token, requiredPerm, dpopProof, htm, htu)
}

// quicSealedFixture composes the PRODUCTION native-opcode dispatch order for
// tests: the server sealed-window gate (sealedWindowDeniesOp) runs FIRST,
// the handler's validator enforcement with the shared sealed-window bypass
// runs SECOND — exactly the handleStream ordering.
type quicSealedFixture struct {
	server  *Server
	handler *Handler
}

// dispatchOutcome models the composed decision for one native request.
type dispatchOutcome struct {
	sealedDenied bool
	authzResp    *Response
}

func newQUICSealedFixture(
	probe *quicTogglableProbe,
	operational sealedwindow.OperationalProbe,
	authorizer ValidatorAuthorizer,
	sink *quicRecordingSink,
) *quicSealedFixture {
	server := &Server{}
	server.SetSealedWindowGate(probe, operational,
		sealedwindow.QUICAllowlist(), sealedwindow.QRDBQUICAllowlist())

	bypass := sealedwindow.NewAuthBypass(probe, operational, sink)
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(authorizer),
		WithHandlerSealedWindowBypass(bypass),
	)
	return &quicSealedFixture{server: server, handler: handler}
}

// dispatch mirrors handleStream: sealed gate, then handler authorization.
func (f *quicSealedFixture) dispatch(req *Request) dispatchOutcome {
	if f.server.sealedWindowDeniesOp(req.Opcode, req.Payload) {
		return dispatchOutcome{sealedDenied: true}
	}
	_, resp := f.handler.authorizeAndInject(context.Background(), req)
	return dispatchOutcome{authzResp: resp}
}

// TestQUICSealedWindowAuthBypass_Composition is the per-transport contract
// table for the native quicbaas-quic opcode path:
//
//	sealed+allowlisted            -> handler, no auth, audited
//	sealed+non-allowlisted        -> sealed error (server gate)
//	unsealed+allowlisted no token -> StatusUnauthorized (bypass is sealed-only)
//	unsealed+valid scope          -> admitted (normal enforcement)
func TestQUICSealedWindowAuthBypass_Composition(t *testing.T) {
	unsealReq := &Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierUnseal}}
	statusReq := &Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierStatus}}
	sealReq := &Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierSeal}}
	ceremonyStatusReq := &Request{Opcode: OpCeremony, Payload: []byte{SubOpCeremonyStatus}}
	claimCertReq := &Request{Opcode: OpCeremony, Payload: []byte{SubOpCeremonyClaimCertBegin}}
	kvReq := &Request{Opcode: OpGet}

	cases := []struct {
		name         string
		sealed       bool
		operational  bool
		req          *Request
		token        string
		wantSealed   bool
		wantAdmitted bool
		wantStatus   byte
		wantAudit    int
		wantAuthz    int
	}{
		{
			name:         "sealed allowlisted unseal admitted unauthenticated",
			sealed:       true,
			req:          unsealReq,
			wantAdmitted: true,
			wantAudit:    1,
		},
		{
			name:         "sealed allowlisted status admitted unauthenticated",
			sealed:       true,
			req:          statusReq,
			wantAdmitted: true,
			wantAudit:    1,
		},
		{
			name:         "sealed pre-operational ceremony status admitted unauthenticated",
			sealed:       true,
			req:          ceremonyStatusReq,
			wantAdmitted: true,
			wantAudit:    1,
		},
		// Scope axis, end to end: the ceremony status / claim sub-ops are
		// ScopePreOperational. On an already-initialized (operational) sealed
		// daemon the server gate still SERVES them — they stay on the deny-gate
		// allowlist — but the exemption has expired, so the handler demands a
		// token and the unauthenticated request is refused.
		{
			name:        "sealed operational ceremony status requires token",
			sealed:      true,
			operational: true,
			req:         ceremonyStatusReq,
			wantStatus:  StatusUnauthorized,
		},
		{
			name:        "sealed operational claim-cert begin requires token",
			sealed:      true,
			operational: true,
			req:         claimCertReq,
			wantStatus:  StatusUnauthorized,
		},
		{
			name:         "sealed operational unseal still admitted",
			sealed:       true,
			operational:  true,
			req:          unsealReq,
			wantAdmitted: true,
			wantAudit:    1,
		},
		{
			name:       "sealed barrier SEAL sub-op sealed error (deliberate exclusion)",
			sealed:     true,
			req:        sealReq,
			wantSealed: true,
		},
		{
			name:       "sealed KV opcode sealed error",
			sealed:     true,
			req:        kvReq,
			wantSealed: true,
		},
		{
			name:       "unsealed allowlisted verb without token Unauthorized",
			sealed:     false,
			req:        unsealReq,
			wantStatus: StatusUnauthorized,
		},
		{
			name:         "unsealed valid scope admitted",
			sealed:       false,
			req:          unsealReq,
			token:        "good",
			wantAdmitted: true,
			wantAuthz:    1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &quicTogglableProbe{sealedState: tc.sealed}
			sink := &quicRecordingSink{}
			authorizer := &countingQUICAuthorizer{
				fakeValidatorAuthorizer: fakeValidatorAuthorizer{
					authzCtx: &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "system"},
				},
			}
			fixture := newQUICSealedFixture(
				probe, &quicCeremonyStateProbe{operational: tc.operational}, authorizer, sink)

			req := &Request{Opcode: tc.req.Opcode, Payload: tc.req.Payload, Token: tc.token}
			out := fixture.dispatch(req)

			assert.Equal(t, tc.wantSealed, out.sealedDenied, "sealed gate denial")
			if tc.wantAdmitted {
				assert.Nil(t, out.authzResp, "request must be admitted to the handler")
			} else if !tc.wantSealed {
				require.NotNil(t, out.authzResp)
				assert.Equal(t, tc.wantStatus, out.authzResp.Status)
			}
			assert.Len(t, sink.events, tc.wantAudit, "audit emissions")
			if tc.wantAudit > 0 {
				assert.Equal(t, "quic", sink.events[0][0])
			}
			assert.Equal(t, tc.wantAuthz, authorizer.calls, "validator consultations")
		})
	}
}

// TestQUICSealedWindowAuthBypass_ResealCycle drives ONE composed fixture
// through unsealed -> re-sealed -> unsealed and asserts the bypass window
// opens and closes with the probe.
func TestQUICSealedWindowAuthBypass_ResealCycle(t *testing.T) {
	probe := &quicTogglableProbe{sealedState: false}
	sink := &quicRecordingSink{}
	authorizer := &countingQUICAuthorizer{
		fakeValidatorAuthorizer: fakeValidatorAuthorizer{
			authzCtx: &platformtoken.AuthorizedContext{Subject: "u1"},
		},
	}
	fixture := newQUICSealedFixture(probe, &quicCeremonyStateProbe{}, authorizer, sink)

	unseal := func(token string) dispatchOutcome {
		return fixture.dispatch(&Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierUnseal}, Token: token})
	}

	// Unsealed: no token -> Unauthorized; valid token -> admitted.
	out := unseal("")
	require.NotNil(t, out.authzResp)
	assert.Equal(t, StatusUnauthorized, out.authzResp.Status)
	out = unseal("good")
	assert.Nil(t, out.authzResp)

	// Runtime RE-SEAL: unseal admits with no token; KV stays sealed.
	probe.sealedState = true
	out = unseal("")
	assert.False(t, out.sealedDenied)
	assert.Nil(t, out.authzResp, "sealed+allowlisted must reach the handler unauthenticated")
	require.Len(t, sink.events, 1)
	out = fixture.dispatch(&Request{Opcode: OpGet, Token: "good"})
	assert.True(t, out.sealedDenied, "sealed+non-allowlisted stays sealed even with a token")

	// Unseal completes: enforcement returns immediately.
	probe.sealedState = false
	out = unseal("")
	require.NotNil(t, out.authzResp)
	assert.Equal(t, StatusUnauthorized, out.authzResp.Status, "bypass must close when unsealed")
	assert.Len(t, sink.events, 1, "no further audit after the window closes")
}

// TestSetSealedWindowGate_BuildsAuthBypass pins that installing the gate with
// a probe derives the shared bypass predicate, and that a nil probe leaves it
// uninstalled (nil never bypasses).
func TestSetSealedWindowGate_BuildsAuthBypass(t *testing.T) {
	s := &Server{}
	s.SetSealedWindowGate(&quicTogglableProbe{sealedState: true}, &quicCeremonyStateProbe{},
		sealedwindow.QUICAllowlist(), sealedwindow.QRDBQUICAllowlist())
	assert.NotNil(t, s.sealedBypass)
	assert.True(t, s.sealedBypass.BypassesNativeQUIC(OpBarrier, []byte{SubOpBarrierUnseal}))

	s.SetSealedWindowGate(nil, nil, nil, nil)
	assert.Nil(t, s.sealedBypass)
	assert.False(t, s.sealedBypass.BypassesNativeQUIC(OpBarrier, []byte{SubOpBarrierUnseal}))
}
