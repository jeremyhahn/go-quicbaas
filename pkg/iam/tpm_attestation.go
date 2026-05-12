//go:build iam

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

package iam

import (
	"crypto/x509"
)

// AttestMethod constants describe how a service-account client certificate
// was provisioned.
const (
	// AttestMethodTPM indicates the cert chain validates against a known TPM EK CA root.
	AttestMethodTPM = "tpm"

	// AttestMethodXKey indicates the cert chain validates against the xkey CA root.
	AttestMethodXKey = "xkey"

	// AttestMethodManual indicates the cert was admin-minted outside any known CA hierarchy.
	AttestMethodManual = "manual"
)

// TPMConfig holds operator-provided configuration for TPM EK CA recognition.
type TPMConfig struct {
	// EKRoots is the pool of trusted TPM Endorsement Key CA roots.
	// When a client cert chain validates against this pool the issued
	// access_token receives amr:["mtls","hwk"] and attest_method:"tpm".
	EKRoots *x509.CertPool

	// XKeyRoots is the pool of trusted xkey CA roots.
	// When a client cert chain validates against this pool the issued
	// access_token receives attest_method:"xkey".
	XKeyRoots *x509.CertPool
}

// TPMAttestationResolver determines the attestation method for a client
// certificate by verifying its chain against known TPM EK and xkey CA roots.
type TPMAttestationResolver struct {
	cfg TPMConfig
}

// NewTPMAttestationResolver creates a resolver with the given configuration.
func NewTPMAttestationResolver(cfg TPMConfig) *TPMAttestationResolver {
	return &TPMAttestationResolver{cfg: cfg}
}

// Resolve returns the attestation method for the given certificate.
// It does not validate the certificate for mTLS client auth purposes; that is
// handled by MTLSClientAuthenticator. This resolver only classifies the cert
// origin for claim enrichment.
func (r *TPMAttestationResolver) Resolve(cert *x509.Certificate) string {
	if cert == nil {
		return AttestMethodManual
	}

	if r.cfg.EKRoots != nil {
		opts := x509.VerifyOptions{
			Roots:     r.cfg.EKRoots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}
		if _, err := cert.Verify(opts); err == nil {
			return AttestMethodTPM
		}
	}

	if r.cfg.XKeyRoots != nil {
		opts := x509.VerifyOptions{
			Roots:     r.cfg.XKeyRoots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}
		if _, err := cert.Verify(opts); err == nil {
			return AttestMethodXKey
		}
	}

	return AttestMethodManual
}

// ServiceAccountAMR returns the AMR slice appropriate for a service-account
// mTLS token. TPM-attested certificates additionally include "hwk" per RFC 8176.
func ServiceAccountAMR(attestMethod string) []string {
	if attestMethod == AttestMethodTPM {
		return []string{"mtls", "hwk"}
	}
	return []string{"mtls"}
}
