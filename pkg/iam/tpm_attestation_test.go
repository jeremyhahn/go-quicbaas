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

package iam_test

import (
	"crypto/x509/pkix"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestTPMAttestationResolver_TPMChain verifies that a cert issued by a
// configured TPM EK CA root resolves to AttestMethodTPM.
func TestTPMAttestationResolver_TPMChain(t *testing.T) {
	caCert, caKey, ekPool := makeCA(t)
	leaf := makeIssuedCert(t, caCert, caKey, pkix.Name{CommonName: "tpm-idevid"}, nil)

	resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{EKRoots: ekPool})
	method := resolver.Resolve(leaf)
	if method != iam.AttestMethodTPM {
		t.Fatalf("expected %q, got %q", iam.AttestMethodTPM, method)
	}
}

// TestTPMAttestationResolver_XKeyChain verifies that a cert issued by a
// configured xkey CA root resolves to AttestMethodXKey.
func TestTPMAttestationResolver_XKeyChain(t *testing.T) {
	caCert, caKey, xkeyPool := makeCA(t)
	leaf := makeIssuedCert(t, caCert, caKey, pkix.Name{CommonName: "xkey-client"}, nil)

	resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{XKeyRoots: xkeyPool})
	method := resolver.Resolve(leaf)
	if method != iam.AttestMethodXKey {
		t.Fatalf("expected %q, got %q", iam.AttestMethodXKey, method)
	}
}

// TestTPMAttestationResolver_SelfSigned verifies that an admin-minted
// self-signed cert (not rooted in any known CA) resolves to AttestMethodManual.
func TestTPMAttestationResolver_SelfSigned(t *testing.T) {
	selfSigned, _ := makeSelfSigned(t, pkix.Name{CommonName: "admin-cert"}, nil)

	// Provide a real EK pool that the self-signed cert will NOT validate against.
	_, _, realPool := makeCA(t)
	resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{EKRoots: realPool})
	method := resolver.Resolve(selfSigned)
	if method != iam.AttestMethodManual {
		t.Fatalf("expected %q, got %q", iam.AttestMethodManual, method)
	}
}

// TestTPMAttestationResolver_NilCert verifies graceful handling of a nil cert.
func TestTPMAttestationResolver_NilCert(t *testing.T) {
	_, _, pool := makeCA(t)
	resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{EKRoots: pool})
	method := resolver.Resolve(nil)
	if method != iam.AttestMethodManual {
		t.Fatalf("expected manual for nil cert, got %q", method)
	}
}

// TestTPMAttestationResolver_NoPools verifies that an unconfigured resolver
// returns AttestMethodManual for any cert.
func TestTPMAttestationResolver_NoPools(t *testing.T) {
	cert, _ := makeSelfSigned(t, pkix.Name{CommonName: "any"}, nil)
	resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{})
	method := resolver.Resolve(cert)
	if method != iam.AttestMethodManual {
		t.Fatalf("expected manual for unconfigured resolver, got %q", method)
	}
}

// TestServiceAccountAMR_TPM verifies that TPM attestation produces ["mtls","hwk"].
func TestServiceAccountAMR_TPM(t *testing.T) {
	amr := iam.ServiceAccountAMR(iam.AttestMethodTPM)
	if len(amr) != 2 || amr[0] != "mtls" || amr[1] != "hwk" {
		t.Fatalf("expected [mtls hwk], got %v", amr)
	}
}

// TestServiceAccountAMR_Manual verifies that non-TPM attestation produces ["mtls"].
func TestServiceAccountAMR_Manual(t *testing.T) {
	amr := iam.ServiceAccountAMR(iam.AttestMethodManual)
	if len(amr) != 1 || amr[0] != "mtls" {
		t.Fatalf("expected [mtls], got %v", amr)
	}
}

// TestServiceAccountAMR_XKey verifies that xkey attestation produces ["mtls"].
func TestServiceAccountAMR_XKey(t *testing.T) {
	amr := iam.ServiceAccountAMR(iam.AttestMethodXKey)
	if len(amr) != 1 || amr[0] != "mtls" {
		t.Fatalf("expected [mtls], got %v", amr)
	}
}
