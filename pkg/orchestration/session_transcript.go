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

import "context"

// =============================================================================
// Session transcript — the conversation a replacement session continues
//
// A job runner that replaces a worker's session (its pooled account rotated
// under it, go-quicbaas#997) and whose project resumes rather than starts
// fresh (SessionRotationResume) carries the old session's conversation into
// the new one. The conversation is the one the durable session event log
// already holds for every session, local or remote, because every turn is
// dispatched through this service: it is the same bounded transcript a
// restored session is primed with (buildReplayTranscript), read BEFORE the old
// session is closed.
//
// The replacement itself goes through the runner's ordinary spawn, not through
// SessionResumer. A worker's session is a sandboxed container, often on a
// fleet host: its checkout, its signing socket and its placement are built by
// the spawn path, which a backend-level resume does not take. What resume adds
// on top of that spawn is exactly the conversation, and that is what this
// reads.
// =============================================================================

// SessionTranscriptReader reads the bounded conversation a session has had so
// far.
//
// It is a CAPABILITY narrowed the way SessionAccountFreshnessReader is, rather
// than a method on AgentService: a remote fleet node proxy or a test double keeps
// no session event log. JobRun.SessionTranscript reports such a service as
// SessionTranscriptUnavailableError.
type SessionTranscriptReader interface {
	// SessionTranscript returns the session's conversation, bounded by the
	// service's replay budget. It carries no system prompt: the session's
	// instructions are configuration a replacement is spawned with, not
	// conversation.
	SessionTranscript(ctx context.Context, sessionID string) (*ReplayTranscript, error)
}

// Compile-time interface satisfaction check.
var _ SessionTranscriptReader = (*agentServiceImpl)(nil)

// SessionTranscript implements SessionTranscriptReader.
//
// A caller naming another tenant's session is told it does not exist rather
// than that it belongs to someone else, the ownership rule every session verb
// applies.
func (s *agentServiceImpl) SessionTranscript(ctx context.Context, sessionID string) (*ReplayTranscript, error) {
	if sessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired, Message: "session_id is required"}
	}
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	if caller := TenantFromContext(ctx); caller != "" && caller != mem.TenantID {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	return buildReplayTranscript(ctx, s.sessionLog, mem.TenantID, sessionID, "", s.replayBudget())
}

// SessionTranscript reads sessionID's conversation through the job's agent
// service.
//
// A service that keeps no transcripts answers SessionTranscriptUnavailableError,
// the same typed error a service with no event log answers, so a caller has
// one case to handle for "there is no conversation to carry".
func (r *JobRun) SessionTranscript(ctx context.Context, sessionID string) (*ReplayTranscript, error) {
	reader, ok := r.Agents.(SessionTranscriptReader)
	if !ok {
		return nil, &SessionTranscriptUnavailableError{
			SessionID: sessionID,
			Reason:    "the job's agent service keeps no session transcripts",
		}
	}
	return reader.SessionTranscript(ctx, sessionID)
}
