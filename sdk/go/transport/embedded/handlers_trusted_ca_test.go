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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// trustedCAQuicBaaS answers TrustedCA with the given CA; nothing else is
// reached by the TrustedCA handlers.
type trustedCAQuicBaaS struct {
	service.QuicBaaS
	ca service.TrustedCAService
}

func (q *trustedCAQuicBaaS) TrustedCA() service.TrustedCAService { return q.ca }

// fakeTrustedCA records each go-xkms request it receives.
type fakeTrustedCA struct{ got interface{} }

func (f *fakeTrustedCA) GetCABundle(_ context.Context, r *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	f.got = r
	return &xkmstransport.GetCABundleResponse{}, nil
}

func (f *fakeTrustedCA) GetCACertificate(_ context.Context, r *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	f.got = r
	return &xkmstransport.GetCACertificateResponse{}, nil
}

func (f *fakeTrustedCA) SignCSR(_ context.Context, r *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	f.got = r
	return &xkmstransport.SignCSRResponse{SerialNumber: "0a"}, nil
}

func (f *fakeTrustedCA) IssueCertificate(_ context.Context, r *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	f.got = r
	return &xkmstransport.IssueCertificateResponse{}, nil
}

func (f *fakeTrustedCA) RevokeCertificate(_ context.Context, r *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	f.got = r
	return &xkmstransport.RevokeCertificateResponse{}, nil
}

func (f *fakeTrustedCA) GenerateCRL(_ context.Context, r *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	f.got = r
	return &xkmstransport.GenerateCRLResponse{}, nil
}

func (f *fakeTrustedCA) IsRevoked(_ context.Context, r *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	f.got = r
	return &xkmstransport.IsRevokedResponse{Revoked: true}, nil
}

// TestTrustedCAHandlers pins #1330 on the embedded transport: every
// quicbaas.TrustedCA method is registered and hands go-xkms's own request to
// the CA the server wired.
func TestTrustedCAHandlers(t *testing.T) {
	ca := &fakeTrustedCA{}
	svc := &trustedCAQuicBaaS{ca: ca}
	ctx := context.Background()

	reqs := map[string]interface{}{
		"GetCABundle":       &xkmstransport.GetCABundleRequest{TenantID: "t1"},
		"GetCACertificate":  &xkmstransport.GetCACertificateRequest{Identity: "root"},
		"SignCSR":           &xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM")},
		"IssueCertificate":  &xkmstransport.IssueCertificateRequest{CommonName: "node-1"},
		"RevokeCertificate": &xkmstransport.RevokeCertificateRequest{SerialNumber: "0a"},
		"GenerateCRL":       &xkmstransport.GenerateCRLRequest{},
		"IsRevoked":         &xkmstransport.IsRevokedRequest{SerialNumber: "0a"},
	}
	for method, req := range reqs {
		t.Run(method, func(t *testing.T) {
			h, ok := unaryHandlers["quicbaas.TrustedCA/"+method]
			require.True(t, ok, "quicbaas.TrustedCA/%s is not registered", method)
			ca.got = nil
			resp, err := h(ctx, svc, req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Same(t, req, ca.got)

			// A foreign request type is refused before the CA is reached.
			ca.got = nil
			_, err = h(ctx, svc, &struct{}{})
			require.Error(t, err)
			assert.Nil(t, ca.got)

			// No CA wired: the service is not configured.
			_, err = h(ctx, &trustedCAQuicBaaS{}, req)
			require.Error(t, err)
		})
	}
}
