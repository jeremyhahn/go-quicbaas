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
)

// fakeTCGCA is the wired CA adapter with go-xkms's TCGCAService as well.
type fakeTCGCA struct{ fakeTrustedCA }

func (f *fakeTCGCA) IssueEKCertificate(_ context.Context, r *xkmstransport.IssueEKCertificateRequest) (*xkmstransport.IssueEKCertificateResponse, error) {
	f.got = r
	return &xkmstransport.IssueEKCertificateResponse{SerialNumber: "ek"}, nil
}

func (f *fakeTCGCA) IssueAKCertificate(_ context.Context, r *xkmstransport.IssueAKCertificateRequest) (*xkmstransport.IssueAKCertificateResponse, error) {
	f.got = r
	return &xkmstransport.IssueAKCertificateResponse{SerialNumber: "ak"}, nil
}

func (f *fakeTCGCA) SignTCGCSR(_ context.Context, r *xkmstransport.SignTCGCSRRequest) (*xkmstransport.SignTCGCSRResponse, error) {
	f.got = r
	return &xkmstransport.SignTCGCSRResponse{IAKCertDER: []byte{1}}, nil
}

func (f *fakeTCGCA) EnrollDevice(_ context.Context, r *xkmstransport.EnrollDeviceRequest) (*xkmstransport.EnrollDeviceResponse, error) {
	f.got = r
	return &xkmstransport.EnrollDeviceResponse{IAKCertDER: []byte{1}}, nil
}

// TestTCGCAHandlers: every quicbaas.TCGCA verb (Security().TCGCA()) is
// registered on the embedded transport and reaches the wired CA's
// TCGCAService with go-xkms's own request.
func TestTCGCAHandlers(t *testing.T) {
	ca := &fakeTCGCA{}
	svc := &trustedCAQuicBaaS{ca: ca}
	ctx := context.Background()

	reqs := map[string]interface{}{
		"IssueEKCertificate": &xkmstransport.IssueEKCertificateRequest{CommonName: "ek", EKPublicKey: []byte{1}},
		"IssueAKCertificate": &xkmstransport.IssueAKCertificateRequest{CommonName: "ak", PublicKey: []byte{2}},
		"SignTCGCSR":         &xkmstransport.SignTCGCSRRequest{CommonName: "dev", TCGCSR: []byte{3}},
		"EnrollDevice":       &xkmstransport.EnrollDeviceRequest{CommonName: "dev", PackedCSR: []byte{4}},
	}
	for method, req := range reqs {
		t.Run(method, func(t *testing.T) {
			h, ok := unaryHandlers["quicbaas.TCGCA/"+method]
			require.True(t, ok, "quicbaas.TCGCA/%s is not registered", method)
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

			// No CA wired, or a CA without TCG support: not configured.
			_, err = h(ctx, &trustedCAQuicBaaS{}, req)
			require.Error(t, err)
			_, err = h(ctx, &trustedCAQuicBaaS{ca: &fakeTrustedCA{}}, req)
			require.Error(t, err)
		})
	}
}
