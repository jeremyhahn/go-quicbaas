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

package orchestration

import (
	"context"
	"strings"
	"time"
)

// SessionTurnRole names who produced a turn.
type SessionTurnRole string

const (
	// SessionTurnRoleUser is a prompt submitted by an operator or by the
	// orchestrator acting on one's behalf.
	SessionTurnRoleUser SessionTurnRole = "user"

	// SessionTurnRoleAssistant is the agent's answer, coalesced from however
	// many fragments the backend streamed.
	SessionTurnRoleAssistant SessionTurnRole = "assistant"

	// SessionTurnRoleSystem is the orchestrator narrating something that
	// happened to the conversation: a spawn, a resume, an account rotation, a
	// failed turn, an ending.
	SessionTurnRoleSystem SessionTurnRole = "system"
)

// DefaultSessionHistoryLimit is the number of turns a history page returns
// when the caller names no limit. It is a screenful of scrollback with room to
// spare, and small enough that the default request never reads a large
// transcript end to end.
const DefaultSessionHistoryLimit = 100

// MaxSessionHistoryLimit caps a caller-supplied page size. A page is assembled
// in memory, so an unbounded limit would let one request pull an entire
// multi-thousand-turn transcript into the daemon's heap.
const MaxSessionHistoryLimit = 500

// sessionHistoryEventPage is how many raw events one Range call reads while
// folding a page of turns. Turns are coarser than events — a single answer
// with tool use is a dozen of them — so reading in blocks keeps the number of
// store round-trips proportional to the page rather than to the turn count.
const sessionHistoryEventPage = 256

// SessionToolUse summarises one tool call inside an assistant turn.
//
// It is a SUMMARY and deliberately not the invocation itself: the arguments
// and the output are already on the event stream for a client that wants to
// expand them, and copying them here would make every history page carry the
// full weight of the transcript it is meant to index.
type SessionToolUse struct {
	// InvocationID is the backend's identifier, and what pairs the call with
	// its result on the underlying event stream.
	InvocationID string `json:"invocation_id,omitempty"`

	// Name is the tool that was called ("Read", "Bash", "Edit", ...).
	Name string `json:"name"`

	// IsError reports that the tool returned an error shape or a non-zero
	// exit status.
	IsError bool `json:"is_error,omitempty"`

	// ResultBytes is the size of the tool's output as it was stored, after
	// any truncation. Zero when no result was recorded — a call the session
	// ended in the middle of.
	ResultBytes int64 `json:"result_bytes,omitempty"`
}

// SessionTurn is one record in a session's conversation.
type SessionTurn struct {
	// Sequence is the log sequence of the first event that formed this turn.
	// It is the cursor a caller pages from, so it addresses the turn exactly.
	Sequence uint64 `json:"sequence"`

	// SessionID and TenantID address the conversation the turn belongs to.
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id,omitempty"`

	// Role names the producer. See SessionTurnRole.
	Role SessionTurnRole `json:"role"`

	// Timestamp is when the turn's first event was produced. UTC.
	Timestamp time.Time `json:"timestamp"`

	// Text is the turn's narrative: the prompt, the coalesced answer, or the
	// system message. Capped at MaxSessionEventTextBytes per contributing
	// event on the way into the log.
	Text string `json:"text"`

	// TruncatedBytes is how much text was dropped by that cap across the
	// events this turn was folded from. Non-zero means Text is not the whole
	// of what was said, and the stored text carries a visible marker where it
	// was cut.
	TruncatedBytes int64 `json:"truncated_bytes,omitempty"`

	// Model is the model that produced an assistant turn, when the backend
	// reported one.
	Model string `json:"model,omitempty"`

	// StopReason is why an assistant turn ended ("end_turn", "max_tokens").
	StopReason string `json:"stop_reason,omitempty"`

	// Tools summarises the tool calls the agent made inside this turn.
	Tools []SessionToolUse `json:"tools,omitempty"`

	// Usage is the turn's token accounting, present only when the backend
	// reported it.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// SessionHistoryRequest addresses one page of a session's conversation.
type SessionHistoryRequest struct {
	// TenantID scopes the read. An empty value resolves the same way every
	// other orchestration record does — see logTenant — so an untenanted
	// in-process caller reads the transcript it wrote.
	TenantID string

	// SessionID is the conversation to read. Required.
	SessionID string

	// Cursor is the log sequence to resume from, taken from a previous page's
	// NextCursor. Zero starts at the beginning of the retained transcript.
	Cursor uint64

	// Limit is the maximum number of turns to return. Zero selects
	// DefaultSessionHistoryLimit; anything above MaxSessionHistoryLimit is
	// clamped to it.
	Limit int
}

// SessionHistoryPage is one page of a session's conversation.
type SessionHistoryPage struct {
	// SessionID and TenantID echo what was read.
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id,omitempty"`

	// Turns is the page, oldest first.
	Turns []*SessionTurn `json:"turns"`

	// NextCursor is the Cursor to pass for the following page, or 0 when the
	// transcript has been read to its end.
	NextCursor uint64 `json:"next_cursor,omitempty"`

	// OldestSequence is the lowest sequence the log still holds for this
	// session. A value above 1 means retention has dropped earlier turns, so
	// a client showing "the beginning of the conversation" can say honestly
	// that it is showing the beginning of what remains.
	OldestSequence uint64 `json:"oldest_sequence,omitempty"`
}

// SessionLogAccessor is implemented by an agent service that can hand out the
// transcript it appends to. Every transport's history handler reaches the log
// through this one declaration rather than through its own copy, so a service
// that gains or loses a log changes behaviour identically on all of them.
type SessionLogAccessor interface {
	// SessionEventLog returns the installed log, or nil when none is wired.
	SessionEventLog() SessionEventLog
}

// SessionHistoryError reports that a page of conversation could not be read.
type SessionHistoryError struct {
	// TenantID and SessionID address the transcript.
	TenantID  string
	SessionID string

	// Cause is the underlying failure.
	Cause error
}

// Error implements the error interface.
func (e *SessionHistoryError) Error() string {
	return "orchestration session history: tenant=" + e.TenantID +
		" session=" + e.SessionID + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying failure to errors.Is/As.
func (e *SessionHistoryError) Unwrap() error { return e.Cause }

// ReadSessionHistory folds a session's raw event log into the conversation an
// operator reads, one page at a time.
//
// IT IS A PROJECTION, NOT A SECOND STORE. The transcript is written once, to
// the session event log, by the one write path every producer shares; this
// derives the conversational view from it on read. The alternative — writing
// turn records alongside the events — would mean two records of the same
// conversation that can disagree, and a second keyspace to bound, retain and
// delete in step with the first.
//
// Turns are coarser than events: a streaming answer arrives as many
// assistant_chunk events and is presented as ONE assistant turn, with the tool
// calls it made along the way summarised on it. The page boundary always falls
// between turns, so a caller never has to stitch two pages together to read
// one answer.
func ReadSessionHistory(ctx context.Context, log SessionEventLog, req *SessionHistoryRequest) (*SessionHistoryPage, error) {
	if log == nil {
		return nil, &SessionHistoryError{Cause: ErrSessionLogRequired}
	}
	if req == nil || req.SessionID == "" {
		return nil, &SessionHistoryError{Cause: ErrSessionIDRequired}
	}
	tenantID := logTenant(req.TenantID)
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultSessionHistoryLimit
	}
	if limit > MaxSessionHistoryLimit {
		limit = MaxSessionHistoryLimit
	}

	page := &SessionHistoryPage{SessionID: req.SessionID, TenantID: tenantID}
	// The retained floor comes from the metadata record when there is one. A
	// session with no metadata is not an error here: an operator opening a
	// session whose transcript retention has removed, or that was never
	// registered, gets an empty page rather than a failure.
	if meta, err := log.GetMeta(ctx, tenantID, req.SessionID); err == nil {
		page.OldestSequence = meta.OldestSequence
	}

	folder := &sessionTurnFolder{sessionID: req.SessionID, tenantID: tenantID}
	cursor := req.Cursor
	for {
		events, next, err := log.Range(ctx, tenantID, req.SessionID, cursor, sessionHistoryEventPage)
		if err != nil {
			return nil, &SessionHistoryError{TenantID: tenantID, SessionID: req.SessionID, Cause: err}
		}
		for _, ev := range events {
			folder.add(ev)
		}
		// One turn past the page is enough to know where the next page starts
		// and that there IS one.
		if len(folder.turns) > limit || next == 0 {
			break
		}
		cursor = next
	}
	folder.close()

	turns := folder.turns
	if len(turns) > limit {
		page.NextCursor = turns[limit].Sequence
		turns = turns[:limit]
	}
	page.Turns = turns
	return page, nil
}

// sessionTurnFolder accumulates events into turns. It exists as a type rather
// than a closure because folding needs three pieces of state — the turn being
// built, the turns already closed, and the tool calls awaiting their results —
// carried across two loops (the event page and the pages themselves).
type sessionTurnFolder struct {
	sessionID string
	tenantID  string

	turns   []*SessionTurn
	open    *SessionTurn
	text    strings.Builder
	pending map[string]int // invocation id -> index into open.Tools
}

// add folds one event into the turn sequence.
func (f *sessionTurnFolder) add(ev *SessionEvent) {
	if ev == nil {
		return
	}
	switch ev.Kind {
	case KindPromptSubmitted:
		f.close()
		turn := f.begin(ev, SessionTurnRoleUser)
		if ev.Prompt != nil {
			f.text.WriteString(ev.Prompt.Prompt)
			turn.TruncatedBytes += ev.Prompt.TruncatedBytes
		}
		f.close()
	case KindAssistantChunk:
		if ev.AssistantChunk == nil {
			return
		}
		turn := f.openAssistant(ev)
		f.text.WriteString(ev.AssistantChunk.Text)
		turn.TruncatedBytes += ev.AssistantChunk.TruncatedBytes
		if ev.AssistantChunk.Model != "" {
			turn.Model = ev.AssistantChunk.Model
		}
		if ev.AssistantChunk.StopReason != "" {
			turn.StopReason = ev.AssistantChunk.StopReason
		}
		if ev.AssistantChunk.Usage != nil {
			turn.Usage = ev.AssistantChunk.Usage
		}
	case KindToolInvocation:
		if ev.ToolInvocation == nil {
			return
		}
		turn := f.openAssistant(ev)
		turn.Tools = append(turn.Tools, SessionToolUse{
			InvocationID: ev.ToolInvocation.InvocationID,
			Name:         ev.ToolInvocation.Name,
		})
		if id := ev.ToolInvocation.InvocationID; id != "" {
			if f.pending == nil {
				f.pending = map[string]int{}
			}
			f.pending[id] = len(turn.Tools) - 1
		}
	case KindToolResult:
		f.applyToolResult(ev)
	case KindTurnCompleted:
		f.completeTurn(ev)
	case KindTurnFailed:
		f.close()
		if ev.TurnFailed == nil {
			return
		}
		f.begin(ev, SessionTurnRoleSystem)
		f.text.WriteString(ev.TurnFailed.Error)
		f.close()
	case KindSystemMessage:
		f.close()
		if ev.System == nil {
			return
		}
		f.begin(ev, SessionTurnRoleSystem)
		f.text.WriteString(ev.System.Message)
		f.close()
	case KindSessionResumed:
		f.close()
		if ev.Resumed == nil {
			return
		}
		f.begin(ev, SessionTurnRoleSystem)
		f.text.WriteString("session resumed (" + ev.Resumed.ResumedBy + ")")
		f.close()
	case KindSessionClosed:
		f.close()
		if ev.Closed == nil {
			return
		}
		f.begin(ev, SessionTurnRoleSystem)
		f.text.WriteString("session closed (" + ev.Closed.Reason + ")")
		if ev.Closed.Detail != "" {
			f.text.WriteString(": " + ev.Closed.Detail)
		}
		f.close()
	}
	// Every other kind — presence, thoughts, file operations, account
	// rotations, stateless inference — stays off the conversation and remains
	// available on the raw event stream. Presence is rendered by its own
	// indicator, and a chain of thought is the agent's working, not its
	// answer; folding either into the transcript would bury the conversation
	// the operator opened the page to read.
}

// begin starts a turn of the given role at this event and returns it.
func (f *sessionTurnFolder) begin(ev *SessionEvent, role SessionTurnRole) *SessionTurn {
	f.open = &SessionTurn{
		Sequence:  ev.Sequence,
		SessionID: f.sessionID,
		TenantID:  f.tenantID,
		Role:      role,
		Timestamp: ev.Timestamp,
	}
	f.text.Reset()
	f.pending = nil
	return f.open
}

// openAssistant returns the assistant turn in progress, starting one at this
// event when the previous turn has already closed.
func (f *sessionTurnFolder) openAssistant(ev *SessionEvent) *SessionTurn {
	if f.open != nil && f.open.Role == SessionTurnRoleAssistant {
		return f.open
	}
	f.close()
	return f.begin(ev, SessionTurnRoleAssistant)
}

// applyToolResult records a result against the invocation that produced it.
// A result whose invocation is not in the open turn is dropped rather than
// invented: it belongs to a turn retention has already removed, and attaching
// it to the current one would report a tool call that did not happen here.
func (f *sessionTurnFolder) applyToolResult(ev *SessionEvent) {
	if ev.ToolResult == nil || f.open == nil {
		return
	}
	idx, ok := f.pending[ev.ToolResult.InvocationID]
	if !ok || idx >= len(f.open.Tools) {
		return
	}
	f.open.Tools[idx].IsError = ev.ToolResult.IsError
	f.open.Tools[idx].ResultBytes = int64(len(ev.ToolResult.Content))
}

// completeTurn stamps a terminal turn_completed onto the assistant turn it
// closes. A terminal event with no answer above it — a turn whose output never
// reached the log — closes nothing and is dropped, because a turn record with
// usage and no text tells an operator less than its absence does.
func (f *sessionTurnFolder) completeTurn(ev *SessionEvent) {
	if ev.TurnCompleted == nil || f.open == nil || f.open.Role != SessionTurnRoleAssistant {
		return
	}
	if ev.TurnCompleted.Model != "" {
		f.open.Model = ev.TurnCompleted.Model
	}
	if ev.TurnCompleted.StopReason != "" {
		f.open.StopReason = ev.TurnCompleted.StopReason
	}
	if ev.TurnCompleted.Usage != nil {
		f.open.Usage = ev.TurnCompleted.Usage
	}
	f.close()
}

// close finishes the turn in progress, if any, and appends it.
func (f *sessionTurnFolder) close() {
	if f.open == nil {
		return
	}
	f.open.Text = f.text.String()
	f.turns = append(f.turns, f.open)
	f.open = nil
	f.text.Reset()
	f.pending = nil
}
