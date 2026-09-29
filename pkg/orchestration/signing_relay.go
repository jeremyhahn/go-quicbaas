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

package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"slices"
	"strconv"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// The signing relay — the agent protocol travels, the key does not
// =============================================================================
//
// A spawn that signs commits or clones an ssh remote mounts the daemon's
// KMS-backed ssh agent socket (WorkspaceSigningAgentSocket). That socket is
// bound on the daemon's host, so a fleet node on any other host cannot mount
// it, and until go-quicbaas#1013 such a spawn could only run where the socket
// was local.
//
// THE RELAY CARRIES THE AGENT PROTOCOL OVER THE AGENT'S EXISTING mTLS LINK. The
// fleet node binds a socket of its own, under its own sockets root, whose
// signers forward every sign request over a FleetRuntime.SigningRelay stream
// the control plane opens right after the spawn. The control plane answers
// each one by dialing the session's own daemon-local agent socket — the same
// one a co-located container would have mounted — so:
//
//   - the key never leaves the daemon's host; the agent host sees requests and
//     signatures, for exactly the lifetime of the session's signing window;
//   - a reader's StopSigning still closes that window: the daemon-local socket
//     is gone and every later relayed sign fails;
//   - every relayed signature is audited here, by the party that holds the
//     key, with the data's digest and never the data.
//
// Placement prefers a direct mount where one exists — an embedded agent, or one
// advertising this daemon's signing-agent:<id> trait — and relays only to an
// agent that advertises FleetNodeTraitSigningRelay.

// FleetNodeTraitSigningRelay marks a fleet node that can serve a session's
// signing socket from a relay over its FleetRuntime link. Like FleetNodeTraitGPU it
// rides in Capabilities and names a property of the agent, never a backend.
const FleetNodeTraitSigningRelay = "signing-relay"

// SigningRelaySpec asks the executing agent to serve the session's signing
// socket from a relay. It is set only on the agent link — by RemoteACPBackend
// on the wire, and by the agent's spawn server on the SpawnRequest it hands its
// runtime — and is never read from a public API.
type SigningRelaySpec struct {
	// PublicKeys are the keys the relayed agent advertises, in ssh wire form:
	// the keys of the control plane's session agent.
	PublicKeys [][]byte
}

// SigningRelaySignEvent is one relayed signature, as the audit trail records
// it. It carries the data's digest and never the data.
type SigningRelaySignEvent struct {
	// TenantID owns the session.
	TenantID string

	// SessionID is the session the signature was made for.
	SessionID string

	// AgentID is the fleet node the request came from.
	AgentID string

	// KeyFingerprint is the SHA256 fingerprint of the key asked for, empty
	// when the request named no parseable key.
	KeyFingerprint string

	// DataSHA256 is the hex SHA-256 of the data signed.
	DataSHA256 string

	// Success reports whether a signature was returned.
	Success bool

	// Reason is why the request was refused or failed, empty on success.
	Reason string
}

// SigningRelayAuditor records relayed signatures on the platform audit trail.
// The server binds it to the same logger the orchestration stores use.
type SigningRelayAuditor interface {
	// RecordSigningRelaySign appends one relayed signature. It must not block
	// the caller.
	RecordSigningRelaySign(ctx context.Context, event SigningRelaySignEvent)
}

// signingRelayMount returns the spawn's mount of the daemon's signing socket.
func signingRelayMount(req *SpawnRequest) (ContainerMount, bool) {
	for _, mount := range req.ExtraMounts {
		if mount.Target == WorkspaceSigningAgentSocket {
			return mount, true
		}
	}
	return ContainerMount{}, false
}

// WithoutSigningMount returns the mounts less the signing socket's: the relay
// replaces it on the agent, which could not have mounted it anyway.
func WithoutSigningMount(mounts []ContainerMount) []ContainerMount {
	kept := make([]ContainerMount, 0, len(mounts))
	for _, mount := range mounts {
		if mount.Target != WorkspaceSigningAgentSocket {
			kept = append(kept, mount)
		}
	}
	return kept
}

// relaySigningFor reports whether a spawn to reg must relay the signing agent:
// it mounts the daemon's socket, and reg can mount it neither by being
// embedded in this daemon nor by advertising this daemon's signing-agent trait,
// but does advertise the relay. An agent that can do none of these is left to
// refuse the mount itself — placement should never have chosen it.
func relaySigningFor(reg *FleetNode, localTrait string) bool {
	if reg == nil || reg.EffectiveKind() == FleetNodeKindEmbedded {
		return false
	}
	if localTrait != "" && slices.Contains(reg.Capabilities, localTrait) {
		return false
	}
	return slices.Contains(reg.Capabilities, FleetNodeTraitSigningRelay)
}

// dialSigningAgent connects to the ssh agent at socket.
func dialSigningAgent(ctx context.Context, socket string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", socket)
}

// listSigningAgentKeys returns the public keys, in ssh wire form, of the agent
// at socket: what the relayed agent will advertise.
func listSigningAgentKeys(ctx context.Context, socket string) ([][]byte, error) {
	conn, err := dialSigningAgent(ctx, socket)
	if err != nil {
		return nil, &SigningRelayKeysError{Socket: socket, Reason: "the session's signing agent is not reachable", Cause: err}
	}
	keys, listErr := sshagent.NewClient(conn).List()
	closeErr := conn.Close()
	if listErr != nil {
		return nil, &SigningRelayKeysError{Socket: socket, Reason: "the session's signing agent listed no keys", Cause: listErr}
	}
	if closeErr != nil {
		return nil, &SigningRelayKeysError{Socket: socket, Reason: "the agent connection did not close", Cause: closeErr}
	}
	if len(keys) == 0 {
		return nil, &SigningRelayKeysError{Socket: socket, Reason: "the session's signing agent holds no key"}
	}
	blobs := make([][]byte, 0, len(keys))
	for _, key := range keys {
		blobs = append(blobs, key.Blob)
	}
	return blobs, nil
}

// signingRelayServer answers one session's relayed sign requests from the
// session's daemon-local agent socket.
type signingRelayServer struct {
	tenantID  string
	sessionID string
	agentID   string

	// socket is the session's agent on THIS host: the mount source the spawn
	// named before the relay replaced it.
	socket string

	// keys is the set of key blobs the spawn advertised; a request for any
	// other is refused.
	keys map[string]struct{}

	audit  SigningRelayAuditor
	logger interfaces.Logger
}

// newSigningRelayServer builds the server for one session.
func newSigningRelayServer(tenantID, sessionID, agentID, socket string, keys [][]byte, audit SigningRelayAuditor, logger interfaces.Logger) *signingRelayServer {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[string(key)] = struct{}{}
	}
	return &signingRelayServer{
		tenantID: tenantID, sessionID: sessionID, agentID: agentID,
		socket: socket, keys: set, audit: audit, logger: logger,
	}
}

// serve answers sign requests until the stream ends. It returns the error that
// ended it; io.EOF is the agent closing the session.
func (s *signingRelayServer) serve(ctx context.Context, stream proto.FleetRuntime_SigningRelayClient) error {
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		sign := frame.GetSign()
		if sign == nil {
			return &SigningRelayProtocolError{SessionID: s.sessionID, Reason: "the agent sent a frame that is not a sign request"}
		}
		result := s.sign(ctx, sign)
		reply := &proto.SigningRelayControlFrame{Frame: &proto.SigningRelayControlFrame_Result{Result: result}}
		if err := stream.Send(reply); err != nil {
			return err
		}
	}
}

// sign answers one request, auditing it whatever the outcome.
func (s *signingRelayServer) sign(ctx context.Context, req *proto.SigningRelaySign) *proto.SigningRelayResult {
	digest := sha256.Sum256(req.GetData())
	event := SigningRelaySignEvent{
		TenantID:   s.tenantID,
		SessionID:  s.sessionID,
		AgentID:    s.agentID,
		DataSHA256: hex.EncodeToString(digest[:]),
	}
	key, parseErr := ssh.ParsePublicKey(req.GetKeyBlob())
	if parseErr == nil {
		event.KeyFingerprint = ssh.FingerprintSHA256(key)
	}
	signature, err := s.signChecked(ctx, req, key, parseErr)
	result := &proto.SigningRelayResult{Id: req.GetId()}
	if err != nil {
		event.Reason = err.Error()
		result.Error = err.Error()
		s.logger.Warnf("signing relay: session=%s tenant=%s agent=%s refused: %v", s.sessionID, s.tenantID, s.agentID, err)
	} else {
		event.Success = true
		result.Signature = ssh.Marshal(signature)
	}
	if s.audit != nil {
		s.audit.RecordSigningRelaySign(ctx, event)
	}
	return result
}

// signChecked refuses a request made for another session, tenant or key, and
// otherwise signs it with the session's daemon-local agent.
func (s *signingRelayServer) signChecked(ctx context.Context, req *proto.SigningRelaySign, key ssh.PublicKey, parseErr error) (*ssh.Signature, error) {
	if req.GetTenantId() != s.tenantID {
		return nil, &SigningRelayMismatchError{SessionID: s.sessionID, Field: "tenant", Got: req.GetTenantId()}
	}
	if req.GetSessionId() != s.sessionID {
		return nil, &SigningRelayMismatchError{SessionID: s.sessionID, Field: "session", Got: req.GetSessionId()}
	}
	if parseErr != nil {
		return nil, &SigningRelayMismatchError{SessionID: s.sessionID, Field: "key", Got: "an unparseable key"}
	}
	if _, advertised := s.keys[string(req.GetKeyBlob())]; !advertised {
		return nil, &SigningRelayMismatchError{SessionID: s.sessionID, Field: "key", Got: ssh.FingerprintSHA256(key)}
	}
	// A FRESH CONNECTION PER SIGNATURE, to the session's own socket, so the
	// session's signing window is the daemon's to close: once StopSigning has
	// removed the socket, this dial fails and so does every relayed sign.
	conn, err := dialSigningAgent(ctx, s.socket)
	if err != nil {
		return nil, &SigningRelaySignError{SessionID: s.sessionID, Reason: "the session's signing agent is not serving", Cause: err}
	}
	signature, signErr := sshagent.NewClient(conn).SignWithFlags(key, req.GetData(), sshagent.SignatureFlags(req.GetFlags()))
	closeErr := conn.Close()
	if signErr != nil {
		return nil, &SigningRelaySignError{SessionID: s.sessionID, Reason: "the session's signing agent refused", Cause: signErr}
	}
	if closeErr != nil {
		return nil, &SigningRelaySignError{SessionID: s.sessionID, Reason: "the agent connection did not close", Cause: closeErr}
	}
	return signature, nil
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrSigningRelay is the sentinel every signing-relay error unwraps to.
var ErrSigningRelay = errors.New("signing relay")

// SigningRelayKeysError reports that the session's agent could not supply the
// keys a relay would advertise.
type SigningRelayKeysError struct {
	// Socket is the session's daemon-local agent socket.
	Socket string

	// Reason states what failed.
	Reason string

	// Cause is the underlying error, nil when there is none.
	Cause error
}

// Error implements the error interface.
func (e *SigningRelayKeysError) Error() string {
	msg := ErrSigningRelay.Error() + ": " + e.Reason + " (" + e.Socket + ")"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *SigningRelayKeysError) Unwrap() []error { return relayUnwrap(e.Cause) }

// SigningRelayOpenError reports a relay the control plane could not open, or
// that the agent did not attach. The spawn it was opened for is refused.
type SigningRelayOpenError struct {
	// SessionID is the session the relay was for.
	SessionID string

	// Reason states what failed.
	Reason string

	// Cause is the underlying error, nil when there is none.
	Cause error
}

// Error implements the error interface.
func (e *SigningRelayOpenError) Error() string {
	msg := ErrSigningRelay.Error() + ": session " + e.SessionID + ": " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *SigningRelayOpenError) Unwrap() []error { return relayUnwrap(e.Cause) }

// SigningRelayMismatchError reports a relayed sign request made for another
// tenant, another session or a key the session did not advertise.
type SigningRelayMismatchError struct {
	// SessionID is the session the relay serves.
	SessionID string

	// Field names what did not match: tenant, session or key.
	Field string

	// Got is what the request named.
	Got string
}

// Error implements the error interface.
func (e *SigningRelayMismatchError) Error() string {
	return ErrSigningRelay.Error() + ": session " + e.SessionID + " refused a sign request for " +
		e.Field + " " + strconv.Quote(e.Got) + " that is not its own"
}

// Unwrap returns the sentinel.
func (e *SigningRelayMismatchError) Unwrap() error { return ErrSigningRelay }

// SigningRelaySignError reports a relayed sign the session's agent could not
// answer — above all one made after the session's signing window closed.
type SigningRelaySignError struct {
	// SessionID is the session the relay serves.
	SessionID string

	// Reason states what failed.
	Reason string

	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *SigningRelaySignError) Error() string {
	msg := ErrSigningRelay.Error() + ": session " + e.SessionID + ": " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *SigningRelaySignError) Unwrap() []error { return relayUnwrap(e.Cause) }

// SigningRelayProtocolError reports a frame out of the relay's protocol.
type SigningRelayProtocolError struct {
	// SessionID is the session the relay serves.
	SessionID string

	// Reason states what was out of protocol.
	Reason string
}

// Error implements the error interface.
func (e *SigningRelayProtocolError) Error() string {
	return ErrSigningRelay.Error() + ": session " + e.SessionID + ": " + e.Reason
}

// Unwrap returns the sentinel.
func (e *SigningRelayProtocolError) Unwrap() error { return ErrSigningRelay }

// relayUnwrap returns the sentinel, and the cause when there is one.
func relayUnwrap(cause error) []error {
	if cause == nil {
		return []error{ErrSigningRelay}
	}
	return []error{ErrSigningRelay, cause}
}
