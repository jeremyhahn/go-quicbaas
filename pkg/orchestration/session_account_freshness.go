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
	"time"
)

// =============================================================================
// Session account freshness — has the pool moved under a live session?
//
// A session materializes its pooled provider credential ONCE, at spawn: the
// access token goes into the agent process's environment, the provider config
// files into a per-session directory. Neither is re-read afterwards, so the
// session keeps serving turns with the copy it took.
//
// The account itself does not stand still. A host rotates its credential on a
// cadence (RotateAccountCredential), and an oauth_refresh account renews itself
// (AccountTokenManager). Either replaces the envelope the pool holds — and the
// copy a session took before that moment stops being honoured by the provider.
// The session's NEXT prompt then fails in seconds against a provider that no
// longer recognises the token, which reads as an environment fault and costs
// whatever the session was in the middle of (go-quicbaas#997: three review
// rounds lost in one day on the burn-in fleet).
//
// This file is how a caller asks the question BEFORE it spends a turn finding
// out: "is the credential this session holds still the one its account holds?"
// It answers from two records and no secret material.
//
// THE ANSWER IS AN IDENTITY COMPARISON, NOT A RACE BETWEEN TWO CLOCKS. Every
// write that replaces an account's material stamps a GENERATION into the
// envelope itself (AccountSecretStore.Replace); the resolver reads that stamp
// out of the very envelope it materialized and records it on the session, and
// the account's metadata record mirrors the same stamp. Comparing the two
// numbers answers the question exactly, including for the case that made the
// timestamp version wrong: a rotation landing BETWEEN the credential being read
// and the container finishing its handshake. The session's stamp is taken
// before the container starts, so such a session reports the copy it actually
// holds rather than the one that existed by the time it said hello.
//
// The generation travels every path a session is created by, INCLUDING the
// remote-daemon boundary (SpawnSessionResponse's account_generation): a remote
// session materializes its credential on the daemon, so the daemon is the only
// party that can say which copy it took, and a remote session reporting none
// would be left on exactly the comparison this replaces.
//
// A session whose generation is unknown — one spawned before stamping existed —
// falls back to the timestamp comparison that preceded this: the account's
// CredentialRotatedAt against the instant the session's current account segment
// opened. That is strictly the behaviour those sessions had before, which is
// the right floor for a check whose only failure mode must be "says nothing".
// =============================================================================

// SessionAccountFreshness is what a caller learns about the pooled credential
// a live session is holding.
type SessionAccountFreshness struct {
	// SessionID is the session asked about.
	SessionID string

	// AccountID is the pooled account currently serving the session, empty
	// when the session uses no pooled account at all.
	AccountID string

	// Stale reports that the account's credential envelope has been replaced
	// since this session materialized it, so the material the session holds is
	// no longer the material the account holds.
	Stale bool

	// MaterializedAt is when the session took its copy: the instant its current
	// account segment opened (see SessionRecord.AccountHistory), which for a
	// session that has never rotated is its spawn time.
	MaterializedAt time.Time

	// RotatedAt is when the account's envelope was last replaced
	// (Account.CredentialRotatedAt). Zero when the account still holds the
	// envelope it was imported with.
	RotatedAt time.Time

	// SessionGeneration is the copy of the account's credential the session
	// materialized (SessionRecord.AccountGeneration); AccountGeneration is the
	// copy the account holds now (Account.CredentialGeneration). Zero on either
	// means unknown, and the verdict came from the timestamps above instead.
	//
	// Both are reported so an operator reading a roll in the audit trail can
	// see WHICH two copies disagreed, rather than only that something did.
	SessionGeneration int64
	AccountGeneration int64
}

// SessionAccountFreshnessReader answers whether a session's materialized
// pooled credential is still current.
//
// It is a CAPABILITY, narrowed the way SessionAccountRotationService and
// SessionExecutor are, rather than a method on AgentService: the pooled account
// subsystem is optional, and a deployment that runs no pool — or an embedded
// client that is only a proxy — has no honest answer to give. A caller that
// cannot find this capability must read that as "nothing is known to have
// changed" and carry on, which is exactly the behaviour every deployment had
// before the question could be asked.
type SessionAccountFreshnessReader interface {
	SessionAccountFreshness(ctx context.Context, sessionID string) (*SessionAccountFreshness, error)
}

// Compile-time interface satisfaction check.
var _ SessionAccountFreshnessReader = (*agentServiceImpl)(nil)

// AccountMaterializedAt reports when the account currently in force was
// materialized into this session, in Unix nanoseconds.
//
// That is the OPEN account segment's start — the one AccountHistory leaves with
// no end time — because a session that has rotated took its current copy when
// that segment opened, not when the process was spawned. A record with no
// history at all (every session spawned before rotation existed, and every one
// written by a path that records only the scalar) falls back to its spawn time,
// which is the same instant for a session that has never rotated.
//
// Zero means the record can date neither, and a caller must treat the answer as
// unknown rather than as "materialized at the epoch" — the latter would declare
// every such session stale forever.
func (r *SessionRecord) AccountMaterializedAt() int64 {
	if r == nil {
		return 0
	}
	for _, segment := range r.AccountHistory {
		if segment.To == 0 && segment.From > 0 {
			return segment.From
		}
	}
	return r.CreatedAt
}

// SessionAccountFreshness reports whether the pooled credential sessionID
// materialized at spawn is still the one its account holds.
//
// FAIL-SOFT BY CONSTRUCTION, in one direction only. Every case where the
// question cannot be answered — no pooled account on the session, no account
// store wired, an account record that cannot be read, a session record that can
// date neither its materialization nor its account's last rotation — answers
// "not stale". Reporting stale on missing evidence would roll a healthy session
// every turn; reporting fresh costs at worst the failure the caller already
// has today. Only a rotation this daemon can actually SEE, recorded after the
// session took its copy, is reported.
//
// A caller naming another tenant's session is told it does not exist rather
// than that it belongs to someone else — the same ownership rule
// RotateSessionAccount applies.
func (s *agentServiceImpl) SessionAccountFreshness(ctx context.Context, sessionID string) (*SessionAccountFreshness, error) {
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

	record, err := s.store.GetSession(ctx, mem.TenantID, sessionID)
	if err != nil {
		return nil, err
	}

	answer := &SessionAccountFreshness{
		SessionID:         sessionID,
		AccountID:         record.AccountID,
		SessionGeneration: record.AccountGeneration,
	}
	if record.AccountID == "" || s.accountStore == nil {
		return answer, nil
	}

	if materialized := record.AccountMaterializedAt(); materialized > 0 {
		answer.MaterializedAt = time.Unix(0, materialized)
	}

	account, err := s.accountStore.Get(ctx, record.TenantID, record.AccountID)
	if err != nil {
		// The pool cannot be read. That is worth reporting to a caller that
		// wants to log it, but it is not evidence of a rotation, so the answer
		// stays "not stale" and the error travels alongside it.
		return answer, err
	}
	if account == nil {
		return answer, nil
	}
	answer.AccountGeneration = account.CredentialGeneration
	if s.accountGenerations != nil {
		// THE AUTHORITY, WHEN IT IS WIRED. Its answer replaces the mirror's
		// outright: a referenced account's mirror never moves when its secret
		// is rewritten, so the mirror is not evidence the authority can be
		// checked against. A read that fails is reported alongside a "not
		// stale" answer, for the reason the account read above is.
		generation, genErr := s.accountGenerations.CredentialGeneration(ctx, account)
		if genErr != nil {
			return answer, genErr
		}
		answer.AccountGeneration = generation
	}
	answer.RotatedAt = account.CredentialRotatedAt
	answer.Stale = staleAgainstAccount(answer)
	return answer, nil
}

// staleAgainstAccount decides the verdict from an answer whose four inputs are
// already filled in.
//
// GENERATIONS FIRST, AND ALONE WHEN BOTH ARE KNOWN. Two generations are two
// names for two copies of the same account's material: they disagree exactly
// when the pool has replaced the copy the session is presenting, with no
// dependence on when either event was clocked. That is what makes a rotation
// landing mid-spawn visible — the session's stamp was taken when its material
// was produced, not when its container finished starting (go-quicbaas#997).
//
// TIMESTAMPS ONLY AS A FLOOR, for the sessions that can offer no generation:
// one spawned before stamping existed, or one served by a remote daemon whose
// wire protocol does not carry it. For those the account's CredentialRotatedAt
// against the instant the session's segment opened is the best evidence there
// is — imprecise across a slow spawn, but strictly what those sessions had
// before, and never worse.
//
// Everything else answers "not stale": an account whose generation and rotation
// instant are both unrecorded has shown this daemon no evidence of a rotation,
// and rolling a session on an absence would roll it every single turn.
func staleAgainstAccount(answer *SessionAccountFreshness) bool {
	if answer.SessionGeneration > 0 && answer.AccountGeneration > 0 {
		return answer.AccountGeneration > answer.SessionGeneration
	}
	if answer.RotatedAt.IsZero() || answer.MaterializedAt.IsZero() {
		return false
	}
	return answer.RotatedAt.After(answer.MaterializedAt)
}

// AccountRotated reports the pooled account rotation a session has already been
// left behind by, or nil when there is none to act on.
//
// It is the job-facing shape of SessionAccountFreshness: a runner about to
// spend minutes on a turn asks here first, and a deployment whose agent service
// cannot answer — no pooled accounts, a remote proxy, a test double — gets nil
// and prompts exactly as it always did. Every error is swallowed onto the log
// for the same reason the reader fails soft: a pool that cannot be read is not
// evidence that anything changed.
func (r *JobRun) AccountRotated(ctx context.Context, sessionID string) *SessionAccountFreshness {
	if sessionID == "" || r.Agents == nil {
		return nil
	}
	reader, ok := r.Agents.(SessionAccountFreshnessReader)
	if !ok {
		return nil
	}
	freshness, err := reader.SessionAccountFreshness(ctx, sessionID)
	if err != nil {
		r.Logger.Warnf("job run: failed to read the pooled account freshness of session %s: %s",
			sessionID, err.Error())
		return nil
	}
	if freshness == nil || !freshness.Stale {
		return nil
	}
	return freshness
}

// AccountCredentialGenerationReader reports which copy of an account's
// credential the pool holds NOW, read from the authority rather than from the
// metadata record's mirror.
//
// The mirror (Account.CredentialGeneration) is written only where QuicBaaS
// itself replaces an envelope. It cannot see the rotation a referenced account
// goes through — a new version of its tenant secret, written by whoever owns
// that secret, moves no account field at all — and it can lag an envelope
// whose second, metadata write was lost. Reading the authority closes both:
// the envelope for an inline account, the secret's current version for a
// referenced one.
type AccountCredentialGenerationReader interface {
	CredentialGeneration(ctx context.Context, account *Account) (int64, error)
}

// Compile-time interface satisfaction check.
var _ AccountCredentialGenerationReader = (*AccountTokenManager)(nil)

// CredentialGeneration implements AccountCredentialGenerationReader.
//
// It reads the generation the stored material carries and nothing else: no
// renewal is attempted and nothing is recorded on the account, because the
// question is asked before every prompt of every live session and must not
// have side effects. Zero means the material predates generation stamping.
func (m *AccountTokenManager) CredentialGeneration(ctx context.Context, account *Account) (int64, error) {
	if account == nil {
		return 0, &InputValidationError{Field: "account", Message: "cannot be nil"}
	}
	if account.UsesSecretRef() {
		cred, err := ReadAccountSecretRef(ctx, m.secretRefs, account)
		if err != nil {
			return 0, err
		}
		return cred.Generation, nil
	}
	cred, err := m.secrets.Get(ctx, account.TenantID, account.ID)
	if err != nil {
		return 0, err
	}
	return cred.Generation, nil
}

// SetAccountCredentialGenerations wires the authority SessionAccountFreshness
// reads an account's current credential generation from. Without it the
// answer comes from the metadata record's mirror, which cannot see a
// referenced account's secret being rewritten.
func (s *agentServiceImpl) SetAccountCredentialGenerations(reader AccountCredentialGenerationReader) {
	s.accountGenerations = reader
}
