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

// Package main: command-handler coverage for trust.go.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Mock
// =============================================================================

// mockTrustService implements quicbaas.TrustService for command-handler tests.
type mockTrustService struct {
	daneName  string
	danePort  int
	daneCert  *x509.Certificate
	daneErr   error
	aiaChain  []*x509.Certificate
	aiaErr    error
	pool      *x509.CertPool
	poolErr   error
	bootstrap quicbaas.TrustBootstrapClient
}

func (m *mockTrustService) PublishDANE(_ context.Context, name string, port int, cert *x509.Certificate) error {
	m.daneName, m.danePort, m.daneCert = name, port, cert
	return m.daneErr
}

func (m *mockTrustService) CrossSignedRootPool(_ context.Context) (*x509.CertPool, error) {
	if m.poolErr != nil {
		return nil, m.poolErr
	}
	return m.pool, nil
}

func (m *mockTrustService) AutoImportAIA(_ context.Context, chain []*x509.Certificate) error {
	m.aiaChain = chain
	return m.aiaErr
}

func (m *mockTrustService) Bootstrap() quicbaas.TrustBootstrapClient { return m.bootstrap }

// =============================================================================
// Helpers
// =============================================================================

// withMockTrust installs a trustClientFactory returning mt.
func withMockTrust(t *testing.T, mt *mockTrustService) {
	t.Helper()
	origFactory := trustClientFactory
	origTimeout := globalTimeout
	origOutput := trustOutputFmt

	trustClientFactory = func() (quicbaas.TrustService, error) { return mt, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	trustOutputFmt = "table"

	t.Cleanup(func() {
		trustClientFactory = origFactory
		globalTimeout = origTimeout
		trustOutputFmt = origOutput
		resetTrustFlags()
	})
}

// withFailingTrustFactory installs a trustClientFactory that always fails.
func withFailingTrustFactory(t *testing.T, err error) {
	t.Helper()
	orig := trustClientFactory
	trustClientFactory = func() (quicbaas.TrustService, error) { return nil, err }
	t.Cleanup(func() { trustClientFactory = orig })
}

// resetTrustFlags clears every package-level trust flag.
func resetTrustFlags() {
	trustDANEName = ""
	trustDANEPort = 0
	trustDANECertFile = ""
	trustAIAChainFile = ""
	trustRootsOut = ""
}

// selfSignedCert mints a throwaway self-signed certificate for tests. It is
// in-memory only and never touches the host trust store.
func selfSignedCert(t *testing.T, commonName string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName, Organization: []string{"QuicBaaS Test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// certPEMFile writes the supplied certificates to a temp PEM bundle.
func certPEMFile(t *testing.T, certs ...*x509.Certificate) string {
	t.Helper()
	var buf bytes.Buffer
	for _, c := range certs {
		require.NoError(t, pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
	}
	path := filepath.Join(t.TempDir(), "chain.pem")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path
}

// =============================================================================
// Command construction
// =============================================================================

func TestNewTrustCommand_VerbTree(t *testing.T) {
	cmd := newTrustCommand()
	require.Equal(t, "trust", cmd.Use)

	got := make(map[string]bool)
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, verb := range []string{"publish-dane", "cross-signed-roots", "auto-import-aia"} {
		assert.True(t, got[verb], "trust is missing the %q verb", verb)
	}
	assert.Len(t, cmd.Commands(), 3)

	require.NotNil(t, cmd.PersistentFlags().Lookup("output"))
	assert.Contains(t, cmd.Long, "quicbaas bootstrap",
		"the help text must point operators at the canonical bootstrap tree")
	assert.Contains(t, cmd.Long, "DANE publisher",
		"the help text must surface the missing-publisher caveat")
}

// =============================================================================
// Flag validation
// =============================================================================

func TestRunTrustPublishDANE_FlagValidation(t *testing.T) {
	cert := selfSignedCert(t, "api.example.com")
	certPath := certPEMFile(t, cert)

	tests := []struct {
		name     string
		dnsName  string
		port     int
		certFile string
		wantFlag string
		wantPort bool
	}{
		{name: "missing name", port: 443, certFile: certPath, wantFlag: "name"},
		{name: "missing port", dnsName: "api.example.com", certFile: certPath, wantFlag: "port"},
		{name: "missing cert", dnsName: "api.example.com", port: 443, wantFlag: "cert"},
		{name: "port too large", dnsName: "api.example.com", port: 70000, certFile: certPath, wantPort: true},
		{name: "port negative", dnsName: "api.example.com", port: -1, certFile: certPath, wantPort: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMockTrust(t, &mockTrustService{})
			trustDANEName, trustDANEPort, trustDANECertFile = tt.dnsName, tt.port, tt.certFile

			err := runTrustPublishDANE(captureCmd(&bytes.Buffer{}), nil)
			require.Error(t, err)
			if tt.wantPort {
				var typed *TrustInvalidPortError
				require.True(t, stderrors.As(err, &typed))
				return
			}
			var typed *TrustFlagRequiredError
			require.True(t, stderrors.As(err, &typed))
			assert.Equal(t, tt.wantFlag, typed.Flag)
		})
	}
}

func TestRunTrustAutoImportAIA_RequiresChain(t *testing.T) {
	withMockTrust(t, &mockTrustService{})

	err := runTrustAutoImportAIA(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	var typed *TrustFlagRequiredError
	require.True(t, stderrors.As(err, &typed))
	assert.Equal(t, "chain", typed.Flag)
}

// =============================================================================
// Certificate input
// =============================================================================

func TestReadTrustCertificates_FileAndStdin(t *testing.T) {
	leaf := selfSignedCert(t, "leaf.example.com")
	root := selfSignedCert(t, "root.example.com")
	path := certPEMFile(t, leaf, root)

	certs, err := readTrustCertificates(&cobra.Command{}, path)
	require.NoError(t, err)
	require.Len(t, certs, 2)
	assert.Equal(t, "leaf.example.com", certs[0].Subject.CommonName)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewReader(raw))
	certs, err = readTrustCertificates(cmd, "-")
	require.NoError(t, err)
	assert.Len(t, certs, 2)
}

func TestReadTrustCertificates_Invalid(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := readTrustCertificates(&cobra.Command{}, filepath.Join(t.TempDir(), "absent.pem"))
		require.Error(t, err)
		var typed *TrustReadError
		require.True(t, stderrors.As(err, &typed))
	})

	t.Run("no certificate blocks", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "key.pem")
		require.NoError(t, os.WriteFile(path,
			[]byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), 0o600))
		_, err := readTrustCertificates(&cobra.Command{}, path)
		require.Error(t, err)
		var typed *TrustNoCertificatesError
		require.True(t, stderrors.As(err, &typed))
	})

	t.Run("corrupt certificate DER", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.pem")
		var buf bytes.Buffer
		require.NoError(t, pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x01, 0x02}}))
		require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

		_, err := readTrustCertificates(&cobra.Command{}, path)
		require.Error(t, err)
		var typed *TrustCertificateParseError
		require.True(t, stderrors.As(err, &typed))
	})
}

// =============================================================================
// Success paths
// =============================================================================

func TestRunTrustPublishDANE_Success(t *testing.T) {
	cert := selfSignedCert(t, "api.example.com")
	mt := &mockTrustService{}
	withMockTrust(t, mt)
	trustDANEName, trustDANEPort, trustDANECertFile = "api.example.com", 443, certPEMFile(t, cert)

	var buf bytes.Buffer
	require.NoError(t, runTrustPublishDANE(captureCmd(&buf), nil))

	assert.Equal(t, "api.example.com", mt.daneName)
	assert.Equal(t, 443, mt.danePort)
	require.NotNil(t, mt.daneCert)
	assert.Equal(t, cert.Raw, mt.daneCert.Raw)
	assert.Contains(t, buf.String(), "api.example.com:443")
}

func TestRunTrustAutoImportAIA_Success(t *testing.T) {
	leaf := selfSignedCert(t, "leaf.example.com")
	inter := selfSignedCert(t, "inter.example.com")
	mt := &mockTrustService{}
	withMockTrust(t, mt)
	trustAIAChainFile = certPEMFile(t, leaf, inter)

	var buf bytes.Buffer
	require.NoError(t, runTrustAutoImportAIA(captureCmd(&buf), nil))

	require.Len(t, mt.aiaChain, 2)
	assert.Contains(t, buf.String(), "2-certificate chain")
}

func TestRunTrustCrossSignedRoots_Success(t *testing.T) {
	root := selfSignedCert(t, "root.example.com")
	pool := x509.NewCertPool()
	pool.AddCert(root)

	mt := &mockTrustService{pool: pool}
	withMockTrust(t, mt)

	var buf bytes.Buffer
	require.NoError(t, runTrustCrossSignedRoots(captureCmd(&buf), nil))
	assert.Contains(t, buf.String(), "root.example.com")
}

func TestRunTrustCrossSignedRoots_WritesFile(t *testing.T) {
	root := selfSignedCert(t, "root.example.com")
	pool := x509.NewCertPool()
	pool.AddCert(root)

	withMockTrust(t, &mockTrustService{pool: pool})
	trustRootsOut = filepath.Join(t.TempDir(), "roots.txt")

	var buf bytes.Buffer
	require.NoError(t, runTrustCrossSignedRoots(captureCmd(&buf), nil))

	body, err := os.ReadFile(trustRootsOut)
	require.NoError(t, err)
	assert.Contains(t, string(body), "root.example.com")
	assert.Contains(t, buf.String(), "written to")
}

func TestRunTrustCrossSignedRoots_WriteErrorIsTyped(t *testing.T) {
	root := selfSignedCert(t, "root.example.com")
	pool := x509.NewCertPool()
	pool.AddCert(root)

	withMockTrust(t, &mockTrustService{pool: pool})
	// A directory path can never be opened for writing as a regular file.
	trustRootsOut = t.TempDir()

	err := runTrustCrossSignedRoots(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	var typed *TrustWriteError
	require.True(t, stderrors.As(err, &typed))
}

func TestCrossSignedRootSubjects_NilPool(t *testing.T) {
	assert.Nil(t, crossSignedRootSubjects(nil))
	assert.Empty(t, crossSignedRootSubjects(x509.NewCertPool()))
}

// =============================================================================
// Error propagation
// =============================================================================

func TestRunTrustPublishDANE_MissingPublisherErrorIsExplained(t *testing.T) {
	cert := selfSignedCert(t, "api.example.com")
	sentinel := stderrors.New("no DANE publisher registered")
	withMockTrust(t, &mockTrustService{daneErr: sentinel})
	trustDANEName, trustDANEPort, trustDANECertFile = "api.example.com", 443, certPEMFile(t, cert)

	err := runTrustPublishDANE(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)

	var typed *TrustDANEPublisherError
	require.True(t, stderrors.As(err, &typed))
	assert.True(t, strings.Contains(err.Error(), "go-trusted-platform"),
		"the decorated error must name where a publisher comes from; got %q", err.Error())
}

func TestRunTrust_FactoryErrorPropagates(t *testing.T) {
	cert := selfSignedCert(t, "api.example.com")
	certPath := certPEMFile(t, cert)
	sentinel := stderrors.New("no transport")

	withFailingTrustFactory(t, sentinel)
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	t.Cleanup(resetTrustFlags)

	trustDANEName, trustDANEPort, trustDANECertFile = "api.example.com", 443, certPath
	require.ErrorIs(t, runTrustPublishDANE(captureCmd(&bytes.Buffer{}), nil), sentinel)

	trustAIAChainFile = certPath
	require.ErrorIs(t, runTrustAutoImportAIA(captureCmd(&bytes.Buffer{}), nil), sentinel)

	trustRootsOut = ""
	require.ErrorIs(t, runTrustCrossSignedRoots(captureCmd(&bytes.Buffer{}), nil), sentinel)
}

func TestRunTrust_ServerErrorPropagates(t *testing.T) {
	cert := selfSignedCert(t, "leaf.example.com")
	sentinel := stderrors.New("trust boom")
	withMockTrust(t, &mockTrustService{aiaErr: sentinel, poolErr: sentinel})

	trustAIAChainFile = certPEMFile(t, cert)
	require.ErrorIs(t, runTrustAutoImportAIA(captureCmd(&bytes.Buffer{}), nil), sentinel)
	require.ErrorIs(t, runTrustCrossSignedRoots(captureCmd(&bytes.Buffer{}), nil), sentinel)
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatTrustRoots_Formats(t *testing.T) {
	subjects := []string{"CN=root-a", "CN=root-b"}
	for _, format := range []string{"table", "json", "yaml", ""} {
		t.Run("format="+format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatTrustRoots(subjects, format, &buf))
			assert.Contains(t, buf.String(), "root-a")
		})
	}
}

func TestFormatTrustRoots_EmptyAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatTrustRoots(nil, "table", &buf))
	assert.Contains(t, buf.String(), "No cross-signed roots")

	buf.Reset()
	require.Error(t, formatTrustRoots(nil, "toml", &buf))
}

func TestFormatTrustAck_BadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.Error(t, formatTrustAck(map[string]interface{}{"ok": true}, "done\n", "toml", &buf))
}
