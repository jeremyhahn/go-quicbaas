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
	"errors"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// Resume contract
// =============================================================================

// SessionResumer is implemented by backends that can reconstitute a session
// whose agent process is gone — the situation every persistent session is in
// after the daemon that hosted it restarts.
//
// A resume is not a fresh spawn wearing an old identifier. The implementation
// must either restore the conversation from the agent's own session store
// (ResumeModeNative) or prime a new process with the transcript the caller
// supplies (ResumeModeReplay), and it must report which of the two it did. A
// backend that can do neither returns a SessionResumeUnsupportedError rather
// than handing back a context-free agent under the old session ID.
type SessionResumer interface {
	// Resume reconstitutes req.SessionID and registers it under that same ID,
	// so callers never have to learn a new identifier. The returned Session
	// carries the mode actually used.
	Resume(ctx context.Context, req *ResumeRequest) (*ResumeResult, error)
}

// ResumeRequest carries everything needed to reconstitute one session: the
// original spawn configuration, the identifier the session must keep, and the
// transcript to replay when the backend has no session store of its own.
type ResumeRequest struct {
	// SessionID is the identifier the reconstituted session must be
	// registered under.
	SessionID string

	// Spawn is the original spawn configuration, replayed verbatim.
	Spawn *SpawnRequest

	// Transcript is the bounded conversation history to prime a fresh process
	// with. It is ignored by a backend that resumes natively. An empty
	// transcript on a backend that has no native resume is an error, not a
	// silent fresh start.
	Transcript *ReplayTranscript
}

// ResumeResult reports the outcome of a resume.
type ResumeResult struct {
	// Session is the reconstituted session, registered under the original ID.
	Session *Session

	// Mode is the mechanism that restored the conversation context.
	Mode ResumeMode

	// ReplayedTurns is the number of prior turns primed into the process.
	// Zero for a native resume, where the agent restores its own history.
	ReplayedTurns int

	// OmittedTurns is the number of oldest turns dropped to fit the replay
	// budget. Non-zero means the resumed agent's view of the conversation is
	// deliberately partial; it is told so in the priming message.
	OmittedTurns int
}

// =============================================================================
// Transcript replay
// =============================================================================

// DefaultReplayBudget bounds the transcript text primed into a resumed agent,
// in characters.
//
// A resumed agent is charged for every character of history it is given, so the
// budget is a cost control as much as a context-window control. 48 KiB is
// roughly 12k tokens at the conventional four-characters-per-token estimate —
// enough to carry the recent arc of a working session (a dozen or so
// substantial turns) while keeping the priming turn an order of magnitude
// cheaper than the context window it feeds.
const DefaultReplayBudget = 48 * 1024

// ReplayRole names the author of a replayed turn.
type ReplayRole string

const (
	// ReplayRoleUser marks a turn authored by the operator.
	ReplayRoleUser ReplayRole = "user"

	// ReplayRoleAssistant marks a turn authored by the agent.
	ReplayRoleAssistant ReplayRole = "assistant"
)

// ReplayTurn is one turn of the conversation being replayed.
type ReplayTurn struct {
	// Role is the author of the turn.
	Role ReplayRole

	// Text is the turn's content.
	Text string
}

// ReplayTranscript is the bounded conversation history handed to a backend that
// must prime a fresh agent process.
type ReplayTranscript struct {
	// SessionID identifies the session being resumed.
	SessionID string

	// SystemPrompt is the session's system prompt. It is configuration rather
	// than conversation, so it is always carried in full and never counted
	// against the budget.
	SystemPrompt string

	// Turns are the retained turns, oldest first.
	Turns []ReplayTurn

	// Omitted is the number of oldest turns dropped to fit the budget.
	Omitted int
}

// Empty reports whether the transcript carries no conversation to replay.
func (t *ReplayTranscript) Empty() bool {
	return t == nil || len(t.Turns) == 0
}

// buildReplayTranscript reads a session's durable event log and distils it into
// the bounded conversation history a fresh agent process is primed with.
//
// Only the two event kinds that carry conversation are replayed: the operator's
// prompts and the agent's own output. Tool invocations and their results are
// deliberately left out — replaying them would spend most of the budget on
// mechanics the agent will redo anyway, and a tool result restated as prose is
// not something the agent can act on as if it had run the tool itself.
func buildReplayTranscript(
	ctx context.Context,
	log SessionEventLog,
	tenantID, sessionID, systemPrompt string,
	budget int,
) (*ReplayTranscript, error) {
	if log == nil {
		return nil, &SessionTranscriptUnavailableError{
			SessionID: sessionID,
			Reason:    "no session event log is configured",
		}
	}
	if budget <= 0 {
		budget = DefaultReplayBudget
	}

	events, _, err := log.Range(ctx, tenantID, sessionID, 1, 0)
	if err != nil {
		return nil, &SessionTranscriptUnavailableError{
			SessionID: sessionID,
			Reason:    "failed to read the session event log: " + err.Error(),
			Cause:     err,
		}
	}

	turns := replayTurnsFromEvents(events)
	retained, omitted := boundReplayTurns(turns, budget)

	return &ReplayTranscript{
		SessionID:    sessionID,
		SystemPrompt: systemPrompt,
		Turns:        retained,
		Omitted:      omitted,
	}, nil
}

// replayTurnsFromEvents projects a session's events onto conversation turns.
// Consecutive assistant chunks belong to one turn and are merged, so a long
// streamed answer replays as the single message the operator saw.
func replayTurnsFromEvents(events []*SessionEvent) []ReplayTurn {
	turns := make([]ReplayTurn, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		switch event.Kind {
		case KindPromptSubmitted:
			if event.Prompt == nil || event.Prompt.Prompt == "" {
				continue
			}
			turns = append(turns, ReplayTurn{Role: ReplayRoleUser, Text: event.Prompt.Prompt})
		case KindAssistantChunk:
			if event.AssistantChunk == nil || event.AssistantChunk.Text == "" {
				continue
			}
			if n := len(turns); n > 0 && turns[n-1].Role == ReplayRoleAssistant {
				turns[n-1].Text += event.AssistantChunk.Text
				continue
			}
			turns = append(turns, ReplayTurn{Role: ReplayRoleAssistant, Text: event.AssistantChunk.Text})
		}
	}
	return turns
}

// boundReplayTurns trims a transcript to fit budget, dropping whole turns from
// the oldest end first. Recency is what a resumed agent needs: the turn it is
// about to answer follows the newest turns, not the oldest.
//
// A single turn larger than the entire budget is kept whole rather than cut
// mid-sentence — a truncated turn reads as a different statement from the one
// that was made, which is worse than being over budget by one turn.
func boundReplayTurns(turns []ReplayTurn, budget int) ([]ReplayTurn, int) {
	if len(turns) == 0 {
		return nil, 0
	}

	total := 0
	first := len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		size := len(turns[i].Text)
		if total+size > budget && first < len(turns) {
			break
		}
		total += size
		first = i
	}

	retained := make([]ReplayTurn, len(turns[first:]))
	copy(retained, turns[first:])
	return retained, first
}

// =============================================================================
// Priming message
// =============================================================================

// replayPrimingHeader introduces the replayed transcript to the agent.
func replayPrimingHeader() string {
	return workflowPrompts.MustRender("resume_priming", nil)
}

// PrimingMessage renders the transcript as the single message a fresh agent
// process is primed with.
//
// It states plainly that the history is a reproduction and, when the budget
// forced turns out, exactly how many are missing. An agent that is told its
// view is partial can say so; one that is handed a silently truncated history
// cannot.
func (t *ReplayTranscript) PrimingMessage() string {
	if t == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(replayPrimingHeader())
	if t.SystemPrompt != "" {
		b.WriteString("\n\n--- session instructions ---\n")
		b.WriteString(t.SystemPrompt)
	}
	b.WriteString("\n\n--- transcript of session ")
	b.WriteString(t.SessionID)
	b.WriteString(" ---\n")
	if t.Omitted > 0 {
		b.WriteString(workflowPrompts.MustRender("resume_priming_omitted", prompts.Vars{
			"count": strconv.Itoa(t.Omitted),
		}))
		b.WriteString("\n")
	}
	for _, turn := range t.Turns {
		b.WriteString("\n[")
		b.WriteString(string(turn.Role))
		b.WriteString("]\n")
		b.WriteString(turn.Text)
		b.WriteString("\n")
	}
	b.WriteString("\n--- end of transcript ---")
	return b.String()
}

// =============================================================================
// Errors
// =============================================================================

// Resume sentinels.
var (
	// ErrSessionNotResumable indicates a session cannot be reconstituted.
	ErrSessionNotResumable = errors.New("agent: session is not resumable")

	// ErrSessionResumeUnsupported indicates the backend has no mechanism for
	// reconstituting a session.
	ErrSessionResumeUnsupported = errors.New("agent: backend cannot resume sessions")

	// ErrSessionTranscriptUnavailable indicates the durable transcript needed
	// to restore a session's context could not be read.
	ErrSessionTranscriptUnavailable = errors.New("agent: session transcript unavailable")

	// ErrSessionResumeFailed indicates the backend failed to reconstitute a
	// session.
	ErrSessionResumeFailed = errors.New("agent: session resume failed")
)

// SessionNotResumableError reports a session that cannot be reconstituted
// because of what it is, rather than because something went wrong.
type SessionNotResumableError struct {
	// SessionID identifies the session.
	SessionID string

	// Reason states why the session cannot be resumed.
	Reason string
}

// Error implements the error interface.
func (e *SessionNotResumableError) Error() string {
	return ErrSessionNotResumable.Error() + ": " + e.SessionID + ": " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionNotResumableError) Unwrap() error { return ErrSessionNotResumable }

// SessionResumeUnsupportedError reports a backend with no resume mechanism.
type SessionResumeUnsupportedError struct {
	// Backend names the backend that was asked to resume.
	Backend string

	// SessionID identifies the session.
	SessionID string

	// Reason states what the backend would have needed to resume.
	Reason string
}

// Error implements the error interface.
func (e *SessionResumeUnsupportedError) Error() string {
	msg := ErrSessionResumeUnsupported.Error() + ": backend " + e.Backend + ", session " + e.SessionID
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionResumeUnsupportedError) Unwrap() error { return ErrSessionResumeUnsupported }

// SessionTranscriptUnavailableError reports that the conversation history
// needed to restore a session's context could not be obtained. It is returned
// rather than starting a context-free agent under the resumed session's ID.
type SessionTranscriptUnavailableError struct {
	// SessionID identifies the session.
	SessionID string

	// Reason states why the transcript is unavailable.
	Reason string

	// Cause is the underlying failure, when one exists.
	Cause error
}

// Error implements the error interface.
func (e *SessionTranscriptUnavailableError) Error() string {
	return ErrSessionTranscriptUnavailable.Error() + ": " + e.SessionID + ": " + e.Reason
}

// Unwrap exposes the sentinel and, when set, the underlying cause.
func (e *SessionTranscriptUnavailableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrSessionTranscriptUnavailable}
	}
	return []error{ErrSessionTranscriptUnavailable, e.Cause}
}

// SessionResumeError reports a failed attempt to reconstitute a session.
type SessionResumeError struct {
	// SessionID identifies the session.
	SessionID string

	// Mode is the mechanism that was attempted.
	Mode ResumeMode

	// Cause is the underlying failure.
	Cause error
}

// Error implements the error interface.
func (e *SessionResumeError) Error() string {
	msg := ErrSessionResumeFailed.Error() + ": " + e.SessionID
	if e.Mode != ResumeModeNone {
		msg += " (" + string(e.Mode) + ")"
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the sentinel and the underlying cause.
func (e *SessionResumeError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrSessionResumeFailed}
	}
	return []error{ErrSessionResumeFailed, e.Cause}
}
