//go:build !integration

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

package service

import (
	"context"
	stderrors "errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
)

// TestTrustService_NilCADefensiveBranches exercises the defensive s.ca == nil
// guards on CrossSignedRootPool and AutoImportAIA. NewTrustService rejects a nil
// CA, so these branches are only reachable by constructing the struct directly,
// which the same-package test does to prove the guards return the typed error
// rather than panicking on a nil dereference.
func TestTrustService_NilCADefensiveBranches(t *testing.T) {
	svc := &trustService{ca: nil}

	t.Run("CrossSignedRootPool", func(t *testing.T) {
		_, err := svc.CrossSignedRootPool(context.Background(), &CrossSignedRootPoolRequest{})
		require.ErrorIs(t, err, ErrTrustCAMissing)
	})

	t.Run("AutoImportAIA", func(t *testing.T) {
		_, err := svc.AutoImportAIA(context.Background(), &AutoImportAIARequest{
			ChainDER: [][]byte{selfSignedDER(t)},
		})
		require.ErrorIs(t, err, ErrTrustCAMissing)
	})
}

// TestTrustService_AutoImportAIA_CanceledContext covers the ctx.Err() branch in
// AutoImportAIA, which sits after the request-shape validation so a populated,
// valid request is required to reach it.
func TestTrustService_AutoImportAIA_CanceledContext(t *testing.T) {
	svc := NewTrustService(&fakeTrustCA{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.AutoImportAIA(ctx, &AutoImportAIARequest{
		ChainDER: [][]byte{selfSignedDER(t)},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestACMEService_RestServicer proves RestServicer returns the composed handler
// set held on the struct.
func TestACMEService_RestServicer(t *testing.T) {
	svc := newTestACMEService(nil)
	// No real RestService is wired by newTestACMEService, so the accessor must
	// surface the zero value rather than moduleate one.
	assert.Nil(t, svc.RestServicer())
}

// TestACMEService_CanceledContext covers the ctx.Err() guard on every ACME
// control verb. Each verb checks context cancellation only after its
// request-shape validation, so each sub-test supplies an otherwise-valid
// request to reach the guard.
func TestACMEService_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	leaf := selfSignedLeafPEM(t, big.NewInt(0x7b))

	t.Run("RegisterChallenge", func(t *testing.T) {
		svc := newTestACMEService(nil)
		_, err := svc.RegisterChallenge(ctx, &ACMERegisterChallengeRequest{Name: xkmsacme.ACMEChallengeTypeHTTP01})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("Issue", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{certPEM: leaf}, nil)
		_, err := svc.Issue(ctx, &ACMEIssueRequest{CSRPEM: []byte("CSR"), ChallengeType: "http-01"})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("NewOrder", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{}, nil)
		_, err := svc.NewOrder(ctx, &ACMENewOrderRequest{
			Identifiers: []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("FinalizeOrder", func(t *testing.T) {
		svc := newTestACMEServiceFull(&fakeACMECA{certPEM: leaf}, nil)
		_, err := svc.FinalizeOrder(ctx, &ACMEFinalizeOrderRequest{CSRPEM: []byte("CSR")})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("Renew", func(t *testing.T) {
		svc := newTestACMEServiceFull(nil, &fakeACMECertResolver{certPEM: leaf})
		_, err := svc.Renew(ctx, &ACMERenewRequest{CertURL: "7b"})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// TestACMEService_Renew_ResolveErrorWrapped covers the non-sentinel resolve
// error branch: when CertificateBySerial fails with an error that is NOT
// ErrACMECertNotFound, Renew must wrap it as an internal QBError rather than
// surface the not-found sentinel.
func TestACMEService_Renew_ResolveErrorWrapped(t *testing.T) {
	boom := stderrors.New("backend unavailable")
	svc := newTestACMEServiceFull(nil, &fakeACMECertResolver{resolveErr: boom})
	_, err := svc.Renew(context.Background(), &ACMERenewRequest{CertURL: "ff"})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, ErrACMECertNotFound)
}

// TestSerialFromCertPEM_DecodableButUnparseable covers the branch where pem
// decoding succeeds but x509 parsing fails, yielding an empty serial.
func TestSerialFromCertPEM_DecodableButUnparseable(t *testing.T) {
	// A valid PEM block whose body is not a valid certificate.
	bad := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")
	assert.Equal(t, "", serialFromCertPEM(bad))
}
