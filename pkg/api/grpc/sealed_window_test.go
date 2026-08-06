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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// grpcTogglableProbe flips sealed state so one composed chain instance can be
// driven through the full re-seal cycle, exactly as the daemon-lifetime
// interceptors are.
type grpcTogglableProbe struct{ sealedState bool }

func (p *grpcTogglableProbe) IsSealed() bool { return p.sealedState }

// grpcRecordingSink captures the sealed-window bypass audit events.
type grpcRecordingSink struct{ events [][2]string }

func (s *grpcRecordingSink) SealedWindowAuthBypass(transport, operation string) {
	s.events = append(s.events, [2]string{transport, operation})
}

// countingAuthorizer wraps fakeValidatorAuthorizer and counts Authorize calls
// so tests can assert the sealed-window bypass never consults the validator.
type countingAuthorizer struct {
	fakeValidatorAuthorizer
	calls int
}

func (c *countingAuthorizer) Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	c.calls++
	return c.fakeValidatorAuthorizer.Authorize(ctx, token, requiredPerm, dpopProof, htm, htu)
}

// sealedAuthzChain composes the PRODUCTION interceptor order for tests: the
// qrdb sealed-window interceptor (via the SDK re-export, exactly what
// lifecycle installs) runs FIRST, the quicbaas authorization interceptor with
// the shared sealed-window bypass runs SECOND. Both consume the same probe
// and the same canonical-table derivation, mirroring startGRPCServer.
type sealedAuthzChain struct {
	sealed grpc.UnaryServerInterceptor
	authz  grpc.UnaryServerInterceptor
}

func newSealedAuthzChain(probe *grpcTogglableProbe, authorizer GRPCValidatorAuthorizer, sink *grpcRecordingSink) *sealedAuthzChain {
	bypass := sealedwindow.NewAuthBypass(probe, sink)
	return &sealedAuthzChain{
		sealed: qrdbsdk.SealedWindowUnaryInterceptor(probe, sealedwindow.GRPCAllowlist()),
		authz:  UnaryAuthorizationInterceptor(authorizer, WithSealedWindowAuthBypass(bypass)),
	}
}

// invoke runs one request through sealed -> authz -> handler and reports
// whether the final handler was reached.
func (c *sealedAuthzChain) invoke(ctx context.Context, fullMethod string) (bool, error) {
	info := &grpc.UnaryServerInfo{FullMethod: fullMethod}
	handlerReached := false
	final := func(context.Context, interface{}) (interface{}, error) {
		handlerReached = true
		return "ok", nil
	}
	_, err := c.sealed(ctx, nil, info, func(innerCtx context.Context, req interface{}) (interface{}, error) {
		return c.authz(innerCtx, req, info, final)
	})
	return handlerReached, err
}

// TestGRPCSealedWindowAuthBypass_Composition is the per-transport contract
// table for gRPC (and Unix, which shares the chain):
//
//	sealed+allowlisted            -> handler, no auth, audited
//	sealed+non-allowlisted        -> Unavailable "barrier sealed"
//	unsealed+allowlisted no token -> Unauthenticated (bypass is sealed-only)
//	unsealed+valid scope          -> handler (normal enforcement)
func TestGRPCSealedWindowAuthBypass_Composition(t *testing.T) {
	const unsealMethod = "/qrdb.v1.QRDBService/BarrierUnseal"
	const kvMethod = "/quicbaas.v1.QuicBaaS/Get"

	cases := []struct {
		name        string
		sealed      bool
		method      string
		ctx         context.Context
		wantHandler bool
		wantCode    codes.Code
		wantAudit   int
		wantAuthz   int
	}{
		{
			name:        "sealed allowlisted unauthenticated admitted",
			sealed:      true,
			method:      unsealMethod,
			ctx:         context.Background(),
			wantHandler: true,
			wantCode:    codes.OK,
			wantAudit:   1,
			wantAuthz:   0,
		},
		{
			name:        "sealed allowlisted status admitted",
			sealed:      true,
			method:      "/qrdb.v1.QRDBService/BarrierStatus",
			ctx:         context.Background(),
			wantHandler: true,
			wantCode:    codes.OK,
			wantAudit:   1,
			wantAuthz:   0,
		},
		{
			name:        "sealed non-allowlisted sealed error",
			sealed:      true,
			method:      kvMethod,
			ctx:         context.Background(),
			wantHandler: false,
			wantCode:    codes.Unavailable,
			wantAudit:   0,
			wantAuthz:   0,
		},
		{
			name:        "sealed barrier SEAL verb sealed error (deliberate exclusion)",
			sealed:      true,
			method:      "/qrdb.v1.QRDBService/BarrierSeal",
			ctx:         context.Background(),
			wantHandler: false,
			wantCode:    codes.Unavailable,
			wantAudit:   0,
			wantAuthz:   0,
		},
		{
			name:        "unsealed allowlisted verb without token Unauthenticated",
			sealed:      false,
			method:      unsealMethod,
			ctx:         context.Background(),
			wantHandler: false,
			wantCode:    codes.Unauthenticated,
			wantAudit:   0,
			wantAuthz:   0,
		},
		{
			name:        "unsealed valid scope admitted",
			sealed:      false,
			method:      unsealMethod,
			ctx:         ctxWithBearer("good"),
			wantHandler: true,
			wantCode:    codes.OK,
			wantAudit:   0,
			wantAuthz:   1,
		},
		{
			name:        "unsealed missing scope PermissionDenied",
			sealed:      false,
			method:      kvMethod,
			ctx:         ctxWithBearer("good"),
			wantHandler: false,
			wantCode:    codes.PermissionDenied,
			wantAudit:   0,
			wantAuthz:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &grpcTogglableProbe{sealedState: tc.sealed}
			sink := &grpcRecordingSink{}
			authorizer := &countingAuthorizer{
				fakeValidatorAuthorizer: fakeValidatorAuthorizer{
					token:       "good",
					grantedPerm: "seal:unseal",
					authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "system"},
				},
			}
			chain := newSealedAuthzChain(probe, authorizer, sink)

			handlerReached, err := chain.invoke(tc.ctx, tc.method)
			assert.Equal(t, tc.wantHandler, handlerReached, "handler reached")
			if tc.wantCode == codes.OK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tc.wantCode, status.Code(err))
			}
			assert.Len(t, sink.events, tc.wantAudit, "audit emissions")
			if tc.wantAudit > 0 {
				assert.Equal(t, "grpc", sink.events[0][0])
				assert.Equal(t, tc.method, sink.events[0][1])
			}
			assert.Equal(t, tc.wantAuthz, authorizer.calls, "validator consultations")
		})
	}
}

// TestGRPCSealedWindowAuthBypass_ResealCycle drives ONE composed chain
// (daemon-lifetime interceptor instances) through unsealed -> re-sealed ->
// unsealed and asserts the bypass window opens and closes with the probe.
func TestGRPCSealedWindowAuthBypass_ResealCycle(t *testing.T) {
	const unsealMethod = "/qrdb.v1.QRDBService/BarrierUnseal"

	probe := &grpcTogglableProbe{sealedState: false}
	sink := &grpcRecordingSink{}
	authorizer := &countingAuthorizer{
		fakeValidatorAuthorizer: fakeValidatorAuthorizer{
			token:       "good",
			grantedPerm: "seal:unseal",
			authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1"},
		},
	}
	chain := newSealedAuthzChain(probe, authorizer, sink)

	// Unsealed: no token -> Unauthenticated; valid token -> admitted.
	handlerReached, err := chain.invoke(context.Background(), unsealMethod)
	assert.False(t, handlerReached)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	handlerReached, err = chain.invoke(ctxWithBearer("good"), unsealMethod)
	require.NoError(t, err)
	assert.True(t, handlerReached)

	// Runtime RE-SEAL: the allowlist becomes the whole control surface —
	// the unseal verb is admitted with no token, everything else is sealed.
	probe.sealedState = true
	handlerReached, err = chain.invoke(context.Background(), unsealMethod)
	require.NoError(t, err)
	assert.True(t, handlerReached, "sealed+allowlisted must reach the handler unauthenticated")
	require.Len(t, sink.events, 1)
	assert.Equal(t, unsealMethod, sink.events[0][1])

	handlerReached, err = chain.invoke(ctxWithBearer("good"), "/quicbaas.v1.QuicBaaS/Get")
	assert.False(t, handlerReached)
	assert.Equal(t, codes.Unavailable, status.Code(err), "sealed+non-allowlisted stays sealed even with a token")

	// Unseal completes: full enforcement returns immediately.
	probe.sealedState = false
	handlerReached, err = chain.invoke(context.Background(), unsealMethod)
	assert.False(t, handlerReached)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "bypass must close when unsealed")
	assert.Len(t, sink.events, 1, "no further audit after the window closes")
}

// TestStreamAuthorizationInterceptor_SealedWindowBypass covers the streaming
// sibling: sealed+allowlisted admits without a token; unsealed demands one.
func TestStreamAuthorizationInterceptor_SealedWindowBypass(t *testing.T) {
	const unsealMethod = "/qrdb.v1.QRDBService/BarrierUnseal"

	probe := &grpcTogglableProbe{sealedState: true}
	sink := &grpcRecordingSink{}
	bypass := sealedwindow.NewAuthBypass(probe, sink)
	authorizer := &countingAuthorizer{}
	interceptor := StreamAuthorizationInterceptor(authorizer, WithSealedWindowAuthBypass(bypass))

	served := false
	err := interceptor(nil, &dummyStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: unsealMethod},
		func(interface{}, grpc.ServerStream) error { served = true; return nil })
	require.NoError(t, err)
	assert.True(t, served, "sealed+allowlisted stream must be admitted unauthenticated")
	assert.Equal(t, 0, authorizer.calls)
	require.Len(t, sink.events, 1)

	// Unsealed: the same interceptor demands authentication again.
	probe.sealedState = false
	served = false
	err = interceptor(nil, &dummyStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: unsealMethod},
		func(interface{}, grpc.ServerStream) error { served = true; return nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, served)
}

// TestUnaryAuthorizationInterceptor_NoBypassOption pins that constructing the
// interceptor WITHOUT the sealed-window option preserves the fully
// fail-closed posture byte-for-byte (nil predicate never bypasses).
func TestUnaryAuthorizationInterceptor_NoBypassOption(t *testing.T) {
	authorizer := &countingAuthorizer{}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	_, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/qrdb.v1.QRDBService/BarrierUnseal"},
		func(context.Context, interface{}) (interface{}, error) { return "ok", nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}
