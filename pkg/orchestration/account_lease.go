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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
)

// =============================================================================
// THE PROVIDER CREDENTIAL GETS A LEASE — over its DELIVERY, not over the token.
//
// An agent's subscription login has to exist inside the container: the CLI dials
// the provider itself and a request proxy was ruled out. So it cannot be
// removed — but it can stop being DURABLE. Every hand-over of an account's
// credential (in-process into a spawn's environment / materialization
// directory, or over the wire to an enrolled daemon) now takes a lease that is
// scoped to (tenant, project, job, account), short-lived, audited, and revoked
// when the session that asked for it ends.
//
// WHAT THIS IS NOT. It is not a second expiry model competing with the
// credential's own. A subscription account is OAuth: AccountCredential.ExpiresAt
// is the access token's real lifetime and account_refresher_oauth2.go is the
// authority that extends it. THAT RELATIONSHIP IS UNTOUCHED. A lease says
// nothing about how long the token works; it says how long this DELIVERY stays
// authorised without renewal. The two therefore cannot fight:
//
//   - a lease lapsing does NOT shorten a token the refresher would have kept
//     alive — the copy already delivered keeps working until the provider
//     rejects it, which is the only thing that could have stopped it before;
//   - a token being refreshed does NOT extend a lease — the refresher writes a
//     new envelope, and the next delivery of it takes its own lease;
//   - what a lapsed or revoked lease DOES stop is the next delivery. A daemon
//     whose lease has been revoked cannot materialize the credential again, so
//     its in-memory copy ages out (pkg/orchestration/fleet bounds it at
//     accountCacheMaxTTL) and cannot be replaced.
//
// That is the honest boundary of server-side enforcement for a credential
// issued by somebody else's identity provider: the platform controls DELIVERY,
// and it enforces that control at the one place material crosses the boundary.
//
// WHAT THIS IS NOT, PART TWO. It is not a reimplementation of the account pool.
// Selection, status, cooldown, the usage-limit classifier, the quota window,
// the breaker, the OAuth refresher and rotation all already exist and are
// untouched; this file adds a lifecycle AROUND them. Nor is it a second lease
// implementation: it composes pkg/lease.LeaseManager, the same one
// Security().DynamicSecrets() issues through, so an account delivery lease
// appears in the same ListLeases and is revocable with the same RevokeLease as
// every other lease in the platform.
// =============================================================================

const (
	// accountLeasePathPrefix namespaces account-delivery leases inside the
	// lease keyspace. It is the prefix an operator revokes wholesale with
	// RevokePrefix, and the prefix ListLeases filters on to answer "which hosts
	// currently hold one of my provider logins?".
	accountLeasePathPrefix = "accounts/delivery/"

	// accountLeaseScopeNone is the path segment standing in for an absent
	// project or job. A missing segment would make two different scopes share
	// one path — a tenant-wide delivery and a project delivery would collide —
	// so the absence is spelled rather than skipped.
	accountLeaseScopeNone = "_none"

	// DefaultAccountDeliveryTTL is how long one delivery of an account
	// credential stays authorised without renewal.
	//
	// An hour, because the bound that matters is the SESSION, and the session's
	// own conclusion revokes the lease explicitly (session_conclusion.go). The
	// TTL is the backstop for the case where no conclusion ever runs — a node
	// that was destroyed rather than shut down — and an hour is short enough
	// that such a delivery stops being renewable quickly while being long
	// enough that an ordinary job never has to renew mid-turn.
	DefaultAccountDeliveryTTL = time.Hour

	// accountLeaseKind labels the lease's purpose in its metadata. Metadata is
	// non-secret bookkeeping ONLY; see AccountCredentialLease.
	accountLeaseKind = "agent_account_credential_delivery"
)

// Lease metadata keys. Every one of them is an IDENTIFIER. No field of an
// AccountCredential — no access token, no refresh token, no file content — is
// ever written into a lease, which is what makes a lease record safe to list,
// log and audit.
const (
	accountLeaseMetaKind      = "kind"
	accountLeaseMetaTenantID  = "tenant_id"
	accountLeaseMetaProjectID = "project_id"
	accountLeaseMetaJobID     = "job_id"
	accountLeaseMetaAccountID = "account_id"
	accountLeaseMetaProvider  = "provider"
	accountLeaseMetaAgentID   = "agent_id"
)

// AccountLeaseScope identifies ONE delivery of ONE account's credential.
//
// It is the unit of revocation: everything an operator or the session reaper
// needs to say "that copy is no longer authorised" without naming the copy.
type AccountLeaseScope struct {
	// TenantID owns the account. Required.
	TenantID string

	// AccountID is the account whose credential was delivered. Required.
	AccountID string

	// Provider is the account's provider, recorded so a listing can be read
	// per provider without joining back to the account record.
	Provider AccountProvider

	// ProjectID scopes the delivery to a project, empty for a tenant-wide one.
	ProjectID string

	// JobID names the job the delivery served, empty for a session with no job.
	JobID string

	// AgentID names the enrolled daemon a REMOTE delivery went to, empty for an
	// in-process spawn. It is what turns "this login is out there somewhere"
	// into "this host has it".
	AgentID string
}

// AccountCredentialLease is an issued delivery grant.
//
// It carries no credential material — deliberately, and permanently. A caller
// that has this value knows a delivery is authorised and until when; it learns
// nothing about what was delivered.
type AccountCredentialLease struct {
	// LeaseID is the grant's identifier, and the handle every later operation
	// (renew, validate, revoke) uses.
	LeaseID string `json:"lease_id"`

	// ExpiresAt is when the delivery stops being authorised without renewal.
	// It is NOT the credential's expiry — see the file comment.
	ExpiresAt time.Time `json:"expires_at"`

	// Scope is what the grant covers.
	Scope AccountLeaseScope `json:"scope"`
}

// AccountDeliveryGrantor supplies the grant covering one hand-over of an
// account's credential.
//
// IT IS SEPARATE FROM THE FULL LEASER BECAUSE ONE IMPLEMENTATION DOES NOT ISSUE.
// An account resolution needs exactly this one verb, and there are two honest
// answers to it:
//
//   - on the control plane, accountCredentialLeaser CREATES the grant in the
//     lease store — it owns the store, so it is the issuing authority;
//   - on a worker daemon, the grant has already been created. A daemon's
//     credential release IS the control plane's materialize verb, and that verb
//     issues the grant; the daemon's grantor REPORTS the one its release was
//     served under (pkg/orchestration/fleet/account_grant.go) so the resolution can carry it
//     back to the control plane's session record, where the conclusion that
//     withdraws it can find it.
//
// A daemon cannot implement Validate or Revoke — it holds no lease store and
// has no authority over one — so requiring it to would force a stub. Naming
// the verb GrantDelivery rather than Issue says what both implementations
// actually promise: that the returned grant covers this delivery.
type AccountDeliveryGrantor interface {
	// GrantDelivery returns the grant covering a hand-over for scope, or nil
	// when the deployment leases no deliveries at all.
	GrantDelivery(ctx context.Context, scope AccountLeaseScope) (*AccountCredentialLease, error)
}

// AccountCredentialLeaser is the full authority over delivery grants: it
// supplies them, checks them and withdraws them.
//
// It is an interface because both credential release paths compose it — the
// in-process resolver (account_resolver.go) and the remote materialize verb
// (pkg/service/account_materialize.go) — and because a deployment with no lease
// manager wired must be able to run without one rather than fail every spawn.
type AccountCredentialLeaser interface {
	AccountDeliveryGrantor

	// Validate reports whether leaseID is still a live grant FOR scope. A
	// revoked, expired, unknown or differently-scoped lease is refused.
	Validate(ctx context.Context, leaseID string, scope AccountLeaseScope) (*AccountCredentialLease, error)

	// Revoke withdraws a grant. Revoking an unknown or already-revoked grant
	// succeeds, so a retrying session conclusion converges.
	Revoke(ctx context.Context, leaseID string) error
}

// =============================================================================
// Typed errors
// =============================================================================

// AccountLeaseNotConfiguredError reports an operation that needs a delivery
// leaser on a deployment that wired none.
type AccountLeaseNotConfiguredError struct {
	// Operation names what was attempted.
	Operation string
}

// Error implements the error interface.
func (e *AccountLeaseNotConfiguredError) Error() string {
	return "account credential delivery leases are not configured: cannot " + e.Operation
}

// AccountLeaseScopeError reports a lease presented for a delivery it does not
// cover.
//
// It is REFUSED rather than widened. A lease is the authority for one account's
// delivery to one job; honouring it for another account would make the scope
// decorative, and honouring it for another tenant would be a cross-tenant
// credential release.
type AccountLeaseScopeError struct {
	// LeaseID is the presented lease.
	LeaseID string

	// Field names the scope component that did not match.
	Field string

	// Want is what the lease says.
	Want string

	// Got is what the request claimed.
	Got string
}

// Error implements the error interface.
func (e *AccountLeaseScopeError) Error() string {
	return "account credential lease " + e.LeaseID + " is scoped to " + e.Field +
		" " + quoteScope(e.Want) + " and was presented for " + quoteScope(e.Got)
}

// AccountLeaseIssueError reports a delivery grant that could not be created.
//
// The release FAILS CLOSED on it. A credential handed over without a lease is a
// credential nothing can revoke, which is the state this whole mechanism exists
// to remove — so a lease store that will not answer stops the release rather
// than being stepped over.
type AccountLeaseIssueError struct {
	// TenantID owns the account.
	TenantID string

	// AccountID is the account whose delivery was refused.
	AccountID string

	// Cause is the lease manager's error.
	Cause error
}

// Error implements the error interface.
func (e *AccountLeaseIssueError) Error() string {
	return "failed to lease credential delivery for account " + e.AccountID +
		" (tenant " + e.TenantID + "): " + e.Cause.Error()
}

// Unwrap exposes the lease manager's error.
func (e *AccountLeaseIssueError) Unwrap() error { return e.Cause }

// quoteScope renders a scope value, naming an empty one rather than printing
// nothing.
func quoteScope(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// =============================================================================
// The implementation
// =============================================================================

// accountCredentialLeaser is the only AccountCredentialLeaser. It holds no
// state of its own: durability, replication and at-rest encryption are
// properties of the lease manager's backing store.
type accountCredentialLeaser struct {
	leases  lease.LeaseManager
	auditor StoreAuditor
	ttl     time.Duration
}

// Compile-time interface satisfaction check.
var _ AccountCredentialLeaser = (*accountCredentialLeaser)(nil)
var _ AccountDeliveryGrantor = (*accountCredentialLeaser)(nil)

// NewAccountCredentialLeaser returns a leaser over leases.
//
// ttl of zero selects DefaultAccountDeliveryTTL. auditor may be nil, which
// disables the issuance trail — the default for unit tests and embedded use;
// production binds the platform audit logger.
func NewAccountCredentialLeaser(
	leases lease.LeaseManager,
	ttl time.Duration,
	auditor StoreAuditor,
) AccountCredentialLeaser {
	if ttl <= 0 {
		ttl = DefaultAccountDeliveryTTL
	}
	return &accountCredentialLeaser{leases: leases, auditor: auditor, ttl: ttl}
}

// AccountLeasePath renders the lease path one delivery scope occupies.
//
// Exported alongside the prefix so an operator tool (and the test that pins the
// layout) can address the same keyspace the issuer writes, rather than
// reconstructing the scheme and drifting from it.
func AccountLeasePath(scope AccountLeaseScope) (string, error) {
	if err := validateKeySegment("tenant_id", scope.TenantID); err != nil {
		return "", accountKeyError(err)
	}
	if err := validateKeySegment("account_id", scope.AccountID); err != nil {
		return "", accountKeyError(err)
	}
	project := scope.ProjectID
	if project == "" {
		project = accountLeaseScopeNone
	}
	if err := validateKeySegment("project_id", project); err != nil {
		return "", accountKeyError(err)
	}
	job := scope.JobID
	if job == "" {
		job = accountLeaseScopeNone
	}
	if err := validateKeySegment("job_id", job); err != nil {
		return "", accountKeyError(err)
	}
	return accountLeasePathPrefix + scope.TenantID + keySeparator + project +
		keySeparator + job + keySeparator + scope.AccountID, nil
}

// GrantDelivery creates a delivery grant for scope.
func (l *accountCredentialLeaser) GrantDelivery(ctx context.Context, scope AccountLeaseScope) (*AccountCredentialLease, error) {
	if l == nil || l.leases == nil {
		return nil, &AccountLeaseNotConfiguredError{Operation: "issue a delivery grant"}
	}

	path, err := AccountLeasePath(scope)
	if err != nil {
		return nil, err
	}

	// RENEWABLE, UNBOUNDED IN COUNT, BOUNDED IN TIME. The number of renewals is
	// not the control — the session's conclusion is, and it revokes outright.
	// Capping renewals would instead cut off a long-running job at an arbitrary
	// multiple of the TTL, which is the "lease TTL silently truncating a
	// credential the refresher would have kept alive" failure in another shape.
	issued, err := l.leases.CreateLease(ctx, path, l.ttl, &lease.LeaseOptions{
		Renewable:   true,
		MaxRenewals: 0,
		Metadata:    accountLeaseMetadata(scope),
	})
	if err != nil {
		wrapped := &AccountLeaseIssueError{TenantID: scope.TenantID, AccountID: scope.AccountID, Cause: err}
		l.audit(ctx, scope, "", wrapped)
		return nil, wrapped
	}

	out := &AccountCredentialLease{LeaseID: issued.LeaseID, ExpiresAt: issued.ExpiresAt, Scope: scope}
	l.audit(ctx, scope, issued.LeaseID, nil)
	return out, nil
}

// Validate reports whether leaseID is still a live grant for scope.
func (l *accountCredentialLeaser) Validate(
	ctx context.Context,
	leaseID string,
	scope AccountLeaseScope,
) (*AccountCredentialLease, error) {
	if l == nil || l.leases == nil {
		return nil, &AccountLeaseNotConfiguredError{Operation: "validate a delivery grant"}
	}

	// The state check comes FIRST and is the lease manager's: revoked, expired
	// and unknown are its answers, from its store, not from anything cached
	// here. A scope check on a revoked lease would leak which scope it had.
	held, err := l.leases.Validate(ctx, leaseID)
	if err != nil {
		return nil, err
	}

	// THE SCOPE IS PART OF THE GRANT. A live lease for another account, tenant
	// or job is not a lease for this delivery, and accepting it would mean one
	// authorised delivery could unlock every other one.
	if mismatch := accountLeaseScopeMismatch(leaseID, held.Metadata, scope); mismatch != nil {
		return nil, mismatch
	}

	return &AccountCredentialLease{LeaseID: held.LeaseID, ExpiresAt: held.ExpiresAt, Scope: scope}, nil
}

// Revoke withdraws a grant.
func (l *accountCredentialLeaser) Revoke(ctx context.Context, leaseID string) error {
	if l == nil || l.leases == nil {
		return &AccountLeaseNotConfiguredError{Operation: "revoke a delivery grant"}
	}
	if leaseID == "" {
		// Nothing was ever issued. A session that used no pooled account, or
		// one spawned before this mechanism existed, has no grant to withdraw;
		// treating that as a failure would make it unconcludable.
		return nil
	}
	return l.leases.RevokeLease(ctx, leaseID)
}

// audit records one issuance attempt.
//
// THE LEASE ID IS THE SUBJECT, AND IT IS NOT A SECRET. The trail answers "which
// host was authorised to hold which account's login, for which job, and when" —
// the question an operator asks when they revoke an account — and it can answer
// it without any credential content ever reaching an audit record.
func (l *accountCredentialLeaser) audit(ctx context.Context, scope AccountLeaseScope, leaseID string, err error) {
	if l.auditor == nil {
		return
	}
	l.auditor.RecordStoreEvent(ctx, StoreAuditEvent{
		TenantID:   scope.TenantID,
		Resource:   AuditResourceAccount,
		ResourceID: accountLeaseAuditSubject(scope, leaseID),
		Action:     AuditActionUse,
		Success:    err == nil,
	})
}

// accountLeaseAuditSubject formats the audited subject of one delivery grant.
func accountLeaseAuditSubject(scope AccountLeaseScope, leaseID string) string {
	subject := scope.AccountID + " delivery"
	if leaseID != "" {
		subject += " " + leaseID
	}
	if scope.JobID != "" {
		subject += " for job " + scope.JobID
	}
	if scope.AgentID != "" {
		subject += " to agent " + scope.AgentID
	}
	return subject
}

// accountLeaseMetadata renders the non-secret bookkeeping stored on a lease.
func accountLeaseMetadata(scope AccountLeaseScope) map[string]string {
	meta := map[string]string{
		accountLeaseMetaKind:      accountLeaseKind,
		accountLeaseMetaTenantID:  scope.TenantID,
		accountLeaseMetaAccountID: scope.AccountID,
	}
	if scope.Provider != "" {
		meta[accountLeaseMetaProvider] = string(scope.Provider)
	}
	if scope.ProjectID != "" {
		meta[accountLeaseMetaProjectID] = scope.ProjectID
	}
	if scope.JobID != "" {
		meta[accountLeaseMetaJobID] = scope.JobID
	}
	if scope.AgentID != "" {
		meta[accountLeaseMetaAgentID] = scope.AgentID
	}
	return meta
}

// accountLeaseScopeMismatch reports the first scope component a lease does not
// cover, or nil when it covers the whole request.
//
// Only the components that identify WHO the credential is for are checked.
// AgentID is deliberately not: a lease issued to one daemon and presented by
// another is a scope violation, but the daemon identity on a materialize
// request is already bound by the transport to the authenticated principal, so
// the check would compare a field against itself. Project and job ARE checked,
// because both are caller-supplied on the request.
func accountLeaseScopeMismatch(leaseID string, meta map[string]string, scope AccountLeaseScope) *AccountLeaseScopeError {
	checks := []struct {
		field string
		want  string
		got   string
	}{
		{field: "tenant_id", want: meta[accountLeaseMetaTenantID], got: scope.TenantID},
		{field: "account_id", want: meta[accountLeaseMetaAccountID], got: scope.AccountID},
		{field: "project_id", want: meta[accountLeaseMetaProjectID], got: scope.ProjectID},
		{field: "job_id", want: meta[accountLeaseMetaJobID], got: scope.JobID},
	}
	for _, c := range checks {
		if c.want != c.got {
			return &AccountLeaseScopeError{LeaseID: leaseID, Field: c.field, Want: c.want, Got: c.got}
		}
	}
	return nil
}

// DeliveryGrantSlot carries the delivery grant ONE resolution was served under,
// from the credential lookup that learned it to the grantor that reports it.
//
// IT EXISTS BECAUSE A SHARED CACHE CANNOT ANSWER "WHICH GRANT WAS MINE". A
// daemon's grantor used to read the grant back out of its credential cache
// after the lookup. With single-use releases (a credential read from a tenant
// secret is never reused) a concurrent spawn of the same account replaces that
// entry in between, and the first spawn then reports the second one's grant,
// or none, leaving its own release untracked by session teardown. The resolver
// opens a slot per resolution and threads it through the context it already
// passes to both calls, so the grant travels with the resolution itself.
type DeliveryGrantSlot struct {
	mu        sync.Mutex
	recorded  bool
	leaseID   string
	expiresAt time.Time
}

// Record stores the grant this resolution's credential was served under. The
// last write wins; a resolution serves one credential.
func (s *DeliveryGrantSlot) Record(leaseID string, expiresAt time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorded, s.leaseID, s.expiresAt = true, leaseID, expiresAt
}

// Grant returns the recorded grant, and whether anything was recorded at all.
// A recorded empty lease ID means the credential was served under no grant.
func (s *DeliveryGrantSlot) Grant() (leaseID string, expiresAt time.Time, recorded bool) {
	if s == nil {
		return "", time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leaseID, s.expiresAt, s.recorded
}

// deliveryGrantSlotKey is the context key for a resolution's DeliveryGrantSlot.
type deliveryGrantSlotKey struct{}

// WithDeliveryGrantSlot returns ctx carrying a fresh DeliveryGrantSlot.
func WithDeliveryGrantSlot(ctx context.Context) (context.Context, *DeliveryGrantSlot) {
	slot := &DeliveryGrantSlot{}
	return context.WithValue(ctx, deliveryGrantSlotKey{}, slot), slot
}

// DeliveryGrantSlotFrom returns the resolution's slot, or nil when ctx carries
// none (a caller outside a resolution).
func DeliveryGrantSlotFrom(ctx context.Context) *DeliveryGrantSlot {
	slot, _ := ctx.Value(deliveryGrantSlotKey{}).(*DeliveryGrantSlot)
	return slot
}
