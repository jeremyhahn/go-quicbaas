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

package service

import (
	"context"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Account credential MATERIALIZATION — the one verb in the account surface
// whose response body is secret material.
//
// Every other AccountService method deals in metadata: the credential goes in
// on create/rotate and never comes back out. This one exists because the
// credential has to reach the host that actually runs the agent, and in a
// fleet deployment that host is not the one holding the pool.
//
// The central owns the pool; an enrolled agent daemon owns the worker host.
// The daemon asks for exactly one account's envelope over its own
// mTLS + bearer channel, caches it in its own secret store, and injects it into
// the subprocess it spawns. The Spawn RPC itself still carries only account
// SELECTORS (AccountID / AccountAuto / AccountStrategy / AccountAllow /
// AccountExclude) — the credential never travels in a dispatch request, which
// is the boundary pkg/orchestration/remote_acp_backend.go documents and
// go-quicbaas#734 implemented the daemon half of.
//
// Because the response IS the secret, this method is deliberately the
// narrowest one in the file:
//
//   - it is guarded by its own permission (agent_accounts:materialize), not by
//     the read verb that lists the pool;
//   - it refuses any account that is not selectable RIGHT NOW — revoked,
//     errored, or under a live limit hold — so a daemon can never obtain a
//     credential the pool has already judged unusable;
//   - it honours the account's project restriction, so a daemon working a
//     project may not extract an account scoped to a different one;
//   - it emits exactly one audit event per release, naming the agent and the
//     account, whether the release succeeded or was refused.
// =============================================================================

// MaterializeAccountCredentialRequest asks for one account's credential
// envelope.
type MaterializeAccountCredentialRequest struct {
	// TenantID owns the account. Required; the transport binds it from the
	// authenticated caller's own tenant, never from a caller-chosen field.
	TenantID string `json:"tenant_id"`

	// AccountID names the account to release. Required. This verb never
	// selects: SELECTION runs on the daemon, over the metadata it already
	// listed, and only the chosen account is materialized.
	AccountID string `json:"account_id"`

	// ProjectID is the project the requesting spawn belongs to. It is the
	// scope test, not a filter: an account restricted to project P is
	// released only to a request that names P. Empty means the caller is
	// working outside any project, which only tenant-wide accounts satisfy.
	ProjectID string `json:"project_id,omitempty"`

	// AgentID identifies the requesting daemon for the audit trail. Supplied
	// by the transport from the authenticated principal, so a caller cannot
	// attribute its release to another agent.
	AgentID string `json:"agent_id,omitempty"`

	// JobID names the job the requesting spawn works for, empty when it works
	// none. It scopes the delivery grant (see orchestration/account_lease.go),
	// so a job's releases can be listed and revoked as a set.
	//
	// AN ENROLLED DAEMON LEAVES IT EMPTY, deliberately. A daemon's credential
	// cache is keyed by account (pkg/agent/account_secrets.go), so one release
	// serves every job running on that host; a grant naming one of them would be
	// false, and validating a later refresh against it would refuse the daemon
	// its own copy. A caller that releases for a single job — the control
	// plane's own tooling — names it and gets a job-scoped grant.
	JobID string `json:"job_id,omitempty"`

	// LeaseID presents an EXISTING delivery grant.
	//
	// Empty asks for a FIRST release, which issues a new grant. Non-empty says
	// "I already hold a copy under this grant and want a fresh one" — a cache
	// that aged out, a re-spawn inside the same session — and is the case the
	// server ENFORCES on: a grant that has been revoked (the session ended) or
	// has lapsed is refused, so the host cannot replace the copy it holds and
	// that copy ages out of its memory cache with nothing to renew it.
	//
	// A caller that has lost track of its grant may always ask for a first
	// release again; what it cannot do is re-use a withdrawn one.
	LeaseID string `json:"lease_id,omitempty"`
}

// MaterializeAccountCredentialResponse carries one released envelope plus the
// non-secret metadata a caller needs to cache it correctly: what it is for,
// when it stops working, and when the central last changed it.
type MaterializeAccountCredentialResponse struct {
	AccountID   string                        `json:"account_id"`
	Provider    orchestration.AccountProvider `json:"provider"`
	DisplayName string                        `json:"display_name,omitempty"`
	ProjectID   string                        `json:"project_id,omitempty"`

	// RefreshPolicy is the account's credential mode. A caller must not act on
	// it by refreshing: renewal is the central's job (it happens inside this
	// call), and the mode is reported so a cache can explain itself.
	RefreshPolicy orchestration.AccountCredentialMode `json:"refresh_policy"`

	// Credential is the released envelope. THE ONLY SECRET-BEARING FIELD IN
	// ANY ACCOUNT RESPONSE TYPE IN THIS PACKAGE.
	Credential *orchestration.AccountCredential `json:"credential"`

	// ExpiresAt is when the released access token stops working, zero when
	// unknown. It is the envelope's own expiry, after any renewal this call
	// performed, so a caching caller can bound the lifetime of what it holds.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// UpdatedAt is the account record's last write. Reported for diagnostics
	// only: it moves on ANY write to the record, including the usage stamp this
	// very release makes, so it cannot be used as a staleness test.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// LeaseID is the delivery grant this release is authorised by: the one the
	// request presented, or the one this release issued. Empty when the
	// deployment wired no leaser.
	//
	// A caller MUST present it on every subsequent re-release of the same copy,
	// and must expect the release to be refused once it has been revoked.
	LeaseID string `json:"lease_id,omitempty"`

	// LeaseExpiresAt is when the delivery grant stops being authorised without
	// renewal. It is NOT the credential's expiry — that is ExpiresAt above, and
	// the OAuth refresher on this side owns it. See
	// orchestration/account_lease.go for why the two are deliberately
	// independent.
	LeaseExpiresAt time.Time `json:"lease_expires_at,omitempty"`

	// LastRefreshedAt is when the ENVELOPE was last replaced — an operator's
	// rotate, or an oauth renewal here. It is the CACHE INVALIDATION SIGNAL: a
	// caller that later observes a newer LastRefreshedAt than the one it
	// materialized under knows its copy is superseded, without asking for the
	// secret again. See pkg/agent's accountCredentialCache.Observe.
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Typed errors
// =============================================================================

// AccountNotReleasableError reports a materialize refused because the account
// is not in a state that may be handed to a spawn. It is deliberately distinct
// from AccountNotFoundError: the account exists and the caller is entitled to
// it, but the pool has judged it unusable and releasing it would produce a
// worker that fails on its first provider call with no explanation.
type AccountNotReleasableError struct {
	// TenantID owns the account.
	TenantID string

	// AccountID is the refused account.
	AccountID string

	// Status is the lifecycle state that refused it.
	Status orchestration.AccountStatus

	// AvailableAt is when a limit hold lapses, zero for every other status and
	// for a hold with no stated expiry.
	AvailableAt time.Time
}

// Error implements the error interface.
func (e *AccountNotReleasableError) Error() string {
	msg := "account not releasable: tenant=" + e.TenantID + " id=" + e.AccountID +
		": status " + string(e.Status)
	if !e.AvailableAt.IsZero() {
		msg += " until " + e.AvailableAt.UTC().Format(time.RFC3339)
	}
	return msg
}

// AccountProjectScopeError reports a materialize refused because the account
// is restricted to a project the request does not name.
//
// The restriction is on the ACCOUNT, so this is not a filter a caller could
// have avoided by asking differently: a project-scoped login is one an operator
// deliberately fenced, and honouring the fence at release time is what makes it
// mean anything on a worker host the operator never sees.
type AccountProjectScopeError struct {
	// AccountID is the refused account.
	AccountID string

	// AccountProjectID is the project the account is restricted to.
	AccountProjectID string

	// RequestProjectID is the project the caller named, empty when it named
	// none.
	RequestProjectID string
}

// Error implements the error interface.
func (e *AccountProjectScopeError) Error() string {
	requested := e.RequestProjectID
	if requested == "" {
		requested = "(none)"
	}
	return "account " + strconv.Quote(e.AccountID) + " is restricted to project " +
		strconv.Quote(e.AccountProjectID) + " and was requested for project " + requested
}

// AccountTokenManagerNotConfiguredError reports a materialize attempted on a
// deployment whose account token manager was never wired.
//
// It fails rather than reading the stored envelope directly, deliberately. The
// token manager is what renews an oauth_refresh account before it is handed
// out and what fills in a provider descriptor's defaults; bypassing it would
// release a credential that is stale, or missing the environment variable name
// its CLI reads, and the failure would surface on the worker host as an
// unexplained provider rejection.
type AccountTokenManagerNotConfiguredError struct{}

// Error implements the error interface.
func (e *AccountTokenManagerNotConfiguredError) Error() string {
	return "account token manager is not configured: this deployment cannot release pooled credentials"
}

// =============================================================================
// Implementation
// =============================================================================

// SetTokenManager wires the AccountTokenManager credential release resolves
// through. A nil manager is ignored, leaving the verb failing closed with
// AccountTokenManagerNotConfiguredError.
//
// It is a settable dependency for the same reason SetUsageTracker is: the
// manager is built during orchestration bring-up (it needs the provider
// registry and the refreshers), which happens strictly later than the service
// container that owns this store pair.
func (s *accountServiceImpl) SetTokenManager(tokens *orchestration.AccountTokenManager) {
	if tokens != nil {
		s.tokens = tokens
	}
}

// SetSpendAuthority wires the cap a credential release is checked against.
// A nil authority is ignored, leaving the release guarded only by the
// account's lifecycle state — which is what a deployment with no consumption
// ledger has.
//
// Settable for the same reason SetTokenManager is: the authority is built
// during orchestration bring-up, over the ledger and the orchestration KV,
// which happens strictly later than the service container that owns this
// store pair.
func (s *accountServiceImpl) SetSpendAuthority(spend orchestration.SpendAuthority) {
	if spend != nil {
		s.spend = spend
	}
}

// SetAccountCredentialLeaser wires the authority that leases each credential
// DELIVERY (see orchestration/account_lease.go). A nil leaser is ignored,
// leaving releases unleased.
//
// Settable for the same reason SetTokenManager is: the leaser is built over the
// lease manager during bring-up, which happens strictly later than the service
// container that owns this store pair.
func (s *accountServiceImpl) SetAccountCredentialLeaser(leases orchestration.AccountCredentialLeaser) {
	if leases != nil {
		s.leases = leases
	}
}

// SetAuditor wires the sink each credential release is recorded on. A nil
// auditor disables the record, which is the default for unit tests and
// embedded use; production binds the platform audit logger.
func (s *accountServiceImpl) SetAuditor(auditor orchestration.StoreAuditor) {
	if auditor != nil {
		s.auditor = auditor
	}
}

// MaterializeAccountCredential releases one account's credential envelope.
func (s *accountServiceImpl) MaterializeAccountCredential(
	ctx context.Context,
	req *MaterializeAccountCredentialRequest,
) (*MaterializeAccountCredentialResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)

	resp, err := s.materialize(ctx, req)
	s.auditRelease(ctx, req, err)
	return resp, err
}

// materialize is the release decision itself, split out so exactly one audit
// event is emitted per attempt regardless of which check refused it.
func (s *accountServiceImpl) materialize(
	ctx context.Context,
	req *MaterializeAccountCredentialRequest,
) (*MaterializeAccountCredentialResponse, error) {
	if s.tokens == nil {
		return nil, &AccountTokenManagerNotConfiguredError{}
	}

	account, err := s.store.Get(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, convertAccountStoreError(err)
	}

	// A restricted account is released only into the project it was fenced to.
	if account.ProjectID != "" && account.ProjectID != req.ProjectID {
		return nil, &AccountProjectScopeError{
			AccountID:        account.ID,
			AccountProjectID: account.ProjectID,
			RequestProjectID: req.ProjectID,
		}
	}

	// Selectable is the SAME test AccountResolver applies before handing a
	// credential to an in-process spawn (revoked and errored never, limited
	// only once its hold has lapsed). Using it here rather than restating the
	// status list keeps the remote path from drifting into releasing something
	// the local path would refuse.
	if !account.Selectable(time.Now().UTC()) {
		return nil, &AccountNotReleasableError{
			TenantID:    req.TenantID,
			AccountID:   account.ID,
			Status:      account.Status,
			AvailableAt: account.AvailableAt,
		}
	}

	// THE CAP, ASKED IN THE SAME PLACE AND OF THE SAME AUTHORITY the
	// in-process spawn asks it (accountResolver.authorizeSpend). This is the
	// path that hands a live paid credential to a host the central does not
	// run, so it is the one where an unmetered release costs the most: the
	// worker caches the envelope and spends against it without asking again.
	//
	// Before tokens.Resolve for the same reason the resolver does it in that
	// order — a refused release must not first renew the account's oauth
	// credential.
	if s.spend != nil {
		if err := s.spend.Authorize(ctx, orchestration.SpendAuthorizeRequest{Account: account}, time.Now().UTC()); err != nil {
			return nil, err
		}
	}

	// THE DELIVERY GRANT IS DECIDED BEFORE THE ENVELOPE IS RESOLVED, and for
	// the same reason the cap is: a release that is going to be refused must
	// not first renew the account's OAuth credential — a provider round trip
	// and a rotation of live secret material on behalf of a request that gets
	// nothing.
	//
	// This is THE server-side enforcement point. A daemon re-materializing
	// under a grant the session's ending has revoked is refused here, from the
	// lease store, whatever it believes about its own state. It is the only
	// place a credential crosses out of the control plane, so it is the only
	// place the control has to hold.
	delivery, leaseErr := s.authorizeDelivery(ctx, req, account)
	if leaseErr != nil {
		return nil, leaseErr
	}

	// Resolve through the token manager, not the secret store: an
	// oauth_refresh account is renewed HERE, on the central that owns its
	// refresh token, so the daemon never holds one and never presents one.
	// The envelope handed back is the effective one — provider-descriptor
	// defaults already merged in.
	cred, resolveErr := s.tokens.Resolve(ctx, account)
	if resolveErr != nil {
		return nil, convertAccountStoreError(resolveErr)
	}

	// The release IS the use. Stamping it here is what keeps the central's
	// bookkeeping honest for accounts whose spawns happen on another host:
	// without it a fleet-wide pool would report every remote account as never
	// used. LastUsedAt on such an account therefore means "last released to a
	// worker host", which is the only thing the central can truthfully observe.
	if touchErr := s.store.Touch(ctx, req.TenantID, account.ID, orchestration.TouchAccountOptions{
		MarkUsed:      true,
		MarkValidated: true,
	}); touchErr != nil {
		// Bookkeeping, not the release. The credential is already resolved and
		// refusing to hand it over would turn a stamp failure into an outage —
		// the same judgment accountResolver.Resolve makes for its own Touch.
		s.logReleaseTouchFailure(req.TenantID, account.ID, touchErr)
	}

	// Re-read is deliberately NOT done. The staleness signal a caller caches
	// against is LastRefreshedAt, which the usage stamp above does not touch;
	// re-reading would only pick up that stamp's own UpdatedAt, which is
	// diagnostics.
	resp := &MaterializeAccountCredentialResponse{
		AccountID:       account.ID,
		Provider:        account.Provider,
		DisplayName:     account.DisplayName,
		ProjectID:       account.ProjectID,
		RefreshPolicy:   account.RefreshPolicy,
		Credential:      cred,
		ExpiresAt:       cred.ExpiresAt,
		UpdatedAt:       account.UpdatedAt,
		LastRefreshedAt: account.LastRefreshedAt,
		Timestamp:       time.Now().UTC(),
	}
	if delivery != nil {
		resp.LeaseID = delivery.LeaseID
		resp.LeaseExpiresAt = delivery.ExpiresAt
	}
	return resp, nil
}

// authorizeDelivery decides whether this release may hand the envelope over,
// and under which grant.
//
// Two cases, and the asymmetry between them is the whole mechanism:
//
//   - NO LEASE PRESENTED — a first release. A grant is ISSUED, and a failure to
//     issue one fails the release: a credential on a worker host that nothing
//     can revoke is exactly what the grant exists to prevent.
//   - A LEASE PRESENTED — a re-release of a copy the caller already holds. The
//     grant is VALIDATED, and revoked / expired / wrongly-scoped are all
//     refusals. The caller's own belief about its copy is irrelevant; the lease
//     store decides.
func (s *accountServiceImpl) authorizeDelivery(
	ctx context.Context,
	req *MaterializeAccountCredentialRequest,
	account *orchestration.Account,
) (*orchestration.AccountCredentialLease, error) {
	if s.leases == nil {
		// A deployment with no lease manager releases unleased. It cannot
		// pretend otherwise: refusing a presented lease it has no way to check
		// would be the more dangerous failure, because a caller could then
		// obtain a release simply by omitting the field.
		return nil, nil
	}

	scope := orchestration.AccountLeaseScope{
		TenantID:  req.TenantID,
		AccountID: account.ID,
		Provider:  account.Provider,
		ProjectID: req.ProjectID,
		JobID:     req.JobID,
		AgentID:   req.AgentID,
	}

	if req.LeaseID != "" {
		return s.leases.Validate(ctx, req.LeaseID, scope)
	}
	return s.leases.GrantDelivery(ctx, scope)
}

// auditRelease records one release attempt. Every outcome is recorded,
// including a refusal: an agent repeatedly asking for a revoked account is
// exactly the pattern an operator wants to see.
func (s *accountServiceImpl) auditRelease(ctx context.Context, req *MaterializeAccountCredentialRequest, err error) {
	if s.auditor == nil {
		return
	}
	s.auditor.RecordStoreEvent(ctx, orchestration.StoreAuditEvent{
		TenantID: req.TenantID,
		Resource: orchestration.AuditResourceAccount,
		// The resource ID names both principals of the release: which agent
		// took which account. The account alone would not answer "which host
		// is holding this login right now", which is the question an operator
		// asks when they revoke one.
		ResourceID: accountReleaseSubject(req),
		Action:     orchestration.AuditActionUse,
		Success:    err == nil,
	})
}

// accountReleaseSubject formats the audited subject of one release. An
// unattributed request (no agent identity on the transport) is recorded as
// such rather than being silently reduced to the account, so an unattributed
// release is visible as a gap in the trail.
func accountReleaseSubject(req *MaterializeAccountCredentialRequest) string {
	agent := req.AgentID
	if agent == "" {
		agent = "(unattributed)"
	}
	subject := req.AccountID + " -> agent " + agent
	// THE GRANT IS NAMED, THE CREDENTIAL NEVER IS. A lease ID is an opaque
	// identifier of an authorization, so recording it makes the trail answer
	// "under which grant was this login released, and is that grant still
	// live?" without any credential content reaching an audit record.
	if req.LeaseID != "" {
		subject += " under lease " + req.LeaseID
	}
	if req.JobID != "" {
		subject += " for job " + req.JobID
	}
	return subject
}

// logReleaseTouchFailure reports a usage stamp that did not land. Best effort:
// a deployment that wired no logger loses the report, which is why the release
// does not depend on it.
func (s *accountServiceImpl) logReleaseTouchFailure(tenantID, accountID string, err error) {
	if s.logger == nil {
		return
	}
	s.logger.Warnf("service: failed to stamp usage on released account %s (tenant %s): %s",
		accountID, tenantID, err.Error())
}
