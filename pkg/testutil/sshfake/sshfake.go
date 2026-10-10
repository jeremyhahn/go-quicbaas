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

// Package sshfake provides an in-memory go-xkms SSH client for tests.
//
// It lives in its own leaf package (importing nothing from go-quicbaas) so
// pkg/service's own tests can use it without the import cycle that
// pkg/testutil would create.
package sshfake

import (
	"context"
	"io"
	"net"
	"strings"

	gossh "golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// =============================================================================
// Client — an in-memory go-xkms SSH client for tests
// =============================================================================
//
// The SSH surface is served on five transports, each of which delegates to
// service.SSHService, which composes an xkmsssh.Client. A single in-memory
// fake here lets every transport's handler tests drive both the success and
// the failure path without a filesystem store or a live xkmsd — and keeps
// one definition of the fake instead of five.

// Client implements xkmsssh.Client against in-memory maps. Set the
// Err field to make every verb fail with that error.
type Client struct {
	// Err, when non-nil, is returned by every verb. Used to drive the
	// error path of a handler under test.
	Err error

	// Keys, Pins, and Entries are the in-memory stores, keyed by name/host.
	KeysByName    map[string]*xkmsssh.Key
	PinsByHost    map[string]*xkmsssh.Pin
	EntriesByName map[string]*xkmsssh.AuthorizedEntry

	// Signature is returned by the signer's Sign.
	Signature *gossh.Signature

	// SignerPublicKey backs the signer returned by Keys().Signer. When nil
	// the signer reports a NotFoundError.
	SignerPublicKey gossh.PublicKey

	// LastImportPassphrase records the plaintext passphrase the client was
	// handed on the most recent Import, so tests can assert the base64
	// passphrase survived the transport hop.
	LastImportPassphrase string

	// Closed records whether Close was called.
	Closed bool
}

// NewClient returns an empty in-memory SSH client.
func NewClient() *Client {
	return &Client{
		KeysByName:    map[string]*xkmsssh.Key{},
		PinsByHost:    map[string]*xkmsssh.Pin{},
		EntriesByName: map[string]*xkmsssh.AuthorizedEntry{},
	}
}

// compile-time contract.
var _ xkmsssh.Client = (*Client)(nil)

// Keys returns the key sub-client.
func (c *Client) Keys() xkmsssh.KeysClient { return &keysClient{c: c} }

// KnownHosts returns the known-hosts sub-client.
func (c *Client) KnownHosts() xkmsssh.KnownHostsClient { return &knownHostsClient{c: c} }

// AuthorizedKeys returns the authorized-keys sub-client.
func (c *Client) AuthorizedKeys() xkmsssh.AuthorizedKeysClient {
	return &authorizedClient{c: c}
}

// Session returns a sub-client whose Dial always reports not-implemented.
func (c *Client) Session() xkmsssh.SessionClient { return sessionClient{} }

// Console always reports unsupported, matching every remote transport.
func (c *Client) Console(_ context.Context, _ xkmsssh.ConsoleConnectRequest, _ xkmsssh.ConsoleConfig) (xkmsssh.ConsoleSession, error) {
	return nil, &xkmsssh.ConsoleUnsupportedError{Transport: "fake"}
}

// Close marks the client closed.
func (c *Client) Close() error {
	c.Closed = true
	return nil
}

// =============================================================================
// Keys
// =============================================================================

type keysClient struct{ c *Client }

func (k *keysClient) Generate(_ context.Context, req xkmsssh.GenerateRequest) (*xkmsssh.Key, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	key := &xkmsssh.Key{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
		Algorithm: req.Algorithm, Comment: req.Comment, Backend: req.Backend,
		Fingerprint: "SHA256:" + req.Name,
		PublicKey:   "ssh-ed25519 AAAA" + req.Name,
	}
	k.c.KeysByName[req.Name] = key
	return key, nil
}

func (k *keysClient) Import(ctx context.Context, req xkmsssh.ImportRequest, provider secure.PassphraseProvider) (*xkmsssh.Key, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	if provider != nil {
		buf, err := provider.Passphrase(ctx, "")
		if err != nil {
			return nil, err
		}
		//secure:unwiped test-observation fake client stores the passphrase string for assertions only.
		k.c.LastImportPassphrase = string(buf.Bytes()) //secure:unborrowed injected-interface test fake exposes the passphrase to assertion code.
		buf.Destroy()
	}
	key := &xkmsssh.Key{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
		Comment: req.Comment, Algorithm: "ed25519",
		Fingerprint: "SHA256:" + req.Name,
	}
	k.c.KeysByName[req.Name] = key
	return key, nil
}

func (k *keysClient) Get(_ context.Context, req xkmsssh.GetKeyRequest) (*xkmsssh.Key, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	key, ok := k.c.KeysByName[req.Name]
	if !ok {
		return nil, &xkmsssh.NotFoundError{Resource: "key", Message: req.Name}
	}
	return key, nil
}

func (k *keysClient) List(_ context.Context, _ xkmsssh.ListKeysRequest) ([]*xkmsssh.Key, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	out := make([]*xkmsssh.Key, 0, len(k.c.KeysByName))
	for _, key := range k.c.KeysByName {
		out = append(out, key)
	}
	return out, nil
}

func (k *keysClient) Delete(_ context.Context, req xkmsssh.DeleteKeyRequest) error {
	if k.c.Err != nil {
		return k.c.Err
	}
	if _, ok := k.c.KeysByName[req.Name]; !ok {
		return &xkmsssh.NotFoundError{Resource: "key", Message: req.Name}
	}
	delete(k.c.KeysByName, req.Name)
	return nil
}

func (k *keysClient) ExportPublic(_ context.Context, req xkmsssh.ExportRequest) (*xkmsssh.PublicKey, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	key, ok := k.c.KeysByName[req.Name]
	if !ok {
		return nil, &xkmsssh.NotFoundError{Resource: "key", Message: req.Name}
	}
	return &xkmsssh.PublicKey{
		Algorithm:   key.Algorithm,
		Line:        key.PublicKey,
		Fingerprint: key.Fingerprint,
	}, nil
}

func (k *keysClient) Signer(_ context.Context, req xkmsssh.GetKeyRequest) (gossh.Signer, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	if k.c.SignerPublicKey == nil {
		return nil, &xkmsssh.NotFoundError{Resource: "key", Message: req.Name}
	}
	return &signer{c: k.c}, nil
}

type signer struct{ c *Client }

func (s *signer) PublicKey() gossh.PublicKey { return s.c.SignerPublicKey }

func (s *signer) Sign(_ io.Reader, _ []byte) (*gossh.Signature, error) {
	if s.c.Signature == nil {
		return &gossh.Signature{Format: "ssh-ed25519", Blob: []byte("sig")}, nil
	}
	return s.c.Signature, nil
}

// =============================================================================
// Known hosts
// =============================================================================

type knownHostsClient struct{ c *Client }

func (k *knownHostsClient) Add(_ context.Context, req xkmsssh.AddKnownHostRequest) error {
	if k.c.Err != nil {
		return k.c.Err
	}
	// Mirrors go-xkms: the pin is the full key, and its fingerprint and
	// algorithm are derived from that key rather than taken from the caller.
	if strings.TrimSpace(req.PublicKey) == "" {
		return &xkmsssh.MissingPublicKeyError{Host: req.Host}
	}
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(req.PublicKey))
	if err != nil {
		return &xkmsssh.InvalidPublicKeyError{Host: req.Host, Err: err}
	}
	k.c.PinsByHost[req.Host] = &xkmsssh.Pin{
		Scope: req.Scope, Owner: req.Owner, Host: req.Host,
		Fingerprint: gossh.FingerprintSHA256(pub), Algorithm: pub.Type(), Source: req.Source,
		PublicKey: strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub))),
	}
	return nil
}

func (k *knownHostsClient) List(_ context.Context, _ xkmsssh.ListKnownHostsRequest) ([]*xkmsssh.Pin, error) {
	if k.c.Err != nil {
		return nil, k.c.Err
	}
	out := make([]*xkmsssh.Pin, 0, len(k.c.PinsByHost))
	for _, p := range k.c.PinsByHost {
		out = append(out, p)
	}
	return out, nil
}

func (k *knownHostsClient) Remove(_ context.Context, req xkmsssh.RemoveKnownHostRequest) error {
	if k.c.Err != nil {
		return k.c.Err
	}
	if _, ok := k.c.PinsByHost[req.Host]; !ok {
		return &xkmsssh.NotFoundError{Resource: "known_host", Message: req.Host}
	}
	delete(k.c.PinsByHost, req.Host)
	return nil
}

func (k *knownHostsClient) Verify(_ context.Context, _, _, host string, received gossh.PublicKey) error {
	if k.c.Err != nil {
		return k.c.Err
	}
	pin, ok := k.c.PinsByHost[host]
	if !ok {
		return &xkmsssh.NotFoundError{Resource: "known_host", Message: host}
	}
	want := gossh.FingerprintSHA256(received)
	if pin.Fingerprint != want {
		return &xkmsssh.MismatchError{Host: host, Expected: pin.Fingerprint, Received: want}
	}
	return nil
}

func (k *knownHostsClient) Callback(ctx context.Context, scope, owner string) gossh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
		return k.Verify(ctx, scope, owner, hostname, key)
	}
}

// =============================================================================
// Authorized keys
// =============================================================================

type authorizedClient struct{ c *Client }

func (a *authorizedClient) Add(_ context.Context, req xkmsssh.AddAuthorizedKeyRequest) error {
	if a.c.Err != nil {
		return a.c.Err
	}
	a.c.EntriesByName[req.Name] = &xkmsssh.AuthorizedEntry{
		Scope: req.Scope, Owner: req.Owner, Name: req.Name,
		Line: req.Line, Options: req.Options, Comment: req.Comment,
	}
	return nil
}

func (a *authorizedClient) Get(_ context.Context, _, _, name string) (*xkmsssh.AuthorizedEntry, error) {
	if a.c.Err != nil {
		return nil, a.c.Err
	}
	entry, ok := a.c.EntriesByName[name]
	if !ok {
		return nil, &xkmsssh.NotFoundError{Resource: "authorized_key", Message: name}
	}
	return entry, nil
}

func (a *authorizedClient) List(_ context.Context, _ xkmsssh.ListAuthorizedKeysRequest) ([]*xkmsssh.AuthorizedEntry, error) {
	if a.c.Err != nil {
		return nil, a.c.Err
	}
	out := make([]*xkmsssh.AuthorizedEntry, 0, len(a.c.EntriesByName))
	for _, e := range a.c.EntriesByName {
		out = append(out, e)
	}
	return out, nil
}

func (a *authorizedClient) Remove(_ context.Context, _, _, name string) error {
	if a.c.Err != nil {
		return a.c.Err
	}
	if _, ok := a.c.EntriesByName[name]; !ok {
		return &xkmsssh.NotFoundError{Resource: "authorized_key", Message: name}
	}
	delete(a.c.EntriesByName, name)
	return nil
}

func (a *authorizedClient) Emit(_ context.Context, req xkmsssh.EmitRequest) (string, error) {
	if a.c.Err != nil {
		return "", a.c.Err
	}
	line := req.PublicKey
	if req.Comment != "" {
		line += " " + req.Comment
	}
	return line, nil
}

func (a *authorizedClient) Verify(_ context.Context, _, _ string, received gossh.PublicKey) (*xkmsssh.AuthorizedEntry, error) {
	if a.c.Err != nil {
		return nil, a.c.Err
	}
	want := gossh.FingerprintSHA256(received)
	for _, e := range a.c.EntriesByName {
		pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(e.Line))
		if err != nil {
			continue
		}
		if gossh.FingerprintSHA256(pub) == want {
			return e, nil
		}
	}
	return nil, &xkmsssh.NotFoundError{Resource: "authorized_key", Message: want}
}

// =============================================================================
// Session
// =============================================================================

type sessionClient struct{}

func (sessionClient) Dial(_ context.Context, _ xkmsssh.SessionDialRequest) (xkmsssh.Session, error) {
	return nil, &xkmsssh.NotImplementedError{Transport: "fake", Op: "Session.Dial"}
}
