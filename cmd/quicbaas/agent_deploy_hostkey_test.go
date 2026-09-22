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

// Package main: the deploy host-key path.
//
// These tests drive the real `quicbaas ssh known-hosts add` handler and
// then ask the deploy wiring to verify a host key, so they prove the two
// read and write ONE store. That was the defect: `agent deploy` never
// consulted the pins its own help text told operators to add.
package main

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// pinHost runs the real known-hosts add handler against the test store.
func pinHost(t *testing.T, host, fingerprint, algorithm string) {
	t.Helper()
	cmd := newSSHKnownHostsAddCmd()
	require.NoError(t, cmd.Flags().Set("host", host))
	require.NoError(t, cmd.Flags().Set("fingerprint", fingerprint))
	require.NoError(t, cmd.Flags().Set("algorithm", algorithm))
	require.NoError(t, runSSHKnownHostsAdd(cmd, nil))
}

func TestBuildDeploySSHResolvers_VerifiesAPinAddedByTheKnownHostsCommand(t *testing.T) {
	withSSHHome(t)
	t.Setenv("USER", "operator")

	target := sshfixture.Host(t)
	pinHost(t, "omega", target.Fingerprint, target.Public.Type())

	signer, hostKeys, err := buildDeploySSHResolvers()
	require.NoError(t, err)
	require.NotNil(t, signer)
	require.NotNil(t, hostKeys)

	// The ssh target is "user@omega:22"; the pin is stored per host.
	cb, err := hostKeys.HostKeyCallback(context.Background(), service.SSHHostRef{Host: "omega:22"})
	require.NoError(t, err)
	require.NotNil(t, cb)

	assert.NoError(t, cb("omega:22", nil, target.Public), "the pinned key is accepted")

	err = cb("omega:22", nil, sshfixture.HostAlt(t).Public)
	var mismatch *service.SSHHostKeyMismatchError
	require.True(t, stderrors.As(err, &mismatch), "want SSHHostKeyMismatchError, got %T: %v", err, err)
	assert.Equal(t, target.Fingerprint, mismatch.Pinned[0])
	assert.Equal(t, sshfixture.HostAltFingerprint, mismatch.Presented)
}

func TestBuildDeploySSHResolvers_UnpinnedHostTellsTheOperatorToPinIt(t *testing.T) {
	withSSHHome(t)
	t.Setenv("USER", "operator")

	_, hostKeys, err := buildDeploySSHResolvers()
	require.NoError(t, err)

	cb, err := hostKeys.HostKeyCallback(context.Background(), service.SSHHostRef{Host: "omega"})
	assert.Nil(t, cb)

	var notPinned *service.SSHHostKeyNotPinnedError
	require.True(t, stderrors.As(err, &notPinned), "want SSHHostKeyNotPinnedError, got %T: %v", err, err)
	assert.Equal(t, "omega", notPinned.Host)
	assert.Equal(t, "operator", notPinned.Owner)
	assert.Contains(t, err.Error(), "quicbaas ssh known-hosts add --host omega")
}

func TestBuildDeploySSHResolvers_PropagatesAClientFailure(t *testing.T) {
	boom := stderrors.New("no transport configured")
	original := sshClientFactory
	sshClientFactory = func() (sshSDK.Client, error) { return nil, boom }
	t.Cleanup(func() { sshClientFactory = original })

	signer, hostKeys, err := buildDeploySSHResolvers()
	assert.Nil(t, signer)
	assert.Nil(t, hostKeys)
	assert.ErrorIs(t, err, boom)
}
