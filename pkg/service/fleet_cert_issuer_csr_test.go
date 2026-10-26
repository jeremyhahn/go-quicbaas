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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// fleetNodeCSR returns a PEM CSR for commonName under a fresh P-256 key, the
// shape `quicbaas fleet deploy` builds on the admin workstation.
func fleetNodeCSR(t *testing.T, commonName string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{CommonName: commonName, Organization: testAgentTenant},
		SANS:    &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"agent-07"}},
	}, key)
	if err != nil {
		t.Fatalf("CreateCSRWithKey: %v", err)
	}
	return csrPEM, key
}

// TestXKMSFleetNodeCertIssuer_SignsAWorkstationCSRAsAnEnrollableAgent proves a
// fleet node CSR built off the server host — where the deploy CLI holds the
// key — comes back signed with everything enrolment checks: the agent SPIFFE
// kind under the tenant, clientAuth, the CSR's own key, and an issued-cert
// record for the tenant.
func TestXKMSFleetNodeCertIssuer_SignsAWorkstationCSRAsAnEnrollableAgent(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS Fleet CSR Root CA")
	records := newTestFleetCertRecords(t)
	issuer, err := NewXKMSFleetNodeCertIssuer(ca, records)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}
	csrPEM, key := fleetNodeCSR(t, "agent-workstation-07")

	signed, err := issuer.SignFleetNodeCSR(context.Background(), &FleetNodeCSRRequest{
		TenantID:          testAgentTenant,
		CSRPEM:            csrPEM,
		ValidityDays:      1,
		SPIFFETrustDomain: testAgentTrustDomain,
	})
	if err != nil {
		t.Fatalf("SignFleetNodeCSR: %v", err)
	}

	leaf := parseAgentLeaf(t, signed.CertPEM)
	assertVerifiesAsClientCert(t, leaf, signed.CAPEM)
	assertHasExtKeyUsage(t, leaf, x509.ExtKeyUsageClientAuth)
	assertSPIFFETenant(t, leaf, testAgentTenant)
	if leaf.Subject.CommonName != "agent-workstation-07" {
		t.Errorf("CommonName = %q", leaf.Subject.CommonName)
	}
	if !key.PublicKey.Equal(leaf.PublicKey) {
		t.Error("the certificate does not carry the CSR's public key")
	}
	if err := records.VerifyFleetNodeCertificate(context.Background(), testAgentTenant, leaf); err != nil {
		t.Errorf("the signed certificate was not recorded for enrolment: %v", err)
	}
}

// TestXKMSFleetNodeCertIssuer_SignFleetNodeCSRRefusesBadRequests proves a CSR
// that is missing, malformed, forged or nameless, or a request with no tenant
// or trust domain, is refused before anything is signed or recorded.
func TestXKMSFleetNodeCertIssuer_SignFleetNodeCSRRefusesBadRequests(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS Fleet CSR Refusal Root CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(ca, newTestFleetCertRecords(t))
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}
	valid, _ := fleetNodeCSR(t, "agent-workstation-07")
	nameless := namelessCSR(t)
	forged := forgeCSRSignature(t, valid)

	var fieldErr *FleetNodeCertRequestFieldError
	var noTrustDomain *FleetNodeCertNoTrustDomainError
	var csrErr *FleetNodeCSRInvalidError
	cases := map[string]struct {
		req  *FleetNodeCSRRequest
		want any
	}{
		"no tenant":       {&FleetNodeCSRRequest{CSRPEM: valid, SPIFFETrustDomain: testAgentTrustDomain}, &fieldErr},
		"no csr":          {&FleetNodeCSRRequest{TenantID: testAgentTenant, SPIFFETrustDomain: testAgentTrustDomain}, &fieldErr},
		"no trust domain": {&FleetNodeCSRRequest{TenantID: testAgentTenant, CSRPEM: valid}, &noTrustDomain},
		"not pem":         {&FleetNodeCSRRequest{TenantID: testAgentTenant, CSRPEM: []byte("garbage"), SPIFFETrustDomain: testAgentTrustDomain}, &fieldErr},
		"wrong pem type": {&FleetNodeCSRRequest{TenantID: testAgentTenant, SPIFFETrustDomain: testAgentTrustDomain,
			CSRPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}})}, &csrErr},
		"forged signature": {&FleetNodeCSRRequest{TenantID: testAgentTenant, CSRPEM: forged, SPIFFETrustDomain: testAgentTrustDomain}, &csrErr},
		"no common name":   {&FleetNodeCSRRequest{TenantID: testAgentTenant, CSRPEM: nameless, SPIFFETrustDomain: testAgentTrustDomain}, &fieldErr},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			signed, err := issuer.SignFleetNodeCSR(context.Background(), tc.req)
			if signed != nil {
				t.Fatal("a refused request returned a certificate")
			}
			if !errors.As(err, tc.want) {
				t.Fatalf("err = %v, want %T", err, tc.want)
			}
		})
	}

	if _, err := issuer.SignFleetNodeCSR(context.Background(), nil); !errors.Is(err, ErrFleetNodeCertRequestNil) {
		t.Fatalf("nil request: err = %v", err)
	}
}

// namelessCSR returns a well-signed CSR with no subject CommonName, which
// go-xkms's CSR builder refuses to produce.
func namelessCSR(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{"agent-07"}}, key)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// forgeCSRSignature flips a bit of the CSR's signature so it no longer proves
// possession of the key it names.
func forgeCSRSignature(t *testing.T, csrPEM []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatal("csr is not PEM")
	}
	der := append([]byte(nil), block.Bytes...)
	der[len(der)-1] ^= 0x01
	return pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der})
}

// TestNewFleetNodeKey_BuildsTheAgentCSRForTheKeyItHolds proves the key and CSR
// a remote deploy builds carry the agent subject defaults, and the private key
// handed back is the one the CSR names.
func TestNewFleetNodeKey_BuildsTheAgentCSRForTheKeyItHolds(t *testing.T) {
	key, err := NewFleetNodeKey(&FleetNodeCertificateRequest{
		TenantID:      testAgentTenant,
		Hostname:      "agent-07",
		ExtraDNSNames: []string{"agent-07", " ", "agent-07.lan"},
	})
	if err != nil {
		t.Fatalf("NewFleetNodeKey: %v", err)
	}
	csr, err := parseFleetNodeCSR(key.CSRPEM)
	if err != nil {
		t.Fatalf("parseFleetNodeCSR: %v", err)
	}
	if key.CommonName != "agent-agent-07" || csr.Subject.CommonName != key.CommonName {
		t.Errorf("CommonName = %q, csr CN = %q", key.CommonName, csr.Subject.CommonName)
	}
	if len(csr.Subject.Organization) != 1 || csr.Subject.Organization[0] != testAgentTenant {
		t.Errorf("Organization = %v", csr.Subject.Organization)
	}
	if len(csr.DNSNames) != 2 || csr.DNSNames[0] != "agent-07" || csr.DNSNames[1] != "agent-07.lan" {
		t.Errorf("DNSNames = %v", csr.DNSNames)
	}

	keyBuf, err := key.PrivateKeyPEM()
	if err != nil {
		t.Fatalf("PrivateKeyPEM: %v", err)
	}
	defer keyBuf.Destroy()
	keyPEM, err := secure.CopyOut(keyBuf)
	if err != nil {
		t.Fatalf("CopyOut: %v", err)
	}
	if !parsePrivateKeyPEM(t, keyPEM).Public().(interface{ Equal(crypto.PublicKey) bool }).Equal(csr.PublicKey) {
		t.Error("the private key does not match the CSR's public key")
	}
}

// TestNewFleetNodeKey_RefusesAnIncompleteRequest proves no key is generated
// for a request with no tenant or hostname.
func TestNewFleetNodeKey_RefusesAnIncompleteRequest(t *testing.T) {
	if _, err := NewFleetNodeKey(nil); !errors.Is(err, ErrFleetNodeCertRequestNil) {
		t.Fatalf("nil request: err = %v", err)
	}
	var fieldErr *FleetNodeCertRequestFieldError
	if _, err := NewFleetNodeKey(&FleetNodeCertificateRequest{TenantID: testAgentTenant}); !errors.As(err, &fieldErr) || fieldErr.Field != "hostname" {
		t.Fatalf("no hostname: err = %v", err)
	}
}

// parsePrivateKeyPEM decodes a SEC 1 or PKCS#8 private key.
func parsePrivateKeyPEM(t *testing.T, keyPEM []byte) crypto.Signer {
	t.Helper()
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("private key is not PEM")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		t.Fatalf("private key %T is not a signer", key)
	}
	return signer
}
