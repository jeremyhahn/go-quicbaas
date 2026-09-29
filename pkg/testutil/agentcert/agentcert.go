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

// Package agentcert writes the certificate material a fleet node daemon's
// configuration names: an mTLS client certificate, its private key, and the
// CA that verifies the QuicBaaS server. Enrollment is certificate-based, so
// every agent.Config a test builds needs these three files on disk.
//
// Nothing here creates a key or signs a certificate itself: go-xkms owns both
// for the whole stack (`make check-local-keygen`). The CA is a real in-memory
// go-xkms CA from pkg/testutil/xkmsca, the agent key comes from
// pkg/xkms.GenerateRequesterKey, the CSR from go-xkms CreateCSRWithKey, the
// signature from the CA's SignCSR and the key encoding from go-xkms
// EncodePrivateKeyPEM -- the steps the production XKMSFleetNodeCertIssuer
// (pkg/service) takes. That issuer cannot be called from here: pkg/service
// imports pkg/fleet, whose own tests use this package.
package agentcert

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// File names Write creates inside the directory it is given.
const (
	CertFileName = "agent.crt"
	KeyFileName  = "agent.key"
	CAFileName   = "server-ca.crt"
)

// Subject values of the issued agent certificate.
const (
	caCommonName    = "agentcert-test-ca"
	agentCommonName = "agent-test"
	agentTenant     = "tenant-test"
	agentOU         = "agents"
)

// Write emits agent.crt, agent.key and server-ca.crt into dir and returns
// their paths, ready for agent.Config's NodeCertFile, NodeKeyFile and
// ServerCAFile. A fresh in-memory go-xkms CA issues the agent certificate
// from a CSR, with the "agents" OU and exactly the ClientAuth EKU, the shape
// the production fleet certificate issuer produces. The key is written 0600,
// as agent.Config requires. Any failure fails the test.
func Write(tb testing.TB, dir string) (certPath, keyPath, caPath string) {
	tb.Helper()
	ctx := context.Background()

	ca, err := xkmsca.Open(ctx, xkmsca.Config{CommonName: caCommonName})
	if err != nil {
		tb.Fatalf("agentcert: open ca: %v", err)
	}
	caCert, err := ca.CACertificate()
	if err != nil {
		tb.Fatalf("agentcert: ca certificate: %v", err)
	}

	signer, keyAlgorithm, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		tb.Fatalf("agentcert: generate agent key: %v", err)
	}
	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:         agentCommonName,
			Organization:       agentTenant,
			OrganizationalUnit: agentOU,
		},
	}, signer)
	if err != nil {
		tb.Fatalf("agentcert: create agent csr: %v", err)
	}
	agentCert, err := ca.SignCSR(ctx, csrPEM, &xkmssdk.TrustedCASignOptions{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		tb.Fatalf("agentcert: sign agent csr: %v", err)
	}
	keyPEM, err := xkmssdk.EncodePrivateKeyPEM(signer, keyAlgorithm, nil)
	if err != nil {
		tb.Fatalf("agentcert: encode agent key: %v", err)
	}

	certPath = filepath.Join(dir, CertFileName)
	keyPath = filepath.Join(dir, KeyFileName)
	caPath = filepath.Join(dir, CAFileName)

	writeFile(tb, certPath, encodeCertificate(agentCert), 0o644)
	writeFile(tb, keyPath, keyPEM, 0o600)
	writeFile(tb, caPath, encodeCertificate(caCert), 0o644)
	return certPath, keyPath, caPath
}

// encodeCertificate returns cert as a CERTIFICATE PEM block.
func encodeCertificate(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// writeFile writes data to path with the given mode.
func writeFile(tb testing.TB, path string, data []byte, mode os.FileMode) {
	tb.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		tb.Fatalf("agentcert: write %s: %v", path, err)
	}
}
