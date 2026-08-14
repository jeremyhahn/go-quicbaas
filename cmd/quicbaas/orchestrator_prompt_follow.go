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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TurnFailedError reports that the agent turn the CLI was following ended in a
// failure. The turn ran to a definite conclusion, so this is not a transport
// error; it is the agent's own outcome, surfaced as a non-zero exit so scripts
// can branch on it.
type TurnFailedError struct {
	// SessionID is the session the turn ran in.
	SessionID string
	// TurnID identifies the failed turn in the session transcript.
	TurnID string
	// Reason is the failure text the orchestrator recorded.
	Reason string
}

// Error implements the error interface.
func (e *TurnFailedError) Error() string {
	return fmt.Sprintf("agent turn %s failed in session %s: %s", e.TurnID, e.SessionID, e.Reason)
}

// TurnStreamEndedError reports that the session event stream closed before the
// turn produced a terminal event. The turn itself is unaffected — it keeps
// running on the server — but the CLI can no longer observe it, so it says so
// instead of exiting zero as though the turn had finished.
type TurnStreamEndedError struct {
	// SessionID is the session whose stream ended.
	SessionID string
	// TurnID identifies the turn that was still in flight.
	TurnID string
}

// Error implements the error interface.
func (e *TurnStreamEndedError) Error() string {
	return fmt.Sprintf("event stream for session %s ended while turn %s was still running; reattach with: quicbaas orchestrator attach %s",
		e.SessionID, e.TurnID, e.SessionID)
}

// turnLifecycleEvent is the slice of a session event the follower needs to
// decide whether the turn it is watching has ended. Decoding only these fields
// keeps the CLI independent of the server-side orchestration package, matching
// genericEvent in orchestrator_attach.go.
type turnLifecycleEvent struct {
	Kind          string `json:"kind"`
	TurnCompleted *struct {
		TurnID string `json:"turn_id"`
	} `json:"turn_completed,omitempty"`
	TurnFailed *struct {
		TurnID string `json:"turn_id"`
		Error  string `json:"error"`
	} `json:"turn_failed,omitempty"`
}

// followPromptTurn renders an accepted turn as it happens.
//
// It subscribes at the acknowledgement's sequence — the sequence of the turn's
// own turn_started event — so the replay-then-tail stream delivers the whole
// turn with no gap and no duplicates, even when the agent finished before the
// subscription opened. The follow ends when the turn's terminal event arrives;
// events for other turns on the same session are rendered too, because an
// operator watching a shared session wants to see them.
//
// Interrupting the follow (ctrl-c) only detaches the CLI. The turn keeps
// running on the server, which is the point of the asynchronous contract.
func followPromptTurn(client OrchestratorClient, ack *orchestratorSDK.PromptSessionResponse, format string, out io.Writer) error {
	// No deadline: the whole reason the turn is asynchronous is that it runs
	// far longer than any request timeout. Cancellation comes from the
	// operator, via a signal.
	ctx, cancel := context.WithCancel(orchTenantContext())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	events, err := client.SubscribeSessionEvents(ctx, &orchestratorSDK.SubscribeSessionEventsRequest{
		SessionID:    ack.SessionID,
		FromSequence: ack.Sequence,
		Follow:       true,
	})
	if err != nil {
		return formatError(err)
	}

	renderer, err := pickPromptFollowRenderer(format, out)
	if err != nil {
		return formatError(err)
	}

	for env := range events {
		if renderErr := renderer.Render(env); renderErr != nil {
			return formatError(renderErr)
		}
		done, turnErr := turnTerminal(env, ack)
		if turnErr != nil {
			return turnErr
		}
		if done {
			return nil
		}
	}

	// The channel closed without a terminal event. If the operator asked for
	// that (ctrl-c), it is not an error; otherwise the stream dropped.
	if ctx.Err() != nil {
		return nil
	}
	return &TurnStreamEndedError{SessionID: ack.SessionID, TurnID: ack.TurnID}
}

// turnTerminal reports whether the envelope closes the turn identified by the
// acknowledgement, returning *TurnFailedError when the turn ended badly. A
// terminal event for a different turn on the same session is ignored: several
// operators may drive one session, and each follower waits for its own turn.
func turnTerminal(env *orchestratorSDK.SessionEventEnvelope, ack *orchestratorSDK.PromptSessionResponse) (bool, error) {
	var ev turnLifecycleEvent
	if err := json.Unmarshal(env.PayloadJSON, &ev); err != nil {
		return false, &OpError{
			Op:    fmt.Sprintf("prompt: decode payload for session=%s seq=%d", env.SessionID, env.Sequence),
			Cause: err,
		}
	}
	switch {
	case ev.TurnCompleted != nil && ev.TurnCompleted.TurnID == ack.TurnID:
		return true, nil
	case ev.TurnFailed != nil && ev.TurnFailed.TurnID == ack.TurnID:
		return true, &TurnFailedError{
			SessionID: ack.SessionID,
			TurnID:    ack.TurnID,
			Reason:    ev.TurnFailed.Error,
		}
	}
	return false, nil
}

// pickPromptFollowRenderer selects the event renderer for the follow stream.
// It reuses the attach renderers so `prompt --follow` and `attach` present a
// transcript identically; the table format (the CLI default for one-shot
// output) has no per-event meaning and maps to the human-readable renderer.
func pickPromptFollowRenderer(format string, out io.Writer) (attachRenderer, error) {
	switch format {
	case "json", "ndjson":
		return &jsonAttachRenderer{out: out}, nil
	default:
		return &prettyAttachRenderer{out: out}, nil
	}
}
