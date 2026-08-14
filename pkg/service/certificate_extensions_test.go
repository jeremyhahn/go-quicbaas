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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// Test doubles
// =============================================================================

// extCAStub is a CA service that satisfies both xkmstransport.Client (via the
// nil embedded interface, the pattern the rest of this package uses) and
// service.CertificateExtensionsCA.
type extCAStub struct {
	xkmstransport.Client // intentionally nil: only the capability is invoked

	crossSignPEM []byte
	crossSignErr error
	crossSignArg []byte

	chainPEM [][]byte
	chainErr error
	chainArg []byte

	issued    *IssuedDeviceIdentity
	issueErr  error
	issuedReq *DeviceIdentityIssuance
}

func (s *extCAStub) CrossSignRoot(_ context.Context, foreignRootPEM []byte) ([]byte, error) {
	s.crossSignArg = foreignRootPEM
	return s.crossSignPEM, s.crossSignErr
}

func (s *extCAStub) IssuerChainPEM(_ context.Context, certPEM []byte) ([][]byte, error) {
	s.chainArg = certPEM
	return s.chainPEM, s.chainErr
}

func (s *extCAStub) IssueDeviceIdentityCert(_ context.Context, req *DeviceIdentityIssuance) (*IssuedDeviceIdentity, error) {
	s.issuedReq = req
	return s.issued, s.issueErr
}

// plainCAStub satisfies xkmstransport.Client but NOT
// CertificateExtensionsCA — the interface-drift case the service must fail
// closed on.
type plainCAStub struct {
	xkmstransport.Client
}

func newExtService(t *testing.T, ca xkmstransport.Client) CertificateService {
	t.Helper()
	svc := NewCertificateService(ca)
	require.NotNil(t, svc)
	return svc
}

// =============================================================================
// CrossSign
// =============================================================================

func TestCertificateService_CrossSign(t *testing.T) {
	stub := &extCAStub{crossSignPEM: []byte("-----BEGIN CERTIFICATE-----\ncross\n-----END CERTIFICATE-----\n")}
	svc := newExtService(t, stub)

	resp, err := svc.CrossSign(context.Background(), &CrossSignRequest{
		ForeignRootPEM: []byte("foreign-root"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, stub.crossSignPEM, resp.CrossSignedPEM)
	assert.Equal(t, []byte("foreign-root"), stub.crossSignArg,
		"the service must forward the caller's foreign root unchanged")
}

func TestCertificateService_CrossSign_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.CrossSign(ctx, nil)
		assert.ErrorIs(t, err, ErrCertNilRequest)
	})

	t.Run("missing foreign root", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.CrossSign(ctx, &CrossSignRequest{})
		assert.ErrorIs(t, err, ErrCertExtMissingForeignRoot)
	})

	t.Run("CA does not implement the capability", func(t *testing.T) {
		svc := newExtService(t, &plainCAStub{})
		_, err := svc.CrossSign(ctx, &CrossSignRequest{ForeignRootPEM: []byte("x")})
		assert.ErrorIs(t, err, ErrCertExtNotSupported)
	})

	t.Run("CA failure is wrapped, not swallowed", func(t *testing.T) {
		cause := stderrors.New("signer unavailable")
		svc := newExtService(t, &extCAStub{crossSignErr: cause})
		_, err := svc.CrossSign(ctx, &CrossSignRequest{ForeignRootPEM: []byte("x")})
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
	})
}

// =============================================================================
// Chain
// =============================================================================

func TestCertificateService_Chain(t *testing.T) {
	stub := &extCAStub{chainPEM: [][]byte{[]byte("intermediate"), []byte("root")}}
	svc := newExtService(t, stub)

	resp, err := svc.Chain(context.Background(), &CertificateChainRequest{
		CertificatePEM: []byte("leaf"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, [][]byte{[]byte("intermediate"), []byte("root")}, resp.ChainPEM,
		"the chain must be returned issuer-first, root-last, unreordered")
	assert.Equal(t, []byte("leaf"), stub.chainArg)
}

func TestCertificateService_Chain_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.Chain(ctx, nil)
		assert.ErrorIs(t, err, ErrCertNilRequest)
	})

	t.Run("missing certificate", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.Chain(ctx, &CertificateChainRequest{})
		assert.ErrorIs(t, err, ErrCertExtMissingCertificate)
	})

	t.Run("CA does not implement the capability", func(t *testing.T) {
		svc := newExtService(t, &plainCAStub{})
		_, err := svc.Chain(ctx, &CertificateChainRequest{CertificatePEM: []byte("x")})
		assert.ErrorIs(t, err, ErrCertExtNotSupported)
	})

	t.Run("CA failure is wrapped", func(t *testing.T) {
		cause := stderrors.New("AIA fetch failed")
		svc := newExtService(t, &extCAStub{chainErr: cause})
		_, err := svc.Chain(ctx, &CertificateChainRequest{CertificatePEM: []byte("x")})
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
	})
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func TestCertificateService_IssueDeviceIdentity(t *testing.T) {
	notBefore := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	notAfter := notBefore.Add(24 * time.Hour)

	cases := []struct {
		name string
		req  *IssueDeviceIdentityRequest
	}{
		{
			name: "ek",
			req: &IssueDeviceIdentityRequest{
				Type: DeviceIdentityTypeEK, DeviceID: "dev-1", EKPub: []byte("ek"),
			},
		},
		{
			name: "ak",
			req: &IssueDeviceIdentityRequest{
				Type: DeviceIdentityTypeAK, DeviceID: "dev-2", AKPub: []byte("ak"),
			},
		},
		{
			name: "idevid",
			req: &IssueDeviceIdentityRequest{
				Type: DeviceIdentityTypeIDevID, DeviceID: "dev-3", CSRPEM: []byte("tcg-csr"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &extCAStub{issued: &IssuedDeviceIdentity{
				CertificatePEM: []byte("cert"),
				ChainPEM:       []byte("chain"),
				SerialNumber:   "beef",
				NotBefore:      notBefore,
				NotAfter:       notAfter,
			}}
			svc := newExtService(t, stub)

			resp, err := svc.IssueDeviceIdentity(context.Background(), tc.req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, []byte("cert"), resp.CertificatePEM)
			assert.Equal(t, []byte("chain"), resp.ChainPEM)
			assert.Equal(t, "beef", resp.SerialNumber)
			assert.Equal(t, notBefore, resp.NotBefore)
			assert.Equal(t, notAfter, resp.NotAfter)

			require.NotNil(t, stub.issuedReq)
			assert.Equal(t, tc.req.Type, stub.issuedReq.Type)
			assert.Equal(t, tc.req.DeviceID, stub.issuedReq.DeviceID)
			assert.Equal(t, tc.req.CSRPEM, stub.issuedReq.CSR)
			assert.Equal(t, tc.req.EKPub, stub.issuedReq.EKPub)
			assert.Equal(t, tc.req.AKPub, stub.issuedReq.AKPub)
		})
	}
}

func TestCertificateService_IssueDeviceIdentity_PropagatesRequestTenant(t *testing.T) {
	stub := &extCAStub{issued: &IssuedDeviceIdentity{CertificatePEM: []byte("cert")}}
	svc := newExtService(t, stub)

	_, err := svc.IssueDeviceIdentity(context.Background(), &IssueDeviceIdentityRequest{
		Type:     DeviceIdentityTypeEK,
		DeviceID: "dev-tenant",
		EKPub:    []byte("ek"),
		TenantID: "tenant-a",
	})
	require.NoError(t, err)
	require.NotNil(t, stub.issuedReq)
	assert.Equal(t, "tenant-a", stub.issuedReq.TenantID,
		"an explicit request tenant must override the context tenant")
}

func TestCertificateService_IssueDeviceIdentity_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, nil)
		assert.ErrorIs(t, err, ErrCertNilRequest)
	})

	t.Run("missing device ID", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{Type: DeviceIdentityTypeEK})
		assert.ErrorIs(t, err, ErrCertExtMissingDeviceID)
	})

	t.Run("unknown identity type", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: "ldevid", DeviceID: "dev",
		})
		assert.ErrorIs(t, err, ErrCertExtUnknownIdentityType)
	})

	t.Run("empty identity type", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{DeviceID: "dev"})
		assert.ErrorIs(t, err, ErrCertExtUnknownIdentityType)
	})

	t.Run("EK without public key", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: DeviceIdentityTypeEK, DeviceID: "dev",
		})
		assert.ErrorIs(t, err, ErrCertExtMissingEKPub)
	})

	t.Run("AK without public key", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: DeviceIdentityTypeAK, DeviceID: "dev",
		})
		assert.ErrorIs(t, err, ErrCertExtMissingAKPub)
	})

	t.Run("IDevID without TCG CSR", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: DeviceIdentityTypeIDevID, DeviceID: "dev",
		})
		assert.ErrorIs(t, err, ErrCertExtMissingTCGCSR)
	})

	t.Run("CA does not implement the capability", func(t *testing.T) {
		svc := newExtService(t, &plainCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: DeviceIdentityTypeEK, DeviceID: "dev", EKPub: []byte("ek"),
		})
		assert.ErrorIs(t, err, ErrCertExtNotSupported)
	})

	t.Run("CA failure is wrapped", func(t *testing.T) {
		cause := stderrors.New("TCG CSR verification failed")
		svc := newExtService(t, &extCAStub{issueErr: cause})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: DeviceIdentityTypeIDevID, DeviceID: "dev", CSRPEM: []byte("csr"),
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
	})

	t.Run("CA returns no certificate", func(t *testing.T) {
		svc := newExtService(t, &extCAStub{})
		_, err := svc.IssueDeviceIdentity(ctx, &IssueDeviceIdentityRequest{
			Type: DeviceIdentityTypeEK, DeviceID: "dev", EKPub: []byte("ek"),
		})
		require.Error(t, err, "a nil issuance result must not be reported as success")
	})
}

// TestDeviceIdentityValidatorsCoverEveryType keeps the flavor vocabulary and
// its validation table in lockstep: adding a constant without a validator
// would silently make that flavor unreachable.
func TestDeviceIdentityValidatorsCoverEveryType(t *testing.T) {
	for _, typ := range []string{DeviceIdentityTypeEK, DeviceIdentityTypeAK, DeviceIdentityTypeIDevID} {
		_, ok := deviceIdentityValidators[typ]
		assert.True(t, ok, "device identity type %q has no validator", typ)
	}
	assert.Len(t, deviceIdentityValidators, 3)
}
