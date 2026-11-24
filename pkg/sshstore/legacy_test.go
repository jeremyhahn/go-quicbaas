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

package sshstore

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	sshscope "github.com/jeremyhahn/go-xkms/pkg/ssh/scope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// testKey is a key pair generated for a legacy fixture.
type testKey struct {
	pub  gossh.PublicKey
	line string // authorized_keys line, no newline
	pem  []byte // OpenSSH PEM of the private key
}

func newTestKey(t *testing.T, priv crypto.PrivateKey) testKey {
	t.Helper()
	signer, err := gossh.NewSignerFromKey(priv)
	require.NoError(t, err)
	block, err := gossh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	pub := signer.PublicKey()
	return testKey{
		pub:  pub,
		line: strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub))),
		pem:  pem.EncodeToMemory(block),
	}
}

func newEd25519TestKey(t *testing.T) testKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return newTestKey(t, priv)
}

// legacyKeyJSON is a key record exactly as the legacy file store wrote it:
// the v2 key envelope, with the PEM as a plain JSON string.
func legacyKeyJSON(t *testing.T, sc sshscope.Scope, owner, name, algo string, k testKey) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"v":       2,
		"scope":   string(sc),
		"owner":   owner,
		"name":    name,
		"algo":    algo,
		"fp":      gossh.FingerprintSHA256(k.pub),
		"created": time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
		"comment": name + "@legacy",
		"pub":     k.line,
		"pem_b64": string(k.pem),
	})
	require.NoError(t, err)
	return data
}

// legacyPinJSON is a host pin record in the legacy v2 pin envelope.
func legacyPinJSON(t *testing.T, sc sshscope.Scope, owner, host string, pub gossh.PublicKey) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"v":      2,
		"scope":  string(sc),
		"owner":  owner,
		"host":   host,
		"fp":     gossh.FingerprintSHA256(pub),
		"algo":   pub.Type(),
		"added":  time.Date(2025, 3, 2, 12, 0, 0, 0, time.UTC),
		"source": "manual",
		"pub":    base64.StdEncoding.EncodeToString(pub.Marshal()),
	})
	require.NoError(t, err)
	return data
}

// legacyAuthorizedJSON is an authorized-keys record in the legacy v1
// envelope.
func legacyAuthorizedJSON(t *testing.T, sc sshscope.Scope, owner, name, line string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"v":     1,
		"scope": string(sc),
		"owner": owner,
		"name":  name,
		"added": time.Date(2025, 3, 3, 12, 0, 0, 0, time.UTC),
		"line":  line,
	})
	require.NoError(t, err)
	return data
}

func TestDecodeLegacyKey_V049Envelope(t *testing.T) {
	k := newEd25519TestKey(t)

	got, err := decodeLegacyKey(legacyKeyJSON(t, sshscope.User, "alice", "fleet-deploy", "ed25519", k))
	require.NoError(t, err)

	assert.Equal(t, sshscope.User, got.Scope)
	assert.Equal(t, "alice", got.Owner)
	assert.Equal(t, "fleet-deploy", got.Name)
	assert.Equal(t, "ed25519", got.Algorithm)
	assert.Equal(t, gossh.FingerprintSHA256(k.pub), got.Fingerprint)
	assert.Equal(t, "fleet-deploy@legacy", got.Comment)
	assert.True(t, got.CreatedAt.Equal(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)))
	require.NotNil(t, got.PublicKey)
	assert.Equal(t, k.pub.Marshal(), got.PublicKey.Marshal())
	assert.Equal(t, k.pem, got.PEM)
	assert.Empty(t, got.BackendID)
}

func TestDecodeLegacyKey_BackendBacked(t *testing.T) {
	k := newEd25519TestKey(t)
	data, err := json.Marshal(map[string]any{
		"v": 2, "scope": "service", "owner": "ci", "name": "hsm", "algo": "ed25519",
		"fp": gossh.FingerprintSHA256(k.pub), "pub": k.line,
		"backend_id": "pkcs11", "backend_key_id": "service/ci/hsm",
	})
	require.NoError(t, err)

	got, err := decodeLegacyKey(data)
	require.NoError(t, err)
	assert.Equal(t, sshscope.Service, got.Scope)
	assert.Equal(t, "pkcs11", got.BackendID)
	assert.Equal(t, "service/ci/hsm", got.BackendKeyID)
	assert.Empty(t, got.PEM)
}

func TestDecodeLegacyKey_Rejects(t *testing.T) {
	k := newEd25519TestKey(t)
	valid := func(mutate func(map[string]any)) []byte {
		env := map[string]any{
			"v": 2, "scope": "user", "owner": "alice", "name": "k", "algo": "ed25519",
			"fp": gossh.FingerprintSHA256(k.pub), "pub": k.line, "pem_b64": string(k.pem),
		}
		mutate(env)
		data, err := json.Marshal(env)
		require.NoError(t, err)
		return data
	}
	tests := []struct {
		name string
		data []byte
	}{
		{name: "not json", data: []byte("not an envelope")},
		{name: "bad scope", data: valid(func(e map[string]any) { e["scope"] = "planet" })},
		{name: "bad public key", data: valid(func(e map[string]any) { e["pub"] = "ssh-ed25519 AAAA" })},
		{name: "no material", data: valid(func(e map[string]any) { delete(e, "pem_b64") })},
		{name: "bad material", data: valid(func(e map[string]any) { e["pem_b64"] = "-----BEGIN NOTHING-----" })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeLegacyKey(tt.data)
			require.Error(t, err)
		})
	}
}

func TestDecodeLegacyPin_V049Envelope(t *testing.T) {
	k := newEd25519TestKey(t)

	got, err := decodeLegacyPin(legacyPinJSON(t, sshscope.User, "alice", "git.example.com", k.pub))
	require.NoError(t, err)
	assert.Equal(t, sshscope.User, got.Scope)
	assert.Equal(t, "alice", got.Owner)
	assert.Equal(t, "git.example.com", got.Host)
	assert.Equal(t, gossh.FingerprintSHA256(k.pub), got.Fingerprint)
	assert.Equal(t, k.pub.Type(), got.Algorithm)
	assert.Equal(t, "manual", got.Source)
	assert.Equal(t, k.pub.Marshal(), got.PublicKey)

	_, err = decodeLegacyPin([]byte(`{"v":1,"scope":"user","owner":"alice","host":"h","fp":"SHA256:x"}`))
	require.Error(t, err, "a pin without a public key cannot be migrated")
}

func TestDecodeLegacyAuthorized_V049Envelope(t *testing.T) {
	k := newEd25519TestKey(t)

	got, err := decodeLegacyAuthorized(legacyAuthorizedJSON(t, sshscope.User, "alice", "laptop", k.line))
	require.NoError(t, err)
	assert.Equal(t, sshscope.User, got.Scope)
	assert.Equal(t, "alice", got.Owner)
	assert.Equal(t, "laptop", got.Name)
	assert.Equal(t, k.line, got.Line)

	_, err = decodeLegacyAuthorized(legacyAuthorizedJSON(t, sshscope.User, "alice", "laptop", "garbage"))
	require.Error(t, err)
}
