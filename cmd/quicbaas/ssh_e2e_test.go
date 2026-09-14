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

// Package main: hermetic end-to-end coverage of the runSSH* handlers.
//
// The handlers call sshClientFactory, the same seam production uses to reach
// quicbaas.SDK().Security().SSH(). Here the seam is pointed at a file-backed
// go-xkms store under a t.TempDir() so the command bodies (flag parsing,
// defaulting, output formatting, error mapping) run end to end without a
// live quicbaasd. Production code never constructs a go-xkms client — that
// is asserted separately in ssh_client_test.go.
package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// withSSHHome points the CLI's SSH client factory at a fresh file-backed
// store under a t.TempDir().
func withSSHHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	root := t.TempDir()
	original := sshClientFactory
	sshClientFactory = func() (sshSDK.Client, error) {
		return xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: root})
	}
	t.Cleanup(func() { sshClientFactory = original })
}

// =============================================================================
// keys generate + list + get + delete + export
// =============================================================================

func TestRunSSHKeysGenerate_Ed25519(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysGenerateCmd()
	require.NoError(t, cmd.Flags().Set("name", "k1"))
	require.NoError(t, cmd.Flags().Set("type", "ed25519"))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKeysGenerate(cmd, nil))
	assert.Contains(t, buf.String(), "k1")
}

func TestRunSSHKeysGenerate_BadAlgorithm(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysGenerateCmd()
	require.NoError(t, cmd.Flags().Set("name", "k1"))
	require.NoError(t, cmd.Flags().Set("type", "bogus"))
	err := runSSHKeysGenerate(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHInvalidKeyType)
}

func TestRunSSHKeysGenerate_MissingName(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysGenerateCmd()
	err := runSSHKeysGenerate(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingKeyName)
}

func TestRunSSHKeysList_Empty(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysListCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKeysList(cmd, nil))
	assert.Contains(t, buf.String(), "No SSH keys found")
}

func TestRunSSHKeysList_AfterGenerate(t *testing.T) {
	withSSHHome(t)

	gen := newSSHKeysGenerateCmd()
	require.NoError(t, gen.Flags().Set("name", "k1"))
	gen.SetOut(&bytes.Buffer{})
	require.NoError(t, runSSHKeysGenerate(gen, nil))

	cmd := newSSHKeysListCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKeysList(cmd, nil))
	assert.Contains(t, buf.String(), "k1")
}

func TestRunSSHKeysGet_RoundTrip(t *testing.T) {
	withSSHHome(t)

	gen := newSSHKeysGenerateCmd()
	require.NoError(t, gen.Flags().Set("name", "k1"))
	gen.SetOut(&bytes.Buffer{})
	require.NoError(t, runSSHKeysGenerate(gen, nil))

	cmd := newSSHKeysGetCmd()
	require.NoError(t, cmd.Flags().Set("name", "k1"))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKeysGet(cmd, nil))
	assert.Contains(t, buf.String(), "k1")
}

func TestRunSSHKeysGet_MissingName(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysGetCmd()
	err := runSSHKeysGet(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingKeyName)
}

func TestRunSSHKeysDelete_RoundTrip(t *testing.T) {
	withSSHHome(t)

	gen := newSSHKeysGenerateCmd()
	require.NoError(t, gen.Flags().Set("name", "k1"))
	gen.SetOut(&bytes.Buffer{})
	require.NoError(t, runSSHKeysGenerate(gen, nil))

	cmd := newSSHKeysDeleteCmd()
	require.NoError(t, cmd.Flags().Set("name", "k1"))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKeysDelete(cmd, nil))
	assert.Contains(t, buf.String(), "Deleted")
}

func TestRunSSHKeysExport_RoundTrip(t *testing.T) {
	withSSHHome(t)

	gen := newSSHKeysGenerateCmd()
	require.NoError(t, gen.Flags().Set("name", "k1"))
	gen.SetOut(&bytes.Buffer{})
	require.NoError(t, runSSHKeysGenerate(gen, nil))

	cmd := newSSHKeysExportCmd()
	require.NoError(t, cmd.Flags().Set("name", "k1"))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKeysExport(cmd, nil))
	assert.Contains(t, buf.String(), "ssh-")
}

// =============================================================================
// runSSHKeysImport — empty --from-file + missing file
// =============================================================================

func TestRunSSHKeysImport_MissingFromFile(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysImportCmd()
	err := runSSHKeysImport(cmd, nil)
	require.Error(t, err)
	var typed *SSHReadPEMError
	require.ErrorAs(t, err, &typed)
	assert.Contains(t, typed.Err.Error(), "--from-file is required")
}

func TestRunSSHKeysImport_MissingFile(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKeysImportCmd()
	require.NoError(t, cmd.Flags().Set("from-file", "/no/such/key.pem"))
	err := runSSHKeysImport(cmd, nil)
	require.Error(t, err)
	var typed *SSHReadPEMError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "/no/such/key.pem", typed.Path)
}

// =============================================================================
// known-hosts: validation errors + happy paths
// =============================================================================

func TestRunSSHKnownHostsAdd_MissingHost(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKnownHostsAddCmd()
	require.NoError(t, cmd.Flags().Set("fingerprint", testKnownHostFingerprint))
	err := runSSHKnownHostsAdd(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingHost)
}

func TestRunSSHKnownHostsAdd_MissingFingerprint(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKnownHostsAddCmd()
	require.NoError(t, cmd.Flags().Set("host", "git.example.com:22"))
	err := runSSHKnownHostsAdd(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingFingerprint)
}

// testKnownHostFingerprint is the OpenSSH SHA256 fingerprint of a real
// deterministic ed25519 key, so the pin this CLI writes describes a key
// that actually exists. (go-xkms known_hosts.ValidateFingerprint also
// rejects anything whose base64 body is not a 32-byte digest, so a
// placeholder such as "SHA256:abcdef0123" never reaches the store.)
const testKnownHostFingerprint = sshfixture.HostFingerprint

func TestRunSSHKnownHosts_RoundTrip(t *testing.T) {
	withSSHHome(t)

	add := newSSHKnownHostsAddCmd()
	require.NoError(t, add.Flags().Set("host", "git.example.com:22"))
	require.NoError(t, add.Flags().Set("fingerprint", testKnownHostFingerprint))
	require.NoError(t, add.Flags().Set("algorithm", "ssh-ed25519"))
	require.NoError(t, add.Flags().Set("source", "manual"))
	var aBuf bytes.Buffer
	add.SetOut(&aBuf)
	require.NoError(t, runSSHKnownHostsAdd(add, nil))
	assert.Contains(t, aBuf.String(), "Pinned")

	lst := newSSHKnownHostsListCmd()
	var lBuf bytes.Buffer
	lst.SetOut(&lBuf)
	require.NoError(t, runSSHKnownHostsList(lst, nil))
	assert.Contains(t, lBuf.String(), "git.example.com")

	rem := newSSHKnownHostsRemoveCmd()
	require.NoError(t, rem.Flags().Set("host", "git.example.com:22"))
	var rBuf bytes.Buffer
	rem.SetOut(&rBuf)
	require.NoError(t, runSSHKnownHostsRemove(rem, nil))
	assert.Contains(t, rBuf.String(), "Removed")
}

func TestRunSSHKnownHostsRemove_MissingHost(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKnownHostsRemoveCmd()
	err := runSSHKnownHostsRemove(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingHost)
}

func TestRunSSHKnownHostsList_Empty(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHKnownHostsListCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHKnownHostsList(cmd, nil))
	assert.Contains(t, buf.String(), "No host-key pins found")
}

// =============================================================================
// authorized-keys: validation errors + happy paths
// =============================================================================

// Helper for tests: a fixed ed25519 public-key line in authorized_keys
// format. The SSH store sees this string as opaque text — we only need
// add+list to see the same input.
const fixedSSHPubLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILUz5hHQfqM+VKO4U/JLuc6sGwlsr0M3uuLOSPg6XEhg test@local"

func TestRunSSHAuthorizedAdd_MissingName(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHAuthorizedAddCmd()
	err := runSSHAuthorizedAdd(cmd, []string{fixedSSHPubLine})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingKeyName)
}

func TestRunSSHAuthorized_RoundTrip(t *testing.T) {
	withSSHHome(t)

	add := newSSHAuthorizedAddCmd()
	require.NoError(t, add.Flags().Set("name", "ci-bot"))
	var aBuf bytes.Buffer
	add.SetOut(&aBuf)
	require.NoError(t, runSSHAuthorizedAdd(add, []string{fixedSSHPubLine}))
	assert.Contains(t, aBuf.String(), "Added")

	lst := newSSHAuthorizedListCmd()
	var lBuf bytes.Buffer
	lst.SetOut(&lBuf)
	require.NoError(t, runSSHAuthorizedList(lst, nil))

	rem := newSSHAuthorizedRemoveCmd()
	require.NoError(t, rem.Flags().Set("name", "ci-bot"))
	var rBuf bytes.Buffer
	rem.SetOut(&rBuf)
	require.NoError(t, runSSHAuthorizedRemove(rem, nil))
	assert.Contains(t, rBuf.String(), "Removed")
}

func TestRunSSHAuthorizedRemove_MissingName(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHAuthorizedRemoveCmd()
	err := runSSHAuthorizedRemove(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingKeyName)
}

func TestRunSSHAuthorizedList_Empty(t *testing.T) {
	withSSHHome(t)
	cmd := newSSHAuthorizedListCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	require.NoError(t, runSSHAuthorizedList(cmd, nil))
	assert.Contains(t, buf.String(), "No authorized entries")
}

// =============================================================================
// sshClientWithCloser — happy path
// =============================================================================

func TestSSHClientWithCloser(t *testing.T) {
	withSSHHome(t)
	c, closer, err := sshClientWithCloser()
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NotNil(t, closer)
	closer()
}
