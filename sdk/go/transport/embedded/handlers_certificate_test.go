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

package embedded

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// errCert is a sentinel error returned by the fake certificate service to
// drive the error-propagation paths through the embedded handlers.
var errCert = errors.New("certificate: boom")

// fakeCertService is a configurable CertificateService. When failNext is
// true every method returns errCert; otherwise it returns a populated
// response so the convert helpers exercise their non-nil branches.
type fakeCertService struct {
	failNext    bool
	initialized bool

	CrossSignFunc           func(ctx context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error)
	ChainFunc               func(ctx context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error)
	IssueDeviceIdentityFunc func(ctx context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error)
}

func (f *fakeCertService) maybeErr() error {
	if f.failNext {
		return errCert
	}
	return nil
}

func (f *fakeCertService) IssueCertificate(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.IssueCertificateResponse{}, nil
}

func (f *fakeCertService) SignCSR(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.SignCSRResponse{}, nil
}

func (f *fakeCertService) RevokeCertificate(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.RevokeCertificateResponse{}, nil
}

func (f *fakeCertService) VerifyCertificate(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.VerifyCertificateResponse{}, nil
}

func (f *fakeCertService) GetCACertificate(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GetCACertificateResponse{}, nil
}

func (f *fakeCertService) GetTLSConfig(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GetTLSConfigResponse{}, nil
}

func (f *fakeCertService) GetCABundle(ctx context.Context, req *service.GetCABundleRequest) (*service.GetCABundleResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GetCABundleResponse{}, nil
}

func (f *fakeCertService) GenerateCRL(ctx context.Context, req *service.GenerateCRLRequest) (*service.GenerateCRLResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GenerateCRLResponse{}, nil
}

func (f *fakeCertService) IsRevoked(ctx context.Context, req *service.IsRevokedRequest) (*service.IsRevokedResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.IsRevokedResponse{}, nil
}

func (f *fakeCertService) CAInfo(ctx context.Context, req *service.CAInfoRequest) (*service.CAInfoResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.CAInfoResponse{
		CommonName:   "Test CA",
		SerialNumber: "01",
		IsCA:         true,
	}, nil
}

func (f *fakeCertService) ListCertificates(ctx context.Context, req *service.ListCertificatesRequest) (*service.ListCertificatesResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.ListCertificatesResponse{
		Certificates: []service.CertificateSummary{
			{CommonName: "leaf.example.com", SerialNumber: "02", DNSNames: []string{"leaf.example.com"}},
		},
		TotalCount: 1,
	}, nil
}

func (f *fakeCertService) ListStoredCertificates(ctx context.Context) ([]*x509.Certificate, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (f *fakeCertService) IsInitialized() bool { return f.initialized }

func (f *fakeCertService) CAService() xkmstransport.Client { return nil }

// certMock embeds the base mock and overrides Certificate().
type certMock struct {
	*mockQuicBaaS
	cert service.CertificateService
}

func (m *certMock) Certificate() service.CertificateService { return m.cert }

func newCertTransport(t *testing.T, cert service.CertificateService) *Transport {
	t.Helper()
	tr, err := New(&certMock{mockQuicBaaS: &mockQuicBaaS{}, cert: cert})
	require.NoError(t, err)
	return tr
}

// certCase covers a certificate dispatch: the method, a service-typed
// request, the equivalent SDK-client-typed request, and a wrong type.
type certCase struct {
	method  string
	svcReq  interface{}
	sdkReq  interface{}
	wrongRq interface{}
}

func certCases() []certCase {
	return []certCase{
		{"IssueCertificate", &service.IssueCertificateRequest{CommonName: "a"}, &certclient.IssueCertificateRequest{CommonName: "a"}, &service.PutRequest{}},
		{"SignCSR", &service.SignCSRRequest{CSRPEM: "p"}, &certclient.SignCSRRequest{CSRPEM: "p"}, &service.PutRequest{}},
		{"RevokeCertificate", &service.RevokeCertificateRequest{SerialNumber: "1"}, &certclient.RevokeCertificateRequest{SerialNumber: "1"}, &service.PutRequest{}},
		{"VerifyCertificate", &service.VerifyCertificateRequest{CertificatePEM: "p"}, &certclient.VerifyCertificateRequest{CertificatePEM: "p"}, &service.PutRequest{}},
		{"GetCACertificate", &service.GetCACertificateRequest{}, &certclient.GetCACertificateRequest{}, &service.PutRequest{}},
		{"GetCABundle", &service.GetCABundleRequest{StoreType: "root"}, &certclient.GetCABundleRequest{StoreType: "root"}, &service.PutRequest{}},
		{"GetTLSConfig", &service.GetTLSConfigRequest{ServerName: "s"}, &certclient.GetTLSConfigRequest{ServerName: "s"}, &service.PutRequest{}},
		{"GenerateCRL", &service.GenerateCRLRequest{}, &certclient.GenerateCRLRequest{}, &service.PutRequest{}},
		{"IsRevoked", &service.IsRevokedRequest{SerialNumber: "1"}, &certclient.IsRevokedRequest{SerialNumber: "1"}, &service.PutRequest{}},
		{"CAInfo", &service.CAInfoRequest{}, &certclient.CAInfoRequest{}, &service.PutRequest{}},
		{"ListCertificates", &service.ListCertificatesRequest{Limit: 10}, &certclient.ListCertificatesRequest{Limit: 10}, &service.PutRequest{}},

		// Certificate extensions — Security().CertificateExt().
		{"CrossSign", &service.CrossSignRequest{ForeignRootPEM: []byte("r")}, &certclient.CrossSignRequest{ForeignRootPEM: []byte("r")}, &service.PutRequest{}},
		{"Chain", &service.CertificateChainRequest{CertificatePEM: []byte("c")}, &certclient.ChainRequest{CertificatePEM: []byte("c")}, &service.PutRequest{}},
		{"IssueDeviceIdentity",
			&service.IssueDeviceIdentityRequest{Type: "ek", DeviceID: "d", EKPub: []byte("k")},
			&certclient.DeviceIdentityRequest{Type: certclient.DeviceIdentityEK, DeviceID: "d", EKPub: []byte("k")},
			&service.PutRequest{}},
	}
}

func TestTransport_CertificateOperations_ServiceType(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{})
	ctx := context.Background()

	for _, tc := range certCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Certificate", tc.method, tc.svcReq)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func TestTransport_CertificateOperations_SDKClientType(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{})
	ctx := context.Background()

	for _, tc := range certCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Certificate", tc.method, tc.sdkReq)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func TestTransport_CertificateOperations_ServiceError(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{failNext: true})
	ctx := context.Background()

	for _, tc := range certCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Certificate", tc.method, tc.svcReq)
			require.ErrorIs(t, err, errCert)
			assert.Nil(t, resp)
		})
	}
}

func TestTransport_CertificateOperations_WrongType(t *testing.T) {
	tr := newCertTransport(t, &fakeCertService{})
	ctx := context.Background()

	for _, tc := range certCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Certificate", tc.method, tc.wrongRq)
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

func TestTransport_CertificateOperations_NotConfigured(t *testing.T) {
	// Base mock returns a nil Certificate() sub-service.
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	ctx := context.Background()

	methods := append(certCases(), certCase{method: "IsInitialized", svcReq: &certclient.IsInitializedResponse{}})
	for _, tc := range methods {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Certificate", tc.method, tc.svcReq)
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

func TestTransport_CertificateIsInitialized(t *testing.T) {
	t.Run("returns true", func(t *testing.T) {
		tr := newCertTransport(t, &fakeCertService{initialized: true})
		resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "IsInitialized", struct{}{})
		require.NoError(t, err)
		typed, ok := resp.(*certclient.IsInitializedResponse)
		require.True(t, ok)
		assert.True(t, typed.Initialized)
	})

	t.Run("returns false", func(t *testing.T) {
		tr := newCertTransport(t, &fakeCertService{initialized: false})
		resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "IsInitialized", struct{}{})
		require.NoError(t, err)
		typed, ok := resp.(*certclient.IsInitializedResponse)
		require.True(t, ok)
		assert.False(t, typed.Initialized)
	})
}

// TestConvertCertResponses_NilInput exercises the nil-guard branches of the
// response converters that the dispatch paths never hit (handlers only call
// them on non-nil service responses).
func TestConvertCertResponses_NilInput(t *testing.T) {
	assert.Nil(t, convertCAInfoResponse(nil))
	assert.Nil(t, convertListCertificatesResponse(nil))
}

func TestConvertCertResponses_Populated(t *testing.T) {
	t.Run("CAInfo", func(t *testing.T) {
		out := convertCAInfoResponse(&service.CAInfoResponse{
			CommonName:   "Test CA",
			SerialNumber: "01",
			IsCA:         true,
			KeyType:      "RSA",
			KeyBits:      2048,
		})
		require.NotNil(t, out)
		assert.Equal(t, "Test CA", out.CommonName)
		assert.True(t, out.IsCA)
		assert.Equal(t, 2048, out.KeyBits)
	})

	t.Run("ListCertificates", func(t *testing.T) {
		out := convertListCertificatesResponse(&service.ListCertificatesResponse{
			Certificates: []service.CertificateSummary{
				{CommonName: "a.example.com", DNSNames: []string{"a.example.com"}},
				{CommonName: "b.example.com"},
			},
			TotalCount: 2,
		})
		require.NotNil(t, out)
		require.Len(t, out.Certificates, 2)
		assert.Equal(t, 2, out.TotalCount)
		assert.Equal(t, "a.example.com", out.Certificates[0].CommonName)
	})
}

// CrossSign satisfies service.CertificateService, honouring failNext so the
// shared certCases table exercises the error path for the extension verbs too.
func (f *fakeCertService) CrossSign(ctx context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
	if f.CrossSignFunc != nil {
		return f.CrossSignFunc(ctx, req)
	}
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.CrossSignResponse{CrossSignedPEM: []byte("cross-signed")}, nil
}

// Chain satisfies service.CertificateService.
func (f *fakeCertService) Chain(ctx context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
	if f.ChainFunc != nil {
		return f.ChainFunc(ctx, req)
	}
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.CertificateChainResponse{ChainPEM: [][]byte{[]byte("issuer"), []byte("root")}}, nil
}

// IssueDeviceIdentity satisfies service.CertificateService.
func (f *fakeCertService) IssueDeviceIdentity(ctx context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
	if f.IssueDeviceIdentityFunc != nil {
		return f.IssueDeviceIdentityFunc(ctx, req)
	}
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.IssueDeviceIdentityResponse{
		CertificatePEM: []byte("device-cert"),
		ChainPEM:       []byte("chain"),
		SerialNumber:   "cafe",
	}, nil
}
