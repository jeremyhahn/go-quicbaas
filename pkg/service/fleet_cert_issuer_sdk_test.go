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
	"errors"
	"strconv"
	"strings"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

const (
	sdkIssuerTenant = "automatethethings"
	sdkIssuerHost   = "agent-1.lan"
)

// issueLeafPEM returns one real CA-signed leaf certificate PEM, for tests that
// need a certificate rather than an issuance.
func issueLeafPEM(t *testing.T, ca *xkmsca.CAService) []byte {
	t.Helper()
	resp, err := ca.IssueCertificate(context.Background(), &xkmssdk.IssueCertificateRequest{
		TenantID:     sdkIssuerTenant,
		CommonName:   "agent-" + sdkIssuerHost,
		ValidityDays: 1,
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	return append([]byte(nil), resp.CertificatePEM...)
}

// assertSDKIssuerRefusesTheKind checks the SDK issuer refuses req with
// *FleetNodeCertKindUnavailableError and never asks the CA to sign: the
// transport cannot request the agent SPIFFE kind, so any certificate it got
// back would be one fleet enrolment refuses.
func assertSDKIssuerRefusesTheKind(t *testing.T, ca *xkmsca.CAService, req *FleetNodeCertificateRequest) {
	t.Helper()
	issuer, err := NewSDKFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}
	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), req)
	var kindErr *FleetNodeCertKindUnavailableError
	if !errors.As(err, &kindErr) {
		t.Fatalf("error = %v, want FleetNodeCertKindUnavailableError", err)
	}
	if kindErr.TenantID != req.TenantID {
		t.Errorf("TenantID = %q, want %q", kindErr.TenantID, req.TenantID)
	}
	if issued != nil {
		t.Fatal("a certificate was returned alongside the refusal")
	}
	if ca.SignCSRCalls() != 0 {
		t.Fatalf("SignCSRCalls = %d, want 0: the refusal must precede the CA call", ca.SignCSRCalls())
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
		TenantID:     "fixtures",
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

// The transport's SignCSRRequest has no SPIFFE kind or trust-domain field, so
// the certificate it returns is user-kind or SAN-less and fleet enrolment
// refuses it. The issuer must say so, with the tenant, before the CA is asked.
func TestSDKAgentCertIssuer_RefusesBecauseTheTransportCannotRequestTheAgentKind(t *testing.T) {
	ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Kind CA")
	req := &FleetNodeCertificateRequest{
		TenantID:          sdkIssuerTenant,
		Hostname:          sdkIssuerHost,
		SPIFFETrustDomain: testAgentTrustDomain,
	}
	assertSDKIssuerRefusesTheKind(t, ca, req)

	msg := (&FleetNodeCertKindUnavailableError{TenantID: sdkIssuerTenant}).Error()
	for _, want := range []string{"cannot request the agent SPIFFE kind", strconv.Quote(sdkIssuerTenant)} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name %q", msg, want)
		}
	}
}

// The issuer's only signing route is CAService.SignCSR, whose request cannot
// name the agent SPIFFE kind. A fully specified request is refused before a key
// is generated or the CA is called, so no certificate leaves this path that
// fleet enrolment would refuse.
func TestSDKAgentCertIssuer_SignsACSRAndKeepsTheMatchingKey(t *testing.T) {
	assertSDKIssuerRefusesTheKind(t, xkmsca.NewCAService(t, "QuicBaaS SDK Issuer CA"), &FleetNodeCertificateRequest{
		TenantID: sdkIssuerTenant,
		Hostname: sdkIssuerHost,
		Subject: FleetNodeCertificateSubject{
			CommonName:         "agent-ses-4c1d20",
			Organization:       sdkIssuerTenant,
			OrganizationalUnit: DefaultFleetNodeCertOU,
		},
		ExtraDNSNames:     []string{"alias.lan"},
		ValidityDays:      30,
		SPIFFETrustDomain: testAgentTrustDomain,
	})
}

// A request that names no subject is refused the same way: defaults do not
// give the transport a way to ask for the agent kind.
func TestSDKAgentCertIssuer_AppliesDefaults(t *testing.T) {
	assertSDKIssuerRefusesTheKind(t, xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Defaults CA"), &FleetNodeCertificateRequest{
		TenantID:      sdkIssuerTenant,
		Hostname:      sdkIssuerHost,
		ExtraDNSNames: []string{"", sdkIssuerHost, "  ", "extra.lan"},
	})
}

// Every call is refused, so no key is ever minted on this path; a second call
// must not slip through where the first was refused.
func TestSDKAgentCertIssuer_MintsADistinctKeyPerCall(t *testing.T) {
	ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Distinct CA")
	req := &FleetNodeCertificateRequest{TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost}
	assertSDKIssuerRefusesTheKind(t, ca, req)
	assertSDKIssuerRefusesTheKind(t, ca, req)
}

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

	// The CA conditions below used to surface as FleetNodeCertIssueError. The
	// issuer now refuses before the CA call, so none of them is reached.
	t.Run("a refused CSR is never requested", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Refusal CA")
		ca.SetSignCSRErr(errors.New("ca down"))
		assertSDKIssuerRefusesTheKind(t, ca, &FleetNodeCertificateRequest{
			TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
		})
	})

	t.Run("an unparseable certificate is never requested", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Corrupt CA")
		ca.SetCorruptCertificatePEM(true)
		assertSDKIssuerRefusesTheKind(t, ca, &FleetNodeCertificateRequest{
			TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
		})
	})

	t.Run("no bundle and no chain is never reached", func(t *testing.T) {
		ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer No Anchor CA")
		ca.SetBundleErr(errors.New("no bundle"))
		ca.SetSuppressChain(true)
		assertSDKIssuerRefusesTheKind(t, ca, &FleetNodeCertificateRequest{
			TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
		})
	})
}

// =============================================================================
// the trust-bundle fallback
// =============================================================================

// A failing GetCABundle no longer matters to issuance, which is refused before
// any CA call; FleetCABundle keeps its own tests
// (TestSDKAgentCertIssuer_AgentCABundle).
func TestSDKAgentCertIssuer_FallsBackToTheSigningChain(t *testing.T) {
	for name, configure := range map[string]func(*xkmsca.CAService){
		"bundle call fails":       func(ca *xkmsca.CAService) { ca.SetBundleErr(errors.New("no bundle")) },
		"bundle call returns nil": func(ca *xkmsca.CAService) { ca.SetNilBundleResponse(true) },
	} {
		t.Run(name, func(t *testing.T) {
			ca := xkmsca.NewCAService(t, "QuicBaaS SDK Issuer Fallback CA "+name)
			configure(ca)
			assertSDKIssuerRefusesTheKind(t, ca, &FleetNodeCertificateRequest{
				TenantID: sdkIssuerTenant, Hostname: sdkIssuerHost,
			})
		})
	}
}
