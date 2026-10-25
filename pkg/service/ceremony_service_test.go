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
	initReq     *transport.CeremonyInitializeRequest
	initSecret  []byte
	initResp    *transport.CeremonyInitializeResponse
	initErr     error
	statusResp  *transport.CeremonyStatusResponse
	statusErr   error
	signCSRResp *transport.SignCSRInitResponse
	signCSRErr  error
}

func (m *mockCeremonyClient) CeremonyInitialize(_ context.Context, req *transport.CeremonyInitializeRequest) (*transport.CeremonyInitializeResponse, error) {
	m.initReq = req
	// The service opens the Secret only for the call and wipes it after,
	// so the passphrase is copied while it is still live.
	m.initSecret = append([]byte(nil), req.Secret...)
	return m.initResp, m.initErr
}

func (m *mockCeremonyClient) CeremonyStatus(_ context.Context) (*transport.CeremonyStatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockCeremonyClient) CeremonySignCSR(_ context.Context, _ *transport.SignCSRInitRequest) (*transport.SignCSRInitResponse, error) {
	return m.signCSRResp, m.signCSRErr
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

	_, err = svc.CeremonySignCSR(ctx, "officer", "pin", "csr", "role")
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
		Hostname:      "host.example",
		SANs:          []string{"alt.example"},
		AdminGenerate: true,
	}

	info, err := svc.CeremonyInitialize(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "pin-base64", info.SPKIPin)
	assert.Equal(t, []byte("CA"), info.CACertPEM)
	assert.Equal(t, []byte("TLS"), info.TLSCertPEM)
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

// TestCeremonyService_CeremonyInitialize_DeliversIssuedCredentials pins
// Codex 6800 P1: qrdb returns a generated admin key exactly once, so the
// service must hand it, the admin certificate and every holder certificate to
// the caller.
func TestCeremonyService_CeremonyInitialize_DeliversIssuedCredentials(t *testing.T) {
	holders := []transport.CeremonyHolderCert{
		{Username: "alice", Role: "so", CertPEM: "alice-cert"},
		{Username: "carol", Role: "custodian", CertPEM: "carol-cert"},
	}
	mc := &mockCeremonyClient{
		initResp: &transport.CeremonyInitializeResponse{
			State:                 "initialized",
			Holders:               holders,
			AdminCertPEM:          "admin-cert",
			AdminKeyPEM:           []byte("admin-key"),
			AdminCredentialSource: "generated",
		},
	}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{AdminGenerate: true})
	require.NoError(t, err)
	assert.Equal(t, holders, info.Holders)
	assert.Equal(t, []byte("admin-cert"), info.AdminCertPEM)
	assert.Equal(t, []byte("admin-key"), info.AdminKeyPEM)
	assert.Equal(t, "generated", info.AdminCredentialSource)
}

// TestCeremonyService_CeremonyInitialize_ForwardsEveryOfficerField pins that
// the officer roster and policy reach qrdb unchanged: a custodian must not
// silently become a Security Officer.
func TestCeremonyService_CeremonyInitialize_ForwardsEveryOfficerField(t *testing.T) {
	mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{}}
	svc := service.NewCeremonyService(mc)
	officer := service.CeremonyOfficer{
		Username:      "carol",
		Role:          "custodian",
		CSRPEM:        "csr-c",
		EncryptionKEM: "dhkem-x25519",
		EncryptionKey: []byte{1, 2, 3},
		Factors:       []transport.HolderFactorEnrolment{{Name: "totp", Params: map[string]any{"digits": float64(6)}}},
		Designated:    "totp",
	}

	_, err := svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{
		Threshold:     2,
		Policy:        "any-enrolled",
		Officers:      []service.CeremonyOfficer{officer},
		AdminGenerate: true,
	})
	require.NoError(t, err)
	require.NotNil(t, mc.initReq)
	assert.Equal(t, "any-enrolled", mc.initReq.Policy)
	assert.Equal(t, []transport.CeremonyOfficerRequest{officer}, mc.initReq.Officers)
}

func TestCeremonyService_CeremonyInitialize_NilRequestRefused(t *testing.T) {
	mc := &mockCeremonyClient{
		initResp: &transport.CeremonyInitializeResponse{State: "started"},
	}
	svc := service.NewCeremonyService(mc)

	// A nil request is defaulted rather than panicking, and the default
	// carries no admin credential selector, so it is refused before the
	// client is reached.
	info, err := svc.CeremonyInitialize(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, info)
	var missing *service.CeremonyAdminSelectorMissingError
	assert.ErrorAs(t, err, &missing)
	assert.Nil(t, mc.initReq)
}

func TestCeremonyService_CeremonyInitialize_Error(t *testing.T) {
	mc := &mockCeremonyClient{initErr: errCeremonyBackend}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{AdminGenerate: true})
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errCeremonyBackend)
}

func TestCeremonyService_CeremonyInitialize_AdminSelectorReachesClient(t *testing.T) {
	tests := []struct {
		name  string
		req   service.CeremonyInitRequest
		check func(t *testing.T, got *transport.CeremonyInitializeRequest)
	}{
		{
			name: "admin CSR",
			req:  service.CeremonyInitRequest{AdminCSRPEM: "admin-csr"},
			check: func(t *testing.T, got *transport.CeremonyInitializeRequest) {
				assert.Equal(t, "admin-csr", got.AdminCSRPEM)
				assert.Empty(t, got.AdminCertPEM)
				assert.False(t, got.AdminGenerate)
			},
		},
		{
			name: "admin certificate with pool",
			req:  service.CeremonyInitRequest{AdminCertPEM: "admin-cert", AdminCertPoolPEM: "admin-pool"},
			check: func(t *testing.T, got *transport.CeremonyInitializeRequest) {
				assert.Equal(t, "admin-cert", got.AdminCertPEM)
				assert.Equal(t, "admin-pool", got.AdminCertPoolPEM)
				assert.Empty(t, got.AdminCSRPEM)
				assert.False(t, got.AdminGenerate)
			},
		},
		{
			name: "admin generate",
			req:  service.CeremonyInitRequest{AdminGenerate: true},
			check: func(t *testing.T, got *transport.CeremonyInitializeRequest) {
				assert.True(t, got.AdminGenerate)
				assert.Empty(t, got.AdminCSRPEM)
				assert.Empty(t, got.AdminCertPEM)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{State: "complete"}}
			svc := service.NewCeremonyService(mc)
			req := tc.req
			secret, err := service.NewCeremonySecret(context.Background(), []byte("barrier-secret"))
			require.NoError(t, err)
			defer secret.Destroy()
			req.Secret = secret

			_, err = svc.CeremonyInitialize(context.Background(), &req)
			require.NoError(t, err)
			require.NotNil(t, mc.initReq)
			tc.check(t, mc.initReq)
			assert.Equal(t, []byte("barrier-secret"), mc.initSecret)
			assert.Nil(t, mc.initReq.Secret, "the wire request must not keep the passphrase after the call")
		})
	}
}

func TestCeremonyService_CeremonyInitialize_NoAdminSelector(t *testing.T) {
	mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{}}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{SOPin: "so-pin"})
	require.Error(t, err)
	assert.Nil(t, info)
	var missing *service.CeremonyAdminSelectorMissingError
	assert.ErrorAs(t, err, &missing)
	assert.Nil(t, mc.initReq, "the client must not be reached")
}

func TestCeremonyService_CeremonyInitialize_TwoAdminSelectors(t *testing.T) {
	mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{}}
	svc := service.NewCeremonyService(mc)

	info, err := svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{
		AdminCSRPEM:   "admin-csr",
		AdminGenerate: true,
	})
	require.Error(t, err)
	assert.Nil(t, info)
	var conflict *service.CeremonyAdminSelectorConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 2, conflict.Count)
	assert.Nil(t, mc.initReq, "the client must not be reached")
}

// TestNewCeremonySecret_SealsAndWipesSource proves the passphrase is moved
// into a *secure.Secret, the service hands exactly those bytes to qrdb, and
// the caller's plaintext copy is zeroed.
func TestNewCeremonySecret_SealsAndWipesSource(t *testing.T) {
	src := []byte("barrier-secret")
	secret, err := service.NewCeremonySecret(context.Background(), src)
	require.NoError(t, err)
	require.NotNil(t, secret)
	defer secret.Destroy()
	assert.Equal(t, make([]byte, len("barrier-secret")), src, "source bytes must be wiped")

	mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{}}
	svc := service.NewCeremonyService(mc)
	_, err = svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{
		AdminGenerate: true,
		Secret:        secret,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("barrier-secret"), mc.initSecret)
}

// TestNewCeremonySecret_EmptyMeansNoSecret proves an absent passphrase maps
// to a nil Secret and the wire request carries no secret (strategy "none").
func TestNewCeremonySecret_EmptyMeansNoSecret(t *testing.T) {
	secret, err := service.NewCeremonySecret(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, secret)

	mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{}}
	svc := service.NewCeremonyService(mc)
	_, err = svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{AdminGenerate: true})
	require.NoError(t, err)
	assert.Empty(t, mc.initSecret)
}

// TestCeremonyService_CeremonyInitialize_DestroyedSecretRefused proves a
// Secret released before the call is refused and never reaches qrdb.
func TestCeremonyService_CeremonyInitialize_DestroyedSecretRefused(t *testing.T) {
	secret, err := service.NewCeremonySecret(context.Background(), []byte("barrier-secret"))
	require.NoError(t, err)
	secret.Destroy()

	mc := &mockCeremonyClient{initResp: &transport.CeremonyInitializeResponse{}}
	svc := service.NewCeremonyService(mc)
	_, err = svc.CeremonyInitialize(context.Background(), &service.CeremonyInitRequest{
		AdminGenerate: true,
		Secret:        secret,
	})
	require.Error(t, err)
	assert.Nil(t, mc.initReq, "qrdb must not be called with a destroyed secret")
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

// =============================================================================
// CeremonyCompleteClaimCert
// =============================================================================

// =============================================================================
// CeremonyBeginClaimShare
// =============================================================================

// =============================================================================
// CeremonyCompleteClaimShare
// =============================================================================

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
