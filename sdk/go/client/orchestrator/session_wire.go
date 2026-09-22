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

package orchestrator

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// This file is the single source of truth for translating the orchestrator
// service's session wire shapes into the flat SDK response types.
//
// A prompt on an existing session is asynchronous on every transport: the
// server persists a turn_started marker, dispatches the turn on a background
// context, and answers with an acknowledgement carrying a turn ID. The turn's
// output — assistant chunks, tool calls, and the terminal turn_completed /
// turn_failed event — arrives on the durable session event stream, reachable
// through SubscribeSessionEvents and GetSessionHistory.
//
// A prompt with no session ID is a different operation: a stateless one-shot
// against a named backend, which has no event log to publish to and therefore
// answers with the completed turn inline. Both shapes travel on the same wire
// type and are told apart by the status field, so every transport keeps one
// decode path.
//
// The service emits a structured turn — content blocks plus a stop reason —
// while the SDK exposes a flattened {response, done} view. The two shapes share
// no field names, so decoding the service payload directly into the SDK struct
// yields a zero-valued response: that is how a completed agent turn used to be
// reported as "Done: false" with no text. Every transport MUST route session
// responses through the converters below instead of unmarshalling the service
// payload into an SDK type.

// TurnStatusAccepted is the status the server reports when it has accepted a
// turn for background execution. It mirrors orchestration.TurnStatusAccepted;
// the constant is restated here so the SDK stays free of a dependency on the
// server-side orchestration package.
const TurnStatusAccepted = "accepted"

// EmptyTurnError reports a stateless prompt response that carried neither
// content nor a stop reason. A stateless prompt is synchronous, so a
// successful call always describes a finished turn; a response with nothing in
// it means the backend produced no turn at all and must surface as an error
// rather than as an empty success.
type EmptyTurnError struct {
	// SessionID is the session whose turn came back empty. It may be empty
	// when the response omitted it as well.
	SessionID string
}

// Error implements the error interface.
func (e *EmptyTurnError) Error() string {
	if e.SessionID == "" {
		return "orchestrator: agent turn returned no content and no stop reason"
	}
	return "orchestrator: agent turn returned no content and no stop reason: session " + e.SessionID
}

// MissingTurnIDError reports an acceptance acknowledgement with no turn ID.
// The turn ID is what correlates the ack with the turn_started event and its
// terminal event, so an ack without one leaves the caller unable to follow the
// turn it just started.
type MissingTurnIDError struct {
	// SessionID is the session whose acknowledgement was malformed.
	SessionID string
}

// Error implements the error interface.
func (e *MissingTurnIDError) Error() string {
	if e.SessionID == "" {
		return "orchestrator: turn accepted without a turn ID"
	}
	return "orchestrator: turn accepted without a turn ID: session " + e.SessionID
}

// PromptContentBlock is one fragment of an agent turn's content as emitted by
// the orchestrator service. Only text blocks carry rendered output; other
// types (for example tool_use) are structural and contribute no text.
type PromptContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// isText reports whether the block carries rendered text. An unset type is
// treated as text: a block that carries text but no type is still the agent's
// output, and dropping it is the very failure this package guards against.
func (b PromptContentBlock) isText() bool {
	return b.Type == "text" || b.Type == ""
}

// PromptSessionWire mirrors the orchestrator service's PromptSession response
// as serialized by the JSON transports (REST, QUIC, MCP). It deliberately
// omits the service's envelope timestamp: the transports disagree on its
// encoding (epoch nanoseconds over REST and QUIC, RFC 3339 over MCP) and the
// SDK does not expose it, so leaving it out keeps one wire type valid
// everywhere.
type PromptSessionWire struct {
	SessionID string `json:"session_id"`

	// Status is TurnStatusAccepted on the asynchronous session-turn path and
	// empty on the stateless one-shot path. It is the discriminator between
	// the two response shapes.
	Status string `json:"status"`

	// TurnID identifies an accepted turn. Set only when Status is
	// TurnStatusAccepted.
	TurnID string `json:"turn_id"`

	// Sequence is the session-log sequence of the accepted turn's
	// turn_started event. Set only when Status is TurnStatusAccepted.
	Sequence uint64 `json:"sequence"`

	Content    []PromptContentBlock `json:"content"`
	Model      string               `json:"model"`
	StopReason string               `json:"stop_reason"`
}

// Response converts the wire payload into the SDK response type, dispatching
// on Status so an accepted asynchronous turn and a completed stateless turn
// each get their own construction rules.
func (w *PromptSessionWire) Response() (*PromptSessionResponse, error) {
	if w.Status == TurnStatusAccepted {
		return NewPromptSessionAccepted(w.SessionID, w.TurnID, w.Sequence, w.Model)
	}
	return NewPromptSessionResponse(w.SessionID, w.Content, w.Model, w.StopReason)
}

// NewPromptSessionAccepted builds the SDK response for a turn the server
// accepted for background execution. Done is false: the turn has only started.
// Callers follow it on the session event stream, starting at Sequence, until
// they observe the turn_completed or turn_failed event carrying TurnID.
func NewPromptSessionAccepted(sessionID, turnID string, sequence uint64, model string) (*PromptSessionResponse, error) {
	if turnID == "" {
		return nil, &MissingTurnIDError{SessionID: sessionID}
	}
	return &PromptSessionResponse{
		SessionID: sessionID,
		TurnID:    turnID,
		Sequence:  sequence,
		Accepted:  true,
		Done:      false,
		Model:     model,
	}, nil
}

// NewPromptSessionResponse builds the flat SDK response from a completed agent
// turn. It is used by every transport — the JSON transports feed it decoded
// wire blocks, gRPC and the embedded transport feed it blocks converted from
// their native representations — so the flattening and completion rules live
// in exactly one place.
//
// Reaching this function means a stateless prompt returned without error,
// which by contract means the agent's turn finished; Done is therefore true. A
// turn that carries neither text nor a stop reason is not a turn at all and
// yields *EmptyTurnError.
func NewPromptSessionResponse(sessionID string, blocks []PromptContentBlock, model, stopReason string) (*PromptSessionResponse, error) {
	var text strings.Builder
	for _, block := range blocks {
		if block.isText() {
			text.WriteString(block.Text)
		}
	}

	response := text.String()
	if response == "" && stopReason == "" {
		return nil, &EmptyTurnError{SessionID: sessionID}
	}

	return &PromptSessionResponse{
		SessionID:  sessionID,
		Response:   response,
		Done:       true,
		Model:      model,
		StopReason: stopReason,
	}, nil
}

// SessionStatusWire mirrors the orchestrator service's session status shape,
// shared by the SessionStatus response and by each entry of the ListSessions
// response. The service names the lifecycle field "state"; the SDK exposes it
// as Status.
type SessionStatusWire struct {
	SessionID    string `json:"session_id"`
	ProjectID    string `json:"project_id"`
	Backend      string `json:"backend"`
	Model        string `json:"model"`
	State        string `json:"state"`
	ContainerID  string `json:"container_id"`
	CreatedAt    int64  `json:"created_at"`
	LastActivity int64  `json:"last_activity"`

	// JobID names the orchestration job the session works for, if any.
	JobID string `json:"job_id"`

	// Runner is the job runner that drove that job — "sdlc", "review",
	// "triage", "workflow". It is what says which ROLE the session played,
	// and the service resolves it per page so a consumer never has to read
	// the job list to find out (see SessionInfo in pkg/orchestration).
	Runner string `json:"runner"`
}

// Response converts the wire status into the SDK response type.
func (w *SessionStatusWire) Response() *SessionStatusResponse {
	return &SessionStatusResponse{
		SessionID:   w.SessionID,
		Backend:     w.Backend,
		Model:       w.Model,
		Status:      w.State,
		ContainerID: w.ContainerID,
		CreatedAt:   w.CreatedAt,
		LastActive:  w.LastActivity,
		JobID:       w.JobID,
		Runner:      w.Runner,
	}
}

// ListSessionsWire mirrors the orchestrator service's ListSessions response,
// including the paging envelope every list endpoint answers with.
type ListSessionsWire struct {
	Sessions []SessionStatusWire `json:"sessions"`
	page.Envelope
}

// Response converts the wire session list into the SDK response type. Count is
// the size of THIS PAGE and is derived here because the service does not put
// it on the wire; Total, which the service does send, is how many sessions
// matched before the page was cut.
func (w *ListSessionsWire) Response() *ListSessionsResponse {
	sessions := make([]*SessionStatusResponse, 0, len(w.Sessions))
	for i := range w.Sessions {
		sessions = append(sessions, w.Sessions[i].Response())
	}
	return &ListSessionsResponse{
		Sessions: sessions,
		Count:    len(sessions),
		Envelope: w.Envelope,
	}
}

// CloseSessionWire mirrors the orchestrator service's CloseSession response.
type CloseSessionWire struct {
	SessionID string `json:"session_id"`
}

// Response converts the wire acknowledgement into the SDK response type. The
// service answers CloseSession only after the session is gone, so a decoded
// response always means Closed.
func (w *CloseSessionWire) Response() *CloseSessionResponse {
	return &CloseSessionResponse{SessionID: w.SessionID, Closed: true}
}
