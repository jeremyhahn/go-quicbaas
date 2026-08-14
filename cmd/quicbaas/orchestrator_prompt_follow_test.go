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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// turnEnvelope builds a session-event envelope carrying the given raw event
// payload, mirroring what the server writes to the durable log.
func turnEnvelope(t *testing.T, seq uint64, payload map[string]interface{}) *orchestratorSDK.SessionEventEnvelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &orchestratorSDK.SessionEventEnvelope{
		SessionID:   "sess-1",
		TenantID:    "tenant-1",
		Sequence:    seq,
		Timestamp:   time.Unix(0, 0).UTC(),
		PayloadJSON: raw,
	}
}

// acceptedAck is the response the server returns for a dispatched turn.
func acceptedAck() *orchestratorSDK.PromptSessionResponse {
	return &orchestratorSDK.PromptSessionResponse{
		SessionID: "sess-1",
		TurnID:    "turn-1",
		Sequence:  2,
		Accepted:  true,
	}
}

// TestFollowPromptTurn_RendersTurnUntilCompletion asserts the CLI's synchronous
// feel over an asynchronous transport: the operator sees the prompt echo, the
// agent's output, and then the command exits when the turn's own terminal event
// arrives — not when the stream happens to end.
func TestFollowPromptTurn_RendersTurnUntilCompletion(t *testing.T) {
	client := &mockOrchestratorClient{}
	var gotReq *orchestratorSDK.SubscribeSessionEventsRequest
	setSubscribeSessionEventsFn(client, func(_ context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		gotReq = req
		ch := make(chan *orchestratorSDK.SessionEventEnvelope, 4)
		ch <- turnEnvelope(t, 2, map[string]interface{}{
			"kind":         "turn_started",
			"turn_started": map[string]interface{}{"turn_id": "turn-1"},
		})
		ch <- turnEnvelope(t, 3, map[string]interface{}{
			"kind":            "assistant_chunk",
			"assistant_chunk": map[string]interface{}{"text": "the answer"},
		})
		ch <- turnEnvelope(t, 4, map[string]interface{}{
			"kind":           "turn_completed",
			"turn_completed": map[string]interface{}{"turn_id": "turn-1"},
		})
		// Deliberately left open: the follow must end on the terminal event,
		// not on the channel closing.
		return ch, nil
	})

	var out bytes.Buffer
	if err := followPromptTurn(client, acceptedAck(), "table", &out); err != nil {
		t.Fatalf("followPromptTurn: %v", err)
	}

	if gotReq == nil {
		t.Fatal("the follow must open a subscription")
	}
	if gotReq.FromSequence != 2 {
		t.Fatalf("subscribed from sequence %d, want the ack's sequence 2", gotReq.FromSequence)
	}
	if !gotReq.Follow {
		t.Fatal("the follow must tail live events, not just replay history")
	}
	if got := out.String(); !strings.Contains(got, "the answer") {
		t.Fatalf("the agent's output must reach the operator; got:\n%s", got)
	}
}

// TestFollowPromptTurn_ReportsFailedTurn asserts a failed turn exits non-zero
// with the agent's own reason, so a script can tell "the agent broke" from "the
// agent finished".
func TestFollowPromptTurn_ReportsFailedTurn(t *testing.T) {
	client := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(client, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope, 1)
		ch <- turnEnvelope(t, 3, map[string]interface{}{
			"kind":        "turn_failed",
			"turn_failed": map[string]interface{}{"turn_id": "turn-1", "error": "backend exploded"},
		})
		return ch, nil
	})

	var out bytes.Buffer
	err := followPromptTurn(client, acceptedAck(), "table", &out)
	var failed *TurnFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("err = %v, want *TurnFailedError", err)
	}
	if failed.Reason != "backend exploded" {
		t.Fatalf("reason = %q, want %q", failed.Reason, "backend exploded")
	}
}

// TestFollowPromptTurn_IgnoresOtherTurns asserts a follower waits for its own
// turn. Several operators can drive one session, so a terminal event for a
// different turn must not end this follow.
func TestFollowPromptTurn_IgnoresOtherTurns(t *testing.T) {
	client := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(client, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope, 2)
		ch <- turnEnvelope(t, 3, map[string]interface{}{
			"kind":           "turn_completed",
			"turn_completed": map[string]interface{}{"turn_id": "someone-elses-turn"},
		})
		ch <- turnEnvelope(t, 4, map[string]interface{}{
			"kind":           "turn_completed",
			"turn_completed": map[string]interface{}{"turn_id": "turn-1"},
		})
		return ch, nil
	})

	var out bytes.Buffer
	if err := followPromptTurn(client, acceptedAck(), "json", &out); err != nil {
		t.Fatalf("followPromptTurn: %v", err)
	}
	// Both events were rendered, proving the follow did not stop at the first.
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("rendered %d events, want 2; got:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "someone-elses-turn") {
		t.Fatalf("the other operator's turn must still be rendered; got:\n%s", lines[0])
	}
}

// TestFollowPromptTurn_StreamEndsMidTurn asserts a dropped stream is reported
// rather than mistaken for a finished turn. The turn keeps running server-side,
// so the error tells the operator how to reattach.
func TestFollowPromptTurn_StreamEndsMidTurn(t *testing.T) {
	client := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(client, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope)
		close(ch)
		return ch, nil
	})

	var out bytes.Buffer
	err := followPromptTurn(client, acceptedAck(), "table", &out)
	var ended *TurnStreamEndedError
	if !errors.As(err, &ended) {
		t.Fatalf("err = %v, want *TurnStreamEndedError", err)
	}
	if !strings.Contains(ended.Error(), "orchestrator attach sess-1") {
		t.Fatalf("the error must tell the operator how to reattach; got %q", ended.Error())
	}
}

// TestFollowPromptTurn_SubscribeFailure asserts a transport that cannot open
// the stream surfaces its error instead of exiting zero.
func TestFollowPromptTurn_SubscribeFailure(t *testing.T) {
	client := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(client, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		return nil, errors.New("stream unavailable")
	})

	var out bytes.Buffer
	if err := followPromptTurn(client, acceptedAck(), "table", &out); err == nil {
		t.Fatal("expected an error when the subscription cannot be opened")
	}
}

// TestFollowPromptTurn_MalformedPayload asserts an undecodable event is a hard
// error: silently skipping it would let the follow hang forever waiting for a
// terminal event it can no longer recognise.
func TestFollowPromptTurn_MalformedPayload(t *testing.T) {
	client := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(client, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope, 1)
		ch <- &orchestratorSDK.SessionEventEnvelope{
			SessionID:   "sess-1",
			Sequence:    3,
			PayloadJSON: []byte("{not json"),
		}
		return ch, nil
	})

	var out bytes.Buffer
	if err := followPromptTurn(client, acceptedAck(), "table", &out); err == nil {
		t.Fatal("expected an error for an undecodable event payload")
	}
}

// TestFormatOrchPromptResponse_AcceptedTurn asserts the non-following output
// tells the operator the turn was dispatched and how to watch it, rather than
// printing "Done: false", which reads like a failure.
func TestFormatOrchPromptResponse_AcceptedTurn(t *testing.T) {
	var out bytes.Buffer
	if err := formatOrchPromptResponse(acceptedAck(), "table", &out); err != nil {
		t.Fatalf("formatOrchPromptResponse: %v", err)
	}
	got := out.String()
	for _, want := range []string{"turn-1", "accepted", "orchestrator attach sess-1 --from 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output must contain %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Done:") {
		t.Fatalf("an accepted turn must not be reported with a Done field; got:\n%s", got)
	}
}
