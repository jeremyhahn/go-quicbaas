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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// -----------------------------------------------------------------------------
// Test doubles
// -----------------------------------------------------------------------------

// fakeKnownHosts is a one-method stand-in for the SDK's known-hosts
// sub-client. It records the request so tests can assert the resolver
// addressed the store with the (scope, owner) pair it was given, and it
// can hold several pins for one host — which the production store does
// during a host-key rotation.
type fakeKnownHosts struct {
	pins []*xkmsssh.Pin
	err  error
	seen xkmsssh.ListKnownHostsRequest
	// calls counts List invocations so a test can prove the insecure
	// path never touches the store.
	calls int
}

func (f *fakeKnownHosts) List(_ context.Context, req xkmsssh.ListKnownHostsRequest) ([]*xkmsssh.Pin, error) {
	f.calls++
	f.seen = req
	if f.err != nil {
		return nil, f.err
	}
	return f.pins, nil
}

// pin builds a stored pin for host from a fixture key.
func pin(host, fingerprint, algorithm string) *xkmsssh.Pin {
	return &xkmsssh.Pin{
		Scope:       "user",
		Owner:       "jhahn",
		Host:        host,
		Fingerprint: fingerprint,
		Algorithm:   algorithm,
	}
}

// -----------------------------------------------------------------------------
// Happy paths
// -----------------------------------------------------------------------------

func TestXKMSSSHHostKeyResolver_AcceptsThePinnedKey(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{
		pin("omega", host.Fingerprint, host.Public.Type()),
		pin("other", sshfixture.HostAltFingerprint, "ssh-ed25519"),
	}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Scope: "user", Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega:22"})
	require.NoError(t, err)
	require.NotNil(t, cb)

	assert.NoError(t, cb("omega:22", nil, host.Public))
	assert.Equal(t, xkmsssh.ListKnownHostsRequest{Scope: "user", Owner: "jhahn"}, store.seen)
}

func TestXKMSSSHHostKeyResolver_AcceptsAnyPinDuringRotation(t *testing.T) {
	outgoing := sshfixture.Host(t)
	incoming := sshfixture.HostAlt(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{
		pin("omega", outgoing.Fingerprint, "ssh-ed25519"),
		pin("omega", incoming.Fingerprint, "ssh-ed25519"),
	}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega"})
	require.NoError(t, err)

	assert.NoError(t, cb("omega:22", nil, outgoing.Public), "the outgoing key is still pinned")
	assert.NoError(t, cb("omega:22", nil, incoming.Public), "the incoming key is pinned too")
}

func TestXKMSSSHHostKeyResolver_PinWithoutAnAlgorithmConstrainsTheFingerprintOnly(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("omega", host.Fingerprint, "")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega"})
	require.NoError(t, err)
	assert.NoError(t, cb("omega:22", nil, host.Public))
}

func TestXKMSSSHHostKeyResolver_MatchesHostCaseAndPortInsensitively(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("omega:22", host.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "OMEGA"})
	require.NoError(t, err)
	assert.NoError(t, cb("OMEGA:22", nil, host.Public))
}

func TestXKMSSSHHostKeyResolver_RefOverridesTheResolverDefaults(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("omega", host.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Scope: "user", Owner: "jhahn"}

	_, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{
		Host: "omega", Scope: "tenant", Owner: "acme",
	})
	require.NoError(t, err)
	assert.Equal(t, xkmsssh.ListKnownHostsRequest{Scope: "tenant", Owner: "acme"}, store.seen)
}

// -----------------------------------------------------------------------------
// Failure paths
// -----------------------------------------------------------------------------

func TestXKMSSSHHostKeyResolver_UnpinnedHostNamesTheCommandThatFixesIt(t *testing.T) {
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("elsewhere", sshfixture.HostFingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega:22"})
	assert.Nil(t, cb)

	var notPinned *SSHHostKeyNotPinnedError
	require.True(t, stderrors.As(err, &notPinned), "want SSHHostKeyNotPinnedError, got %T: %v", err, err)
	assert.Equal(t, "omega", notPinned.Host)
	assert.Equal(t, "user", notPinned.Scope)
	assert.Equal(t, "jhahn", notPinned.Owner)
	assert.Contains(t, err.Error(), "quicbaas ssh known-hosts add --host omega")
	assert.Contains(t, err.Error(), "--ssh-insecure-host-key")
}

func TestXKMSSSHHostKeyResolver_WrongFingerprintNamesBothFingerprints(t *testing.T) {
	pinned := sshfixture.Host(t)
	presented := sshfixture.HostAlt(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("omega", pinned.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega"})
	require.NoError(t, err)

	err = cb("omega:22", nil, presented.Public)
	var mismatch *SSHHostKeyMismatchError
	require.True(t, stderrors.As(err, &mismatch), "want SSHHostKeyMismatchError, got %T: %v", err, err)
	assert.Equal(t, "omega", mismatch.Host)
	assert.Equal(t, presented.Fingerprint, mismatch.Presented)
	assert.Equal(t, []string{pinned.Fingerprint}, mismatch.Pinned)
	assert.Contains(t, err.Error(), presented.Fingerprint)
	assert.Contains(t, err.Error(), pinned.Fingerprint)
}

func TestXKMSSSHHostKeyResolver_RightFingerprintWrongAlgorithmIsAMismatch(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("omega", host.Fingerprint, "ssh-rsa")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega"})
	require.NoError(t, err)

	err = cb("omega:22", nil, host.Public)
	var mismatch *SSHHostKeyMismatchError
	require.True(t, stderrors.As(err, &mismatch), "want SSHHostKeyMismatchError, got %T: %v", err, err)
	assert.Equal(t, "ssh-ed25519", mismatch.Algorithm)
}

func TestXKMSSSHHostKeyResolver_RejectsMisconfiguration(t *testing.T) {
	cases := []struct {
		name     string
		resolver *XKMSSSHHostKeyResolver
		ref      SSHHostRef
		want     error
	}{
		{
			name:     "no known-hosts client",
			resolver: &XKMSSSHHostKeyResolver{Owner: "jhahn"},
			ref:      SSHHostRef{Host: "omega"},
			want:     &XKMSSSHHostKeyNilStoreError{},
		},
		{
			name:     "no host",
			resolver: &XKMSSSHHostKeyResolver{KnownHosts: &fakeKnownHosts{}, Owner: "jhahn"},
			ref:      SSHHostRef{Host: "   "},
			want:     &XKMSSSHHostKeyMissingHostError{},
		},
		{
			name:     "no owner",
			resolver: &XKMSSSHHostKeyResolver{KnownHosts: &fakeKnownHosts{}},
			ref:      SSHHostRef{Host: "omega"},
			want:     &XKMSSSHHostKeyMissingOwnerError{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb, err := tc.resolver.HostKeyCallback(context.Background(), tc.ref)
			assert.Nil(t, cb)
			require.Error(t, err)
			assert.IsType(t, tc.want, err)
			assert.True(t, strings.HasPrefix(err.Error(), "service: "), "error identifies its origin: %v", err)
		})
	}
}

func TestXKMSSSHHostKeyResolver_PropagatesAStoreFailure(t *testing.T) {
	boom := stderrors.New("transport closed")
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: &fakeKnownHosts{err: boom}, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega"})
	assert.Nil(t, cb)
	assert.ErrorIs(t, err, boom)
}

func TestXKMSSSHHostKeyResolver_SkipsNilPins(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{nil, pin("omega", host.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "jhahn"}

	cb, err := resolver.HostKeyCallback(context.Background(), SSHHostRef{Host: "omega"})
	require.NoError(t, err)
	assert.NoError(t, cb("omega:22", nil, host.Public))
}
