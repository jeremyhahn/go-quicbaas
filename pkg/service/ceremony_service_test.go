// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock transport.CeremonyClient for unit testing the CeremonyService facade.
// =============================================================================

type ceremonyBackendError struct{}

func (e *ceremonyBackendError) Error() string { return "ceremony backend failure" }

var errCeremonyBackend = &ceremonyBackendError{}

// mockCeremonyClient implements transport.CeremonyClient. Each method captures
// the request (so tests can assert mapping) and returns the configured
// response/error.
type mockCeremonyClient struct {
	initReq        *transport.CeremonyInitializeRequest
	initResp       *transport.CeremonyInitializeResponse
	initErr        error
	statusResp     *transport.CeremonyStatusResponse
	statusErr      error
	claimBeginResp *transport.ClaimCertBeginResponse
	claimBeginErr  error
	claimDoneResp  *transport.ClaimCertCompleteResponse
	claimDoneErr   error
	beginShareReq  *transport.BeginClaimShareRequest
	beginShareResp *transport.BeginClaimShareResponse
	beginShareErr  error
	completeReq    *transport.CompleteClaimShareRequest
	completeResp   *transport.CompleteClaimShareResponse
	completeErr    error
	signCSRResp    *transport.SignCSRInitResponse
	signCSRErr     error
	enrollReq      *transport.EnrollExternalOfficerRequest
	enrollResp     *transport.EnrollExternalOfficerResponse
	enrollErr      error
}

func (m *mockCeremonyClient) CeremonyInitialize(_ context.Context, req *transport.CeremonyInitializeRequest) (*transport.CeremonyInitializeResponse, error) {
	m.initReq = req
	return m.initResp, m.initErr
}

func (m *mockCeremonyClient) CeremonyStatus(_ context.Context) (*transport.CeremonyStatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockCeremonyClient) CeremonyClaimCertBegin(_ context.Context, _ *transport.ClaimCertBeginRequest) (*transport.ClaimCertBeginResponse, error) {
	return m.claimBeginResp, m.claimBeginErr
}

func (m *mockCeremonyClient) CeremonyClaimCertComplete(_ context.Context, _ *transport.ClaimCertCompleteRequest) (*transport.ClaimCertCompleteResponse, error) {
	return m.claimDoneResp, m.claimDoneErr
}

func (m *mockCeremonyClient) CeremonyBeginClaimShare(_ context.Context, req *transport.BeginClaimShareRequest) (*transport.BeginClaimShareResponse, error) {
	m.beginShareReq = req
	return m.beginShareResp, m.beginShareErr
}

func (m *mockCeremonyClient) CeremonyCompleteClaimShare(_ context.Context, req *transport.CompleteClaimShareRequest) (*transport.CompleteClaimShareResponse, error) {
	m.completeReq = req
	return m.completeResp, m.completeErr
}

func (m *mockCeremonyClient) CeremonySignCSR(_ context.Context, _ *transport.SignCSRInitRequest) (*transport.SignCSRInitResponse, error) {
	return m.signCSRResp, m.signCSRErr
}

func (m *mockCeremonyClient) CeremonyEnrollExternalOfficer(_ context.Context, req *transport.EnrollExternalOfficerRequest) (*transport.EnrollExternalOfficerResponse, error) {
	m.enrollReq = req
	return m.enrollResp, m.enrollErr
}

func (m *mockCeremonyClient) CeremonyClaimShareBegin(_ context.Context, _ *transport.CeremonyClaimShareBeginRequest) (*transport.CeremonyClaimShareBeginResponse, error) {
	return nil, nil
}

func (m *mockCeremonyClient) CeremonyClaimShareComplete(_ context.Context, _ *transport.CeremonyClaimShareCompleteRequest) (*transport.CeremonyCustodianInfo, error) {
	return nil, nil
}

func (m *mockCeremonyClient) CeremonySubmitUnsealShare(_ context.Context, _ *transport.CeremonySubmitUnsealShareRequest) (*transport.CeremonySubmitUnsealShareResponse, error) {
	return nil, nil
}

var _ transport.CeremonyClient = (*mockCeremonyClient)(nil)

// =============================================================================
// Missing-client guard
// =============================================================================

func TestCeremonyService_NilClient_ReturnsMissingError(t *testing.T) {
	svc := service.NewCeremonyService(nil)
	ctx := context.Background()

	var missing *service.CeremonyServiceMissingError
	assertMissing := func(err error) {
		t.Helper()
		require.Error(t, err)
		assert.ErrorAs(t, err, &missing)
		assert.Equal(t, "ceremony service not configured", err.Error())
	}

	_, err := svc.CeremonyInitialize(ctx, &service.CeremonyInitRequest{})
	assertMissing(err)

	_, err = svc.CeremonyStatus(ctx)
	assertMissing(err)

	_, err = svc.CeremonyBeginClaimCert(ctx, "officer")
	assertMissing(err)

	_, err = svc.CeremonyCompleteClaimCert(ctx, "officer", "nonce", "sig")
	assertMissing(err)

	_, err = svc.CeremonyBeginClaimShare(ctx, "officer")
	assertMissing(err)

	_, err = svc.CeremonyCompleteClaimShare(ctx, "officer", "nonce", []byte("sig"))
	assertMissing(err)

	_, err = svc.CeremonySignCSR(ctx, "officer", "pin", "csr", "role")
	assertMissing(err)

	_, err = svc.CeremonyEnrollExternalOfficer(ctx, &service.CeremonyEnrollExternalOfficerRequest{})
	assertMissing(err)
}

// =============================================================================
// CeremonyInitialize
// =============================================================================

func TestCeremonyService_CeremonyInitialize_Success(t *testing.T) {
	mc := &mockCeremonyClient{
		initResp: &transport.CeremonyInitializeResponse{
			SPKIPin:    "pin-base64",
			CACertPEM:  "CA",
			TLSCertPEM: "TLS",
			SOCertPEM:  "SOC",
			SOKeyPEM:   "SOK",
			State:      "complete",
			ShareCount: 3,
		},
	}
	svc := service.NewCeremonyService(mc)

	req := &service.CeremonyInitRequest{
		SOPin:     "so-pin",
		UserPin:   "user-pin",
		Threshold: 2,
		Officers: []service.CeremonyOfficer{
			{Username: "alice", CSRPEM: "csr-a"},
			{Username: "bob", CSRPEM: "csr-b"},
		},
		Hostname: "host.example",
		SANs:     []string{"alt.example"},
	}

	info, err := svc.CeremonyInitialize(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "pin-base64", info.SPKIPin)
	assert.Equal(t, []byte("CA"), info.CACertPEM)
	assert.Equal(t, []byte("TLS"), info.TLSCertPEM)
	assert.Equal(t, []byte("SOC"), info.SOCertPEM)
	assert.Equal(t, []byte("SOK"), info.SOKeyPEM)
	assert.Equal(t, "complete", info.State)
	assert.Equal(t, 3, info.ShareCount)

	// Verify the officer slice and scalar fields were mapped onto the
	// transport request.
	require.NotNil(t, mc.initReq)
	assert.Equal(t, "so-pin", mc.initReq.SOPin)
	assert.Equal(t, 2, mc.initReq.Threshold)
	require.Len(t, mc.initReq.Officers, 2)
	assert.Equal(t, "alice", mc.initReq.Officers[0].Username)
	assert.Equal(t, "csr-b", mc.initReq.Officers[1].CSRPEM)
	assert.Equal(t, []string{"alt.example"}, mc.initReq.SANs)
}

func TestCeremonyService_CeremonyInitialize_NilRequestDefaulted(t *testing.T) {
	mc := &mockCeremonyClient{
		initResp: &transport.CeremonyInitializeResponse{State: "started"},
	}
	svc := service.NewCeremonyService(mc)

	// A nil request must be replaced with a zero-value request rather than
	// panicking.
	info, err := svc.CeremonyInitialize(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "started", info.State)
	require.NotNil(t, mc.initReq)
	assert.Empty(t, mc.initReq.Officers)
}

func TestCeremonyService_CeremonyInitialize_Error(t *testing.T) {
	mc := &mockCeremonyClient{initErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{})
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonyStatus
// =============================================================================

func TestCeremonyService_CeremonyStatus_Success(t *testing.T) {
	mc := &mockCeremonyClient{
		statusResp: &transport.CeremonyStatusResponse{State: "in_progress", SPKIPin: "pin"},
	}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "in_progress", info.State)
	assert.Equal(t, "pin", info.SPKIPin)
}

func TestCeremonyService_CeremonyStatus_Error(t *testing.T) {
	mc := &mockCeremonyClient{statusErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyStatus(context.Background())
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonyBeginClaimCert
// =============================================================================

func TestCeremonyService_CeremonyBeginClaimCert_Success(t *testing.T) {
	expires := "2026-05-29T12:00:00Z"
	mc := &mockCeremonyClient{
		claimBeginResp: &transport.ClaimCertBeginResponse{
			NonceHex:  "deadbeef",
			ExpiresAt: expires,
		},
	}
	svc := service.NewCeremonyService(mc)

	ch, err := svc.CeremonyBeginClaimCert(context.Background(), "officer")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, "deadbeef", ch.NonceHex)

	want, parseErr := time.Parse(time.RFC3339, expires)
	require.NoError(t, parseErr)
	assert.True(t, ch.ExpiresAt.Equal(want))
}

func TestCeremonyService_CeremonyBeginClaimCert_InvalidExpiryIgnored(t *testing.T) {
	mc := &mockCeremonyClient{
		claimBeginResp: &transport.ClaimCertBeginResponse{
			NonceHex:  "deadbeef",
			ExpiresAt: "not-a-time",
		},
	}
	svc := service.NewCeremonyService(mc)

	ch, err := svc.CeremonyBeginClaimCert(context.Background(), "officer")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.True(t, ch.ExpiresAt.IsZero())
}

func TestCeremonyService_CeremonyBeginClaimCert_Error(t *testing.T) {
	mc := &mockCeremonyClient{claimBeginErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	ch, err := svc.CeremonyBeginClaimCert(context.Background(), "officer")
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonyCompleteClaimCert
// =============================================================================

func TestCeremonyService_CeremonyCompleteClaimCert_Success(t *testing.T) {
	mc := &mockCeremonyClient{
		claimDoneResp: &transport.ClaimCertCompleteResponse{
			CertPEM:   "LEAF",
			CACertPEM: "CA",
		},
	}
	svc := service.NewCeremonyService(mc)

	res, err := svc.CeremonyCompleteClaimCert(context.Background(), "officer", "nonce", "sig")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, []byte("LEAF"), res.CertPEM)
	assert.Equal(t, []byte("CA"), res.CACertPEM)
}

func TestCeremonyService_CeremonyCompleteClaimCert_Error(t *testing.T) {
	mc := &mockCeremonyClient{claimDoneErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	res, err := svc.CeremonyCompleteClaimCert(context.Background(), "officer", "nonce", "sig")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonyBeginClaimShare
// =============================================================================

func TestCeremonyService_CeremonyBeginClaimShare_Success(t *testing.T) {
	expires := "2026-08-06T12:00:00Z"
	mc := &mockCeremonyClient{
		beginShareResp: &transport.BeginClaimShareResponse{
			NonceHex:  "deadbeef",
			ExpiresAt: expires,
		},
	}
	svc := service.NewCeremonyService(mc)

	ch, err := svc.CeremonyBeginClaimShare(context.Background(), "officer")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, "deadbeef", ch.NonceHex)

	want, parseErr := time.Parse(time.RFC3339, expires)
	require.NoError(t, parseErr)
	assert.True(t, ch.ExpiresAt.Equal(want))

	require.NotNil(t, mc.beginShareReq)
	assert.Equal(t, "officer", mc.beginShareReq.Username)
}

func TestCeremonyService_CeremonyBeginClaimShare_InvalidExpiryIgnored(t *testing.T) {
	mc := &mockCeremonyClient{
		beginShareResp: &transport.BeginClaimShareResponse{
			NonceHex:  "deadbeef",
			ExpiresAt: "not-a-time",
		},
	}
	svc := service.NewCeremonyService(mc)

	ch, err := svc.CeremonyBeginClaimShare(context.Background(), "officer")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.True(t, ch.ExpiresAt.IsZero())
}

func TestCeremonyService_CeremonyBeginClaimShare_Error(t *testing.T) {
	mc := &mockCeremonyClient{beginShareErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	ch, err := svc.CeremonyBeginClaimShare(context.Background(), "officer")
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonyCompleteClaimShare
// =============================================================================

func TestCeremonyService_CeremonyCompleteClaimShare_Success(t *testing.T) {
	mc := &mockCeremonyClient{
		completeResp: &transport.CompleteClaimShareResponse{
			ShareIndex: 3,
			ShareJSON:  `{"share":"value"}`,
		},
	}
	svc := service.NewCeremonyService(mc)

	result, err := svc.CeremonyCompleteClaimShare(context.Background(), "officer", "deadbeef", []byte("sig"))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 3, result.ShareIndex)
	assert.Equal(t, `{"share":"value"}`, result.ShareJSON)

	// Verify all handshake fields were mapped onto the transport request.
	require.NotNil(t, mc.completeReq)
	assert.Equal(t, "officer", mc.completeReq.Username)
	assert.Equal(t, "deadbeef", mc.completeReq.NonceHex)
	assert.Equal(t, []byte("sig"), mc.completeReq.Signature)
}

func TestCeremonyService_CeremonyCompleteClaimShare_Error(t *testing.T) {
	mc := &mockCeremonyClient{completeErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	result, err := svc.CeremonyCompleteClaimShare(context.Background(), "officer", "deadbeef", []byte("sig"))
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonySignCSR
// =============================================================================

func TestCeremonyService_CeremonySignCSR_Success(t *testing.T) {
	mc := &mockCeremonyClient{
		signCSRResp: &transport.SignCSRInitResponse{CertPEM: "SIGNED"},
	}
	svc := service.NewCeremonyService(mc)

	res, err := svc.CeremonySignCSR(context.Background(), "officer", "pin", "csr", "role")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, []byte("SIGNED"), res.CertPEM)
}

func TestCeremonyService_CeremonySignCSR_Error(t *testing.T) {
	mc := &mockCeremonyClient{signCSRErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	res, err := svc.CeremonySignCSR(context.Background(), "officer", "pin", "csr", "role")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

// =============================================================================
// CeremonyEnrollExternalOfficer
// =============================================================================

func TestCeremonyService_CeremonyEnrollExternalOfficer_Success(t *testing.T) {
	mc := &mockCeremonyClient{
		enrollResp: &transport.EnrollExternalOfficerResponse{SealedShare: "sealed-b64"},
	}
	svc := service.NewCeremonyService(mc)

	req := &service.CeremonyEnrollExternalOfficerRequest{
		Username:   "alice",
		PubKeyPEM:  "PUB",
		ShareIndex: 2,
		AuthFactor: "piv",
		ShareBytes: []byte{0x01, 0x02},
	}

	resp, err := svc.CeremonyEnrollExternalOfficer(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "sealed-b64", resp.SealedShare)

	require.NotNil(t, mc.enrollReq)
	assert.Equal(t, "alice", mc.enrollReq.Username)
	assert.Equal(t, 2, mc.enrollReq.ShareIndex)
	assert.Equal(t, "piv", mc.enrollReq.AuthFactor)
	assert.Equal(t, []byte{0x01, 0x02}, mc.enrollReq.ShareBytes)
}

func TestCeremonyService_CeremonyEnrollExternalOfficer_NilRequestDefaulted(t *testing.T) {
	mc := &mockCeremonyClient{
		enrollResp: &transport.EnrollExternalOfficerResponse{},
	}
	svc := service.NewCeremonyService(mc)

	resp, err := svc.CeremonyEnrollExternalOfficer(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.SealedShare)
	require.NotNil(t, mc.enrollReq)
	assert.Empty(t, mc.enrollReq.Username)
}

func TestCeremonyService_CeremonyEnrollExternalOfficer_Error(t *testing.T) {
	mc := &mockCeremonyClient{enrollErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	resp, err := svc.CeremonyEnrollExternalOfficer(context.Background(), &service.CeremonyEnrollExternalOfficerRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, errCeremonyBackend)
}
