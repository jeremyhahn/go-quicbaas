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
// read and write ONE store. That was the defect: `fleet deploy` never
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
func pinHost(t *testing.T, host, authorizedLine string) {
	t.Helper()
	cmd := newSSHKnownHostsAddCmd()
	require.NoError(t, cmd.Flags().Set("host", host))
	require.NoError(t, cmd.Flags().Set("key", authorizedLine))
	require.NoError(t, runSSHKnownHostsAdd(cmd, nil))
}

func TestBuildDeploySSHResolvers_VerifiesAPinAddedByTheKnownHostsCommand(t *testing.T) {
	withSSHHome(t)
	t.Setenv("USER", "operator")

	target := sshfixture.Host(t)
	pinHost(t, "agent-09", target.AuthorizedLine)

	signer, hostKeys, err := buildDeploySSHResolvers()
	require.NoError(t, err)
	require.NotNil(t, signer)
	require.NotNil(t, hostKeys)

	// The ssh target is "user@agent-09:22"; the pin is stored per host.
	policy, err := hostKeys.HostKeyPolicy(context.Background(), service.SSHHostRef{Host: "agent-09:22"})
	require.NoError(t, err)
	require.NotNil(t, policy)

	assert.NoError(t, policy.Callback("agent-09:22", nil, target.Public), "the pinned key is accepted")
	assert.Equal(t, []string{target.Public.Type()}, policy.Algorithms,
		"the dial asks for the pinned key's algorithm, so the host presents that key")

	err = policy.Callback("agent-09:22", nil, sshfixture.HostAlt(t).Public)
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

	policy, err := hostKeys.HostKeyPolicy(context.Background(), service.SSHHostRef{Host: "agent-09"})
	assert.Nil(t, policy)

	var notPinned *service.SSHHostKeyNotPinnedError
	require.True(t, stderrors.As(err, &notPinned), "want SSHHostKeyNotPinnedError, got %T: %v", err, err)
	assert.Equal(t, "agent-09", notPinned.Host)
	assert.Equal(t, "operator", notPinned.Owner)
	assert.Contains(t, err.Error(), "quicbaas ssh known-hosts add --host agent-09")
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
