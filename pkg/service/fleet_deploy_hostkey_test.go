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

package service

import (
	"context"
	stderrors "errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// -----------------------------------------------------------------------------
// Test doubles
// -----------------------------------------------------------------------------

// fakeHostKeyResolver records what the deploy asked the known-hosts
// store for, and hands back either a policy or a typed failure.
type fakeHostKeyResolver struct {
	policy *deploy.HostKeyPolicy
	err    error
	seen   []SSHHostRef
}

func (f *fakeHostKeyResolver) HostKeyPolicy(_ context.Context, ref SSHHostRef) (*deploy.HostKeyPolicy, error) {
	f.seen = append(f.seen, ref)
	if f.err != nil {
		return nil, f.err
	}
	return f.policy, nil
}

// acceptHostKey is a policy whose callback accepts everything and which
// asks for ed25519 host keys. Tests assert on its presence, not its
// verdict — the verdict is the resolver's test.
func acceptHostKey() *deploy.HostKeyPolicy {
	return &deploy.HostKeyPolicy{
		Callback:   func(string, net.Addr, ssh.PublicKey) error { return nil },
		Algorithms: []string{ssh.KeyAlgoED25519},
	}
}

// pinnedSpec is validSpec without the insecure opt-out, i.e. a deploy
// that must verify the target's host key against its pin.
func pinnedSpec() *deploy.Spec {
	spec := validSpec()
	spec.InsecureHostKey = false
	return spec
}

// -----------------------------------------------------------------------------
// The resolved callback reaches the dial
// -----------------------------------------------------------------------------

func TestDeploy_PasswordDialCarriesTheResolvedHostKeyPolicy(t *testing.T) {
	spec := pinnedSpec()
	spec.SSHKeyScope = "tenant"
	spec.SSHKeyOwner = "acme"

	dialer := &fakeDialer{}
	resolver := &fakeHostKeyResolver{policy: acceptHostKey()}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.hostKeys = resolver

	_, err := svc.Deploy(acmeCtx, spec)
	require.NoError(t, err)

	require.NotNil(t, dialer.seenHostKey, "the dial must carry a host-key policy")
	assert.Equal(t, []string{ssh.KeyAlgoED25519}, dialer.seenHostKey.Algorithms,
		"the pinned algorithms reach the dial, so the host presents a key the pins cover")
	// Preflight and the install each dial on a session of their own, and
	// each verifies the host key.
	require.Len(t, resolver.seen, 2)
	for _, seen := range resolver.seen {
		assert.Equal(t, SSHHostRef{Host: "remote.example.com:22", Scope: "tenant", Owner: "acme"}, seen,
			"the pin is addressed by the same scope/owner as the SSH key")
	}
}

func TestDeploy_StoredKeyDialCarriesTheResolvedHostKeyPolicy(t *testing.T) {
	spec := pinnedSpec()
	spec.SSHPassword = ""
	spec.SSHKeyName = "deploy-key"

	dialer := &fakeDialer{}
	resolver := &fakeHostKeyResolver{policy: acceptHostKey()}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.hostKeys = resolver
	svc.sshKeys = &fakeSignerResolver{signer: sshfixture.User(t).Signer}

	_, err := svc.Deploy(acmeCtx, spec)
	require.NoError(t, err)

	require.NotNil(t, dialer.seenHostKey, "the dial must carry a host-key policy")
	assert.Equal(t, []string{ssh.KeyAlgoED25519}, dialer.seenHostKey.Algorithms)
	assert.NotEmpty(t, dialer.seenAuth, "the dial must still carry the stored-key signer")
}

// -----------------------------------------------------------------------------
// Failure and opt-out paths
// -----------------------------------------------------------------------------

func TestDeploy_UnpinnedHostFailsBeforeTheDial(t *testing.T) {
	notPinned := &SSHHostKeyNotPinnedError{Host: "remote.example.com", Scope: "user", Owner: "operator"}
	dialer := &fakeDialer{}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.hostKeys = &fakeHostKeyResolver{err: notPinned}

	_, err := svc.Deploy(acmeCtx, pinnedSpec())

	var typed *SSHHostKeyNotPinnedError
	require.True(t, stderrors.As(err, &typed), "want SSHHostKeyNotPinnedError, got %T: %v", err, err)
	assert.Nil(t, dialer.seen, "a host we cannot verify is never dialed")
}

func TestDeploy_InsecureHostKeySkipsThePinLookup(t *testing.T) {
	dialer := &fakeDialer{}
	resolver := &fakeHostKeyResolver{err: stderrors.New("the store must not be consulted")}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.hostKeys = resolver

	// validSpec() carries InsecureHostKey: the operator opted out.
	_, err := svc.Deploy(acmeCtx, validSpec())
	require.NoError(t, err)

	assert.Empty(t, resolver.seen, "the insecure opt-out never reads the pin store")
	assert.Nil(t, dialer.seenHostKey, "the dial falls back to the spec's own policy")
}

func TestDeploy_NoResolverLeavesThePolicyToTheSpec(t *testing.T) {
	dialer := &fakeDialer{}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())

	_, err := svc.Deploy(acmeCtx, pinnedSpec())
	require.NoError(t, err)
	assert.Nil(t, dialer.seenHostKey,
		"with no resolver the dialer sees no policy and deploy.Spec decides")
}
