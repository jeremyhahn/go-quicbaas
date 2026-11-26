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

//go:build integration

package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSelfSignedPEM writes a self-signed CA certificate that names no
// issuer URL to a PEM file under t.TempDir and returns its path.
func writeSelfSignedPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate key")

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	require.NoError(t, err, "generate serial")

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "trust-e2e-root"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err, "create certificate")

	path := filepath.Join(t.TempDir(), "trust-e2e-root.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600),
		"write certificate PEM")
	return path
}

// TestTrustCrossSignedRoots: the cross-signed root pool is served on every
// transport and always holds the CA's native root.
func TestTrustCrossSignedRoots(t *testing.T) {
	RunForAllTransports(t, "TrustCrossSignedRoots", func(t *testing.T, runner *TestRunner) {
		t.Run("table", func(t *testing.T) {
			stdout, _, err := runner.Run(TrustCrossSignedRootsCommand())
			require.NoError(t, err, "trust cross-signed-roots should succeed")
			assert.Contains(t, stdout, "SUBJECT", "table output should have its header")
			assert.NotContains(t, stdout, "No cross-signed roots", "the pool holds the native root")
		})

		t.Run("json", func(t *testing.T) {
			stdout, _, err := runner.Run(TrustCrossSignedRootsJSONCommand())
			require.NoError(t, err, "trust cross-signed-roots (json) should succeed")

			var roots struct {
				Count    int      `json:"count"`
				Subjects []string `json:"subjects"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &roots), "cross-signed-roots json should parse")
			require.GreaterOrEqual(t, roots.Count, 1, "the pool holds the native root")
			require.Len(t, roots.Subjects, roots.Count, "one subject per root")
			assert.NotEmpty(t, roots.Subjects[0], "the native root has a subject")
		})
	})
}

// TestTrustAutoImportAIA: a chain whose certificate names no issuer URL is
// accepted on every transport and imports nothing from the network. go-xkms's
// ImportIssuingCAs returns nil for a self-signed certificate or one with no
// AIA URL, so the call succeeds.
func TestTrustAutoImportAIA(t *testing.T) {
	RunForAllTransports(t, "TrustAutoImportAIA", func(t *testing.T, runner *TestRunner) {
		stdout, _, err := runner.Run(TrustAutoImportAIACommand(writeSelfSignedPEM(t)))
		require.NoError(t, err, "trust auto-import-aia should succeed")

		var ack struct {
			ChainLength int  `json:"chain_length"`
			Imported    bool `json:"imported"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &ack), "auto-import-aia json should parse")
		assert.Equal(t, 1, ack.ChainLength, "the chain holds one certificate")
		assert.True(t, ack.Imported, "the import is acknowledged")
	})
}

// TestTrustPublishDANEWithoutPublisher: with no DANE publisher registered,
// publish-dane reaches the service on every transport and is refused as not
// configured, not as an unknown route or an auth failure.
func TestTrustPublishDANEWithoutPublisher(t *testing.T) {
	RunForAllTransports(t, "TrustPublishDANEWithoutPublisher", func(t *testing.T, runner *TestRunner) {
		stdout, stderr, err := runner.Run(TrustPublishDANECommand("trust-e2e.example", 443, writeSelfSignedPEM(t)))
		require.Error(t, err, "publish-dane without a publisher should fail")

		combined := stdout + stderr
		require.Contains(t, combined, "publisher registered on the server",
			"the refusal should be the not-configured one")
		for _, gap := range []string{
			"unknown service",
			"Unimplemented",
			"404 page not found",
			"HTTP 404",
			"PermissionDenied",
			"permission denied",
			"HTTP 401",
			"HTTP 403",
			"authentication required",
		} {
			assert.NotContains(t, combined, gap, "the refusal should not be a route or auth gap")
		}
	})
}

// TestTrustErrorSuite: the CLI's own flag checks refuse before any call.
func TestTrustErrorSuite(t *testing.T) {
	RunForAllTransports(t, "TrustErrorSuite", func(t *testing.T, runner *TestRunner) {
		t.Run("auto-import-aia missing --chain", func(t *testing.T) {
			stdout, stderr, err := runner.Run(TrustAutoImportAIAMissingChainCommand())
			require.Error(t, err, "auto-import-aia without --chain should fail")
			assert.Contains(t, stdout+stderr, "--chain is required")
		})

		t.Run("publish-dane bad --port", func(t *testing.T) {
			stdout, stderr, err := runner.Run(TrustPublishDANEBadPortCommand(writeSelfSignedPEM(t)))
			require.Error(t, err, "publish-dane with --port 70000 should fail")
			assert.Contains(t, stdout+stderr, "out of range")
		})
	})
}
