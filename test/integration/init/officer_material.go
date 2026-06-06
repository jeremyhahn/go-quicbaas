// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

// officer_material.go generates the synthetic CSR / X.509 cert / raw
// public-key PEM files the shamir matrix rows submit to `quicbaasd init
// --so <username>:<kind>:<path>`. Each file is hermetic — generated per
// subtest, stored under the per-subtest tmpdir — so multiple parallel
// shamir rows cannot collide on shared host state.
//
// The cryptography stack is the standard library only: ECDSA P-256 keys
// because they are FIPS 140-3 compatible AND fast enough to keep the
// matrix's wall-clock budget reasonable.

package init

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

// writeOfficerCSR generates an ECDSA P-256 key, builds a CSR for
// CN=<username>, and writes the PEM to path. The CSR's self-signature is
// verified before write so the daemon's loader (which calls
// CSR.CheckSignature) sees valid material.
func writeOfficerCSR(path, username string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("writeOfficerCSR(%s): generate key: %w", username, err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: username},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, priv)
	if err != nil {
		return fmt.Errorf("writeOfficerCSR(%s): create csr: %w", username, err)
	}
	// Sanity-check the signature; matches loadOfficerCSR's verification.
	parsed, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return fmt.Errorf("writeOfficerCSR(%s): parse csr round-trip: %w", username, err)
	}
	if err := parsed.CheckSignature(); err != nil {
		return fmt.Errorf("writeOfficerCSR(%s): self-signature: %w", username, err)
	}
	block := &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return fmt.Errorf("writeOfficerCSR(%s): write %s: %w", username, path, err)
	}
	return nil
}

// writeOfficerCert generates an ECDSA P-256 key, self-signs an X.509
// certificate for CN=<username>, and writes the PEM to path. Used for
// the "cert" officer kind — the daemon's loadOfficerCert simply parses
// the cert; chain validation lives in a later ceremony step that the
// matrix does not exercise here.
func writeOfficerCert(path, username string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("writeOfficerCert(%s): generate key: %w", username, err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("writeOfficerCert(%s): serial: %w", username, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: username},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("writeOfficerCert(%s): create cert: %w", username, err)
	}
	block := &pem.Block{Type: "CERTIFICATE", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return fmt.Errorf("writeOfficerCert(%s): write %s: %w", username, path, err)
	}
	return nil
}

// writeOfficerPubKey generates an ECDSA P-256 key and writes the public
// component as PEM-encoded PKIX. Used for the "pubkey" officer kind
// where trust is admin-asserted (the daemon performs no further
// validation; the matrix asserts the file makes it through the loader
// without error).
func writeOfficerPubKey(path, username string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("writeOfficerPubKey(%s): generate key: %w", username, err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return fmt.Errorf("writeOfficerPubKey(%s): marshal: %w", username, err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return fmt.Errorf("writeOfficerPubKey(%s): write %s: %w", username, path, err)
	}
	return nil
}
