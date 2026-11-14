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

// fleet_cert_issuer_realca_test.go exercises XKMSFleetNodeCertIssuer against a REAL
// go-xkms Certificate Authority, and asserts on the ISSUED CERTIFICATE rather
// than on what the issuer asked the CA for.
//
// Both choices are the point of the file. Until go-quicbaas#910 this issuer
// called TrustedCA.IssueCertificate and then required
// IssuedCertificate.PrivateKeyPEM, a field no go-xkms backend has ever
// populated — the keystore hands back non-exportable opaque handles by design.
// So every invocation against a real CA failed with "ca did not return
// exportable private key", which meant the embedded agent ran without an mTLS
// identity and no per-session agent MCP certificate could be minted at all
// (go-quicbaas#911 builds agent session identity on this path).
//
// Nothing caught it, because the tests that covered this code asserted against a
// stub CA that returned a private key it had invented. Asserting on the
// certificate the CA actually produced is what makes that impossible to repeat.

package service

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

const (
	testAgentTenant      = "automatethethings"
	testAgentTrustDomain = "quicbaas.example.test"
)

// The acceptance test: a real CA issues a real agent certificate, it verifies
// against that CA's chain, the returned key is the certificate's, and the
// certificate carries everything the control plane and go-qrdb require of an
// agent identity.
func TestXKMSAgentCertIssuer_IssuesAVerifiableCertificate(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Agent Root CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID: testAgentTenant,
		Hostname: "agent-04",
		Subject: FleetNodeCertificateSubject{
			CommonName:         "agent-ses-7f3a91",
			Organization:       testAgentTenant,
			OrganizationalUnit: testAgentTenant,
		},
		ExtraDNSNames:     []string{"localhost"},
		ValidityDays:      1,
		SPIFFETrustDomain: testAgentTrustDomain,
	})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()

	if issued.ParsedCert == nil {
		t.Fatal("ParsedCert is nil")
	}
	leaf := parseAgentLeaf(t, issued.CertPEM)

	// THE acceptance assertion: the certificate verifies against the chain the
	// issuer shipped with it, for the usage it was issued for. Verified this way
	// rather than through TrustedCA.Verify because this is what the peer does:
	// go-qrdb's mTLS authenticator validates a client certificate for
	// ExtKeyUsageClientAuth against the bundle, and TrustedCA.Verify hard-codes
	// server-auth options that no agent certificate can satisfy.
	assertVerifiesAsClientCert(t, leaf, issued.CAPEM)

	// The agent writes this key to disk and presents the certificate with it. A
	// mismatch fails at the first handshake, a long way from here.
	assertAgentKeyMatchesCertificate(t, issued.PrivateKey.Bytes(), leaf)

	if leaf.Subject.CommonName != "agent-ses-7f3a91" {
		t.Errorf("CommonName = %q", leaf.Subject.CommonName)
	}
	if len(leaf.Subject.OrganizationalUnit) == 0 || leaf.Subject.OrganizationalUnit[0] != testAgentTenant {
		t.Errorf("OU = %v, want %q: the OU carries the tenant (#1388)",
			leaf.Subject.OrganizationalUnit, testAgentTenant)
	}
	if len(leaf.Subject.Organization) == 0 || leaf.Subject.Organization[0] != testAgentTenant {
		t.Errorf("O = %v, want the tenant", leaf.Subject.Organization)
	}
	assertHasDNSName(t, leaf, "agent-04")
	assertHasDNSName(t, leaf, "localhost")
	assertHasExtKeyUsage(t, leaf, x509.ExtKeyUsageClientAuth)

	// The SPIFFE SAN is what makes the certificate authorizable: go-qrdb derives
	// the identity's TENANT from it, and falls back to the first Subject OU
	// otherwise — "agents" on every agent certificate, a tenant no enrollment
	// can reach (go-quicbaas#901).
	assertSPIFFETenant(t, leaf, testAgentTenant)

	if len(issued.CAPEM) == 0 {
		t.Fatal("no CA bundle returned; the agent cannot verify the endpoint")
	}
	if block, _ := pem.Decode(issued.CAPEM); block == nil {
		t.Fatal("CA bundle is not PEM")
	}
}

// Without a trust domain the certificate still issues and still verifies, but it
// carries no SPIFFE SAN: a caller that names no trust domain must not get a
// certificate silently bound under an invented one, which would bake a fake
// pinned security identifier into it.
func TestXKMSAgentCertIssuer_OmitsTheSPIFFESANWithoutATrustDomain(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Agent No-SPIFFE Root CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}

	for i, trustDomain := range []string{"", "   "} {
		issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			TenantID: testAgentTenant,
			// A distinct CN per iteration so each is its own certificate rather
			// than a re-issue of the previous one.
			Subject:           FleetNodeCertificateSubject{CommonName: "agent-no-spiffe-" + string(rune('a'+i))},
			Hostname:          "agent-04",
			ValidityDays:      1,
			SPIFFETrustDomain: trustDomain,
		})
		if err != nil {
			t.Fatalf("IssueFleetNodeCertificate(trustDomain=%q): %v", trustDomain, err)
		}

		leaf := parseAgentLeaf(t, issued.CertPEM)
		issued.PrivateKey.Destroy()

		assertVerifiesAsClientCert(t, leaf, issued.CAPEM)
		if len(leaf.URIs) != 0 {
			t.Errorf("trustDomain=%q: certificate carries URI SANs %v, want none", trustDomain, leaf.URIs)
		}
	}
}

// Defaults are applied from the request rather than left empty: no CommonName
// derives one from the hostname, no Organization uses the tenant, no OU uses
// the value the control plane requires, and no validity uses the documented
// default.
func TestXKMSAgentCertIssuer_AppliesDefaults(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Agent Defaults Root CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID:          testAgentTenant,
		Hostname:          "agent-04",
		SPIFFETrustDomain: testAgentTrustDomain,
	})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()

	leaf := parseAgentLeaf(t, issued.CertPEM)
	if leaf.Subject.CommonName != "agent-agent-04" {
		t.Errorf("CommonName = %q, want the hostname-derived default", leaf.Subject.CommonName)
	}
	if len(leaf.Subject.Organization) == 0 || leaf.Subject.Organization[0] != testAgentTenant {
		t.Errorf("O = %v, want the tenant default", leaf.Subject.Organization)
	}
	if len(leaf.Subject.OrganizationalUnit) == 0 || leaf.Subject.OrganizationalUnit[0] != testAgentTenant {
		t.Errorf("OU = %v, want %q", leaf.Subject.OrganizationalUnit, testAgentTenant)
	}
	// DefaultFleetNodeCertValidityDays, allowing a day of slack for the CA's own
	// not-before backdating.
	lifetimeDays := leaf.NotAfter.Sub(leaf.NotBefore).Hours() / 24
	if lifetimeDays < DefaultFleetNodeCertValidityDays-2 || lifetimeDays > DefaultFleetNodeCertValidityDays+2 {
		t.Errorf("lifetime = %.1f days, want about %d", lifetimeDays, DefaultFleetNodeCertValidityDays)
	}
	assertSPIFFETenant(t, leaf, testAgentTenant)
}

// Two sessions never share a key. The per-session agent identity path mints one
// certificate per spawn, so a generator that returned the same key twice would
// give every session the same credential.
func TestXKMSAgentCertIssuer_MintsADistinctKeyPerCall(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Agent Distinct Root CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}

	issue := func(cn string) *x509.Certificate {
		t.Helper()
		issued, issueErr := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			TenantID:          testAgentTenant,
			Hostname:          "agent-04",
			Subject:           FleetNodeCertificateSubject{CommonName: cn},
			ValidityDays:      1,
			SPIFFETrustDomain: testAgentTrustDomain,
		})
		if issueErr != nil {
			t.Fatalf("IssueFleetNodeCertificate(%s): %v", cn, issueErr)
		}
		defer issued.PrivateKey.Destroy()
		leaf := parseAgentLeaf(t, issued.CertPEM)
		assertAgentKeyMatchesCertificate(t, issued.PrivateKey.Bytes(), leaf)
		return leaf
	}

	first := issue("agent-session-one")
	second := issue("agent-session-two")

	firstPub, err := x509.MarshalPKIXPublicKey(first.PublicKey)
	if err != nil {
		t.Fatalf("marshal first public key: %v", err)
	}
	secondPub, err := x509.MarshalPKIXPublicKey(second.PublicKey)
	if err != nil {
		t.Fatalf("marshal second public key: %v", err)
	}
	if string(firstPub) == string(secondPub) {
		t.Fatal("two sessions were issued the same key")
	}
	if first.SerialNumber.Cmp(second.SerialNumber) == 0 {
		t.Fatal("two sessions were issued the same serial number")
	}
}

// An invalid request is refused before anything is generated or signed, with the
// typed field error naming what was missing, and a nil CA is refused at
// construction so a mis-wired server fails fast instead of at the first spawn.
func TestXKMSAgentCertIssuer_Errors(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Agent Errors Root CA")

	t.Run("nil ca at construction", func(t *testing.T) {
		issuer, err := NewXKMSFleetNodeCertIssuer(nil)
		if !errors.Is(err, ErrFleetNodeCertIssuerNilCA) {
			t.Fatalf("error = %v, want ErrFleetNodeCertIssuerNilCA", err)
		}
		if issuer != nil {
			t.Fatal("no issuer should be returned with a nil CA")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		issuer, err := NewXKMSFleetNodeCertIssuer(ca)
		if err != nil {
			t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
		}
		issued, err := issuer.IssueFleetNodeCertificate(context.Background(), nil)
		if !errors.Is(err, ErrFleetNodeCertRequestNil) {
			t.Fatalf("error = %v, want ErrFleetNodeCertRequestNil", err)
		}
		if issued != nil {
			t.Fatal("no certificate should be returned for a nil request")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		issuer, err := NewXKMSFleetNodeCertIssuer(ca)
		if err != nil {
			t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
		}
		tests := []struct {
			name  string
			req   *FleetNodeCertificateRequest
			field string
		}{
			{"no tenant", &FleetNodeCertificateRequest{Hostname: "h"}, "tenant_id"},
			{"blank tenant", &FleetNodeCertificateRequest{TenantID: "  ", Hostname: "h"}, "tenant_id"},
			{"no hostname", &FleetNodeCertificateRequest{TenantID: "t"}, "hostname"},
			{"blank hostname", &FleetNodeCertificateRequest{TenantID: "t", Hostname: " "}, "hostname"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				issued, err := issuer.IssueFleetNodeCertificate(context.Background(), tt.req)
				if issued != nil {
					t.Fatal("no certificate should be returned")
				}
				var fieldErr *FleetNodeCertRequestFieldError
				if !errors.As(err, &fieldErr) {
					t.Fatalf("error = %v (%T), want *FleetNodeCertRequestFieldError", err, err)
				}
				if fieldErr.Field != tt.field {
					t.Fatalf("Field = %q, want %q", fieldErr.Field, tt.field)
				}
			})
		}
	})

	t.Run("ca refuses to sign", func(t *testing.T) {
		// An uninitialized CA is the honest "CA is not up" condition: the server
		// starts the issuer only after initializeCA, so a refusal here stands for
		// a CA that has gone away.
		cold := xkmsca.NewUninitialized(t, "QuicBaaS #910 Cold Root CA")
		issuer, err := NewXKMSFleetNodeCertIssuer(cold)
		if err != nil {
			t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
		}
		issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
			TenantID:          testAgentTenant,
			Hostname:          "agent-04",
			ValidityDays:      1,
			SPIFFETrustDomain: testAgentTrustDomain,
		})
		if err == nil {
			issued.PrivateKey.Destroy()
			t.Fatal("expected issuance to fail against an uninitialized CA")
		}
		if issued != nil {
			t.Fatal("no certificate should be returned when the CA refuses")
		}
		var issueErr *FleetNodeCertIssueError
		if !errors.As(err, &issueErr) {
			t.Fatalf("error = %v (%T), want *FleetNodeCertIssueError", err, err)
		}
	})
}

// =============================================================================
// helpers
// =============================================================================

// parseAgentLeaf decodes the leaf certificate from an issuance result.
func parseAgentLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("issued certificate PEM has no block")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	return leaf
}

// assertAgentKeyMatchesCertificate proves the mlocked key really is the leaf's.
func assertAgentKeyMatchesCertificate(t *testing.T, keyPEM []byte, leaf *x509.Certificate) {
	t.Helper()
	if len(keyPEM) == 0 {
		t.Fatal("no private key material; the agent cannot present the certificate")
	}
	key, err := xkmssdk.DecodePrivateKeyPEM(keyPEM, nil)
	if err != nil {
		t.Fatalf("decode agent private key: %v", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		t.Fatalf("agent key %T is not a crypto.Signer", key)
	}
	got, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		t.Fatalf("marshal agent public key: %v", err)
	}
	want, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		t.Fatalf("marshal certificate public key: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("the agent private key does not match its certificate")
	}
}

// assertVerifiesAsClientCert verifies leaf for client authentication against the
// trust anchors in bundlePEM — the check go-qrdb's mTLS authenticator performs
// on the wire, run here against the bundle the issuer handed the agent.
func assertVerifiesAsClientCert(t *testing.T, leaf *x509.Certificate, bundlePEM []byte) {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundlePEM) {
		t.Fatal("CA bundle contains no usable certificates")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("issued agent leaf does not verify against its own CA bundle: %v", err)
	}
}

// assertHasDNSName fails unless name is among the leaf's DNS SANs.
func assertHasDNSName(t *testing.T, leaf *x509.Certificate, name string) {
	t.Helper()
	for _, got := range leaf.DNSNames {
		if got == name {
			return
		}
	}
	t.Errorf("DNS SAN %q missing; have %v", name, leaf.DNSNames)
}

// assertHasExtKeyUsage fails unless the leaf carries the given EKU.
func assertHasExtKeyUsage(t *testing.T, leaf *x509.Certificate, want x509.ExtKeyUsage) {
	t.Helper()
	for _, got := range leaf.ExtKeyUsage {
		if got == want {
			return
		}
	}
	t.Errorf("ExtKeyUsage %v missing; have %v", want, leaf.ExtKeyUsage)
}

// assertSPIFFETenant fails unless go-qrdb would authenticate the certificate
// into wantTenant. It asks go-qrdb, rather than parsing the URI here, so the
// assertion is the production rule and not a restatement of it.
func assertSPIFFETenant(t *testing.T, leaf *x509.Certificate, wantTenant string) {
	t.Helper()
	if len(leaf.URIs) == 0 {
		t.Fatal("certificate carries no SPIFFE URI SAN, so go-qrdb would fall back to OU=agents — a tenant no enrollment can reach")
	}
	if !strings.HasPrefix(leaf.URIs[0].String(), "spiffe://"+testAgentTrustDomain) {
		t.Fatalf("URI SAN %q is not under the requested trust domain", leaf.URIs[0])
	}
	identity, err := qrdbsdk.TenantFromCert(leaf, "")
	if err != nil {
		t.Fatalf("go-qrdb cannot derive a tenant from the issued certificate: %v", err)
	}
	if identity.TenantID != wantTenant {
		t.Fatalf("go-qrdb would authenticate this certificate into tenant %q, want %q",
			identity.TenantID, wantTenant)
	}
}

// When the CA cannot produce a bundle, the issuer must fall back to the CA's own
// certificate rather than ship an agent a certificate with no trust anchor. A
// single-identity CA is the case this exists for, and the fallback is only worth
// having if the anchor it produces actually verifies the leaf.
func TestXKMSAgentCertIssuer_FallsBackToTheCACertificate(t *testing.T) {
	real := xkmsca.New(t, "QuicBaaS #910 Agent Fallback CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(xkmsca.WithoutCABundle(real, errors.New("no bundle")))
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID: testAgentTenant,
		Hostname: "agent-04",
		Subject:  FleetNodeCertificateSubject{OrganizationalUnit: testAgentTenant},
	})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()

	if len(issued.CAPEM) == 0 {
		t.Fatal("no CA bundle; the fallback did not run")
	}
	assertVerifiesAsClientCert(t, parseAgentLeaf(t, issued.CertPEM), issued.CAPEM)
}

// With no bundle AND no CA certificate there is no trust anchor at all, and the
// issuer must refuse rather than hand back a certificate the agent cannot use.
// The private key must not leak on that path either.
func TestXKMSAgentCertIssuer_RefusesWithNoTrustAnchor(t *testing.T) {
	uninitialized := xkmsca.NewUninitialized(t, "QuicBaaS #910 Agent No Anchor CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(uninitialized)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}

	_, err = issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{
		TenantID: testAgentTenant,
		Hostname: "agent-04",
	})
	var issErr *FleetNodeCertIssueError
	if !errors.As(err, &issErr) {
		t.Fatalf("error = %v, want FleetNodeCertIssueError", err)
	}
}
