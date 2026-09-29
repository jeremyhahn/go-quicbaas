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

package agentcert

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrite_ProducesAUsableClientIdentity verifies the three files load as an
// mTLS key pair whose certificate chains to the CA for client authentication,
// with the key private to its owner.
func TestWrite_ProducesAUsableClientIdentity(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, caPath := Write(t, dir)

	assert.Equal(t, filepath.Join(dir, CertFileName), certPath)
	assert.Equal(t, filepath.Join(dir, KeyFileName), keyPath)
	assert.Equal(t, filepath.Join(dir, CAFileName), caPath)

	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"agents"}, leaf.Subject.OrganizationalUnit)
	assert.Equal(t, []string{"tenant-test"}, leaf.Subject.Organization)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)

	caPEM, err := os.ReadFile(caPath)
	require.NoError(t, err)
	block, _ := pem.Decode(caPEM)
	require.NotNil(t, block)
	ca, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	require.NoError(t, err)

	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// fatalRecorder is a testing.TB whose Fatalf records the message and stops
// the calling goroutine, as testing.T does, without failing the outer test.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// TestWrite_UnwritableDirFailsTheTest verifies a directory that cannot be
// written fails the test through tb.Fatalf rather than returning paths to
// files that do not exist.
func TestWrite_UnwritableDirFailsTheTest(t *testing.T) {
	rec := &fatalRecorder{TB: t}
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	done := make(chan struct{})
	go func() {
		defer close(done)
		Write(rec, missing)
	}()
	<-done

	assert.Contains(t, rec.msg, "agentcert: write")
	assert.Contains(t, rec.msg, CertFileName)
}
