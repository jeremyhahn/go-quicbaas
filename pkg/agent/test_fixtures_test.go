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

package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestAgentIdentity emits a minimal agent.crt / agent.key / server-ca.crt
// set into the given directory. The CA is self-signed and the agent cert
// is issued under it with the "agents" OU stamp and ClientAuth EKU — the
// same shape the production CertIssuer produces. All material lives in
// tempDir and is cleaned up by t.TempDir() teardown.
//
// Returns absolute paths to (agentCert, agentKey, serverCA) ready for
// use in agent.Config fields.
func writeTestAgentIdentity(t *testing.T, tempDir string) (certPath, keyPath, caPath string) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "test-ca",
			Organization: []string{"test"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}

	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate agent key: %v", err)
	}
	agentTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName:         "agent-test",
			Organization:       []string{"tenant-test"},
			OrganizationalUnit: []string{"agents"},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca cert: %v", err)
	}
	agentDER, err := x509.CreateCertificate(rand.Reader, agentTmpl, caCert, &agentKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create agent cert: %v", err)
	}

	agentKeyDER, err := x509.MarshalPKCS8PrivateKey(agentKey)
	if err != nil {
		t.Fatalf("marshal agent key: %v", err)
	}

	certPath = filepath.Join(tempDir, "agent.crt")
	keyPath = filepath.Join(tempDir, "agent.key")
	caPath = filepath.Join(tempDir, "server-ca.crt")

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: agentDER}), 0o644); err != nil {
		t.Fatalf("write agent cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: agentKeyDER}), 0o600); err != nil {
		t.Fatalf("write agent key: %v", err)
	}
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return certPath, keyPath, caPath
}
