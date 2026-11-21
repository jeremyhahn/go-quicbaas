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

package fleet

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The relayed signing agent — this host binds the socket, the control plane
// holds the key
// =============================================================================
//
// A session that signs commits or clones an ssh remote needs an ssh agent
// socket in its container. The control plane holds that agent — it is backed by
// the daemon's KMS — and a daemon on another host cannot mount its socket. So
// when the spawn carries a SigningRelaySpec, this daemon binds a socket of its
// own under its sockets root, serving signers whose public keys are the control
// plane's and whose Sign forwards the request over the FleetRuntime.SigningRelay
// stream the control plane opens right after the spawn (go-quicbaas#1013).
//
// THE LINK IS THE ONLY PATH, AND IT IS FINAL. A sign before the control plane
// has attached fails; a sign after the stream dropped fails; nothing retries,
// re-dials or falls back to any other key. A container that could still sign
// with its link gone would be signing with something other than the control
// plane's KMS.

// signingRelaySignTimeout bounds how long a relayed signature may take. The
// control plane's signer can sit behind a hardware token, so it is generous; it
// exists so a sign whose answer never comes cannot hold the container's git
// forever.
const signingRelaySignTimeout = 2 * time.Minute

// signingRelayStopGrace bounds how long releasing a relay waits for the
// agent's connection handlers to finish.
const signingRelayStopGrace = 5 * time.Second

// signingRelayLink is the sending half of a SigningRelay stream.
type signingRelayLink interface {
	Send(*proto.SigningRelayNodeFrame) error
}

// signingRelay is one session's relayed agent: the socket bound on this host,
// and the link its signatures travel over once the control plane attaches it.
type signingRelay struct {
	tenantID string
	keys     []ssh.PublicKey

	// agent is the bound socket; nil until startSigningRelay binds it.
	agent *xkmsssh.AgentSession

	mu        sync.Mutex
	sessionID string
	link      signingRelayLink
	attached  bool
	lost      bool
	lostCause error
	lostCh    chan struct{}
	nextID    uint64
	pending   map[uint64]chan *proto.SigningRelayResult

	// sendMu serialises Send on the stream, which gRPC does not allow
	// concurrently; it is separate from mu so a slow send never blocks the
	// delivery of a result.
	sendMu sync.Mutex
}

// newSigningRelay builds the relay for a session of tenantID.
func newSigningRelay(tenantID string, keys []ssh.PublicKey) *signingRelay {
	return &signingRelay{
		tenantID: tenantID,
		keys:     keys,
		lostCh:   make(chan struct{}),
		pending:  map[uint64]chan *proto.SigningRelayResult{},
	}
}

// signers returns the signers the bound agent serves: one per public key, each
// forwarding over the link.
func (r *signingRelay) signers() []ssh.Signer {
	signers := make([]ssh.Signer, 0, len(r.keys))
	for _, key := range r.keys {
		signers = append(signers, &relaySigner{relay: r, key: key})
	}
	return signers
}

// setSession names the session the relay serves, once the spawn has one.
func (r *signingRelay) setSession(sessionID string) {
	r.mu.Lock()
	r.sessionID = sessionID
	r.mu.Unlock()
}

// attach binds the relay to link and tells the control plane so. A relay is
// attached once: a second attach, or one after the link was lost, is refused.
//
// READINESS IS PUBLISHED UNDER THE SEND LOCK. The send lock is taken before
// attached becomes visible and held until the attached frame is on the
// stream, so a sign that sees the relay attached queues behind that frame.
// Publishing first and locking second left a window in which a concurrent
// sign sent before attached, and the control plane — which requires attached
// first — tore the spawn down.
func (r *signingRelay) attach(link signingRelayLink) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()

	r.mu.Lock()
	sessionID := r.sessionID
	if r.attached || r.lost {
		r.mu.Unlock()
		return &SigningRelayAttachError{SessionID: sessionID, Sentinel: ErrSigningRelayAlreadyAttached}
	}
	r.attached = true
	r.link = link
	r.mu.Unlock()

	err := link.Send(&proto.SigningRelayNodeFrame{Frame: &proto.SigningRelayNodeFrame_Attached{
		Attached: &proto.SigningRelayAttached{},
	}})
	if err != nil {
		r.lose(err)
		return &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayLinkLost, Cause: err}
	}
	return nil
}

// deliver hands a result frame to the sign waiting for it. A result nobody
// waits for — one for a sign that already timed out — is dropped.
func (r *signingRelay) deliver(result *proto.SigningRelayResult) {
	r.mu.Lock()
	waiter, ok := r.pending[result.GetId()]
	delete(r.pending, result.GetId())
	r.mu.Unlock()
	if ok {
		waiter <- result
	}
}

// lose marks the link lost for good. Every waiting sign and every later one
// fails with cause.
func (r *signingRelay) lose(cause error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lost {
		return
	}
	r.lost = true
	r.lostCause = cause
	r.pending = map[uint64]chan *proto.SigningRelayResult{}
	close(r.lostCh)
}

// done is closed once the link is lost or the relay released.
func (r *signingRelay) done() <-chan struct{} { return r.lostCh }

// release ends the relay: the link is lost and the socket unbound. It reports
// how the agent's shutdown went, so the caller can say when a handler had to be
// abandoned.
func (r *signingRelay) release() xkmsssh.AgentShutdownReport {
	r.lose(errSigningRelayReleased)
	if r.agent == nil {
		return xkmsssh.AgentShutdownReport{Joined: true}
	}
	ctx, cancel := context.WithTimeout(context.Background(), signingRelayStopGrace)
	defer cancel()
	return r.agent.Stop(ctx)
}

// sign forwards one signature request over the link and waits for its answer.
func (r *signingRelay) sign(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	r.mu.Lock()
	sessionID := r.sessionID
	if r.lost {
		cause := r.lostCause
		r.mu.Unlock()
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayLinkLost, Cause: cause}
	}
	if !r.attached {
		r.mu.Unlock()
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayNotAttached}
	}
	r.nextID++
	id := r.nextID
	answer := make(chan *proto.SigningRelayResult, 1)
	r.pending[id] = answer
	link := r.link
	r.mu.Unlock()

	frame := &proto.SigningRelayNodeFrame{Frame: &proto.SigningRelayNodeFrame_Sign{Sign: &proto.SigningRelaySign{
		Id:        id,
		TenantId:  r.tenantID,
		SessionId: sessionID,
		KeyBlob:   key.Marshal(),
		Data:      data,
		Flags:     uint32(flags),
	}}}
	r.sendMu.Lock()
	sendErr := link.Send(frame)
	r.sendMu.Unlock()
	if sendErr != nil {
		r.lose(sendErr)
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayLinkLost, Cause: sendErr}
	}

	timer := time.NewTimer(signingRelaySignTimeout)
	defer timer.Stop()
	select {
	case result := <-answer:
		return decodeRelayResult(sessionID, result)
	case <-r.lostCh:
		r.mu.Lock()
		cause := r.lostCause
		r.mu.Unlock()
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayLinkLost, Cause: cause}
	case <-timer.C:
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayTimeout}
	}
}

// decodeRelayResult turns the control plane's answer into a signature, or the
// refusal it carried.
func decodeRelayResult(sessionID string, result *proto.SigningRelayResult) (*ssh.Signature, error) {
	if reason := result.GetError(); reason != "" {
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayRefused, Cause: errors.New(reason)}
	}
	signature := new(ssh.Signature)
	if err := ssh.Unmarshal(result.GetSignature(), signature); err != nil {
		return nil, &SigningRelayLinkError{SessionID: sessionID, Sentinel: ErrSigningRelayRefused, Cause: err}
	}
	return signature, nil
}

// relaySigner is one relayed identity: the control plane's public key, and a
// Sign that forwards over the link.
type relaySigner struct {
	relay *signingRelay
	key   ssh.PublicKey
}

// Compile-time interface check: the go-xkms agent asks an RSA signer for its
// algorithm, and refuses one that cannot select it.
var _ ssh.AlgorithmSigner = (*relaySigner)(nil)

// PublicKey implements ssh.Signer.
func (s *relaySigner) PublicKey() ssh.PublicKey { return s.key }

// Sign implements ssh.Signer: the key's default algorithm.
func (s *relaySigner) Sign(_ io.Reader, data []byte) (*ssh.Signature, error) {
	return s.relay.sign(s.key, data, 0)
}

// SignWithAlgorithm implements ssh.AlgorithmSigner by carrying the algorithm
// as the agent protocol's flag, which is how the control plane's agent is
// asked for it.
func (s *relaySigner) SignWithAlgorithm(_ io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	flags, ok := relayAlgorithmFlags[algorithm]
	if !ok && algorithm != s.key.Type() {
		return nil, &SigningRelayAlgorithmError{Algorithm: algorithm}
	}
	return s.relay.sign(s.key, data, flags)
}

// relayAlgorithmFlags maps a signature algorithm to the agent-protocol flag
// that requests it. The empty algorithm is the key's default.
var relayAlgorithmFlags = map[string]sshagent.SignatureFlags{
	"":                   0,
	ssh.KeyAlgoRSASHA256: sshagent.SignatureFlagRsaSha256,
	ssh.KeyAlgoRSASHA512: sshagent.SignatureFlagRsaSha512,
}

// =============================================================================
// Runtime wiring
// =============================================================================

// startSigningRelay binds the relayed agent a spawn asks for and puts its
// socket on the request, at the target the control plane's socket would have
// been mounted at. A spawn that asks for none is returned untouched with a nil
// relay.
func (r *Runtime) startSigningRelay(tenantID string, req *orchestration.SpawnRequest) (*signingRelay, error) {
	spec := req.SigningRelay
	if spec == nil {
		return nil, nil
	}
	if r.socketsRoot == "" {
		return nil, &SentinelDetailError{Sentinel: ErrSigningRelayUnsupported, Detail: "this runtime has no sockets root"}
	}
	if len(spec.PublicKeys) == 0 {
		return nil, &orchestration.InputValidationError{Field: "signing_relay.public_keys", Message: "a relay serves at least one key"}
	}
	keys := make([]ssh.PublicKey, 0, len(spec.PublicKeys))
	for _, blob := range spec.PublicKeys {
		key, err := ssh.ParsePublicKey(blob)
		if err != nil {
			return nil, &orchestration.InputValidationError{Field: "signing_relay.public_keys", Message: err.Error()}
		}
		keys = append(keys, key)
	}
	name, err := orchestration.NewSigningSocketDirName()
	if err != nil {
		return nil, err
	}
	relay := newSigningRelay(tenantID, keys)
	agent, err := xkmsssh.StartAgent(context.Background(), xkmsssh.AgentOptions{
		Signers:   relay.signers(),
		SocketDir: filepath.Join(r.socketsRoot, name),
	})
	if err != nil {
		return nil, &SigningRelayStartError{Cause: err}
	}
	relay.agent = agent
	req.ExtraMounts = append(orchestration.WithoutSigningMount(req.ExtraMounts), orchestration.ContainerMount{
		Source:   agent.SockPath,
		Target:   orchestration.WorkspaceSigningAgentSocket,
		ReadOnly: true,
	})
	return relay, nil
}

// AttachSigningRelay attaches the control plane's SigningRelay stream to the
// relay of sessionID, for tenantID. It refuses, each with its own typed error,
// a session this runtime does not hold, another tenant's session, a session
// spawned with no relay, and a second attach.
func (r *Runtime) AttachSigningRelay(tenantID, sessionID string, link signingRelayLink) (*signingRelay, error) {
	r.sessionTenantsMu.Lock()
	owner, known := r.sessionTenants[sessionID]
	r.sessionTenantsMu.Unlock()
	if !known {
		return nil, &SigningRelayAttachError{SessionID: sessionID, Sentinel: ErrSigningRelayUnknownSession}
	}
	if owner != tenantID {
		return nil, &SigningRelayAttachError{SessionID: sessionID, Sentinel: ErrSigningRelayForeignSession}
	}
	held, ok := r.signingRelays.Load(sessionID)
	if !ok {
		return nil, &SigningRelayAttachError{SessionID: sessionID, Sentinel: ErrSigningRelayNotRequested}
	}
	relay, isRelay := held.(*signingRelay)
	if !isRelay {
		return nil, &SigningRelayAttachError{SessionID: sessionID, Sentinel: ErrSigningRelayNotRequested}
	}
	if err := relay.attach(link); err != nil {
		return nil, err
	}
	return relay, nil
}

// releaseSigningRelay unbinds the session's relayed agent, if it has one.
func (r *Runtime) releaseSigningRelay(sessionID string) {
	held, ok := r.signingRelays.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	relay, isRelay := held.(*signingRelay)
	if !isRelay {
		return
	}
	if report := relay.release(); !report.Clean() {
		r.logger.Warnf("agent runtime: session %s signing relay stopped uncleanly: %d signature(s) abandoned, handlers joined=%t",
			sessionID, report.Abandoned, report.Joined)
	}
}

// SigningRelayEnabled reports whether this runtime can serve relayed signing
// sockets — whether it has a sockets root — which is what the daemon
// advertises as orchestration.FleetNodeTraitSigningRelay.
func (r *Runtime) SigningRelayEnabled() bool { return r.socketsRoot != "" }

// PrepareSigningRelayRoot prepares this runtime's sockets root and sweeps the
// relay sockets a killed predecessor left bound. Called once at startup,
// before the spawn server accepts anything; a runtime with no sockets root has
// nothing to prepare.
func (r *Runtime) PrepareSigningRelayRoot() (orchestration.SigningSocketSweep, error) {
	if r.socketsRoot == "" {
		return orchestration.SigningSocketSweep{}, nil
	}
	return orchestration.EnsureSigningSocketRoot(r.socketsRoot)
}

// =============================================================================
// Typed errors
// =============================================================================

var (
	// ErrSigningRelayUnsupported reports a relay asked of a runtime with no
	// sockets root to bind it under.
	ErrSigningRelayUnsupported = errors.New("agent: signing relay: not supported by this runtime")

	// ErrSigningRelayUnknownSession reports an attach for a session this
	// runtime does not hold.
	ErrSigningRelayUnknownSession = errors.New("agent: signing relay: unknown session")

	// ErrSigningRelayForeignSession reports an attach for another tenant's
	// session.
	ErrSigningRelayForeignSession = errors.New("agent: signing relay: session belongs to another tenant")

	// ErrSigningRelayNotRequested reports an attach for a session spawned
	// with no relay.
	ErrSigningRelayNotRequested = errors.New("agent: signing relay: session was spawned with no relay")

	// ErrSigningRelayAlreadyAttached reports a second attach.
	ErrSigningRelayAlreadyAttached = errors.New("agent: signing relay: already attached")

	// ErrSigningRelayNotAttached reports a sign before the control plane
	// attached the relay.
	ErrSigningRelayNotAttached = errors.New("agent: signing relay: not attached")

	// ErrSigningRelayLinkLost reports a sign after the relay's stream
	// dropped. It is final.
	ErrSigningRelayLinkLost = errors.New("agent: signing relay: link lost")

	// ErrSigningRelayRefused reports a sign the control plane refused or
	// could not answer.
	ErrSigningRelayRefused = errors.New("agent: signing relay: refused by the control plane")

	// ErrSigningRelayTimeout reports a sign the control plane did not answer
	// in time.
	ErrSigningRelayTimeout = errors.New("agent: signing relay: no answer in time")

	// errSigningRelayReleased is the cause a released relay's signs fail with.
	errSigningRelayReleased = errors.New("the session's relay was released")
)

// SigningRelayAttachError reports an attach the runtime refused.
type SigningRelayAttachError struct {
	// SessionID is the session the attach named.
	SessionID string

	// Sentinel is the refusal: one of the ErrSigningRelay* attach sentinels.
	Sentinel error
}

// Error implements the error interface.
func (e *SigningRelayAttachError) Error() string {
	return e.Sentinel.Error() + ": session " + e.SessionID
}

// Unwrap returns the sentinel.
func (e *SigningRelayAttachError) Unwrap() error { return e.Sentinel }

// SigningRelayLinkError reports a relayed sign that did not produce a
// signature.
type SigningRelayLinkError struct {
	// SessionID is the session the relay serves.
	SessionID string

	// Sentinel is the failure: one of the ErrSigningRelay* link sentinels.
	Sentinel error

	// Cause is the underlying error, nil when there is none.
	Cause error
}

// Error implements the error interface.
func (e *SigningRelayLinkError) Error() string {
	msg := e.Sentinel.Error() + ": session " + e.SessionID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *SigningRelayLinkError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Sentinel}
	}
	return []error{e.Sentinel, e.Cause}
}

// SigningRelayAlgorithmError reports a signature algorithm the relayed key
// cannot be asked for.
type SigningRelayAlgorithmError struct {
	// Algorithm is the algorithm asked for.
	Algorithm string
}

// Error implements the error interface.
func (e *SigningRelayAlgorithmError) Error() string {
	return "agent: signing relay: unsupported signature algorithm " + e.Algorithm
}

// SigningRelayStartError reports a relayed agent socket that could not be
// bound.
type SigningRelayStartError struct {
	// Cause is the go-xkms agent's refusal.
	Cause error
}

// Error implements the error interface.
func (e *SigningRelayStartError) Error() string {
	return "agent: signing relay: the relay socket could not be bound: " + e.Cause.Error()
}

// Unwrap returns the cause.
func (e *SigningRelayStartError) Unwrap() error { return e.Cause }
