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
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// claimRecorder records whether a share-claim verb reached the service and
// the TLS state its context carried.
type claimRecorder struct {
	reached bool
	state   *tls.ConnectionState
}

func (c *claimRecorder) record(ctx context.Context) {
	c.reached = true
	c.state = qrdbsdk.TLSStateFromContext(ctx)
}

type claimBarrierService struct {
	fakeHolderBarrierService
	rec claimRecorder
}

func (f *claimBarrierService) ShareClaimBegin(ctx context.Context, _ *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.rec.record(ctx)
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "aa"}, nil
}

func (f *claimBarrierService) ShareClaimComplete(ctx context.Context, _ *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.rec.record(ctx)
	return &qrdbtransport.ShareClaimCompleteResponse{}, nil
}

func (f *claimBarrierService) RecoveryShareClaimBegin(ctx context.Context, _ *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.rec.record(ctx)
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "aa"}, nil
}

func (f *claimBarrierService) RecoveryShareClaimComplete(ctx context.Context, _ *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.rec.record(ctx)
	return &qrdbtransport.ShareClaimCompleteResponse{}, nil
}

type claimTenantService struct {
	fakeHolderTenantService
	rec claimRecorder
}

func (f *claimTenantService) TenantBarrierShareClaimBegin(ctx context.Context, _ *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.rec.record(ctx)
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "aa"}, nil
}

func (f *claimTenantService) TenantBarrierShareClaimComplete(ctx context.Context, _ *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.rec.record(ctx)
	return &qrdbtransport.ShareClaimCompleteResponse{}, nil
}

// claimToolCase runs one share-claim tool against a fresh recording service.
type claimToolCase struct {
	name string
	run  func(ctx context.Context) (*claimRecorder, error)
}

func claimToolCases() []claimToolCase {
	args := map[string]interface{}{"tenant_id": "t1", "username": "alice", "nonce_hex": "aa", "signature": "AQI="}
	var cases []claimToolCase
	for _, name := range []string{
		"qrdb_barrier_share_claim_begin",
		"qrdb_barrier_share_claim_complete",
		"qrdb_barrier_recovery_share_claim_begin",
		"qrdb_barrier_recovery_share_claim_complete",
	} {
		tool := barrierHolderTools[name]
		cases = append(cases, claimToolCase{name: name, run: func(ctx context.Context) (*claimRecorder, error) {
			svc := &claimBarrierService{}
			_, err := tool.exec(ctx, svc, args)
			return &svc.rec, err
		}})
	}
	for _, name := range []string{
		"qrdb_tenant_barrier_share_claim_begin",
		"qrdb_tenant_barrier_share_claim_complete",
	} {
		tool := tenantBarrierHolderTools[name]
		cases = append(cases, claimToolCase{name: name, run: func(ctx context.Context) (*claimRecorder, error) {
			svc := &claimTenantService{}
			_, err := tool.exec(ctx, svc, args)
			return &svc.rec, err
		}})
	}
	return cases
}

func verifiedClaimState() *tls.ConnectionState {
	cert := &x509.Certificate{Raw: []byte("holder-cert")}
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
}

// TestHolderClaimTools_RefuseWithoutVerifiedCert proves a share-claim tool
// never reaches the service when the MCP connection carries no verified
// client certificate: stdio (no TLS state), TLS without a client
// certificate, and a presented but unverified certificate.
func TestHolderClaimTools_RefuseWithoutVerifiedCert(t *testing.T) {
	unverifiedCert := &x509.Certificate{Raw: []byte("unverified")}
	contexts := map[string]context.Context{
		"no TLS state":        context.Background(),
		"no client cert":      qrdbsdk.WithTLSState(context.Background(), &tls.ConnectionState{}),
		"unverified peer":     qrdbsdk.WithTLSState(context.Background(), &tls.ConnectionState{PeerCertificates: []*x509.Certificate{unverifiedCert}}),
		"empty verified link": qrdbsdk.WithTLSState(context.Background(), &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{}}}),
	}
	for _, tc := range claimToolCases() {
		for label, ctx := range contexts {
			t.Run(tc.name+"/"+label, func(t *testing.T) {
				rec, err := tc.run(ctx)
				var refused *HolderClaimCertRequiredError
				require.True(t, stderrors.As(err, &refused), "got %v", err)
				assert.False(t, rec.reached, "service reached without a verified certificate")
				assert.Equal(t, mcptransport.MCPAuthenticationRequired, toolErrorCode(err))
			})
		}
	}
}

// TestHolderClaimTools_ForwardVerifiedTLSState proves a share-claim tool
// with a verified client certificate reaches the service with the caller's
// TLS state on the context, so go-qrdb binds the claim to that certificate.
func TestHolderClaimTools_ForwardVerifiedTLSState(t *testing.T) {
	state := verifiedClaimState()
	ctx := qrdbsdk.WithTLSState(context.Background(), state)
	for _, tc := range claimToolCases() {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := tc.run(ctx)
			require.NoError(t, err)
			require.True(t, rec.reached)
			assert.Same(t, state, rec.state)
		})
	}
}

func TestHolderClaimCertRequiredError_Message(t *testing.T) {
	err := &HolderClaimCertRequiredError{}
	assert.Contains(t, err.Error(), "verified mTLS client certificate")
}
