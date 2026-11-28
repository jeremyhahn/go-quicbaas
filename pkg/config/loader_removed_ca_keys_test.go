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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfigFile writes body as a quicbaas.yaml in a fresh temp dir.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// requireRefusedForTLSCAFile asserts err is the RemovedKeyError for key and
// that it sends the operator to security.tls_ca_file.
func requireRefusedForTLSCAFile(t *testing.T, err error, key string) {
	t.Helper()
	var removed *RemovedKeyError
	require.True(t, errors.As(err, &removed), "%v", err)
	assert.Equal(t, key, removed.Key)
	assert.Equal(t, "security.tls_ca_file", removed.Replacement)
	assert.Contains(t, err.Error(), key)
	assert.Contains(t, err.Error(), "security.tls_ca_file")
}

// TestLoad_RefusesPIVLoginCAFile: iam.piv_login.ca_file is refused at load,
// never accepted and ignored (go-quicbaas#1318).
func TestLoad_RefusesPIVLoginCAFile(t *testing.T) {
	path := writeConfigFile(t, "iam:\n  piv_login:\n    enabled: true\n    ca_file: /etc/quicbaas/piv-ca.pem\n")

	_, err := NewLoader().Load(path)

	requireRefusedForTLSCAFile(t, err, "iam.piv_login.ca_file")
}

// TestLoad_RefusesTLSClientAuthCAFile: iam.mtls.tls_client_auth_ca_file is
// refused at load, never accepted and ignored (go-quicbaas#1318).
func TestLoad_RefusesTLSClientAuthCAFile(t *testing.T) {
	path := writeConfigFile(t, "iam:\n  mtls:\n    tls_client_auth_ca_file: /etc/quicbaas/client-ca.pem\n")

	_, err := NewLoader().Load(path)

	requireRefusedForTLSCAFile(t, err, "iam.mtls.tls_client_auth_ca_file")
}

// TestLoad_AcceptsTLSCAFileAlone: the replacement key loads.
func TestLoad_AcceptsTLSCAFileAlone(t *testing.T) {
	path := writeConfigFile(t, "security:\n  tls_ca_file: /etc/quicbaas/ca.pem\n")

	cfg, err := NewLoader().Load(path)

	require.NoError(t, err)
	assert.Equal(t, "/etc/quicbaas/ca.pem", cfg.Security.TLSCAFile)
}
