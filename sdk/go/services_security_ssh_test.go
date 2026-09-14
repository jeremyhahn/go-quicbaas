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

package quicbaas

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gossh "golang.org/x/crypto/ssh"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// sshRecordingTransport records every (service, method, request) triple and
// replays canned responses keyed by method.
type sshRecordingTransport struct {
	calls     []sshRecordedCall
	responses map[string]interface{}
	err       error
}

type sshRecordedCall struct {
	service string
	method  string
	req     interface{}
}

func (t *sshRecordingTransport) Request(_ context.Context, service, method string, req interface{}) (interface{}, error) {
	t.calls = append(t.calls, sshRecordedCall{service: service, method: method, req: req})
	if t.err != nil {
		return nil, t.err
	}
	return t.responses[method], nil
}

func (t *sshRecordingTransport) RequestStream(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
	return nil, stderrors.New("streaming not supported")
}

func (t *sshRecordingTransport) Conn() interface{} { return nil }
func (t *sshRecordingTransport) Close() error      { return nil }
func (t *sshRecordingTransport) Healthy() bool     { return true }

func (t *sshRecordingTransport) last() sshRecordedCall {
	return t.calls[len(t.calls)-1]
}

// newSSHTestClient wires an SDK client to a recording transport.
func newSSHTestClient(tr *sshRecordingTransport) SSHService {
	return NewClientWithTransport(tr).Security().SSH()
}

// =============================================================================
// Accessor
// =============================================================================

// TestSecuritySSHAccessorIsMemoized proves Security().SSH() is the single
// canonical entry point and does not rebuild the client per call.
func TestSecuritySSHAccessorIsMemoized(t *testing.T) {
	client := NewClientWithTransport(&sshRecordingTransport{})
	first := client.Security().SSH()
	second := client.Security().SSH()
	require.NotNil(t, first)
	assert.Same(t, first, second, "Security().SSH() must be memoized")
}

// =============================================================================
// Routing
// =============================================================================

// TestSSHServiceRoutesEveryVerb pins the (service, method) pair each verb
// emits. A drift here routes the call to a handler that does not exist.
func TestSSHServiceRoutesEveryVerb(t *testing.T) {
	tr := &sshRecordingTransport{responses: map[string]interface{}{
		sshclient.MethodGenerateKey:         &sshclient.Key{Name: "laptop"},
		sshclient.MethodImportKey:           &sshclient.Key{Name: "laptop"},
		sshclient.MethodGetKey:              &sshclient.Key{Name: "laptop"},
		sshclient.MethodListKeys:            &sshclient.ListKeysResponse{Items: []*sshclient.Key{{Name: "laptop"}}},
		sshclient.MethodDeleteKey:           &sshclient.AckResponse{Success: true},
		sshclient.MethodExportPublicKey:     &sshclient.PublicKey{Line: "ssh-ed25519 AAAA"},
		sshclient.MethodAddKnownHost:        &sshclient.AckResponse{Success: true},
		sshclient.MethodListKnownHosts:      &sshclient.ListKnownHostsResponse{},
		sshclient.MethodRemoveKnownHost:     &sshclient.AckResponse{Success: true},
		sshclient.MethodAddAuthorizedKey:    &sshclient.AckResponse{Success: true},
		sshclient.MethodListAuthorizedKeys:  &sshclient.ListAuthorizedKeysResponse{},
		sshclient.MethodRemoveAuthorizedKey: &sshclient.AckResponse{Success: true},
		sshclient.MethodEmitAuthorizedKey:   &sshclient.EmitResponse{Line: "ssh-ed25519 AAAA"},
	}}
	ssh := newSSHTestClient(tr)
	ctx := context.Background()

	_, err := ssh.Keys().Generate(ctx, sshclient.GenerateRequest{Name: "laptop"})
	require.NoError(t, err)
	assert.Equal(t, sshclient.MethodGenerateKey, tr.last().method)

	_, err = ssh.Keys().Import(ctx, sshclient.ImportRequest{Name: "laptop", PEM: []byte("pem")}, nil)
	require.NoError(t, err)
	assert.Equal(t, sshclient.MethodImportKey, tr.last().method)

	_, err = ssh.Keys().Get(ctx, sshclient.GetKeyRequest{Name: "laptop"})
	require.NoError(t, err)
	assert.Equal(t, sshclient.MethodGetKey, tr.last().method)

	keys, err := ssh.Keys().List(ctx, sshclient.ListKeysRequest{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, sshclient.MethodListKeys, tr.last().method)

	require.NoError(t, ssh.Keys().Delete(ctx, sshclient.DeleteKeyRequest{Name: "laptop"}))
	assert.Equal(t, sshclient.MethodDeleteKey, tr.last().method)

	_, err = ssh.Keys().ExportPublic(ctx, sshclient.ExportRequest{Name: "laptop"})
	require.NoError(t, err)
	assert.Equal(t, sshclient.MethodExportPublicKey, tr.last().method)

	require.NoError(t, ssh.KnownHosts().Add(ctx,
		sshclient.AddKnownHostRequest{Host: "h", Fingerprint: sshfixture.HostFingerprint}))
	assert.Equal(t, sshclient.MethodAddKnownHost, tr.last().method)

	_, err = ssh.KnownHosts().List(ctx, sshclient.ListKnownHostsRequest{})
	require.NoError(t, err)
	assert.Equal(t, sshclient.MethodListKnownHosts, tr.last().method)

	require.NoError(t, ssh.KnownHosts().Remove(ctx, sshclient.RemoveKnownHostRequest{Host: "h"}))
	assert.Equal(t, sshclient.MethodRemoveKnownHost, tr.last().method)

	require.NoError(t, ssh.AuthorizedKeys().Add(ctx, sshclient.AddAuthorizedKeyRequest{Name: "n", Line: "l"}))
	assert.Equal(t, sshclient.MethodAddAuthorizedKey, tr.last().method)

	_, err = ssh.AuthorizedKeys().List(ctx, sshclient.ListAuthorizedKeysRequest{})
	require.NoError(t, err)
	assert.Equal(t, sshclient.MethodListAuthorizedKeys, tr.last().method)

	require.NoError(t, ssh.AuthorizedKeys().Remove(ctx, "user", "alice", "n"))
	assert.Equal(t, sshclient.MethodRemoveAuthorizedKey, tr.last().method)

	line, err := ssh.AuthorizedKeys().Emit(ctx, sshclient.EmitRequest{PublicKey: "ssh-ed25519 AAAA"})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA", line)
	assert.Equal(t, sshclient.MethodEmitAuthorizedKey, tr.last().method)

	for _, call := range tr.calls {
		assert.Equal(t, sshclient.ServiceName, call.service,
			"%s routed to the wrong service", call.method)
	}
}

// TestSSHListsNeverReturnNil proves an empty server response yields an empty
// slice, so callers can range over it unconditionally.
func TestSSHListsNeverReturnNil(t *testing.T) {
	tr := &sshRecordingTransport{responses: map[string]interface{}{}}
	ssh := newSSHTestClient(tr)
	ctx := context.Background()

	keys, err := ssh.Keys().List(ctx, sshclient.ListKeysRequest{})
	require.NoError(t, err)
	assert.NotNil(t, keys)
	assert.Empty(t, keys)

	pins, err := ssh.KnownHosts().List(ctx, sshclient.ListKnownHostsRequest{})
	require.NoError(t, err)
	assert.NotNil(t, pins)

	entries, err := ssh.AuthorizedKeys().List(ctx, sshclient.ListAuthorizedKeysRequest{})
	require.NoError(t, err)
	assert.NotNil(t, entries)
}

// =============================================================================
// Forward-signing signer
// =============================================================================

// TestSSHSignerDelegatesSignatureOverTheWire proves the private half never
// moves: the signer fetches the public key once and ships only digests.
func TestSSHSignerDelegatesSignatureOverTheWire(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	line := string(gossh.MarshalAuthorizedKey(sshPub))

	tr := &sshRecordingTransport{responses: map[string]interface{}{
		sshclient.MethodExportPublicKey: &sshclient.PublicKey{Line: line},
		sshclient.MethodSignDigest:      &sshclient.SignResponse{Format: "ssh-ed25519", Blob: []byte("sig")},
	}}
	ssh := newSSHTestClient(tr)

	signer, err := ssh.Keys().Signer(context.Background(), sshclient.GetKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
	})
	require.NoError(t, err)
	assert.Equal(t, sshPub.Marshal(), signer.PublicKey().Marshal())

	sig, err := signer.Sign(rand.Reader, []byte("digest"))
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", sig.Format)

	signCall := tr.last()
	assert.Equal(t, sshclient.MethodSignDigest, signCall.method)
	signReq, ok := signCall.req.(*sshclient.SignRequest)
	require.True(t, ok)
	assert.Equal(t, []byte("digest"), signReq.Digest)
	assert.Equal(t, "laptop", signReq.Name)
}

func TestSSHSignerRejectsEmptyDigest(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)

	tr := &sshRecordingTransport{responses: map[string]interface{}{
		sshclient.MethodExportPublicKey: &sshclient.PublicKey{
			Line: string(gossh.MarshalAuthorizedKey(sshPub)),
		},
	}}
	signer, err := newSSHTestClient(tr).Keys().Signer(context.Background(), sshclient.GetKeyRequest{Name: "laptop"})
	require.NoError(t, err)

	_, err = signer.Sign(rand.Reader, nil)
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
}

// =============================================================================
// Local verification (host-key trust is decided client-side)
// =============================================================================

func TestSSHKnownHostsVerify(t *testing.T) {
	// The pin carries the checked-in golden constant while Verify is handed
	// the fixture's public key. They agree only because Verify re-derives
	// the fingerprint with the production function, so a change there turns
	// this match into a mismatch.
	host, other := sshfixture.Host(t), sshfixture.HostAlt(t)

	tr := &sshRecordingTransport{responses: map[string]interface{}{
		sshclient.MethodListKnownHosts: &sshclient.ListKnownHostsResponse{
			Items: []*sshclient.Pin{{Host: "git.example.com", Fingerprint: sshfixture.HostFingerprint}},
		},
	}}
	ssh := newSSHTestClient(tr)
	ctx := context.Background()

	require.NoError(t, ssh.KnownHosts().Verify(ctx, "user", "alice", "git.example.com", host.Public))

	// A different key for the same host is a mismatch, not a miss.
	err := ssh.KnownHosts().Verify(ctx, "user", "alice", "git.example.com", other.Public)
	require.Error(t, err)
	var mismatch *sshclient.HostKeyMismatchError
	assert.ErrorAs(t, err, &mismatch)

	// An unpinned host is not found.
	err = ssh.KnownHosts().Verify(ctx, "user", "alice", "other.example.com", host.Public)
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err))

	// A nil key is rejected before any transport hop.
	err = ssh.KnownHosts().Verify(ctx, "user", "alice", "git.example.com", nil)
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
}

func TestSSHAuthorizedKeysGetAndVerify(t *testing.T) {
	user := sshfixture.User(t)

	tr := &sshRecordingTransport{responses: map[string]interface{}{
		sshclient.MethodListAuthorizedKeys: &sshclient.ListAuthorizedKeysResponse{
			Items: []*sshclient.AuthorizedEntry{{Name: "deploy", Line: user.AuthorizedLine}},
		},
	}}
	ssh := newSSHTestClient(tr)
	ctx := context.Background()

	entry, err := ssh.AuthorizedKeys().Get(ctx, "user", "alice", "deploy")
	require.NoError(t, err)
	assert.Equal(t, "deploy", entry.Name)

	_, err = ssh.AuthorizedKeys().Get(ctx, "user", "alice", "absent")
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err))

	matched, err := ssh.AuthorizedKeys().Verify(ctx, "user", "alice", user.Public)
	require.NoError(t, err)
	assert.Equal(t, "deploy", matched.Name)
	assert.Equal(t, sshfixture.UserFingerprint, gossh.FingerprintSHA256(user.Public),
		"the key Verify matched must fingerprint to the golden constant")

	// An unrelated key must not match the stored entry.
	_, err = ssh.AuthorizedKeys().Verify(ctx, "user", "alice", sshfixture.Host(t).Public)
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err))

	_, err = ssh.AuthorizedKeys().Verify(ctx, "user", "alice", nil)
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHClientValidatesRequests(t *testing.T) {
	ssh := newSSHTestClient(&sshRecordingTransport{responses: map[string]interface{}{}})
	ctx := context.Background()

	_, err := ssh.Keys().Generate(ctx, sshclient.GenerateRequest{})
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err), "name is required")

	_, err = ssh.Keys().Import(ctx, sshclient.ImportRequest{Name: "k"}, nil)
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err), "pem is required")

	_, err = ssh.Keys().Get(ctx, sshclient.GetKeyRequest{})
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))

	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.Keys().Delete(ctx, sshclient.DeleteKeyRequest{})))

	_, err = ssh.Keys().ExportPublic(ctx, sshclient.ExportRequest{})
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))

	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.KnownHosts().Add(ctx,
			sshclient.AddKnownHostRequest{Fingerprint: sshfixture.HostFingerprint})), "host is required")
	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.KnownHosts().Add(ctx, sshclient.AddKnownHostRequest{Host: "h"})), "fingerprint is required")
	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.KnownHosts().Remove(ctx, sshclient.RemoveKnownHostRequest{})))
	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.AuthorizedKeys().Add(ctx, sshclient.AddAuthorizedKeyRequest{Line: "l"})), "name is required")
	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.AuthorizedKeys().Add(ctx, sshclient.AddAuthorizedKeyRequest{Name: "n"})), "line is required")
	assert.Equal(t, sdkerrors.CodeInvalidInput,
		sdkerrors.Code(ssh.AuthorizedKeys().Remove(ctx, "user", "alice", "")))

	_, err = ssh.AuthorizedKeys().Emit(ctx, sshclient.EmitRequest{})
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
}

func TestSSHClientRejectsWrongResponseType(t *testing.T) {
	// The transport returns a response of the wrong shape for every method.
	tr := &sshRecordingTransport{responses: map[string]interface{}{
		sshclient.MethodGetKey:             &sshclient.AckResponse{},
		sshclient.MethodListKeys:           &sshclient.Key{},
		sshclient.MethodExportPublicKey:    &sshclient.AckResponse{},
		sshclient.MethodListKnownHosts:     &sshclient.Key{},
		sshclient.MethodListAuthorizedKeys: &sshclient.Key{},
		sshclient.MethodEmitAuthorizedKey:  &sshclient.Key{},
	}}
	ssh := newSSHTestClient(tr)
	ctx := context.Background()

	_, err := ssh.Keys().Get(ctx, sshclient.GetKeyRequest{Name: "k"})
	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))

	_, err = ssh.Keys().List(ctx, sshclient.ListKeysRequest{})
	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))

	_, err = ssh.Keys().ExportPublic(ctx, sshclient.ExportRequest{Name: "k"})
	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))

	_, err = ssh.KnownHosts().List(ctx, sshclient.ListKnownHostsRequest{})
	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))

	_, err = ssh.AuthorizedKeys().List(ctx, sshclient.ListAuthorizedKeysRequest{})
	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))

	_, err = ssh.AuthorizedKeys().Emit(ctx, sshclient.EmitRequest{PublicKey: "p"})
	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))
}

func TestSSHClientPropagatesTransportError(t *testing.T) {
	transportErr := sdkerrors.NewUnavailable("transport", "server is down")
	tr := &sshRecordingTransport{err: transportErr}
	ssh := newSSHTestClient(tr)

	_, err := ssh.Keys().List(context.Background(), sshclient.ListKeysRequest{})
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeUnavailable, sdkerrors.Code(err))
}

// TestSSHSessionAndConsoleAreUnimplemented documents the deliberate carve-out:
// an outbound SSH session must run in the caller's process, so no QuicBaaS
// transport terminates one.
func TestSSHSessionAndConsoleAreUnimplemented(t *testing.T) {
	ssh := newSSHTestClient(&sshRecordingTransport{})

	_, err := ssh.Session().Dial(context.Background(), sshclient.SessionDialRequest{Host: "h", User: "u"})
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeUnimplemented, sdkerrors.Code(err))

	_, err = ssh.Console(context.Background(), sshclient.ConsoleConnectRequest{Host: "h", User: "u"}, sshclient.ConsoleConfig{})
	require.Error(t, err)
	assert.Equal(t, sdkerrors.CodeUnimplemented, sdkerrors.Code(err))

	// Close is a no-op: the transport belongs to the parent SDK client.
	assert.NoError(t, ssh.Close())
}
