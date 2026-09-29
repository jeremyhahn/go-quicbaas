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

	"golang.org/x/crypto/ssh"

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
		Owner:       "operator",
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
		pin("agent-09", host.Fingerprint, host.Public.Type()),
		pin("other", sshfixture.HostAltFingerprint, "ssh-ed25519"),
	}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Scope: "user", Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09:22"})
	require.NoError(t, err)
	require.NotNil(t, policy)

	assert.NoError(t, policy.Callback("agent-09:22", nil, host.Public))
	assert.Equal(t, xkmsssh.ListKnownHostsRequest{Scope: "user", Owner: "operator"}, store.seen)
}

func TestXKMSSSHHostKeyResolver_AcceptsAnyPinDuringRotation(t *testing.T) {
	outgoing := sshfixture.Host(t)
	incoming := sshfixture.HostAlt(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{
		pin("agent-09", outgoing.Fingerprint, "ssh-ed25519"),
		pin("agent-09", incoming.Fingerprint, "ssh-ed25519"),
	}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	require.NoError(t, err)

	assert.NoError(t, policy.Callback("agent-09:22", nil, outgoing.Public), "the outgoing key is still pinned")
	assert.NoError(t, policy.Callback("agent-09:22", nil, incoming.Public), "the incoming key is pinned too")
}

func TestXKMSSSHHostKeyResolver_PinWithoutAnAlgorithmConstrainsTheFingerprintOnly(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("agent-09", host.Fingerprint, "")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	require.NoError(t, err)
	assert.NoError(t, policy.Callback("agent-09:22", nil, host.Public))
}

func TestXKMSSSHHostKeyResolver_MatchesHostCaseAndPortInsensitively(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("agent-09:22", host.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "AGENT-09"})
	require.NoError(t, err)
	assert.NoError(t, policy.Callback("AGENT-09:22", nil, host.Public))
}

func TestXKMSSSHHostKeyResolver_RefOverridesTheResolverDefaults(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("agent-09", host.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Scope: "user", Owner: "operator"}

	_, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{
		Host: "agent-09", Scope: "tenant", Owner: "acme",
	})
	require.NoError(t, err)
	assert.Equal(t, xkmsssh.ListKnownHostsRequest{Scope: "tenant", Owner: "acme"}, store.seen)
}

// TestXKMSSSHHostKeyResolver_NegotiatesThePinnedAlgorithms is the remote-agent
// fix: a host pinned by its ed25519 key must be asked for its ed25519 key,
// because x/crypto/ssh would otherwise negotiate the ECDSA one first. Only
// the target's pins count — another host's algorithms stay out.
func TestXKMSSSHHostKeyResolver_NegotiatesThePinnedAlgorithms(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{
		pin("agent-09", host.Fingerprint, "ssh-ed25519"),
		pin("other", sshfixture.HostAltFingerprint, "ecdsa-sha2-nistp256"),
	}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	require.NoError(t, err)
	assert.Equal(t, []string{ssh.KeyAlgoED25519}, policy.Algorithms)
}

func TestHostKeyAlgorithmsForPins(t *testing.T) {
	cases := []struct {
		name       string
		algorithms []string
		want       []string
	}{
		{
			name:       "ed25519",
			algorithms: []string{"ssh-ed25519"},
			want:       []string{ssh.KeyAlgoED25519},
		},
		{
			name:       "every ecdsa curve names itself",
			algorithms: []string{"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"},
			want:       []string{ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521},
		},
		{
			name:       "an rsa key expands to its signature algorithms, strongest first",
			algorithms: []string{"ssh-rsa"},
			want:       []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		},
		{
			name:       "mixed pins keep pin order",
			algorithms: []string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256"},
			want: []string{
				ssh.KeyAlgoED25519,
				ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA,
				ssh.KeyAlgoECDSA256,
			},
		},
		{
			name:       "algorithm case is normalised as the pin check normalises it",
			algorithms: []string{"SSH-ED25519"},
			want:       []string{ssh.KeyAlgoED25519},
		},
		{
			name:       "an unknown algorithm passes through",
			algorithms: []string{"sk-ssh-ed25519@openssh.com"},
			want:       []string{"sk-ssh-ed25519@openssh.com"},
		},
		{
			name:       "duplicates collapse, first occurrence wins",
			algorithms: []string{"ssh-ed25519", "ssh-rsa", "ssh-ed25519", "ssh-rsa"},
			want:       []string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		},
		{
			name:       "a fingerprint-only pin accepts any algorithm, so none is forced",
			algorithms: []string{"ssh-ed25519", ""},
			want:       nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pins := make([]*xkmsssh.Pin, 0, len(tc.algorithms))
			for _, algorithm := range tc.algorithms {
				pins = append(pins, pin("agent-09", sshfixture.HostFingerprint, algorithm))
			}
			assert.Equal(t, tc.want, hostKeyAlgorithmsForPins(pins))
		})
	}
}

// -----------------------------------------------------------------------------
// Failure paths
// -----------------------------------------------------------------------------

func TestXKMSSSHHostKeyResolver_UnpinnedHostNamesTheCommandThatFixesIt(t *testing.T) {
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("elsewhere", sshfixture.HostFingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09:22"})
	assert.Nil(t, policy)

	var notPinned *SSHHostKeyNotPinnedError
	require.True(t, stderrors.As(err, &notPinned), "want SSHHostKeyNotPinnedError, got %T: %v", err, err)
	assert.Equal(t, "agent-09", notPinned.Host)
	assert.Equal(t, "user", notPinned.Scope)
	assert.Equal(t, "operator", notPinned.Owner)
	assert.Contains(t, err.Error(), "quicbaas ssh known-hosts add --host agent-09")
	assert.Contains(t, err.Error(), "--ssh-insecure-host-key")
}

func TestXKMSSSHHostKeyResolver_WrongFingerprintNamesBothFingerprints(t *testing.T) {
	pinned := sshfixture.Host(t)
	presented := sshfixture.HostAlt(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("agent-09", pinned.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	require.NoError(t, err)

	err = policy.Callback("agent-09:22", nil, presented.Public)
	var mismatch *SSHHostKeyMismatchError
	require.True(t, stderrors.As(err, &mismatch), "want SSHHostKeyMismatchError, got %T: %v", err, err)
	assert.Equal(t, "agent-09", mismatch.Host)
	assert.Equal(t, presented.Fingerprint, mismatch.Presented)
	assert.Equal(t, []string{pinned.Fingerprint}, mismatch.Pinned)
	assert.Contains(t, err.Error(), presented.Fingerprint)
	assert.Contains(t, err.Error(), pinned.Fingerprint)
}

func TestXKMSSSHHostKeyResolver_RightFingerprintWrongAlgorithmIsAMismatch(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("agent-09", host.Fingerprint, "ssh-rsa")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	require.NoError(t, err)

	err = policy.Callback("agent-09:22", nil, host.Public)
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
			resolver: &XKMSSSHHostKeyResolver{Owner: "operator"},
			ref:      SSHHostRef{Host: "agent-09"},
			want:     &XKMSSSHHostKeyNilStoreError{},
		},
		{
			name:     "no host",
			resolver: &XKMSSSHHostKeyResolver{KnownHosts: &fakeKnownHosts{}, Owner: "operator"},
			ref:      SSHHostRef{Host: "   "},
			want:     &XKMSSSHHostKeyMissingHostError{},
		},
		{
			name:     "no owner",
			resolver: &XKMSSSHHostKeyResolver{KnownHosts: &fakeKnownHosts{}},
			ref:      SSHHostRef{Host: "agent-09"},
			want:     &XKMSSSHHostKeyMissingOwnerError{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := tc.resolver.HostKeyPolicy(context.Background(), tc.ref)
			assert.Nil(t, policy)
			require.Error(t, err)
			assert.IsType(t, tc.want, err)
			assert.True(t, strings.HasPrefix(err.Error(), "service: "), "error identifies its origin: %v", err)
		})
	}
}

func TestXKMSSSHHostKeyResolver_PropagatesAStoreFailure(t *testing.T) {
	boom := stderrors.New("transport closed")
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: &fakeKnownHosts{err: boom}, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	assert.Nil(t, policy)
	assert.ErrorIs(t, err, boom)
}

func TestXKMSSSHHostKeyResolver_SkipsNilPins(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{nil, pin("agent-09", host.Fingerprint, "ssh-ed25519")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "operator"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-09"})
	require.NoError(t, err)
	assert.NoError(t, policy.Callback("agent-09:22", nil, host.Public))
}
