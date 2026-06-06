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

package service

import (
	"context"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// nilBundleCAService returns a successful IssueCertificate response but a nil
// GetCABundle response (no error). This exercises the resp == nil branch of
// fetchCABundle, which the standard stub does not reach.
type nilBundleCAService struct {
	issueResp *xkmstransport.IssueCertificateResponse
}

func (s *nilBundleCAService) IssueCertificate(_ context.Context, _ *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	return s.issueResp, nil
}

func (s *nilBundleCAService) GetCABundle(_ context.Context, _ *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	return nil, nil
}

func (s *nilBundleCAService) GetCACertificate(_ context.Context, _ *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	panic("unexpected GetCACertificate call")
}

func (s *nilBundleCAService) SignCSR(_ context.Context, _ *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	panic("unexpected SignCSR call")
}

func (s *nilBundleCAService) RevokeCertificate(_ context.Context, _ *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	panic("unexpected RevokeCertificate call")
}

func (s *nilBundleCAService) GenerateCRL(_ context.Context, _ *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	panic("unexpected GenerateCRL call")
}

func (s *nilBundleCAService) IsRevoked(_ context.Context, _ *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	panic("unexpected IsRevoked call")
}

// TestSDKAgentCertIssuer_NilBundleResponse drives fetchCABundle through its
// nil-response branch. With GetCABundle yielding a nil response, the issuer
// must fall back to the ChainPEM carried in the issue response.
func TestSDKAgentCertIssuer_NilBundleResponse(t *testing.T) {
	certPEM, keyPEM, bundlePEM := newTestCertPEMs(t)
	stub := &nilBundleCAService{
		issueResp: &xkmstransport.IssueCertificateResponse{
			CertificatePEM: certPEM,
			PrivateKeyPEM:  keyPEM,
			ChainPEM:       bundlePEM,
		},
	}
	issuer, err := NewSDKAgentCertIssuer(stub)
	if err != nil {
		t.Fatalf("NewSDKAgentCertIssuer: %v", err)
	}
	result, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
		TenantID: "t", Hostname: "h",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.PrivateKey.Destroy()
	if len(result.CAPEM) == 0 {
		t.Fatal("expected CAPEM from ChainPEM fallback")
	}
}
