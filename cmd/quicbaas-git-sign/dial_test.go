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

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/gitsign"
)

// sessionConfig is a signing session whose certificate files live in dir.
func sessionConfig(dir, mcpURL string) *gitsign.Config {
	return &gitsign.Config{
		MCPURL:   mcpURL,
		CertFile: filepath.Join(dir, "session.crt"),
		KeyFile:  filepath.Join(dir, "session.key"),
		CAFile:   filepath.Join(dir, "ca.crt"),
		Key:      gitsign.KeyRef{Name: "project-signing", Scope: "tenant", Owner: "acme"},
	}
}

// writeSessionCertificate writes a self-signed client certificate, its key and
// itself as the CA to the files cfg names.
func writeSessionCertificate(t *testing.T, cfg *gitsign.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "agent-session", Organization: []string{"acme"}},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	files := map[string][]byte{
		cfg.CertFile: certPEM,
		cfg.CAFile:   certPEM,
		cfg.KeyFile:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func TestMCPSignerRefusesMissingCertificate(t *testing.T) {
	cfg := sessionConfig(t.TempDir(), "https://quicbaas.internal:8443/mcp")
	signer, conn, err := mcpSigner(cfg)(context.Background())
	var invalid *gitsign.InvalidConfigError
	if !errors.As(err, &invalid) || signer != nil || conn != nil {
		t.Fatalf("open = %v, %v, %v; want an *InvalidConfigError alone", signer, conn, err)
	}
	if invalid.Variable != orchestration.MCPCertEnvVar {
		t.Fatalf("error names %q, want %q", invalid.Variable, orchestration.MCPCertEnvVar)
	}
}

func TestMCPSignerReportsUnreachableServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	cfg := sessionConfig(t.TempDir(), "https://"+address+"/mcp")
	writeSessionCertificate(t, cfg)
	signer, conn, err := mcpSigner(cfg)(context.Background())
	var signErr *gitsign.SignError
	if !errors.As(err, &signErr) || signer != nil || conn != nil {
		t.Fatalf("open = %v, %v, %v; want a *gitsign.SignError alone", signer, conn, err)
	}
}
