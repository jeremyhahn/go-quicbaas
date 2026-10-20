//go:build rest

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

package rest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

const testTrustDomain = "example.test"

// countingCA is a real CA that counts the issuing calls, so a test can prove
// a refused request never reached the CA.
type countingCA struct {
	xkmssdk.TrustedCA
	calls atomic.Int32
}

func (c *countingCA) SignCSR(ctx context.Context, csrPEM []byte, opts *xkmssdk.TrustedCASignOptions) (*x509.Certificate, error) {
	c.calls.Add(1)
	return c.TrustedCA.SignCSR(ctx, csrPEM, opts)
}

func (c *countingCA) IssueCertificate(ctx context.Context, req *xkmssdk.TrustedCACertificateRequest) (*xkmssdk.TrustedCAIssuedCertificate, error) {
	c.calls.Add(1)
	return c.TrustedCA.IssueCertificate(ctx, req)
}

func (c *countingCA) IssueCertificateWithProfile(ctx context.Context, req *xkmssdk.TrustedCACertificateRequest, profile string) (*xkmssdk.TrustedCAIssuedCertificate, error) {
	c.calls.Add(1)
	return c.TrustedCA.IssueCertificateWithProfile(ctx, req, profile)
}

func newTenantTestServicer(t *testing.T) (*xkmsCAServicer, *countingCA) {
	t.Helper()
	ca := &countingCA{TrustedCA: testca.New(t, "xkmsca-tenant-ca")}
	return newXKMSCAServicer(ca, testTrustDomain), ca
}

func newTestCSR(t *testing.T, commonName string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// spiffeTenant returns the tenant named by the certificate's SPIFFE URI SAN.
func spiffeTenant(t *testing.T, certPEM []byte) string {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("certificate PEM did not decode")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	id, err := qrdbsdk.TenantFromCert(cert, testTrustDomain)
	if err != nil {
		t.Fatalf("read SPIFFE tenant: %v", err)
	}
	return id.TenantID
}

// issueVerbs runs SignCSR or IssueCertificate with the given tenant_id and
// returns the issued certificate PEM.
var issueVerbs = map[string]func(t *testing.T, ctx context.Context, a *xkmsCAServicer, tenantID string) ([]byte, error){
	"SignCSR": func(t *testing.T, ctx context.Context, a *xkmsCAServicer, tenantID string) ([]byte, error) {
		resp, err := a.SignCSR(ctx, &xkmstransport.SignCSRRequest{
			CSRPEM:   newTestCSR(t, "alice"),
			Profile:  "client",
			TenantID: tenantID,
		})
		if err != nil {
			return nil, err
		}
		return resp.CertificatePEM, nil
	},
	"IssueCertificate": func(t *testing.T, ctx context.Context, a *xkmsCAServicer, tenantID string) ([]byte, error) {
		resp, err := a.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{
			CommonName:   "alice",
			Profile:      "client",
			ValidityDays: 1,
			TenantID:     tenantID,
		})
		if err != nil {
			return nil, err
		}
		return resp.CertificatePEM, nil
	},
}

func TestXKMSCAServicer_ForeignTenantRefused(t *testing.T) {
	for name, issue := range issueVerbs {
		t.Run(name, func(t *testing.T) {
			a, ca := newTenantTestServicer(t)
			ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

			_, err := issue(t, ctx, a, "tenant-b")

			var cross *tenantctx.CrossTenantError
			if !stderrors.As(err, &cross) {
				t.Fatalf("expected *tenantctx.CrossTenantError, got %T: %v", err, err)
			}
			if cross.CallerTenant != "tenant-a" || cross.TargetTenant != "tenant-b" {
				t.Fatalf("cross-tenant error = %+v", cross)
			}
			if !stderrors.Is(err, qrdbsdk.ErrTenantMissing) {
				t.Fatalf("expected the refusal to unwrap to qrdbsdk.ErrTenantMissing, got %v", err)
			}
			if got := ca.calls.Load(); got != 0 {
				t.Fatalf("CA called %d times, want 0", got)
			}
		})
	}
}

func TestXKMSCAServicer_OwnTenantStampedIntoSPIFFESAN(t *testing.T) {
	for name, issue := range issueVerbs {
		for _, requested := range []string{"tenant-a", ""} {
			t.Run(name+"/tenant_id="+requested, func(t *testing.T) {
				a, _ := newTenantTestServicer(t)
				ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

				certPEM, err := issue(t, ctx, a, requested)
				if err != nil {
					t.Fatalf("issue: %v", err)
				}
				if got := spiffeTenant(t, certPEM); got != "tenant-a" {
					t.Fatalf("SPIFFE tenant = %q, want tenant-a", got)
				}
			})
		}
	}
}

func TestXKMSCAServicer_NoTenantBoundRefused(t *testing.T) {
	for name, issue := range issueVerbs {
		for _, requested := range []string{"tenant-a", ""} {
			t.Run(name+"/tenant_id="+requested, func(t *testing.T) {
				a, ca := newTenantTestServicer(t)

				_, err := issue(t, context.Background(), a, requested)

				var missing *tenantctx.MissingTenantError
				if !stderrors.As(err, &missing) {
					t.Fatalf("expected *tenantctx.MissingTenantError, got %T: %v", err, err)
				}
				if got := ca.calls.Load(); got != 0 {
					t.Fatalf("CA called %d times, want 0", got)
				}
			})
		}
	}
}

func TestXKMSCAServicer_NoTrustDomainNeedsNoTenant(t *testing.T) {
	a := newXKMSCAServicer(testca.New(t, "xkmsca-no-spiffe-ca"), "")

	resp, err := a.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
		CommonName:   "leaf.example.test",
		ValidityDays: 1,
	})
	if err != nil {
		t.Fatalf("issue without a tenant or trust domain: %v", err)
	}
	if len(resp.CertificatePEM) == 0 {
		t.Fatal("empty certificate")
	}

	// A named tenant is still checked: nothing is stamped, but a foreign
	// tenant_id is never accepted silently.
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	_, err = a.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{
		CommonName: "leaf.example.test",
		TenantID:   "tenant-b",
	})
	var cross *tenantctx.CrossTenantError
	if !stderrors.As(err, &cross) {
		t.Fatalf("expected *tenantctx.CrossTenantError, got %T: %v", err, err)
	}
}

func TestXKMSCATenantError(t *testing.T) {
	cause := &tenantctx.MissingTenantError{Operation: "sign csr"}
	err := &XKMSCATenantError{Op: "sign csr", Err: cause}

	if err.Error() != qrdbsdk.ErrTenantMissing.Error() {
		t.Fatalf("Error() = %q, want the not-found message", err.Error())
	}
	if !stderrors.Is(err, qrdbsdk.ErrTenantMissing) {
		t.Fatal("expected errors.Is qrdbsdk.ErrTenantMissing")
	}
	var missing *tenantctx.MissingTenantError
	if !stderrors.As(err, &missing) || missing != cause {
		t.Fatal("expected the tenantctx cause to stay reachable")
	}
}
