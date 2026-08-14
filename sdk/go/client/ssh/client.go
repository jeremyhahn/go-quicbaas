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

package ssh

import (
	"context"
	"encoding/base64"
	"io"
	"net"

	gossh "golang.org/x/crypto/ssh"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// ImportKeyRequest is the wire envelope for Keys().Import. It is the xkms
// ImportRequest plus the base64-encoded passphrase: the xkms contract passes
// the passphrase as a secure.PassphraseProvider callback, which cannot cross
// a transport, so the client resolves it and ships the encoded form. The
// field name matches the REST server's existing body so all six transports
// share one shape.
type ImportKeyRequest struct {
	Scope         string `json:"scope"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Comment       string `json:"comment,omitempty"`
	PEM           []byte `json:"pem"`
	PassphraseB64 string `json:"passphrase_b64,omitempty"`
}

// NewClient returns the transport-backed SSH client. Every verb routes over
// the supplied QuicBaaS transport, so the same client works on embedded,
// REST, gRPC, QUIC, MCP, and Unix.
func NewClient(t transport.Transport) Client {
	return &client{t: t}
}

// client is the transport-backed Client.
type client struct {
	t transport.Transport
}

// Keys returns the key-management sub-client.
func (c *client) Keys() KeysClient { return &keysClient{t: c.t} }

// KnownHosts returns the host-key pin sub-client.
func (c *client) KnownHosts() KnownHostsClient { return &knownHostsClient{t: c.t} }

// AuthorizedKeys returns the authorized_keys sub-client.
func (c *client) AuthorizedKeys() AuthorizedKeysClient { return &authorizedKeysClient{t: c.t} }

// Session returns the session-dial sub-client. Dial reports Unimplemented:
// an outbound SSH session belongs in the caller's own process, and proxying
// it through the QuicBaaS server would turn a key vault into an SSH jump
// host. Callers drive a local session with a remote key by resolving
// Keys().Signer, which delegates each signature over the wire.
func (c *client) Session() SessionClient { return sessionClient{} }

// Console reports Unimplemented for the same reason Session().Dial does: the
// PTY must run in the caller's process. This mirrors go-xkms, whose own
// remote transports all refuse Console.
func (c *client) Console(_ context.Context, _ ConsoleConnectRequest, _ ConsoleConfig) (ConsoleSession, error) {
	return nil, errors.NewUnimplemented("ssh.Console",
		"interactive console is a local capability; use Keys().Signer with a local dialer")
}

// Close releases client resources. The transport is owned by the parent SDK
// client, so this is a no-op and is safe to call repeatedly.
func (c *client) Close() error { return nil }

// =============================================================================
// Keys
// =============================================================================

// keysClient implements KeysClient over a QuicBaaS transport.
type keysClient struct {
	t transport.Transport
}

// Generate creates a new SSH key.
func (k *keysClient) Generate(ctx context.Context, req GenerateRequest) (*Key, error) {
	const op = "ssh.Keys.Generate"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput(op, "name is required")
	}
	resp, err := k.t.Request(ctx, ServiceName, MethodGenerateKey, &req)
	if err != nil {
		return nil, err
	}
	return decodeKey(op, resp)
}

// Import ingests a PEM-encoded private key. When provider is non-nil it is
// invoked once for the passphrase; the plaintext is base64-encoded straight
// out of the mlocked buffer and the buffer is destroyed immediately.
func (k *keysClient) Import(ctx context.Context, req ImportRequest, provider secure.PassphraseProvider) (*Key, error) {
	const op = "ssh.Keys.Import"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput(op, "name is required")
	}
	if len(req.PEM) == 0 {
		return nil, errors.NewInvalidInput(op, "pem is required")
	}
	wire := &ImportKeyRequest{
		Scope:   req.Scope,
		Owner:   req.Owner,
		Name:    req.Name,
		Comment: req.Comment,
		PEM:     req.PEM,
	}
	if provider != nil {
		buf, perr := provider.Passphrase(ctx, "SSH key passphrase: ")
		if perr != nil {
			return nil, perr
		}
		wire.PassphraseB64 = base64.StdEncoding.EncodeToString(buf.Bytes())
		buf.Destroy()
	}
	resp, err := k.t.Request(ctx, ServiceName, MethodImportKey, wire)
	if wire.PassphraseB64 != "" {
		secure.Wipe([]byte(wire.PassphraseB64))
		wire.PassphraseB64 = ""
	}
	if err != nil {
		return nil, err
	}
	return decodeKey(op, resp)
}

// Get loads metadata for a single key.
func (k *keysClient) Get(ctx context.Context, req GetKeyRequest) (*Key, error) {
	const op = "ssh.Keys.Get"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput(op, "name is required")
	}
	resp, err := k.t.Request(ctx, ServiceName, MethodGetKey, &req)
	if err != nil {
		return nil, err
	}
	return decodeKey(op, resp)
}

// List enumerates keys for the (scope, owner) pair.
func (k *keysClient) List(ctx context.Context, req ListKeysRequest) ([]*Key, error) {
	const op = "ssh.Keys.List"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	resp, err := k.t.Request(ctx, ServiceName, MethodListKeys, &req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []*Key{}, nil
	}
	list, ok := resp.(*ListKeysResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	if list.Items == nil {
		return []*Key{}, nil
	}
	return list.Items, nil
}

// Delete removes a key.
func (k *keysClient) Delete(ctx context.Context, req DeleteKeyRequest) error {
	const op = "ssh.Keys.Delete"
	if err := requireContext(ctx, op); err != nil {
		return err
	}
	if req.Name == "" {
		return errors.NewInvalidInput(op, "name is required")
	}
	_, err := k.t.Request(ctx, ServiceName, MethodDeleteKey, &req)
	return err
}

// ExportPublic returns the authorized_keys-format public half.
func (k *keysClient) ExportPublic(ctx context.Context, req ExportRequest) (*PublicKey, error) {
	const op = "ssh.Keys.ExportPublic"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput(op, "name is required")
	}
	resp, err := k.t.Request(ctx, ServiceName, MethodExportPublicKey, &req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal(op, "server returned no public key")
	}
	pub, ok := resp.(*PublicKey)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return pub, nil
}

// Signer returns a forward-signing ssh.Signer: the public half is fetched
// once and every Sign call ships only the digest, so the private key never
// leaves the server-side store.
func (k *keysClient) Signer(ctx context.Context, req GetKeyRequest) (gossh.Signer, error) {
	const op = "ssh.Keys.Signer"
	pub, err := k.ExportPublic(ctx, ExportRequest{Scope: req.Scope, Owner: req.Owner, Name: req.Name})
	if err != nil {
		return nil, err
	}
	parsed, _, _, _, perr := gossh.ParseAuthorizedKey([]byte(pub.Line))
	if perr != nil {
		return nil, errors.Wrap(perr, errors.CodeInternal, op, "failed to parse exported public key")
	}
	return &remoteSigner{t: k.t, ref: req, pub: parsed}, nil
}

// remoteSigner delegates each signature to the server over the transport.
type remoteSigner struct {
	t   transport.Transport
	ref GetKeyRequest
	pub gossh.PublicKey
}

// PublicKey returns the signer's public half.
func (s *remoteSigner) PublicKey() gossh.PublicKey { return s.pub }

// Sign ships the digest to the server and returns the resulting signature.
// The io.Reader is ignored: randomness is the server's responsibility, which
// is where the private key lives.
func (s *remoteSigner) Sign(_ io.Reader, data []byte) (*gossh.Signature, error) {
	const op = "ssh.Keys.Signer.Sign"
	if len(data) == 0 {
		return nil, errors.NewInvalidInput(op, "digest is required")
	}
	resp, err := s.t.Request(context.Background(), ServiceName, MethodSignDigest, &SignRequest{
		Scope:  s.ref.Scope,
		Owner:  s.ref.Owner,
		Name:   s.ref.Name,
		Digest: data,
	})
	if err != nil {
		return nil, err
	}
	sig, ok := resp.(*SignResponse)
	if !ok || sig == nil {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return &gossh.Signature{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}, nil
}

// =============================================================================
// Known hosts
// =============================================================================

// knownHostsClient implements KnownHostsClient over a QuicBaaS transport.
type knownHostsClient struct {
	t transport.Transport
}

// Add pins a host-key fingerprint.
func (k *knownHostsClient) Add(ctx context.Context, req AddKnownHostRequest) error {
	const op = "ssh.KnownHosts.Add"
	if err := requireContext(ctx, op); err != nil {
		return err
	}
	if req.Host == "" {
		return errors.NewInvalidInput(op, "host is required")
	}
	if req.Fingerprint == "" {
		return errors.NewInvalidInput(op, "fingerprint is required")
	}
	_, err := k.t.Request(ctx, ServiceName, MethodAddKnownHost, &req)
	return err
}

// List enumerates pinned host keys.
func (k *knownHostsClient) List(ctx context.Context, req ListKnownHostsRequest) ([]*Pin, error) {
	const op = "ssh.KnownHosts.List"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	resp, err := k.t.Request(ctx, ServiceName, MethodListKnownHosts, &req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []*Pin{}, nil
	}
	list, ok := resp.(*ListKnownHostsResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	if list.Items == nil {
		return []*Pin{}, nil
	}
	return list.Items, nil
}

// Remove deletes a pinned host key.
func (k *knownHostsClient) Remove(ctx context.Context, req RemoveKnownHostRequest) error {
	const op = "ssh.KnownHosts.Remove"
	if err := requireContext(ctx, op); err != nil {
		return err
	}
	if req.Host == "" {
		return errors.NewInvalidInput(op, "host is required")
	}
	_, err := k.t.Request(ctx, ServiceName, MethodRemoveKnownHost, &req)
	return err
}

// Verify compares a received host key against the pin store. Host-key trust
// is decided where the connection is made, so the comparison is local over a
// scoped List — the same posture every remote go-xkms transport takes.
func (k *knownHostsClient) Verify(ctx context.Context, scope, owner, host string, received gossh.PublicKey) error {
	const op = "ssh.KnownHosts.Verify"
	if received == nil {
		return errors.NewInvalidInput(op, "received host key is required")
	}
	pins, err := k.List(ctx, ListKnownHostsRequest{Scope: scope, Owner: owner})
	if err != nil {
		return err
	}
	want := gossh.FingerprintSHA256(received)
	for _, p := range pins {
		if p.Host != host {
			continue
		}
		if p.Fingerprint == want {
			return nil
		}
		return &HostKeyMismatchError{Host: host, Expected: p.Fingerprint, Received: want}
	}
	return errors.NewNotFound(op, "no host pin for "+host)
}

// Callback returns an ssh.HostKeyCallback backed by Verify.
func (k *knownHostsClient) Callback(ctx context.Context, scope, owner string) gossh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
		return k.Verify(ctx, scope, owner, stripPort(hostname), key)
	}
}

// HostKeyMismatchError reports that a host presented a key that does not
// match its pin. It is a distinct type (not a generic error code) because
// callers must be able to tell "unknown host" from "host key changed".
type HostKeyMismatchError struct {
	Host     string
	Expected string
	Received string
}

// Error implements the error interface.
func (e *HostKeyMismatchError) Error() string {
	return "ssh/known_hosts: host key mismatch for " + e.Host +
		": expected " + e.Expected + ", received " + e.Received
}

// stripPort removes the ":port" suffix golang.org/x/crypto/ssh appends to the
// hostname it passes to a HostKeyCallback.
func stripPort(hostname string) string {
	if host, _, err := net.SplitHostPort(hostname); err == nil {
		return host
	}
	return hostname
}

// =============================================================================
// Authorized keys
// =============================================================================

// authorizedKeysClient implements AuthorizedKeysClient over a transport.
type authorizedKeysClient struct {
	t transport.Transport
}

// Add persists an authorized_keys entry.
func (a *authorizedKeysClient) Add(ctx context.Context, req AddAuthorizedKeyRequest) error {
	const op = "ssh.AuthorizedKeys.Add"
	if err := requireContext(ctx, op); err != nil {
		return err
	}
	if req.Name == "" {
		return errors.NewInvalidInput(op, "name is required")
	}
	if req.Line == "" {
		return errors.NewInvalidInput(op, "line is required")
	}
	_, err := a.t.Request(ctx, ServiceName, MethodAddAuthorizedKey, &req)
	return err
}

// Get returns one authorized entry by name. Resolved from the scoped List so
// the wire surface stays minimal (the same approach go-xkms takes on gRPC).
func (a *authorizedKeysClient) Get(ctx context.Context, scope, owner, name string) (*AuthorizedEntry, error) {
	const op = "ssh.AuthorizedKeys.Get"
	if name == "" {
		return nil, errors.NewInvalidInput(op, "name is required")
	}
	entries, err := a.List(ctx, ListAuthorizedKeysRequest{Scope: scope, Owner: owner})
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	return nil, errors.NewNotFound(op, "no authorized key named "+name)
}

// List enumerates authorized entries.
func (a *authorizedKeysClient) List(ctx context.Context, req ListAuthorizedKeysRequest) ([]*AuthorizedEntry, error) {
	const op = "ssh.AuthorizedKeys.List"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	resp, err := a.t.Request(ctx, ServiceName, MethodListAuthorizedKeys, &req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []*AuthorizedEntry{}, nil
	}
	list, ok := resp.(*ListAuthorizedKeysResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	if list.Items == nil {
		return []*AuthorizedEntry{}, nil
	}
	return list.Items, nil
}

// Remove deletes an authorized entry by name.
func (a *authorizedKeysClient) Remove(ctx context.Context, scope, owner, name string) error {
	const op = "ssh.AuthorizedKeys.Remove"
	if err := requireContext(ctx, op); err != nil {
		return err
	}
	if name == "" {
		return errors.NewInvalidInput(op, "name is required")
	}
	_, err := a.t.Request(ctx, ServiceName, MethodRemoveAuthorizedKey, &RemoveAuthorizedKeyRequest{
		Scope: scope, Owner: owner, Name: name,
	})
	return err
}

// Emit formats a public key as an authorized_keys line. The normalization
// rules live in go-xkms, so the call is served by the server's xkms client
// rather than reimplemented here.
func (a *authorizedKeysClient) Emit(ctx context.Context, req EmitRequest) (string, error) {
	const op = "ssh.AuthorizedKeys.Emit"
	if err := requireContext(ctx, op); err != nil {
		return "", err
	}
	if req.PublicKey == "" {
		return "", errors.NewInvalidInput(op, "public key is required")
	}
	resp, err := a.t.Request(ctx, ServiceName, MethodEmitAuthorizedKey, &req)
	if err != nil {
		return "", err
	}
	out, ok := resp.(*EmitResponse)
	if !ok || out == nil {
		return "", errors.NewInternal(op, "invalid response type")
	}
	return out.Line, nil
}

// Verify matches an inbound public key against the stored authorized
// entries. Like KnownHosts().Verify this is a local comparison over a scoped
// List — the decision belongs where the connection is accepted.
func (a *authorizedKeysClient) Verify(ctx context.Context, scope, owner string, received gossh.PublicKey) (*AuthorizedEntry, error) {
	const op = "ssh.AuthorizedKeys.Verify"
	if received == nil {
		return nil, errors.NewInvalidInput(op, "received public key is required")
	}
	entries, err := a.List(ctx, ListAuthorizedKeysRequest{Scope: scope, Owner: owner})
	if err != nil {
		return nil, err
	}
	want := gossh.FingerprintSHA256(received)
	for _, e := range entries {
		pub, _, _, _, perr := gossh.ParseAuthorizedKey([]byte(e.Line))
		if perr != nil {
			continue
		}
		if gossh.FingerprintSHA256(pub) == want {
			return e, nil
		}
	}
	return nil, errors.NewNotFound(op, "no authorized key matching "+want)
}

// =============================================================================
// Session
// =============================================================================

// sessionClient refuses outbound dials on every QuicBaaS transport.
type sessionClient struct{}

// Dial reports Unimplemented. See client.Session for the rationale.
func (sessionClient) Dial(_ context.Context, _ SessionDialRequest) (Session, error) {
	return nil, errors.NewUnimplemented("ssh.Session.Dial",
		"outbound SSH sessions are a local capability; use Keys().Signer with a local dialer")
}

// =============================================================================
// Helpers
// =============================================================================

// decodeKey type-asserts a transport response into a *Key.
func decodeKey(op string, resp interface{}) (*Key, error) {
	if resp == nil {
		return nil, errors.NewInternal(op, "server returned no key")
	}
	key, ok := resp.(*Key)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return key, nil
}

// requireContext rejects a nil context before it reaches a transport.
func requireContext(ctx context.Context, op string) error {
	if ctx == nil {
		return errors.NewInvalidInput(op, "context is required")
	}
	return nil
}
