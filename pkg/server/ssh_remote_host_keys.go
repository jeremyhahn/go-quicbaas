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

package server

import (
	"context"
	"errors"
	"strings"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// SSHRemotePinError reports a host-key pin that cannot cross the
// KnownHosts() client boundary: its scope is invalid or its public key is
// missing or unparseable. The key service never accepts a fingerprint-only
// pin. Err is nil when Reason alone describes the failure.
type SSHRemotePinError struct {
	Host   string
	Reason string
	Err    error
}

// Error implements the error interface.
func (e *SSHRemotePinError) Error() string {
	msg := "server: ssh host-key pin for " + e.Host + ": " + e.Reason
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying failure.
func (e *SSHRemotePinError) Unwrap() error { return e.Err }

const pinHasNoPublicKey = "pin has no public key"

// gitHostKeyStore returns the pin store git-over-SSH clones verify against.
// A node whose SSH keys live on a remote xkmsd keeps their pins there too,
// reached through the same client's KnownHosts(); otherwise the pins live in
// the node's barrier-sealed SSH keyspace.
func (s *Server) gitHostKeyStore(sshClient xkmsssh.Client) (xkmsssh.HostKeyStore, error) {
	if strings.TrimSpace(s.config.XKMS.URL) != "" {
		return newRemoteHostKeyStore(sshClient.KnownHosts()), nil
	}
	return s.sshHostKeyStore()
}

// remoteHostKeyStore serves the known-hosts Store contract over a
// KnownHostsClient, so the connect flow's accept-new pinning persists on the
// key service. Pins cross the client as authorized_keys lines and come back
// as SSH wire-format public keys.
type remoteHostKeyStore struct {
	client xkmsssh.KnownHostsClient
}

func newRemoteHostKeyStore(client xkmsssh.KnownHostsClient) *remoteHostKeyStore {
	return &remoteHostKeyStore{client: client}
}

// Add pins a host key on the key service.
func (r *remoteHostKeyStore) Add(ctx context.Context, pin *xkmssdk.SSHKnownHostsPin) error {
	if len(pin.PublicKey) == 0 {
		return &SSHRemotePinError{Host: pin.Host, Reason: pinHasNoPublicKey}
	}
	key, err := gossh.ParsePublicKey(pin.PublicKey)
	if err != nil {
		return &SSHRemotePinError{Host: pin.Host, Reason: "invalid public key", Err: err}
	}
	return r.client.Add(ctx, xkmsssh.AddKnownHostRequest{
		Scope:       pin.Scope.String(),
		Owner:       pin.Owner,
		Host:        pin.Host,
		Fingerprint: pin.Fingerprint,
		Algorithm:   pin.Algorithm,
		Source:      pin.Source,
		PublicKey:   strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key))),
	})
}

// Get returns the pin for host, or *SSHKnownHostsNotFoundError when the host
// is unpinned — the signal accept-new keys first-contact pinning on.
func (r *remoteHostKeyStore) Get(ctx context.Context, sc xkmssdk.SSHScope, owner, host string) (*xkmssdk.SSHKnownHostsPin, error) {
	pins, err := r.List(ctx, sc, owner)
	if err != nil {
		return nil, err
	}
	for _, pin := range pins {
		if pin.Host == host {
			return pin, nil
		}
	}
	return nil, &xkmssdk.SSHKnownHostsNotFoundError{Scope: sc.String(), Owner: owner, Host: host}
}

// List returns every pin under (scope, owner).
func (r *remoteHostKeyStore) List(ctx context.Context, sc xkmssdk.SSHScope, owner string) ([]*xkmssdk.SSHKnownHostsPin, error) {
	remote, err := r.client.List(ctx, xkmsssh.ListKnownHostsRequest{Scope: sc.String(), Owner: owner})
	if err != nil {
		return nil, err
	}
	pins := make([]*xkmssdk.SSHKnownHostsPin, 0, len(remote))
	for _, entry := range remote {
		pin, err := storePin(entry)
		if err != nil {
			return nil, err
		}
		pins = append(pins, pin)
	}
	return pins, nil
}

// Remove unpins host, reporting *SSHKnownHostsNotFoundError when it was not
// pinned.
func (r *remoteHostKeyStore) Remove(ctx context.Context, sc xkmssdk.SSHScope, owner, host string) error {
	err := r.client.Remove(ctx, xkmsssh.RemoveKnownHostRequest{Scope: sc.String(), Owner: owner, Host: host})
	var notFound *xkmsssh.NotFoundError
	if errors.As(err, &notFound) {
		return &xkmssdk.SSHKnownHostsNotFoundError{Scope: sc.String(), Owner: owner, Host: host}
	}
	return err
}

// storePin converts a client pin into the store's form.
func storePin(entry *xkmsssh.Pin) (*xkmssdk.SSHKnownHostsPin, error) {
	sc, err := xkmssdk.ParseObjectScope(entry.Scope)
	if err != nil {
		return nil, &SSHRemotePinError{Host: entry.Host, Reason: "invalid scope", Err: err}
	}
	if entry.PublicKey == "" {
		return nil, &SSHRemotePinError{Host: entry.Host, Reason: pinHasNoPublicKey}
	}
	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(entry.PublicKey))
	if err != nil {
		return nil, &SSHRemotePinError{Host: entry.Host, Reason: "invalid public key", Err: err}
	}
	return &xkmssdk.SSHKnownHostsPin{
		Host:        entry.Host,
		Fingerprint: entry.Fingerprint,
		Algorithm:   entry.Algorithm,
		Scope:       sc,
		Owner:       entry.Owner,
		AddedAt:     entry.AddedAt,
		Source:      entry.Source,
		PublicKey:   key.Marshal(),
	}, nil
}
