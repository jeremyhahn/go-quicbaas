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

// SDKFleetNodeCertIssuer against a REAL go-xkms CA reached through the transport
// interface, rather than against a stub of it.
//
// The stub these tests replace returned an IssueCertificateResponse carrying a
// private key. No go-xkms backend has ever populated that field — the keystore
// holds the key as a non-exportable opaque handle — so the issuer passed its
// tests and failed on every real call with "ca did not return exportable
// private key" (go-quicbaas#910). A real CA behind the same interface cannot
// agree to that, which is the whole reason it is used here.

package service

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

const (
	sdkIssuerTenant = "automatethethings"
	sdkIssuerHost   = "agent-1.example"
)

// issueLeafPEM returns one real CA-signed leaf certificate PEM, for tests that
// need a certificate rather than an issuance.
// sdkIssuerTrustDomain is the SPIFFE trust domain the test CA binds tenant
// SANs under.
const sdkIssuerTrustDomain = "quicbaas.test"

// newServiceKindCAService returns a test CA service that issues svc-kind
// tenant SPIFFE SANs: the identity the fleet enrollment endpoint requires.
func newServiceKindCAService(t *testing.T, name string) *xkmsca.CAService {
	t.Helper()
	ca := xkmsca.NewCAService(t, name)
	ca.SetSPIFFE(sdkIssuerTrustDomain, qrdbsdk.SpiffeKindService)
	return ca
}

func issueLeafPEM(t *testing.T, ca *xkmsca.CAService) []byte {
	t.Helper()
	// The issuer refuses a leaf without a svc tenant SAN, so the CA stands
	// for a provider that issues one.
	ca.SetSPIFFE(sdkIssuerTrustDomain, qrdbsdk.SpiffeKindService)
	issuer, err := NewSDKFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}
	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID: sdkIssuerTenant,
		Hostname: sdkIssuerHost,
	})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()
	return append([]byte(nil), issued.CertPEM...)
}

// parseSDKLeaf decodes the leaf the issuer returned.
func parseSDKLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("issued certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	return cert
}

// assertSDKKeyMatches checks the returned private key opens the certificate it
// came with. A mismatch fails at the first handshake, a long way from here.
func assertSDKKeyMatches(t *testing.T, keyPEM []byte, leaf *x509.Certificate) {
	t.Helper()
	key, err := xkmssdk.DecodePrivateKeyPEM(keyPEM, nil)
	if err != nil {
		t.Fatalf("decode returned private key: %v", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		t.Fatalf("returned key is not a crypto.Signer, got %T", key)
	}
	got, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		t.Fatalf("marshal returned public key: %v", err)
	}
	want, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		t.Fatalf("marshal certificate public key: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("the returned private key is not the certificate's key")
	}
}

// =============================================================================
// the defect this suite exists for
// =============================================================================

// A served go-xkms CA returns NO private key from IssueCertificate, on any
// backend. This pins the fact the issuer used to depend on being false, so the
// day it changes upstream is a test failure rather than a silent behaviour
// change.
func TestSDKAgentCertIssuer_TheCAServiceCannotExportAKey(t *testing.T) {
	ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Export CA")

	resp, err := ca.IssueCertificate(context.Background(), &xkmssdk.IssueCertificateRequest{
		CommonName:   "leaf.example.test",
		ValidityDays: 1,
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if len(resp.CertificatePEM) == 0 {
		t.Fatal("IssueCertificate returned no certificate")
	}
	if len(resp.PrivateKeyPEM) != 0 {
		t.Fatalf("IssueCertificate returned %d bytes of private key; the go-xkms "+
			"keystore is supposed to make that impossible", len(resp.PrivateKeyPEM))
	}
}

// The issuer must take the CSR-sign route, because it is the only route that
// can produce a credential. If a future change sends it back to
// IssueCertificate, SignCSRCalls goes to zero and this fails.
func TestSDKAgentCertIssuer_SignsACSRAndKeepsTheMatchingKey(t *testing.T) {
	ca := newServiceKindCAService(t, "QuicBaaS SDK Issuer CA")
	issuer, err := NewSDKFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID: sdkIssuerTenant,
		Hostname: sdkIssuerHost,
		Subject: FleetNodeCertificateSubject{
			CommonName:         "agent-ses-4c1d20",
			Organization:       sdkIssuerTenant,
			OrganizationalUnit: sdkIssuerTenant,
		},
		ExtraDNSNames: []string{"alias.example"},
		ValidityDays:  30,
	})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()

	if ca.SignCSRCalls() != 1 {
		t.Fatalf("SignCSRCalls = %d, want 1: the issuer must hand the CA a CSR", ca.SignCSRCalls())
	}
	if ca.LastSignCSR() == nil {
		t.Fatal("no CSR was recorded")
	}
	if ca.LastSignCSR().Profile != FleetNodeCertProfile {
		t.Errorf("CSR profile = %q, want %q", ca.LastSignCSR().Profile, FleetNodeCertProfile)
	}
	if ca.LastSignCSR().TenantID != sdkIssuerTenant {
		t.Errorf("CSR tenant = %q, want %q", ca.LastSignCSR().TenantID, sdkIssuerTenant)
	}
	if ca.LastSignCSR().ValidityDays != 30 {
		t.Errorf("CSR validity = %d, want 30", ca.LastSignCSR().ValidityDays)
	}

	// The CSR must carry the agent's public key and nothing of its private one.
	csrBlock, _ := pem.Decode(ca.LastSignCSR().CSRPEM)
	if csrBlock == nil {
		t.Fatal("recorded CSR is not PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		t.Fatalf("parse recorded CSR: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("the CSR is not self-signed by the agent key: %v", err)
	}

	leaf := parseSDKLeaf(t, issued.CertPEM)

	// THE acceptance assertion: the certificate verifies against the CA that
	// signed it, for the usage it was issued for.
	if err := ca.VerifyAsClientCert(leaf); err != nil {
		t.Fatalf("issued leaf does not verify as a client certificate: %v", err)
	}
	assertSDKKeyMatches(t, issued.PrivateKey.Bytes(), leaf)

	if leaf.Subject.CommonName != "agent-ses-4c1d20" {
		t.Errorf("CommonName = %q", leaf.Subject.CommonName)
	}
	// The OU is what the control-plane register handler matches on. The
	// IssueCertificate route could not express it at all.
	if len(leaf.Subject.OrganizationalUnit) == 0 || leaf.Subject.OrganizationalUnit[0] != sdkIssuerTenant {
		t.Errorf("OU = %v, want %q", leaf.Subject.OrganizationalUnit, sdkIssuerTenant)
	}
	if len(leaf.Subject.Organization) == 0 || leaf.Subject.Organization[0] != sdkIssuerTenant {
		t.Errorf("O = %v, want the tenant", leaf.Subject.Organization)
	}
	assertSDKHasDNSName(t, leaf, sdkIssuerHost)
	assertSDKHasDNSName(t, leaf, "alias.example")

	// The leaf carries the identity the fleet enrollment endpoint requires.
	id, err := qrdbsdk.TenantFromCert(leaf, "")
	if err != nil {
		t.Fatalf("TenantFromCert: %v", err)
	}
	if id.TenantID != sdkIssuerTenant || id.Kind != qrdbsdk.SpiffeKindService {
		t.Errorf("SPIFFE identity = tenant %q kind %q, want tenant %q kind %q", id.TenantID, id.Kind, sdkIssuerTenant, qrdbsdk.SpiffeKindService)
	}

	if len(issued.CAPEM) == 0 {
		t.Fatal("no CA bundle returned; the agent cannot verify the endpoint")
	}
	if issued.ParsedCert == nil {
		t.Fatal("ParsedCert is nil")
	}
}

// assertSDKHasDNSName fails unless name is a DNS SAN on leaf.
func assertSDKHasDNSName(t *testing.T, leaf *x509.Certificate, name string) {
	t.Helper()
	for _, got := range leaf.DNSNames {
		if got == name {
			return
		}
	}
	t.Errorf("DNS SAN %q missing; have %v", name, leaf.DNSNames)
}

// =============================================================================
// defaults
// =============================================================================

// A request that names no subject still yields a usable certificate: the CN is
// derived from the hostname, the O from the tenant, and the OU from the one
// value the register handler accepts.
func TestSDKAgentCertIssuer_AppliesDefaults(t *testing.T) {
	ca := newServiceKindCAService(t, "QuicBaaS SDK Issuer Defaults CA")
	issuer, err := NewSDKFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID: sdkIssuerTenant,
		Hostname: sdkIssuerHost,
		// Duplicate and blank extras must be dropped rather than emitted.
		ExtraDNSNames: []string{"", sdkIssuerHost, "  ", "extra.example"},
	})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()

	leaf := parseSDKLeaf(t, issued.CertPEM)
	if leaf.Subject.CommonName != "agent-"+sdkIssuerHost {
		t.Errorf("CommonName = %q, want %q", leaf.Subject.CommonName, "agent-"+sdkIssuerHost)
	}
	if len(leaf.Subject.Organization) == 0 || leaf.Subject.Organization[0] != sdkIssuerTenant {
		t.Errorf("O = %v, want the tenant as the default", leaf.Subject.Organization)
	}
	if len(leaf.Subject.OrganizationalUnit) == 0 || leaf.Subject.OrganizationalUnit[0] != sdkIssuerTenant {
		t.Errorf("OU = %v, want %q as the default", leaf.Subject.OrganizationalUnit, sdkIssuerTenant)
	}
	if ca.LastSignCSR().ValidityDays != DefaultFleetNodeCertValidityDays {
		t.Errorf("validity = %d, want the default %d", ca.LastSignCSR().ValidityDays, DefaultFleetNodeCertValidityDays)
	}

	var count int
	for _, name := range leaf.DNSNames {
		if name == sdkIssuerHost {
			count++
		}
	}
	if count != 1 {
		t.Errorf("hostname appears %d times in %v, want once", count, leaf.DNSNames)
	}
	assertSDKHasDNSName(t, leaf, "extra.example")
}

// Two agents must never share a key, or one session's credential authenticates
// another's.
func TestSDKAgentCertIssuer_MintsADistinctKeyPerCall(t *testing.T) {
	ca := newServiceKindCAService(t, "QuicBaaS SDK Issuer Distinct CA")
	issuer, err := NewSDKFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}

	req := &FleetNodeCertificateRequest{TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost}

	first, err := issuer.IssueFleetNodeCertificate(context.Background(), req)
	if err != nil {
		t.Fatalf("first IssueFleetNodeCertificate: %v", err)
	}
	defer first.PrivateKey.Destroy()
	second, err := issuer.IssueFleetNodeCertificate(context.Background(), req)
	if err != nil {
		t.Fatalf("second IssueFleetNodeCertificate: %v", err)
	}
	defer second.PrivateKey.Destroy()

	if string(first.PrivateKey.Bytes()) == string(second.PrivateKey.Bytes()) {
		t.Fatal("two issuances returned the same private key")
	}
	if string(first.CertPEM) == string(second.CertPEM) {
		t.Fatal("two issuances returned the same certificate")
	}
}

// =============================================================================
// error handling
// =============================================================================

// Construction refuses a nil CA rather than deferring the failure to the first
// enrollment, which happens during boot where the error is only logged.
func TestSDKAgentCertIssuer_RefusesANilCA(t *testing.T) {
	if _, err := NewSDKFleetNodeCertIssuer(nil); !errors.Is(err, ErrFleetNodeCertIssuerNilCA) {
		t.Fatalf("error = %v, want ErrFleetNodeCertIssuerNilCA", err)
	}
}

// Every failure the issuer can surface, each through a real condition rather
// than a stub that agreed to produce it.
func TestSDKAgentCertIssuer_Errors(t *testing.T) {
	t.Run("an invalid request is refused before any CA call", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Invalid CA")
		issuer, err := NewSDKFleetNodeCertIssuer(ca)
		if err != nil {
			t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
		}
		_, err = issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			Hostname: sdkIssuerHost,
		})
		var fieldErr *FleetNodeCertRequestFieldError
		if !errors.As(err, &fieldErr) || fieldErr.Field != "tenant_id" {
			t.Fatalf("error = %v, want a tenant_id field error", err)
		}
		if ca.SignCSRCalls() != 0 {
			t.Errorf("SignCSRCalls = %d, want 0: validation must precede the CA call", ca.SignCSRCalls())
		}
	})

	t.Run("a refused CSR is wrapped", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Refusal CA")
		ca.SetSignCSRErr(errors.New("ca down"))
		issuer, err := NewSDKFleetNodeCertIssuer(ca)
		if err != nil {
			t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
		}
		_, err = issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
		})
		var issErr *FleetNodeCertIssueError
		if !errors.As(err, &issErr) || issErr.Op != "issue agent certificate" {
			t.Fatalf("error = %v, want FleetNodeCertIssueError(issue agent certificate)", err)
		}
	})

	t.Run("an unparseable certificate is refused", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Corrupt CA")
		ca.SetCorruptCertificatePEM(true)
		issuer, err := NewSDKFleetNodeCertIssuer(ca)
		if err != nil {
			t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
		}
		_, err = issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
		})
		var issErr *FleetNodeCertIssueError
		if !errors.As(err, &issErr) || issErr.Op != "parse issued certificate" {
			t.Fatalf("error = %v, want FleetNodeCertIssueError(parse issued certificate)", err)
		}
	})

	t.Run("no bundle and no chain is refused", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer No Anchor CA")
		ca.SetBundleErr(errors.New("no bundle"))
		ca.SetSuppressChain(true)
		issuer, err := NewSDKFleetNodeCertIssuer(ca)
		if err != nil {
			t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
		}
		_, err = issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
		})
		var issErr *FleetNodeCertIssueError
		if !errors.As(err, &issErr) || issErr.Op != "resolve ca bundle" {
			t.Fatalf("error = %v, want FleetNodeCertIssueError(resolve ca bundle): a "+
				"certificate with no trust anchor cannot be used", err)
		}
	})
}

// =============================================================================
// the trust-bundle fallback
// =============================================================================

// GetCABundle failing must not fail the enrollment: the chain the signing
// response carries is an equally valid anchor, and an agent that cannot verify
// its endpoint is no better off than one with no certificate.
func TestSDKAgentCertIssuer_FallsBackToTheSigningChain(t *testing.T) {
	for name, configure := range map[string]func(*xkmsca.CAService){
		"bundle call fails":       func(ca *xkmsca.CAService) { ca.SetBundleErr(errors.New("no bundle")) },
		"bundle call returns nil": func(ca *xkmsca.CAService) { ca.SetNilBundleResponse(true) },
	} {
		t.Run(name, func(t *testing.T) {
			ca := newServiceKindCAService(t, "QuicBaaS SDK Issuer Fallback CA "+name)
			configure(ca)
			issuer, err := NewSDKFleetNodeCertIssuer(ca)
			if err != nil {
				t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
			}

			issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
				TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
			})
			if err != nil {
				t.Fatalf("IssueFleetNodeCertificate: %v", err)
			}
			defer issued.PrivateKey.Destroy()

			if len(issued.CAPEM) == 0 {
				t.Fatal("no CA bundle; the signing response's chain was not used")
			}
			// The fallback anchor must still verify the leaf, or it is decoration.
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(issued.CAPEM) {
				t.Fatal("the fallback bundle is not a usable trust anchor")
			}
			leaf := parseSDKLeaf(t, issued.CertPEM)
			if _, err := leaf.Verify(x509.VerifyOptions{
				Roots:     pool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}); err != nil {
				t.Fatalf("leaf does not verify against the fallback bundle: %v", err)
			}
		})
	}
}

// TestSDKAgentCertIssuer_RefusesALeafEnrollmentWouldReject pins finding 2 of
// the #2447 review: a provider that cannot issue a svc-kind tenant SAN must
// not hand the deploy a certificate the fleet enrollment endpoint refuses.
func TestSDKAgentCertIssuer_RefusesALeafEnrollmentWouldReject(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(ca *xkmsca.CAService)
		wantKind string
	}{
		{name: "no SPIFFE SAN", setup: func(*xkmsca.CAService) {}},
		{
			name:     "user kind",
			setup:    func(ca *xkmsca.CAService) { ca.SetSPIFFE(sdkIssuerTrustDomain, qrdbsdk.SpiffeKindUser) },
			wantKind: qrdbsdk.SpiffeKindUser,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Kind CA")
			tc.setup(ca)
			issuer, err := NewSDKFleetNodeCertIssuer(ca)
			if err != nil {
				t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
			}
			issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
				TenantID: sdkIssuerTenant,
				Hostname: sdkIssuerHost,
			})
			if issued != nil {
				issued.PrivateKey.Destroy()
				t.Fatal("the issuer returned a certificate the enrollment endpoint would refuse")
			}
			var kindErr *FleetNodeCertKindUnavailableError
			if !errors.As(err, &kindErr) {
				t.Fatalf("err = %v, want *FleetNodeCertKindUnavailableError", err)
			}
			if kindErr.TenantID != sdkIssuerTenant {
				t.Errorf("TenantID = %q, want %q", kindErr.TenantID, sdkIssuerTenant)
			}
			if kindErr.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", kindErr.Kind, tc.wantKind)
			}
			if ca.SignCSRCalls() != 1 {
				t.Errorf("SignCSRCalls = %d, want 1: the CA is asked, the leaf refused after", ca.SignCSRCalls())
			}
		})
	}
}

// selfSignedSPIFFELeaf builds a self-signed leaf whose only URI SAN is uri
// (none when empty), for identity-rule cases a CA would not issue.
func selfSignedSPIFFELeaf(t *testing.T, uri string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "agent-1"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	if uri != "" {
		u, err := url.Parse(uri)
		if err != nil {
			t.Fatalf("parse %q: %v", uri, err)
		}
		tmpl.URIs = []*url.URL{u}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return leaf
}

func TestCheckFleetNodeCertIdentity(t *testing.T) {
	cases := []struct {
		name       string
		leaf       func(t *testing.T) *x509.Certificate
		wantOK     bool
		wantTenant string
		// malformed accepts either a parse cause or an empty kind.
		malformed bool
	}{
		{
			name: "svc for the tenant",
			leaf: func(t *testing.T) *x509.Certificate {
				return parseSDKLeaf(t, issueLeafPEM(t, xkmsca.NewCAService(t, "QuicBaaS Identity CA")))
			},
			wantOK: true,
		},
		{
			name: "another tenant",
			leaf: func(t *testing.T) *x509.Certificate {
				return selfSignedSPIFFELeaf(t, "spiffe://quicbaas.test/tenant/other/svc/agent-1")
			},
			wantTenant: "other",
		},
		{
			name:      "no SAN",
			leaf:      func(t *testing.T) *x509.Certificate { return selfSignedSPIFFELeaf(t, "") },
			malformed: true,
		},
		{
			name: "malformed SPIFFE URI",
			leaf: func(t *testing.T) *x509.Certificate {
				return selfSignedSPIFFELeaf(t, "spiffe://quicbaas.test/nottenant")
			},
			malformed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckFleetNodeCertIdentity(tc.leaf(t), sdkIssuerTenant)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("CheckFleetNodeCertIdentity: %v", err)
				}
				return
			}
			var kindErr *FleetNodeCertKindUnavailableError
			if !errors.As(err, &kindErr) {
				t.Fatalf("err = %v, want *FleetNodeCertKindUnavailableError", err)
			}
			if kindErr.TenantID != sdkIssuerTenant {
				t.Errorf("TenantID = %q, want %q", kindErr.TenantID, sdkIssuerTenant)
			}
			if tc.wantTenant != "" && kindErr.CertificateTenant != tc.wantTenant {
				t.Errorf("CertificateTenant = %q, want %q", kindErr.CertificateTenant, tc.wantTenant)
			}
			if tc.malformed && kindErr.Cause == nil && kindErr.Kind != "" {
				t.Errorf("Kind = %q with no cause, want a cause or an empty kind", kindErr.Kind)
			}
			msg := err.Error()
			if !strings.Contains(msg, sdkIssuerTenant) || !strings.Contains(msg, `"svc"`) {
				t.Errorf("Error() = %q, want it to name the tenant and svc", msg)
			}
		})
	}
}
