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
	"strconv"
	"time"
)

// =============================================================================
// Durable record construction
// =============================================================================

// newDurableSessionRecord builds the record persisted for a spawned session.
//
// The environment is copied rather than referenced, and the derived MCP server
// payload is dropped: it encodes the paths of certificates minted for this one
// spawn, which are deleted when the session closes. A resume re-derives it from
// the retained MCPServers against freshly minted material, so a reconstituted
// agent never presents a certificate that no longer exists.
func newDurableSessionRecord(
	req *SpawnSessionRequest,
	tenantID, workDir, model string,
	sandboxMode SandboxMode,
	trustLevel TrustLevel,
	hostAffinity string,
	backendLocal bool,
) *SessionRecord {
	record := &SessionRecord{
		TenantID:        tenantID,
		ProjectID:       req.ProjectID,
		JobID:           req.JobID,
		Backend:         req.Backend,
		Model:           model,
		SystemPrompt:    req.SystemPrompt,
		WorkDir:         workDir,
		ExtraArgs:       append([]string(nil), req.ExtraArgs...),
		MCPServers:      append([]MCPServerConfig(nil), req.MCPServers...),
		SandboxMode:     sandboxMode,
		TrustLevel:      trustLevel,
		ContainerImage:  req.ContainerImage,
		ExtraMounts:     append([]ContainerMount(nil), req.ExtraMounts...),
		HostAffinity:    hostAffinity,
		BackendLocal:    backendLocal,
		Persistent:      req.Persistent,
		Status:          SessionStateActive,
		AccountID:       req.AccountID,
		AccountStrategy: req.AccountStrategy,
		AccountAuto:     req.AccountAuto,
		AccountAllow:    append([]string(nil), req.AccountAllow...),
	}
	if len(req.Env) > 0 {
		record.Env = make(map[string]string, len(req.Env))
		for k, v := range req.Env {
			if k == MCPServersEnvVar {
				continue
			}
			record.Env[k] = v
		}
	}
	return record
}

// spawnRequestFromRecord rebuilds the backend spawn request a session was
// originally created with, so a reconstituted process runs under exactly the
// parameters the operator asked for.
//
// The pooled provider account is replayed as a PIN, never as a selection: the
// record holds the account the original spawn RESOLVED to, and AccountAuto is
// deliberately left false so no strategy runs a second time. Re-running
// selection here would be a silent correctness bug — a least-usage pool moves
// between the spawn and the resume, so the reconstituted process would
// authenticate as a different provider account than the conversation it is
// continuing was billed to, breaking both a static pin and per-account cost
// attribution. A record with no account (the spawn never used the pool) still
// produces a request with no account, so the untouched default path is
// unchanged.
//
// ROTATION IS THE ONE EXCEPTION, and it is explicit rather than incidental:
// rotationSpawnRequestFromRecord below clears the pin and asks for a fresh
// selection. A plain resume never does. The distinction is the whole invariant
// — an ordinary reconstitution must not be able to change which account a
// conversation is billed to, while an operator (or a detected provider limit)
// deliberately moving the session must.
func spawnRequestFromRecord(record *SessionRecord) *SpawnRequest {
	return &SpawnRequest{
		ProjectID:      record.ProjectID,
		WorkDir:        record.WorkDir,
		Model:          record.Model,
		MCPServers:     append([]MCPServerConfig(nil), record.MCPServers...),
		SystemPrompt:   record.SystemPrompt,
		Env:            record.Clone().Env,
		ExtraArgs:      append([]string(nil), record.ExtraArgs...),
		Persistent:     record.Persistent,
		SandboxMode:    record.SandboxMode,
		Sandboxed:      record.SandboxMode == SandboxModeRequired,
		TrustLevel:     record.TrustLevel,
		ContainerImage: record.ContainerImage,
		ExtraMounts:    append([]ContainerMount(nil), record.ExtraMounts...),
		AccountID:      record.AccountID,
		AccountAllow:   append([]string(nil), record.AccountAllow...),
		// The job is replayed so a resumed session's credential delivery grant
		// carries the same scope the original spawn's did; without it a resume
		// would take out a job-less grant that the job's own revocation sweep
		// could not find.
		JobID: record.JobID,
	}
}

// rotationSpawnRequestFromRecord builds the spawn request that moves a session
// onto a DIFFERENT pooled account.
//
// It is spawnRequestFromRecord with the account pin replaced by the session's
// own rotation policy: the resolve scope the spawn used (AccountAuto plus its
// strategy), narrowed by whatever explicit pool the operator declared, and with
// the outgoing account excluded outright. Excluding it is belt and braces — it
// has already been marked limited durably — but it makes forward progress a
// property of the request rather than of a timing window.
func rotationSpawnRequestFromRecord(record *SessionRecord, excludeAccountID string) *SpawnRequest {
	req := spawnRequestFromRecord(record)
	req.AccountID = ""
	req.AccountAuto = true
	req.AccountStrategy = record.AccountStrategy
	// "pinned" is a marker the resolver writes when a caller named the account
	// outright; it is not a registered strategy, so replaying it as one would
	// fail the lookup. An allow-list-only session falls back to the registry
	// default the same way a first spawn does.
	if req.AccountStrategy == pinnedAccountStrategyName {
		req.AccountStrategy = ""
	}
	if excludeAccountID != "" {
		req.AccountExclude = []string{excludeAccountID}
	}
	return req
}

// =============================================================================
// Boot-time restoration
// =============================================================================

// LoadPersistedSessions restores the session registry from the durable store.
//
// It runs once at startup, before any request is served, and is the reason a
// session survives a daemon restart at all: without it the registry is empty
// and a prompt against a session spawned by the previous process fails with
// "either backend or session_id must be provided".
//
// Restoration is deliberately lazy. A persistent session's agent process died
// with the previous daemon, and recreating every one of them at boot would
// fork a subprocess — and, on the first turn, buy back a full context window —
// for every session anyone ever spawned, including the ones nobody will prompt
// again. Each such session is therefore restored in the resumable state: it is
// addressable and it reports as resumable, and its process is recreated by the
// first prompt that needs it. See resumeSession.
//
// Sessions that ran on a remote daemon are restored as live rather than
// resumable: their process belongs to another host, which this daemon's
// restart did not touch.
func (s *agentServiceImpl) LoadPersistedSessions(ctx context.Context) error {
	var restored, resumable int
	// EVERY TENANT, NOT EVERY KEY. The durable store routes by the context's
	// tenant, and this pass carries none, so a single cross-tenant scan sees
	// only the system tenant's records — every other tenant's session would be
	// neither restored nor reconciled. eachSessionRecord names the tenants and
	// hands each record a context scoped to its owner.
	err := s.eachSessionRecord(ctx, func(tenantCtx context.Context, record *SessionRecord) {
		if record.Status == SessionStateClosed {
			return
		}

		// A REMOTE SESSION IS NOT THIS DAEMON'S TO CONCLUDE, and it is asked
		// about FIRST because the local backend table cannot answer anything
		// about it. Its process belongs to another host, which this restart did
		// not touch (SessionRecord.Resumable says the same: a remote session is
		// re-addressed, not resumed), and it is reached through the remote
		// resolver rather than through the local map — so reading "absent from
		// my backends" as "gone" would mark a still-running remote worker
		// closed and destroy the addressability an operator needs to reach it.
		// That is #792's false terminal state, inverted.
		//
		// So the question is put to the things that can answer it, and only
		// POSITIVE evidence of the daemon's departure concludes the session.
		if !record.BackendLocal {
			if host, gone := s.remoteSessionGone(tenantCtx, record); gone {
				s.concludeStartupSession(tenantCtx, record, ConcludedByAgentGone,
					"the daemon the session ran on ("+host+") is no longer in the fleet, "+
						"so nothing here can reach the process it holds")
				return
			}
			s.restorePersistedSession(tenantCtx, record, &restored, &resumable)
			return
		}

		// From here the session ran on THIS daemon's local backend, so its
		// process died with the previous one.
		//
		// A session whose backend is no longer registered cannot be addressed,
		// let alone reconstituted — so the record is CONCLUDED rather than
		// skipped. Leaving it in place against a boot that might register the
		// backend later is what produced the stranded list: the hope was
		// unfalsifiable, the record stayed ACTIVE for days, and every listing
		// counted a session nothing could reach. A record concluded here is
		// still readable; a session an operator genuinely wants back is
		// spawned again.
		if _, backendErr := s.lookupBackend(record.Backend); backendErr != nil {
			s.concludeStartupSession(tenantCtx, record, ConcludedByNodeRestart,
				"backend "+record.Backend+" is not registered on this node, so the session cannot be re-attached")
			return
		}

		// A non-persistent session ran a fresh process per prompt: there is no
		// process to miss and no context to restore, so it did not survive the
		// restart. Record that plainly instead of leaving it look resumable.
		if !record.Persistent {
			s.concludeStartupSession(tenantCtx, record, ConcludedByNodeRestart,
				"the session is not persistent, so its process did not survive the restart")
			return
		}

		// A SESSION THAT ALREADY FAILED TO RESUME IS LEFT WHERE IT IS, durable
		// and unrestored. It is NOT concluded — concluding would overwrite the
		// error and the cause with a tombstone, and the whole reason
		// recordResumeFailure wrote them is that an operator has to be able to
		// see why. And it is not restored either: a live entry that is not
		// resumable sends SessionStatus and PromptSession past the resumable
		// short-circuit and on to a backend holding no process for it, so the
		// operator gets "session not found" instead of the recorded failure.
		// With no live entry both paths read the durable record and report the
		// error, which is the answer that exists.
		if record.Status == SessionStateError {
			s.logger.Infof("orchestration: session %s is left unrestored: a previous resume "+
				"failed (%s)", record.SessionID, record.ResumeError)
			return
		}

		s.restorePersistedSession(tenantCtx, record, &restored, &resumable)
	})
	if err != nil {
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to load persisted sessions: " + err.Error(),
		}
	}

	if restored > 0 {
		s.logger.Infof("orchestration: restored %d session(s) from durable store (%d awaiting resume)",
			restored, resumable)
	}
	return nil
}

// restorePersistedSession puts one record back into the registry.
//
// A LOCAL persistent session comes back RESUMABLE: its process died with the
// previous daemon and is recreated by the first prompt that needs it. A REMOTE
// one comes back LIVE, because the process it names is still running on
// another host — Resumable() is false for it, so neither the resumable flag
// nor the durable status is touched.
func (s *agentServiceImpl) restorePersistedSession(
	ctx context.Context, record *SessionRecord, restored, resumable *int) {
	mem := &sessionRecord{
		Backend:      record.Backend,
		ProjectID:    record.ProjectID,
		Model:        record.Model,
		TenantID:     record.TenantID,
		ContainerID:  record.ContainerID,
		CreatedAt:    record.CreatedAt,
		JobID:        record.JobID,
		HostAffinity: record.HostAffinity,
		BackendLocal: record.BackendLocal,
		Persistent:   record.Persistent,
	}
	mem.SetAccountID(record.AccountID)
	// A restored session must be able to withdraw the grants it was handed
	// before the restart: without this a close after a restart tears down
	// everything except the authority over the credential.
	mem.SetCredentialLeaseIDs(record.CredentialLeaseIDs)
	if record.Resumable() {
		mem.resumable.Store(true)
		*resumable++
	}
	s.sessions.Store(record.SessionID, mem)
	*restored++

	if record.Resumable() && record.Status != SessionStateIdle {
		if statusErr := s.store.UpdateSessionStatus(ctx, record.TenantID, record.SessionID, SessionStateResumable); statusErr != nil {
			s.logger.Warnf("orchestration: failed to mark session %s resumable: %s",
				record.SessionID, statusErr.Error())
		}
		s.setSessionMetaStatus(ctx, record.TenantID, record.SessionID, SessionStateResumable)
	}
}

// =============================================================================
// Resume on prompt
// =============================================================================

// ensureSessionLive reconstitutes a resumable session so a prompt can run
// against it. It is a no-op for a session whose process is already live, which
// is every session in a daemon that has not restarted.
//
// The reconstitution happens here, on the prompt path, rather than at boot:
// the caller's context carries the tenant, so the transcript is read from that
// tenant's own barrier and any MCP material is minted against the caller's
// identity — neither of which exists during startup.
func (s *agentServiceImpl) ensureSessionLive(ctx context.Context, backend AgentBackend, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok || !mem.resumable.Load() {
		return nil
	}

	// A resume reads the owning tenant's transcript and replays it into an
	// agent that will answer this caller. A caller naming a different tenant
	// must therefore not be able to trigger one; it is told the session does
	// not exist rather than that it belongs to someone else.
	if caller := TenantFromContext(ctx); caller != "" && caller != mem.TenantID {
		return &SessionNotFoundError{SessionID: sessionID}
	}

	unlock := s.resumeMu.lock(sessionID)
	defer unlock()

	// A concurrent prompt may have completed the resume while this one waited.
	if !mem.resumable.Load() {
		return nil
	}

	err := s.resumeSession(ctx, backend, sessionID, mem)
	if err != nil {
		s.recordResumeFailure(ctx, mem, sessionID, err)
		return err
	}
	return nil
}

// resumeSession reconstitutes one session against its backend and, on success,
// promotes it back to live.
func (s *agentServiceImpl) resumeSession(
	ctx context.Context,
	backend AgentBackend,
	sessionID string,
	mem *sessionRecord,
) error {
	resumer, ok := backend.(SessionResumer)
	if !ok {
		return &SessionResumeUnsupportedError{
			Backend:   mem.Backend,
			SessionID: sessionID,
			Reason:    "the backend does not implement session resume",
		}
	}

	record, err := s.store.GetSession(ctx, mem.TenantID, sessionID)
	if err != nil {
		return &SessionResumeError{SessionID: sessionID, Cause: err}
	}
	if !record.Persistent {
		return &SessionNotResumableError{
			SessionID: sessionID,
			Reason:    "the session is not persistent, so it holds no conversation to restore",
		}
	}

	transcript, err := buildReplayTranscript(
		ctx, s.sessionLog, record.TenantID, sessionID, record.SystemPrompt, s.replayBudget())
	if err != nil {
		return err
	}

	// Re-resolve the integrity tier rather than replaying it blindly: the
	// recorded level is what the session was admitted at, but a floor the
	// operator has raised since must apply to the process being started NOW.
	spawn := spawnRequestFromRecord(record)
	trustLevel, trustErr := s.resolveTrustLevel(ctx, spawn.TrustLevel, backend)
	if trustErr != nil {
		return trustErr
	}
	spawn.TrustLevel = trustLevel

	result, err := resumer.Resume(ctx, &ResumeRequest{
		SessionID:  sessionID,
		Spawn:      spawn,
		Transcript: transcript,
	})
	if err != nil {
		return err
	}

	mem.resumable.Store(false)
	s.markSessionResumed(ctx, record, result)
	return nil
}

// markSessionResumed records a successful resume on both the durable record
// and the session transcript, so the restoration is visible to an operator
// reading either one.
func (s *agentServiceImpl) markSessionResumed(ctx context.Context, record *SessionRecord, result *ResumeResult) {
	record.Status = SessionStateActive
	record.ResumeMode = result.Mode
	record.ResumeError = ""
	record.LastActivity = time.Now().UnixNano()
	if err := s.store.PutSession(ctx, record); err != nil {
		s.logger.Warnf("orchestration: failed to persist resumed session %s: %s",
			record.SessionID, err.Error())
	}

	s.logger.Infof("orchestration: resumed session %s (mode=%s replayed_turns=%d omitted_turns=%d)",
		record.SessionID, result.Mode, result.ReplayedTurns, result.OmittedTurns)

	message := "session resumed (" + string(result.Mode) + ")"
	if result.Mode == ResumeModeReplay {
		message += ": replayed " + strconv.Itoa(result.ReplayedTurns) + " prior turn(s)"
		if result.OmittedTurns > 0 {
			message += ", omitted " + strconv.Itoa(result.OmittedTurns) + " that exceeded the replay budget"
		}
	}
	s.setSessionMetaStatus(ctx, record.TenantID, record.SessionID, SessionStateActive)
	s.appendSessionEvent(ctx, &SessionEvent{
		SessionID: record.SessionID,
		TenantID:  record.TenantID,
		Timestamp: time.Now().UTC(),
		Source:    "system:orchestrator",
		Kind:      KindSessionResumed,
		System: &SystemMessagePayload{
			Level:   "info",
			Message: message,
			Code:    "session_resumed",
		},
	})
}

// recordResumeFailure marks a session that could not be reconstituted.
//
// The session stays out of service rather than being quietly replaced by a
// context-free agent: its durable state moves to error and carries the reason,
// so an operator sees why the conversation cannot continue instead of talking
// to an agent that has forgotten it.
func (s *agentServiceImpl) recordResumeFailure(ctx context.Context, mem *sessionRecord, sessionID string, cause error) {
	s.logger.Warnf("orchestration: failed to resume session %s: %s", sessionID, cause.Error())

	record, err := s.store.GetSession(ctx, mem.TenantID, sessionID)
	if err != nil {
		return
	}
	record.Status = SessionStateError
	record.ResumeError = cause.Error()
	record.LastActivity = time.Now().UnixNano()
	if putErr := s.store.PutSession(ctx, record); putErr != nil {
		s.logger.Warnf("orchestration: failed to persist resume failure for session %s: %s",
			sessionID, putErr.Error())
	}

	s.setSessionMetaStatus(ctx, record.TenantID, sessionID, SessionStateError)
	s.appendSessionEvent(ctx, &SessionEvent{
		SessionID: sessionID,
		TenantID:  record.TenantID,
		Timestamp: time.Now().UTC(),
		Source:    "system:orchestrator",
		Kind:      KindSystemMessage,
		System: &SystemMessagePayload{
			Level:   "error",
			Message: "session resume failed: " + cause.Error(),
			Code:    "session_resume_failed",
		},
	})
}

// setSessionMetaStatus mirrors a session's state onto its event-log metadata,
// which is the record the attach surface lists sessions from. Without this an
// operator watching a transcript would see a session described as active long
// after the process serving it was gone. Best effort: a session with no
// metadata registered simply has nothing to update.
func (s *agentServiceImpl) setSessionMetaStatus(ctx context.Context, tenantID, sessionID string, state SessionState) {
	if s.sessionLog == nil {
		return
	}
	meta, err := s.sessionLog.GetMeta(ctx, tenantID, sessionID)
	if err != nil {
		return
	}
	meta.Status = state
	meta.LastActivity = time.Now().UTC()
	if putErr := s.sessionLog.PutMeta(ctx, meta); putErr != nil {
		s.logger.Warnf("orchestration: failed to update session meta for %s: %s", sessionID, putErr.Error())
	}
}

// liveSessionRecord returns the in-memory record for a session.
func (s *agentServiceImpl) liveSessionRecord(sessionID string) (*sessionRecord, bool) {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	rec, ok := val.(*sessionRecord)
	return rec, ok
}

// replayBudget returns the configured transcript replay budget in characters.
func (s *agentServiceImpl) replayBudget() int {
	if s.transcriptReplayBudget > 0 {
		return s.transcriptReplayBudget
	}
	return DefaultReplayBudget
}

// SetTranscriptReplayBudget sets the maximum number of transcript characters
// replayed into a resumed agent. A non-positive value selects
// DefaultReplayBudget.
func (s *agentServiceImpl) SetTranscriptReplayBudget(budget int) {
	s.transcriptReplayBudget = budget
}
