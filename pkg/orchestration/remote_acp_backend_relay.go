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
	"errors"
	"io"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
)

// signingRelayHandle is one session's open relay. It is a pointer so a relay
// that ends on its own removes itself and never a successor's entry.
type signingRelayHandle struct {
	cancel context.CancelFunc
}

// localSigningTrait is this daemon's signing-agent trait, "" when unknown.
func (b *RemoteACPBackend) localSigningTrait() string {
	if b.signingTrait == nil {
		return ""
	}
	return b.signingTrait()
}

// openSigningRelay opens the session's relay stream, names the session, and
// waits — bounded by the spawn's own context — for the agent to attach it.
// From then on a goroutine answers the agent's sign requests from socket, the
// session's agent on this host, until the stream ends or Close cancels it.
//
// THE STREAM OUTLIVES THE SPAWN'S CONTEXT. It lives on a context of its own
// that only Close, or the agent ending it, cancels: the spawn's context is the
// dispatch call's, which returns long before the session's commit is signed.
func (b *RemoteACPBackend) openSigningRelay(
	ctx context.Context,
	client proto.FleetRuntimeClient,
	sessionID, socket string,
	keys [][]byte,
) error {
	relayCtx, cancel := context.WithCancel(context.Background())
	stream, err := client.SigningRelay(b.authCtx(relayCtx))
	if err != nil {
		cancel()
		return &SigningRelayOpenError{SessionID: sessionID, Reason: "the relay stream could not be opened", Cause: err}
	}
	open := &proto.SigningRelayControlFrame{Frame: &proto.SigningRelayControlFrame_Open{
		Open: &proto.SigningRelayOpen{TenantId: b.registration.TenantID, SessionId: sessionID},
	}}
	if err := stream.Send(open); err != nil {
		cancel()
		return &SigningRelayOpenError{SessionID: sessionID, Reason: "the relay could not name its session", Cause: err}
	}

	attached := make(chan error, 1)
	go func() {
		frame, recvErr := stream.Recv()
		switch {
		case recvErr != nil:
			attached <- recvErr
		case frame.GetAttached() == nil:
			attached <- &SigningRelayProtocolError{SessionID: sessionID, Reason: "the agent's first frame was not attached"}
		default:
			attached <- nil
		}
	}()
	select {
	case attachErr := <-attached:
		if attachErr != nil {
			cancel()
			return &SigningRelayOpenError{SessionID: sessionID, Reason: "the agent did not attach the relay", Cause: attachErr}
		}
	case <-ctx.Done():
		// Cancelling ends the stream, which releases the receive above.
		cancel()
		return &SigningRelayOpenError{SessionID: sessionID, Reason: "the spawn ended before the agent attached the relay", Cause: ctx.Err()}
	}

	handle := &signingRelayHandle{cancel: cancel}
	b.relays.Store(sessionID, handle)
	server := newSigningRelayServer(b.registration.TenantID, sessionID, b.registration.ID, socket, keys, b.signingAudit, b.logger)
	go func() {
		serveErr := server.serve(relayCtx, stream)
		// serve returns only when the stream ends, and always says why: EOF is
		// the agent closing the session, and a context cancelled before this
		// point is Close. Anything else is a link lost under a live session.
		closedHere := relayCtx.Err() != nil
		b.relays.CompareAndDelete(sessionID, handle)
		cancel()
		if !closedHere && !errors.Is(serveErr, io.EOF) {
			b.logger.Warnf("remote_acp_backend: signing relay for session %s ended: %v", sessionID, serveErr)
		}
	}()
	return nil
}

// stopSigningRelay ends the session's relay, if it has one.
func (b *RemoteACPBackend) stopSigningRelay(sessionID string) {
	held, ok := b.relays.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	if handle, isHandle := held.(*signingRelayHandle); isHandle {
		handle.cancel()
	}
}
