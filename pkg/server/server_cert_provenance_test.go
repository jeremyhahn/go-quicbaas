package server

import (
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
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// bootedProvenanceServer boots a server with no TLS path configured, so its
// CA issues and stores the server certificate in the default certs dir.
func bootedProvenanceServer(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initializeCA())
	return srv, cfg
}

// copyFile copies src to a new file in dir and returns its path.
func copyFile(t *testing.T, src, dir string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	dst := filepath.Join(dir, filepath.Base(src))
	require.NoError(t, os.WriteFile(dst, data, 0600))
	return dst
}

// writeOperatorPair writes a self-signed cert and key, from no CA the
// server knows, into dir.
func writeOperatorPair(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certFile = filepath.Join(dir, "operator.crt")
	keyFile = filepath.Join(dir, "operator.key")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
	return certFile, keyFile
}

// reboot re-runs initializeCA with the given configured paths.
func reboot(srv *Server, certFile, keyFile string) error {
	srv.config.Security.TLSCertFile = certFile
	srv.config.Security.TLSKeyFile = keyFile
	srv.config.Security.TLSCAFile = ""
	return srv.initializeCA()
}

func TestServerCertProvenance_SelfIssuedAtConfiguredPathIsReissued(t *testing.T) {
	srv, cfg := bootedProvenanceServer(t)
	first := diskLeaf(t, cfg.Security.TLSCertFile)
	dir := t.TempDir()
	certFile := copyFile(t, cfg.Security.TLSCertFile, dir)
	keyFile := copyFile(t, cfg.Security.TLSKeyFile, dir)

	added := net.ParseIP("192.0.2.20")
	cfg.Server.AdditionalSANIPs = []string{"192.0.2.20"}
	require.NoError(t, reboot(srv, certFile, keyFile))

	served := servedLeaf(t, srv.tlsConfig)
	assert.True(t, slices.ContainsFunc(served.IPAddresses, added.Equal), "served cert carries the added IP")
	written := diskLeaf(t, certFile)
	assert.True(t, slices.ContainsFunc(written.IPAddresses, added.Equal), "configured cert path carries the re-issued cert")
	assert.Equal(t, served.Raw, written.Raw)
	assert.NotEqual(t, first.SerialNumber, served.SerialNumber)
	assert.Equal(t, certFile, srv.config.Security.TLSCertFile, "the configured path is kept")
}

func TestServerCertProvenance_OperatorCertLoadsUnchanged(t *testing.T) {
	srv, cfg := bootedProvenanceServer(t)
	certFile, keyFile := writeOperatorPair(t, t.TempDir())
	before, err := os.ReadFile(certFile)
	require.NoError(t, err)

	cfg.Server.AdditionalSANIPs = []string{"192.0.2.21"}
	require.NoError(t, reboot(srv, certFile, keyFile))

	assert.Equal(t, diskLeaf(t, certFile).Raw, servedLeaf(t, srv.tlsConfig).Raw, "the operator cert is served as given")
	after, err := os.ReadFile(certFile)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the operator cert file is not rewritten")
}

func TestServerCertProvenance_StaleStoredCertIsOperators(t *testing.T) {
	srv, cfg := bootedProvenanceServer(t)
	dir := t.TempDir()
	certFile := copyFile(t, cfg.Security.TLSCertFile, dir)
	keyFile := copyFile(t, cfg.Security.TLSKeyFile, dir)
	before, err := os.ReadFile(certFile)
	require.NoError(t, err)

	// Supersede the stored cert, so the copy chains to this CA but is no
	// longer the cert it stores for the CN.
	cfg.Server.AdditionalSANIPs = []string{"192.0.2.22"}
	restartCA(t, srv)
	cfg.Server.AdditionalSANIPs = []string{"192.0.2.22", "192.0.2.23"}
	require.NoError(t, reboot(srv, certFile, keyFile))

	assert.Equal(t, diskLeaf(t, certFile).Raw, servedLeaf(t, srv.tlsConfig).Raw)
	after, err := os.ReadFile(certFile)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a cert the CA no longer stores is not rewritten")
}

func TestServerCertProvenance_UnsetOrAbsentPathIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		cert    bool // configure the cert path
		key     bool // configure the key path
		certOn  bool // a file exists at the cert path
		keyOn   bool // a file exists at the key path
		problem ServerCertPathProblem
		keys    []string
	}{
		{"both absent", true, true, false, false, ServerCertPathAbsent, []string{tlsCertFileKey, tlsKeyFileKey}},
		{"cert absent", true, true, false, true, ServerCertPathAbsent, []string{tlsCertFileKey}},
		{"key absent", true, true, true, false, ServerCertPathAbsent, []string{tlsKeyFileKey}},
		{"only cert configured", true, false, true, false, ServerCertPathUnset, []string{tlsKeyFileKey}},
		{"only key configured", false, true, false, true, ServerCertPathUnset, []string{tlsCertFileKey}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := bootedProvenanceServer(t)
			dir := t.TempDir()
			opCert, opKey := writeOperatorPair(t, t.TempDir())
			certFile, keyFile := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
			if tc.certOn {
				certFile = copyFile(t, opCert, dir)
			}
			if tc.keyOn {
				keyFile = copyFile(t, opKey, dir)
			}
			existing := map[string][]byte{}
			for _, p := range []string{certFile, keyFile} {
				if data, err := os.ReadFile(p); err == nil {
					existing[p] = data
				}
			}
			if !tc.cert {
				certFile = ""
			}
			if !tc.key {
				keyFile = ""
			}

			err := reboot(srv, certFile, keyFile)

			var pathErr *ServerCertPathError
			require.True(t, errors.As(err, &pathErr), "got %v", err)
			assert.ErrorIs(t, err, ErrServerCertPath)
			assert.Equal(t, tc.problem, pathErr.Problem)
			assert.Equal(t, tc.keys, pathErr.Keys)
			assert.Equal(t, certFile, pathErr.CertFile)
			assert.Equal(t, keyFile, pathErr.KeyFile)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, len(existing), "no file is created")
			for p, data := range existing {
				now, err := os.ReadFile(p)
				require.NoError(t, err)
				assert.Equal(t, data, now, "%s stays byte-identical", p)
			}
		})
	}
}

func TestServerCertPathError_NamesPathsAndProblem(t *testing.T) {
	err := &ServerCertPathError{
		CertFile: "/etc/x/server.crt",
		Problem:  ServerCertPathUnset,
		Keys:     []string{tlsKeyFileKey},
	}
	msg := err.Error()
	assert.Contains(t, msg, "security.tls_key_file unset")
	assert.Contains(t, msg, `security.tls_cert_file="/etc/x/server.crt"`)
	assert.Contains(t, msg, `security.tls_key_file=""`)
	assert.ErrorIs(t, err, ErrServerCertPath)
}

func TestServerCertProvenance_UnconfiguredAndUnreadablePair(t *testing.T) {
	source, err := serverCertProvenance(nil, "", "", "localhost")
	require.NoError(t, err)
	assert.Equal(t, serverCertUnconfigured, source)

	// Files that are not a key pair are not this server's: the operator
	// path reports the load error.
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "c"), filepath.Join(dir, "k")
	require.NoError(t, os.WriteFile(certFile, []byte("not pem"), 0600))
	require.NoError(t, os.WriteFile(keyFile, []byte("not pem"), 0600))
	source, err = serverCertProvenance(nil, certFile, keyFile, "localhost")
	require.NoError(t, err)
	assert.Equal(t, serverCertOperator, source)
}
