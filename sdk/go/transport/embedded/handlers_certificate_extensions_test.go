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

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// The SDK asserts on the leaf client/certificate response types. A handler
// that returned the pkg/service struct instead would compile, round-trip and
// then fail the caller's type assertion at runtime — the exact silent-failure
// class this surface previously had. These tests pin the concrete type.

func TestHandleCertCrossSign_ReturnsLeafClientType(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{})

	resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "CrossSign",
		&certclient.CrossSignRequest{ForeignRootPEM: []byte("foreign-root")})
	require.NoError(t, err)

	typed, ok := resp.(*certclient.CrossSignResponse)
	require.True(t, ok, "CrossSign must return *certificate.CrossSignResponse, got %T", resp)
	assert.Equal(t, []byte("cross-signed"), typed.CrossSignedPEM)
}

func TestHandleCertChain_ReturnsLeafClientType(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{})

	resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "Chain",
		&certclient.ChainRequest{CertificatePEM: []byte("leaf")})
	require.NoError(t, err)

	typed, ok := resp.(*certclient.ChainResponse)
	require.True(t, ok, "Chain must return *certificate.ChainResponse, got %T", resp)
	assert.Equal(t, [][]byte{[]byte("issuer"), []byte("root")}, typed.ChainPEM)
}

func TestHandleCertIssueDeviceIdentity_ReturnsLeafClientType(t *testing.T) {
	notAfter := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	svc := &fakeCertService{
		IssueDeviceIdentityFunc: func(_ context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
			assert.Equal(t, "idevid", req.Type)
			assert.Equal(t, "device-9", req.DeviceID)
			assert.Equal(t, []byte("tcg-csr"), req.CSRPEM)
			assert.Equal(t, 90, req.ValidityDays)
			assert.Equal(t, "tenant-x", req.TenantID)
			return &service.IssueDeviceIdentityResponse{
				CertificatePEM: []byte("idevid-cert"),
				SerialNumber:   "01ff",
				NotAfter:       notAfter,
			}, nil
		},
	}
	tr := newCertTransport(t, svc)

	resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "IssueDeviceIdentity",
		&certclient.DeviceIdentityRequest{
			Type:         certclient.DeviceIdentityIDevID,
			DeviceID:     "device-9",
			CSRPEM:       []byte("tcg-csr"),
			ValidityDays: 90,
			TenantID:     "tenant-x",
		})
	require.NoError(t, err)

	typed, ok := resp.(*certclient.DeviceIdentityResponse)
	require.True(t, ok, "IssueDeviceIdentity must return *certificate.DeviceIdentityResponse, got %T", resp)
	assert.Equal(t, []byte("idevid-cert"), typed.CertificatePEM)
	assert.Equal(t, "01ff", typed.SerialNumber)
	assert.Equal(t, notAfter, typed.NotAfter)
}

func TestCertExtHandlers_WrongRequestType(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{})
	ctx := context.Background()

	for _, method := range []string{"CrossSign", "Chain", "IssueDeviceIdentity"} {
		t.Run(method, func(t *testing.T) {
			_, err := tr.Request(ctx, "quicbaas.Certificate", method, &service.PutRequest{})
			require.Error(t, err)
		})
	}
}

func TestCertExtHandlers_ServiceNotConfigured(t *testing.T) {
	tr, err := New(&certMock{mockQuicBaaS: &mockQuicBaaS{}})
	require.NoError(t, err)
	ctx := context.Background()

	for _, method := range []string{"CrossSign", "Chain", "IssueDeviceIdentity"} {
		t.Run(method, func(t *testing.T) {
			_, reqErr := tr.Request(ctx, "quicbaas.Certificate", method, nil)
			require.Error(t, reqErr)
		})
	}
}

// TestDeviceIdentityConversionRoundTrip pins the shared conversion helpers the
// other transports reuse, including the nil-safety both directions need.
func TestDeviceIdentityConversionRoundTrip(t *testing.T) {
	assert.Nil(t, DeviceIdentityServiceRequest(nil))
	assert.Nil(t, DeviceIdentityClientResponse(nil))

	in := &certclient.DeviceIdentityRequest{
		Type:           certclient.DeviceIdentityAK,
		DeviceID:       "dev",
		AKPub:          []byte("ak"),
		QualifyingData: []byte("qd"),
		ValidityDays:   7,
		TenantID:       "t",
	}
	svcReq := DeviceIdentityServiceRequest(in)
	require.NotNil(t, svcReq)
	assert.Equal(t, "ak", svcReq.Type)
	assert.Equal(t, []byte("ak"), svcReq.AKPub)
	assert.Equal(t, []byte("qd"), svcReq.QualifyingData)
	assert.Equal(t, 7, svcReq.ValidityDays)
	assert.Equal(t, "t", svcReq.TenantID)

	out := DeviceIdentityClientResponse(&service.IssueDeviceIdentityResponse{
		CertificatePEM: []byte("c"),
		ChainPEM:       []byte("ch"),
		SerialNumber:   "s",
	})
	require.NotNil(t, out)
	assert.Equal(t, []byte("c"), out.CertificatePEM)
	assert.Equal(t, []byte("ch"), out.ChainPEM)
	assert.Equal(t, "s", out.SerialNumber)
}
