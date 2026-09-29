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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// =============================================================================
// Session account rotation
//
// A pooled account that has hit its provider limit is not a broken account and
// the conversation running on it is not a broken conversation — the session
// simply needs a different credential to keep making progress. Rotation is the
// three-step answer:
//
//	1. hold the outgoing account out of the pool until it recovers,
//	2. select a replacement from the session's OWN rotation policy,
//	3. put the session back in service on it, keeping its identifier, its
//	   transcript, and its cost history.
//
// Step 3 has two mechanisms and the cheap one is tried first. See
// SessionAccountRotator (in place, one-shot CLI) and the close/resume fallback
// below (persistent CLI and sandboxed container).
// =============================================================================

// AccountRotationMode reports which mechanism moved a session.
type AccountRotationMode string

const (
	// AccountRotationModeInPlace means the session's stored credential
	// configuration was swapped without restarting anything. No process died
	// and no transcript was replayed.
	AccountRotationModeInPlace AccountRotationMode = "in_place"

	// AccountRotationModeResume means the session's agent process was closed
	// and reconstituted against the replacement account, replaying the
	// transcript so the conversation continues where it left off.
	AccountRotationModeResume AccountRotationMode = "resume"
)

// RotateSessionAccountRequest asks the orchestrator to move one session onto a
// different pooled provider account.
//
// The JSON tags are the canonical wire shape of the operator-initiated verb on
// every text transport (REST body, QUIC payload, MCP tool arguments); the gRPC
// projection lives in api/proto/orchestrator.proto.
type RotateSessionAccountRequest struct {
	// SessionID is the session to move. Required.
	SessionID string `json:"session_id"`

	// Reason is recorded on the closed account segment and in the session
	// transcript. For an automatic rotation it is the classifier's redacted
	// evidence; for an operator-initiated one it is whatever they supplied.
	Reason string `json:"reason,omitempty"`

	// HoldOutgoing marks the account being left as AccountStatusLimited until
	// AvailableAt. It is what keeps the next selection — for this session or
	// any other — from walking straight back into the limit that triggered the
	// rotation. An operator rotating for a reason of their own (rebalancing,
	// retiring a seat) leaves it false.
	HoldOutgoing bool `json:"hold_outgoing,omitempty"`

	// AvailableAt is when a held outgoing account rejoins the pool. Ignored
	// unless HoldOutgoing is set; zero with HoldOutgoing set applies the
	// service's default backoff.
	AvailableAt time.Time `json:"available_at,omitempty"`
}

// RotateSessionAccountResult reports the outcome of a rotation.
//
// The JSON tags are the canonical wire shape on every text transport; the SDK
// response type (sdk/go/client/orchestrator) mirrors them field for field, so
// no intermediate wire struct is needed.
type RotateSessionAccountResult struct {
	// SessionID is the session, unchanged — a rotation never renames one.
	SessionID string `json:"session_id"`

	// PreviousAccountID is the account the session was moved off.
	PreviousAccountID string `json:"previous_account_id,omitempty"`

	// AccountID is the replacement now in force.
	AccountID string `json:"account_id"`

	// Strategy names the policy that selected the replacement.
	Strategy string `json:"strategy,omitempty"`

	// CredentialLeaseID is the DELIVERY GRANT the replacement credential was
	// handed over under, empty when the deployment wired no leaser.
	CredentialLeaseID string `json:"credential_lease_id,omitempty"`

	// ReservationID is the session-slot reservation the replacement account
	// was resolved under; the rotated record names it.
	ReservationID string `json:"reservation_id,omitempty"`

	// Mode reports which mechanism was used.
	Mode AccountRotationMode `json:"mode"`

	// accountGeneration is which copy of the replacement account's credential
	// the rotation materialized (AccountCredential.Generation), carried from
	// the backend to commitRotation so the session's open account segment
	// records it.
	//
	// DELIBERATELY UNEXPORTED, and therefore deliberately off every wire. It
	// is internal bookkeeping between two halves of one rotation, not
	// something an operator or an SDK caller has any use for: the generation
	// is meaningful only against the account record it is compared with, and
	// publishing it would add a field to five transports plus the proto to say
	// nothing a caller can act on.
	accountGeneration int64
}

// SessionAccountRotationService is the orchestrator-side rotation verb. It is
// declared as an interface so a transport can reach it by type assertion,
// exactly as the Set* wiring in pkg/server does, without every caller of
// AgentService having to know about the account pool.
type SessionAccountRotationService interface {
	// RotateSessionAccount moves a session onto a different pooled account.
	RotateSessionAccount(ctx context.Context, req *RotateSessionAccountRequest) (*RotateSessionAccountResult, error)
}

// Compile-time interface satisfaction check.
var _ SessionAccountRotationService = (*agentServiceImpl)(nil)

// RotationServiceFrom narrows an AgentService to its rotation verb.
//
// Rotation is not on AgentService itself: the pooled account subsystem is
// optional, and widening the interface every transport already depends on
// would force every AgentService implementation — including the remote fleet node
// daemon proxy — to grow a method most of them cannot honour. Each transport
// therefore asks for the capability here, and a deployment whose agent service
// does not have it answers with one typed error instead of five ad-hoc ones.
//
// transport names the caller ("rest", "grpc", "quic", "mcp", "embedded") so an
// unsupported answer says where the gap is.
func RotationServiceFrom(svc AgentService, transport string) (SessionAccountRotationService, error) {
	rotator, ok := svc.(SessionAccountRotationService)
	if !ok {
		return nil, &SessionRotationUnsupportedError{Transport: transport}
	}
	return rotator, nil
}

// =============================================================================
// Wiring
// =============================================================================

// SetAccountStore attaches the pooled account metadata store. It is what lets
// the orchestrator place a limit hold on an exhausted account; without it
// rotation still works (the replacement is selected with the outgoing account
// excluded outright) but the hold is not durable, so another session may
// rediscover the same limit for itself.
func (s *agentServiceImpl) SetAccountStore(store AccountStore) {
	s.accountStore = store
}

// AccountStore returns the pooled account metadata store, or nil when none is
// wired.
//
// It is the read half SpendLedgerAccessor needs: an allowance lives on the
// account record (see AccountSpendPolicy), so a report that cannot reach the
// store reports every account's policy as UNKNOWN rather than as absent.
func (s *agentServiceImpl) AccountStore() AccountStore {
	return s.accountStore
}

// SetAccountCredentialLeaser wires the authority that withdraws a pooled
// credential's DELIVERY GRANT (see account_lease.go).
//
// With it wired, session conclusion becomes a SECURITY CONTROL rather than only
// hygiene: the same transition that tears down the process and releases the
// materialized credential directory also revokes the grant, so the host that
// held that copy cannot obtain another one under it. Nil is ignored, which
// leaves a deployment with no lease manager concluding sessions exactly as it
// did before.
func (s *agentServiceImpl) SetAccountCredentialLeaser(leases AccountCredentialLeaser) {
	if leases != nil {
		s.accountLeases = leases
	}
}

// SetAccountLimitClassifier replaces the heuristic provider-limit classifier.
// A deployment that proxies provider traffic — and therefore sees real status
// codes and Retry-After headers — installs an exact one here. Nil is ignored so
// the built-in heuristic stays in place.
func (s *agentServiceImpl) SetAccountLimitClassifier(classifier AccountLimitClassifier) {
	if classifier != nil {
		s.limitClassifier = classifier
	}
}

// SetAccountLimitBackoff sets how long an account is held out of the pool when
// the provider reported a limit but no machine-readable retry hint. A
// non-positive value selects DefaultAccountLimitBackoff.
func (s *agentServiceImpl) SetAccountLimitBackoff(backoff time.Duration) {
	s.accountLimitBackoff = backoff
}

// limitBackoff returns the configured default limit backoff.
func (s *agentServiceImpl) limitBackoff() time.Duration {
	if s.accountLimitBackoff > 0 {
		return s.accountLimitBackoff
	}
	return DefaultAccountLimitBackoff
}

// classifier returns the configured limit classifier, defaulting to the
// built-in heuristic so rotation works out of the box.
func (s *agentServiceImpl) classifier() AccountLimitClassifier {
	if s.limitClassifier != nil {
		return s.limitClassifier
	}
	return NewAccountLimitClassifier()
}

// =============================================================================
// Automatic rotation on a limited turn
// =============================================================================

// holdOnAccountLimit takes the session's account out of rotation when the
// turn failed because the provider refused it for a limit, and returns the
// turn's error marked as such.
//
// EVERY TURN PATH, NOT ONLY THE ASYNCHRONOUS ONE. Rotation below answers an
// operator's session by moving it; a job's runner answers by rolling its
// worker (pkg/sdlc). Both need the same two things first: the account held
// until the provider's stated reset, so no other spawn selects it in the
// meantime, and an error that SAYS the account was refused, so the caller does
// not have to re-derive it from a string. Doing it here, where the turn failed
// and the session's account is known, is what gives every caller both.
//
// A turn that failed for any other reason is returned untouched.
func (s *agentServiceImpl) holdOnAccountLimit(ctx context.Context, sessionID string, turnErr error) error {
	if sessionID == "" || turnErr == nil {
		return turnErr
	}
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok || mem.AccountID() == "" {
		return turnErr
	}
	provider := AccountProvider(mem.Backend)
	signal, limited := s.classifier().Classify(provider, turnErr)
	if !limited {
		return turnErr
	}
	availableAt := signal.AvailableAt(time.Now().UTC(), s.limitBackoff())
	accountID := mem.AccountID()
	s.holdAccount(ctx, mem.TenantID, accountID, availableAt, signal.Matched)
	metrics.GetMetrics().RecordOrchAccountLimitHold(mem.TenantID, string(provider))
	return &TurnAccountLimitedError{
		SessionID: sessionID,
		Limit: &AccountLimitedError{
			AccountID:   accountID,
			Provider:    provider,
			RetryAfter:  signal.RetryAfter,
			AvailableAt: availableAt,
			Signal:      signal.Matched,
		},
		Cause: turnErr,
	}
}

// rotateOnAccountLimit answers a failed turn that the classifier attributes to
// a provider limit by moving the session to another account.
//
// It is deliberately fire-and-forget from the turn's point of view: the turn
// that hit the limit has already failed and its turn_failed event is already
// written, so the rotation's job is to leave the session ready for the NEXT
// prompt rather than to retry this one. Retrying automatically would double a
// turn's cost against the replacement account without anyone asking, and the
// operator's own next prompt is a better decision point than a guess here.
//
// Returns the rotation result when one happened, so callers (tests above all)
// have something to assert on instead of polling.
func (s *agentServiceImpl) rotateOnAccountLimit(ctx context.Context, sessionID string, turnErr error) *RotateSessionAccountResult {
	if sessionID == "" || turnErr == nil {
		return nil
	}
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok || mem.AccountID() == "" {
		return nil
	}

	signal, limited := s.classifier().Classify(AccountProvider(mem.Backend), turnErr)
	if !limited {
		return nil
	}

	result, err := s.RotateSessionAccount(ctx, &RotateSessionAccountRequest{
		SessionID:    sessionID,
		Reason:       signal.Matched,
		HoldOutgoing: true,
		AvailableAt:  signal.AvailableAt(time.Now().UTC(), s.limitBackoff()),
	})
	if err != nil {
		// A session that is not rotatable (a deliberate pin, no pool) is the
		// expected outcome for most deployments, not a fault: log it at debug
		// so it does not drown the operator in noise every time a pinned
		// session hits its own limit.
		var notRotatable *SessionNotRotatableError
		if errors.As(err, &notRotatable) {
			s.logger.Debugf("orchestration: session %s hit a provider limit but is not rotatable: %s",
				sessionID, notRotatable.Reason)
			return nil
		}
		s.logger.Warnf("orchestration: failed to rotate session %s off a limited account: %s",
			sessionID, err.Error())
		return nil
	}
	return result
}

// =============================================================================
// Rotation
// =============================================================================

// RotateSessionAccount implements SessionAccountRotationService.
func (s *agentServiceImpl) RotateSessionAccount(ctx context.Context, req *RotateSessionAccountRequest) (*RotateSessionAccountResult, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired, Message: "session_id is required"}
	}

	mem, ok := s.liveSessionRecord(req.SessionID)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}

	// Same ownership rule as ensureSessionLive: a caller naming another
	// tenant's session is told it does not exist rather than that it belongs to
	// someone else.
	if caller := TenantFromContext(ctx); caller != "" && caller != mem.TenantID {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}

	// Reuse the resume mutex rather than adding a second keyed lock over the
	// same session. Rotation and resume both replace the process serving one
	// session; letting them interleave would leave two agents answering to one
	// identifier.
	unlock := s.resumeMu.lock(req.SessionID)
	defer unlock()

	record, err := s.store.GetSession(ctx, mem.TenantID, req.SessionID)
	if err != nil {
		return nil, err
	}
	if rotatable, reason := record.CanRotateAccount(); !rotatable {
		return nil, &SessionNotRotatableError{SessionID: req.SessionID, Reason: reason}
	}

	previousAccountID := record.AccountID
	if req.HoldOutgoing {
		s.holdAccount(ctx, record.TenantID, previousAccountID, req.AvailableAt, req.Reason)
	}

	ctx = s.pinSessionBackend(ctx, req.SessionID)
	backend, err := s.lookupBackendCtx(ctx, record.Backend, record.ProjectID)
	if err != nil {
		return nil, err
	}

	spawn := rotationSpawnRequestFromRecord(record, previousAccountID)

	result, err := s.rotateInPlace(ctx, backend, req.SessionID, spawn)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result, err = s.rotateByResume(ctx, backend, record, spawn)
		if err != nil {
			return nil, err
		}
	}
	result.SessionID = req.SessionID
	result.PreviousAccountID = previousAccountID

	s.commitRotation(ctx, mem, record, result, req.Reason)
	return result, nil
}

// rotateInPlace tries the cheap mechanism. A (nil, nil) result means the
// backend cannot rotate this session in place and the caller should fall back
// to close-and-resume; that is a routing outcome, not an error.
func (s *agentServiceImpl) rotateInPlace(
	ctx context.Context,
	backend AgentBackend,
	sessionID string,
	spawn *SpawnRequest,
) (*RotateSessionAccountResult, error) {
	rotator, ok := backend.(SessionAccountRotator)
	if !ok {
		return nil, nil
	}

	rotated, err := rotator.RotateAccount(ctx, &RotateAccountRequest{SessionID: sessionID, Spawn: spawn})
	if err != nil {
		var unsupported *SessionRotateUnsupportedError
		if errors.As(err, &unsupported) {
			return nil, nil
		}
		return nil, err
	}
	return &RotateSessionAccountResult{
		AccountID:         rotated.AccountID,
		Strategy:          rotated.Strategy,
		CredentialLeaseID: rotated.CredentialLeaseID,
		ReservationID:     rotated.ReservationID,
		Mode:              AccountRotationModeInPlace,
		accountGeneration: rotated.AccountGeneration,
	}, nil
}

// rotateByResume closes the session's agent process and reconstitutes it
// against a freshly selected account.
//
// The close comes first for two reasons, both load-bearing: Resume refuses a
// session that is still live (SessionAlreadyLiveError), and the outgoing
// account's materialized credential directory is released by exactly that
// Close — so a sandboxed session's replacement container is built around a
// fresh directory holding only the new account's files, never a merge of two.
func (s *agentServiceImpl) rotateByResume(
	ctx context.Context,
	backend AgentBackend,
	record *SessionRecord,
	spawn *SpawnRequest,
) (*RotateSessionAccountResult, error) {
	resumer, ok := backend.(SessionResumer)
	if !ok {
		return nil, &SessionResumeUnsupportedError{
			Backend:   record.Backend,
			SessionID: record.SessionID,
			Reason:    "the backend can neither rotate an account in place nor reconstitute the session",
		}
	}

	// The transcript is read BEFORE the close so a backend that keeps its
	// transcript in the process cannot lose it, and so a read failure aborts
	// the rotation while the session is still serving its old account.
	transcript, err := buildReplayTranscript(
		ctx, s.sessionLog, record.TenantID, record.SessionID, record.EffectiveSystemPrompt(), s.replayBudget())
	if err != nil {
		return nil, err
	}

	if closeErr := backend.Close(ctx, record.SessionID); closeErr != nil {
		var notFound *SessionNotFoundError
		if !errors.As(closeErr, &notFound) {
			return nil, &SessionResumeError{SessionID: record.SessionID, Cause: closeErr}
		}
	}

	resumed, err := resumer.Resume(ctx, &ResumeRequest{
		SessionID:  record.SessionID,
		Spawn:      spawn,
		Transcript: transcript,
	})
	if err != nil {
		// The old process is already gone. Leaving the session simply "not
		// live" would make every follow-up prompt fail with "session not
		// found" and no explanation; putting it back in the RESUMABLE state
		// instead means the next prompt reconstitutes it through the ordinary
		// path, and the durable record carries why the rotation failed.
		return nil, s.markRotationStranded(ctx, record, err)
	}
	if resumed.Session == nil || resumed.Session.AccountID == "" {
		// The backend reconstituted the session but resolved no account, which
		// means the replacement agent is running on the daemon host's own
		// identity rather than a pooled credential. That is exactly the silent
		// fallback the account subsystem exists to prevent, so the session is
		// torn down again and reported as stranded instead of being left
		// authenticating as somebody it was never meant to be.
		_ = backend.Close(ctx, record.SessionID)
		return nil, s.markRotationStranded(ctx, record, &NoAccountAvailableError{
			TenantID:  record.TenantID,
			ProjectID: record.ProjectID,
			Strategy:  record.AccountStrategy,
		})
	}
	return &RotateSessionAccountResult{
		AccountID:         resumed.Session.AccountID,
		Strategy:          resumed.Session.AccountStrategy,
		CredentialLeaseID: resumed.Session.CredentialLeaseID,
		ReservationID:     resumed.Session.ReservationID,
		Mode:              AccountRotationModeResume,
		accountGeneration: resumed.Session.AccountGeneration,
	}, nil
}

// markRotationStranded returns a session whose process was closed for a
// rotation that then could not select a replacement to the resumable state, and
// records the reason. It returns cause so callers can `return nil, s.mark...`.
func (s *agentServiceImpl) markRotationStranded(ctx context.Context, record *SessionRecord, cause error) error {
	if mem, ok := s.liveSessionRecord(record.SessionID); ok {
		mem.resumable.Store(true)
	}
	record.Status = SessionStateError
	record.ResumeError = "account rotation failed: " + cause.Error()
	record.LastActivity = time.Now().UnixNano()
	if putErr := s.store.PutSession(ctx, record); putErr != nil {
		s.logger.Warnf("orchestration: failed to persist stranded rotation for session %s: %s",
			record.SessionID, putErr.Error())
	}
	s.setSessionMetaStatus(ctx, record.TenantID, record.SessionID, SessionStateError)
	return cause
}

// holdAccount marks the outgoing account limited until availableAt.
//
// Best effort by design: the replacement was already selected with this account
// excluded outright, so a failed hold costs pool hygiene (another session may
// rediscover the same limit) rather than the correctness of this rotation.
func (s *agentServiceImpl) holdAccount(ctx context.Context, tenantID, accountID string, availableAt time.Time, reason string) {
	if s.accountStore == nil || accountID == "" {
		return
	}
	if availableAt.IsZero() {
		availableAt = time.Now().UTC().Add(s.limitBackoff())
	}
	err := s.accountStore.Touch(ctx, tenantID, accountID, TouchAccountOptions{
		SetStatusValid:      true,
		SetStatus:           AccountStatusLimited,
		SetAvailableAt:      availableAt,
		SetAvailableAtValid: true,
		SetLastError:        reason,
	})
	if err != nil {
		s.logger.Warnf("orchestration: failed to hold limited account %s: %s", accountID, err.Error())
		return
	}
	s.logger.Infof("orchestration: account %s held until %s after a provider limit",
		accountID, availableAt.UTC().Format(time.RFC3339))
}

// commitRotation makes the move durable and visible: it closes the outgoing
// account's cost segment, opens the replacement's, refreshes the in-memory
// record the turn path attributes usage from, and writes the transcript entry
// an operator reads the session's history in.
func (s *agentServiceImpl) commitRotation(
	ctx context.Context,
	mem *sessionRecord,
	record *SessionRecord,
	result *RotateSessionAccountResult,
	reason string,
) {
	now := time.Now()

	// THE SUPERSEDED GRANT IS WITHDRAWN AND THE REPLACEMENT IS RECORDED, in
	// that order and before the record is written.
	//
	// A rotation hands the session a second live provider credential, and the
	// outgoing one's materialized directory is already gone (the backend
	// released it). Leaving its grant alive would leave a revocable handle to a
	// credential nobody is using — and, on the remote path, would let the host
	// materialize that account again. Recording the replacement in the same
	// step is what keeps the record's grant list complete, so a later
	// conclusion withdraws every grant the session ever held.
	s.supersedeCredentialLease(ctx, mem, record, result.CredentialLeaseID)

	record.OpenAccountSegment(result.AccountID, result.Strategy, reason, now.UnixNano(), result.accountGeneration)
	record.AddReservation(result.ReservationID)
	record.Status = SessionStateActive
	record.ResumeError = ""
	record.LastActivity = now.UnixNano()
	if err := s.store.PutSession(ctx, record); err != nil {
		s.logger.Warnf("orchestration: failed to persist rotated session %s: %s",
			record.SessionID, err.Error())
	}

	// The turn path reads the account from here on every completed turn, so
	// this store is what makes attribution per-segment rather than retroactive.
	// A resume-mode rotation also produced a live process again.
	mem.SetAccountID(result.AccountID)
	mem.resumable.Store(false)

	message := "rotated pooled account: " + result.PreviousAccountID + " → " + result.AccountID +
		" (" + string(result.Mode) + ")"
	if reason != "" {
		message += ": " + reason
	}
	s.logger.Infof("orchestration: session %s %s", record.SessionID, message)

	s.setSessionMetaStatus(ctx, record.TenantID, record.SessionID, SessionStateActive)
	s.appendSessionEvent(ctx, &SessionEvent{
		SessionID: record.SessionID,
		TenantID:  record.TenantID,
		Timestamp: now.UTC(),
		Source:    "system:orchestrator",
		Kind:      KindAccountRotated,
		System: &SystemMessagePayload{
			Level:   "info",
			Message: message,
			Code:    "account_rotated",
		},
	})
}

// supersedeCredentialLease records the replacement delivery grant and withdraws
// every grant the session held before it.
//
// The withdrawal is reported on the log rather than returned: the rotation has
// already happened — the replacement credential is in force and the outgoing
// one's material is gone — so refusing the rotation now would leave the session
// in a state no caller asked for. The grant it could not withdraw is still
// bounded by its TTL and still named on the record, so the session's eventual
// conclusion retries the withdrawal.
func (s *agentServiceImpl) supersedeCredentialLease(
	ctx context.Context,
	mem *sessionRecord,
	record *SessionRecord,
	replacement string,
) {
	if s.accountLeases == nil {
		return
	}

	superseded := make([]string, 0, len(record.CredentialLeaseIDs))
	for _, id := range record.CredentialLeaseIDs {
		if id != "" && id != replacement {
			superseded = append(superseded, id)
		}
	}

	record.AddCredentialLease(replacement)
	if mem != nil {
		mem.AddCredentialLease(replacement)
	}

	for _, id := range superseded {
		if err := s.accountLeases.Revoke(ctx, id); err != nil {
			s.logger.Warnf("orchestration: session %s: failed to revoke superseded credential delivery lease %s: %s",
				record.SessionID, id, err.Error())
		}
	}
}
