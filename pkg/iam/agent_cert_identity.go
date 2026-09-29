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

package iam

import (
	"context"
	"crypto/x509"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Agent certificate identity
// =============================================================================
//
// A fleet node daemon holds TWO credentials for one identity: the OAuth2
// application registration mints (bearer / token-mtls), and the mTLS client
// certificate the deploy service issued it. go-qrdb authenticates with the
// CERTIFICATE whenever one is presented -- its authenticator chain is
// first-success-wins with mTLS first (go-qrdb pkg/auth/middleware/rest.go),
// and a module's bearer fallback is consulted ONLY when qrdb's own chain
// produces no identity -- and it resolves that principal's authority the one
// way it resolves anybody's: from the user record its Subject names
// (finalizeIdentity -> lookupUser -> identity.Roles = u.RoleIDs).
//
// The daemon presents its certificate on every call, because it needs one for
// /oauth2/token-mtls. So the principal that reached the control plane was the
// certificate's, the grant go-quicbaas#653 stamped onto the OAuth2 application
// never applied to it, and no user record existed for it at all: every
// heartbeat was refused 403 "agent-node-04 cannot agent_heartbeat on
// orchestrator" (go-quicbaas#663).
//
// EnrollFleetNodeCertIdentity closes that by performing, for a fleet node daemon, the
// same enrollment go-qrdb performs for the platform admin certificate at first
// boot (pkg/firstboot: create the user, grant it a role, bind the certificate's
// SPKI thumbprint). It is the ONLY way a certificate principal can hold
// authority in this stack -- qrdb resolves roles from the store and never from
// the credential -- so it is the seam, not a workaround for one.

// FleetRuntimeRoleName is the tenant-scoped role an enrolled fleet node daemon
// holds. It carries the agent runtime grant and nothing else; an agent must
// not be able to administer the platform that runs it, so it is deliberately
// NOT one of qrdb's built-in roles (the narrowest of those, operator, carries
// the whole orchestrator surface).
const FleetRuntimeRoleName = "agent-runtime"

// AgentSessionRoleName is the tenant-scoped role an enrolled agent SESSION
// holds. A session is not a daemon: it is one spawned agent process, alive for
// the length of one job, reaching the platform only over MCP. It therefore
// holds the MCP floor (AgentSessionGrant) and none of the daemon's
// control-plane verbs -- a session cannot heartbeat, push events, or draw a
// pooled provider credential.
const AgentSessionRoleName = "agent-session"

// AuthMethodMTLS is the permitted-authentication-method token for a principal
// that authenticates with an mTLS client certificate. The literal is go-qrdb's
// MTLSAuthenticator.Name(), which is the value its middleware checks a user's
// AuthMethods policy against (pkg/auth/middleware checkAuthMethod), so the two
// must agree exactly.
//
// It is deliberately absent from supportedAuthMethods, the closed set the
// human create/update surface accepts: QuicBaaS accounts are passwordless and
// activated by WebAuthn or PIV, and this is a MACHINE identity's method,
// conferred only by EnrollFleetNodeCertIdentity.
const AuthMethodMTLS = "mtls"

// MCP permissions a spawned agent session exercises. They are the
// qrdb-canonical "resource:action" names of the four MCP tools a session needs
// -- quicbaas_orchestrator_inference, qrdb_get, qrdb_put and qrdb_list -- and
// they are restated here rather than imported for the same reason
// DefaultFleetRuntimeScopes is: pkg/iam does not depend on the orchestration or
// permission packages. pkg/server TestAgentSessionGrant_* pins the literals
// against orchestration.AgentMCPScopes and against the MCP tool table that
// enforces them.
const (
	// AgentSessionScopeInference is what the sessionless inference tool
	// requires (quicbaas_orchestrator_inference -> orchestrator:inference).
	// It is the reason a session holds an identity at all: reaching a model
	// through the platform is how an agent does its work without the platform
	// handing it a provider credential.
	AgentSessionScopeInference = "orchestrator:inference"

	// AgentSessionScopeKVGet is what single-key reads require (qrdb_get ->
	// kv:get).
	AgentSessionScopeKVGet = "kv:get"

	// AgentSessionScopeKVPut is what key writes require (qrdb_put -> kv:put).
	AgentSessionScopeKVPut = "kv:put"

	// AgentSessionScopeKVList is what key listing requires (qrdb_list ->
	// kv:list).
	AgentSessionScopeKVList = "kv:list"
)

// AgentSessionGrant returns the complete permission set granted to an enrolled
// agent session, in qrdb's canonical "resource:action" vocabulary. It is the
// floor AND the ceiling of the certificate path: the role
// EnrollAgentSessionIdentity converges holds exactly this and is rewritten to
// exactly this on every enrollment, so a grant cannot accumulate.
//
// It grants nothing else, and the absences are the point:
//
//   - No kv:delete. A session writes its own workspace; it must not be able to
//     erase another session's or the platform's records. Destructive KV is not
//     something the loop needs, and "needs" is the whole test.
//   - No secrets, kms, iam, barrier, seal, ceremony, cluster or tenant verb.
//     An agent must not be able to administer the platform that runs it, read
//     its tenant's key material, or mint another identity.
//   - No orchestrator control-plane verb beyond inference. A session cannot
//     spawn siblings, dispatch work, or claim another agent's task.
//
// Widening this set widens every spawned agent in every tenant at once, which
// is why it lives in one function with one pinning test rather than at a call
// site.
func AgentSessionGrant() []string {
	return []string{
		AgentSessionScopeInference,
		AgentSessionScopeKVGet,
		AgentSessionScopeKVPut,
		AgentSessionScopeKVList,
	}
}

// AgentRoleID returns the deterministic role ID for one of a tenant's agent
// roles. Deterministic so a repeated enrollment converges on the role it
// already created instead of minting a second one per registration; the tenant
// is part of the ID because role IDs are globally unique while the role's NAME
// is unique only within its tenant.
func AgentRoleID(roleName, tenantID string) string {
	return "role-" + roleName + "-" + tenantID
}

// FleetRuntimeRoleID returns the deterministic role ID for a tenant's agent
// runtime role.
func FleetRuntimeRoleID(tenantID string) string {
	return AgentRoleID(FleetRuntimeRoleName, tenantID)
}

// AgentSessionRoleID returns the deterministic role ID for a tenant's agent
// session role. ONE role per tenant, shared by every session in it: the role
// is the grant DEFINITION and the user is the principal, so a session is
// revoked by deleting its user record and never by churning the role store.
func AgentSessionRoleID(tenantID string) string {
	return AgentRoleID(AgentSessionRoleName, tenantID)
}

// FleetNodeCertIdentityOptions describes the certificate principal to enroll.
type FleetNodeCertIdentityOptions struct {
	// Certificate is the daemon's verified mTLS client certificate. Every
	// identifying value is derived FROM it rather than supplied beside it --
	// the username from its Subject CommonName (the subject go-qrdb's mTLS
	// authenticator resolves roles by), the binding from
	// qrdbsdk.ComputeSPKIThumbprint (the stack's one canonical mTLS identity
	// algorithm) and the binding's expiry from NotAfter -- so the enrolled
	// record cannot describe a principal other than the one the certificate
	// authenticates.
	Certificate *x509.Certificate

	// DisplayName is the presentation-only label for the record. Optional;
	// the certificate's CommonName is used when empty.
	DisplayName string

	// Permissions is the grant the identity holds, in qrdb's canonical
	// "resource:action" vocabulary. The register endpoint passes
	// FleetRuntimeGrant() -- the one definition of what a daemon may do.
	Permissions []string
}

// username returns the subject go-qrdb's mTLS authenticator derives from the
// certificate.
func (o FleetNodeCertIdentityOptions) username() string {
	if o.Certificate == nil {
		return ""
	}
	return strings.TrimSpace(o.Certificate.Subject.CommonName)
}

// fleetNodeCertUsernameRegex is the charset a certificate CommonName may carry as
// an agent identity's username.
//
// It is deliberately NOT the human-handle rule (validateUsername): the qrdb
// subject IS the CommonName verbatim, and an agent certificate's CN is
// whatever the issuer stamped -- the deploy issuer uses the daemon's LAN IP or
// hostname, so dots are ordinary there and a handle rule that forbids them
// would refuse to enroll the very fleet this exists for. What it keeps is the
// part that is load-bearing: an identifier that is bounded, printable, and
// free of the separators a store composes its index keys from, so a CN can
// never address another tenant's keyspace.
var fleetNodeCertUsernameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)

// validateFleetNodeCertUsername checks the certificate CommonName is usable as the
// qrdb subject the mTLS authenticator will resolve.
func validateFleetNodeCertUsername(username string) error {
	if username == "" {
		return fieldValidationError("certificate",
			"an agent certificate must carry a CommonName: it is the subject qrdb authenticates it as")
	}
	if !fleetNodeCertUsernameRegex.MatchString(username) {
		return fieldValidationError("certificate",
			"certificate CommonName "+strconv.Quote(username)+
				" is not usable as an identity: 3-64 characters of letters, digits, dot, dash or underscore")
	}
	return nil
}

// agentRole names the tenant-scoped role an enrollment converges. It is
// internal on purpose: the role a certificate principal holds is decided by
// WHICH enrollment ran, never by its caller, so no call site can hand a session
// the daemon's role or the reverse.
type agentRole struct {
	// name is the role's tenant-unique name.
	name string

	// description is the operator-facing label written when the role is
	// created.
	description string
}

var (
	// agentRuntimeRole is the role an enrolled fleet node daemon holds.
	agentRuntimeRole = agentRole{
		name:        FleetRuntimeRoleName,
		description: "Agent runtime daemons enrolled in this tenant.",
	}

	// agentSessionRole is the role an enrolled agent session holds.
	agentSessionRole = agentRole{
		name:        AgentSessionRoleName,
		description: "Spawned agent sessions reaching this tenant over MCP.",
	}
)

// EnrollFleetNodeCertIdentity ensures the identity an agent's client certificate
// authenticates as exists in tenantID and holds exactly opts.Permissions.
//
// It is idempotent: a re-registration converges the existing record rather
// than failing or duplicating it, which matters because a daemon re-registers
// on every restart and on certificate renewal. Convergence covers the grant
// (the role's permissions are rewritten when the build's grant changed), the
// account state (an enrolled agent must be active to authenticate) and the
// certificate binding (a renewed certificate re-pins, exactly as the
// self_signed_tls_client_auth application does).
//
// The record it writes is a MACHINE identity: active, restricted to mTLS
// authentication, holding one narrow role and no group membership. It is not
// reachable by any human login ceremony.
func (s *IdentityStore) EnrollFleetNodeCertIdentity(ctx context.Context, tenantID string, opts FleetNodeCertIdentityOptions) (*User, error) {
	return s.enrollFleetNodeCertIdentity(ctx, tenantID, agentRuntimeRole, opts)
}

// EnrollAgentSessionIdentity ensures the identity ONE spawned agent session's
// ephemeral certificate authenticates as exists in tenantID and holds exactly
// the MCP floor (AgentSessionGrant).
//
// It is the session half of the seam EnrollFleetNodeCertIdentity opened for
// daemons, and it exists because a per-session certificate was minted and
// delivered to every spawned agent while nothing enrolled it: go-qrdb resolves
// a certificate principal's roles from the user record its Subject CommonName
// names, found none, and proceeded with empty roles, so a spawned agent
// completed its mTLS handshake and was then refused every MCP tool but the
// public ones (go-quicbaas#901).
//
// Neither the role nor the grant is caller-supplied. A session's authority is
// a property of this build, not of whoever spawned it, so there is no argument
// through which a spawn could widen it.
//
// The tenant MUST be the tenant go-qrdb will authenticate the certificate
// into -- derived from its SPIFFE SAN, NOT from its Organization -- or the
// record lands where the authenticator never looks. The caller establishes that
// (pkg/server AgentMCPPresetResolver); this function cannot, because a
// certificate alone does not say which trust domain is pinned.
func (s *IdentityStore) EnrollAgentSessionIdentity(ctx context.Context, tenantID string, cert *x509.Certificate) (*User, error) {
	return s.enrollFleetNodeCertIdentity(ctx, tenantID, agentSessionRole, FleetNodeCertIdentityOptions{
		Certificate: cert,
		Permissions: AgentSessionGrant(),
	})
}

// enrollFleetNodeCertIdentity is the one implementation both enrollments run. The
// role is a parameter and the grant travels in opts, so a daemon and a session
// share every validation, every convergence rule and every race resolution --
// there is no second copy of this to keep in step.
func (s *IdentityStore) enrollFleetNodeCertIdentity(
	ctx context.Context,
	tenantID string,
	role agentRole,
	opts FleetNodeCertIdentityOptions,
) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if opts.Certificate == nil {
		return nil, fieldValidationError("certificate",
			"an agent certificate identity must be enrolled from the certificate it authenticates with")
	}
	if err := validateFleetNodeCertUsername(opts.username()); err != nil {
		return nil, err
	}
	permissions, err := validateFleetNodeGrant(opts.Permissions)
	if err != nil {
		return nil, err
	}

	// The stores underneath route by the tenant bound to the CONTEXT, and the
	// register endpoint that calls this is a public route carrying none. Bind
	// it here, filling in only what is absent so this can never redirect an
	// admitted caller's write into another tenant (the rule #634 established).
	if TenantFromContext(ctx) == "" {
		ctx = ContextWithTenant(ctx, tenantID)
	}

	roleID, err := s.ensureAgentRole(ctx, tenantID, role, permissions)
	if err != nil {
		return nil, err
	}
	return s.ensureFleetNodeCertUser(ctx, tenantID, roleID, opts)
}

// RevokeAgentSessionIdentity removes the principal EnrollAgentSessionIdentity
// created for one session, so the session's certificate stops resolving to any
// authority the moment the session closes.
//
// Deleting the record -- rather than disabling it -- is what actually revokes.
// go-qrdb resolves an mTLS principal's roles by looking the SUBJECT up in the
// user store and proceeds with empty roles when the lookup misses
// (finalizeIdentity), and it never consults the certificate binding on this
// path. With the record gone the certificate authenticates and then holds
// nothing, which is default-deny at AuthorizeMCP. A disabled record would also
// deny, but it would linger for the life of the tenant, one per session, and
// EnrollAgentSessionIdentity refuses to converge an out-of-service record
// (assertFleetNodeIdentityMayBeConverged), so a CommonName collision would become
// an unspawnable session.
//
// It is idempotent: a principal that is already gone is the state this asked
// for, so a cleanup that runs twice -- or after a crash that took the
// enrollment with it -- succeeds. The ephemeral certificate also expires on its
// own (one day), so this is the prompt revocation on top of a bounded one, not
// the only thing standing between a closed session and the platform.
//
// It refuses to delete anything that is not a session principal. The username
// comes from a certificate CommonName, and a CommonName is not a capability:
// without this guard a session whose CN collided with a human account or an
// fleet node daemon would delete that account on close. The record must hold the
// tenant's session role and nothing else, and must be restricted to mTLS --
// which is exactly and only what this enrollment writes.
func (s *IdentityStore) RevokeAgentSessionIdentity(ctx context.Context, tenantID, username string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	subject := strings.TrimSpace(username)
	if err := validateFleetNodeCertUsername(subject); err != nil {
		return err
	}
	if TenantFromContext(ctx) == "" {
		ctx = ContextWithTenant(ctx, tenantID)
	}

	existing, err := s.userStorage.GetUserByUsername(ctx, tenantID, subject)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing == nil {
		return nil
	}
	if err := assertIsAgentSessionIdentity(existing, AgentSessionRoleID(tenantID)); err != nil {
		return err
	}
	if err := s.DeleteUser(ctx, tenantID, existing.ID); err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// assertIsAgentSessionIdentity reports whether existing is a record this
// build's session enrollment wrote, and therefore one a session close may
// delete.
func assertIsAgentSessionIdentity(existing *User, sessionRoleID string) error {
	holdsOnlySessionRole := len(existing.Roles) == 1 && existing.Roles[0] == sessionRoleID
	restrictedToMTLS := len(existing.AuthMethods) == 1 && existing.AuthMethods[0] == AuthMethodMTLS
	if holdsOnlySessionRole && restrictedToMTLS {
		return nil
	}
	return &NotAnAgentSessionIdentityError{
		Username: existing.Username,
		TenantID: existing.TenantID,
		Roles:    append([]string(nil), existing.Roles...),
	}
}

// NotAnAgentSessionIdentityError reports that a session close named a principal
// that is not one of this build's session identities. The close is refused
// rather than deleting an account it does not own.
type NotAnAgentSessionIdentityError struct {
	// Username is the certificate subject the close named.
	Username string
	// TenantID is the tenant the record belongs to.
	TenantID string
	// Roles are the roles the record actually holds, for the operator's log.
	Roles []string
}

// Error implements the error interface.
func (e *NotAnAgentSessionIdentityError) Error() string {
	return "iam: " + strconv.Quote(e.Username) + " in tenant " + strconv.Quote(e.TenantID) +
		" is not an agent session identity (roles: " + strings.Join(e.Roles, ",") +
		"); refusing to delete it"
}

// ensureAgentRole converges the named tenant-scoped agent role onto permissions
// and returns its ID. A role that already exists with a different permission
// set is REWRITTEN: the running build's grant is the authority, so a fleet
// enrolled against an older definition repairs itself at the next registration
// instead of holding a grant nothing in the code names any more.
func (s *IdentityStore) ensureAgentRole(
	ctx context.Context,
	tenantID string,
	role agentRole,
	permissions []string,
) (string, error) {
	roleID := AgentRoleID(role.name, tenantID)

	existing, err := s.GetRole(ctx, tenantID, roleID)
	switch {
	case err == nil:
		if samePermissionSet(existing.Permissions, permissions) {
			return roleID, nil
		}
		granted := append([]string(nil), permissions...)
		if _, updateErr := s.UpdateRole(ctx, tenantID, roleID, UpdateRoleOptions{
			Permissions: &granted,
		}); updateErr != nil {
			return "", updateErr
		}
		return roleID, nil
	case !dberrors.IsNotFound(err):
		return "", err
	}

	if _, createErr := s.CreateRole(ctx, tenantID, CreateRoleOptions{
		ID:          roleID,
		Name:        role.name,
		Description: role.description,
		Permissions: append([]string(nil), permissions...),
	}); createErr != nil {
		// Two daemons registering at once both find the role absent, and the
		// loser is told it exists. That is the role this enrollment wanted, so
		// the conflict IS the success it was racing for.
		if !isAlreadyExistsError(createErr) {
			return "", createErr
		}
	}
	return roleID, nil
}

// ensureFleetNodeCertUser creates or converges the certificate principal's user
// record and returns it.
func (s *IdentityStore) ensureFleetNodeCertUser(
	ctx context.Context,
	tenantID, roleID string,
	opts FleetNodeCertIdentityOptions,
) (*User, error) {
	displayName := strings.TrimSpace(opts.DisplayName)
	if displayName == "" {
		displayName = opts.username()
	}
	binding := qrdbsdk.CertBinding{
		Thumbprint: qrdbsdk.ComputeSPKIThumbprint(opts.Certificate),
		Kind:       qrdbsdk.CertBindingSPKIThumbprint,
		NotAfter:   opts.Certificate.NotAfter,
	}
	now := time.Now().UTC()

	existing, err := s.userStorage.GetUserByUsername(ctx, tenantID, opts.username())
	if err != nil && !dberrors.IsNotFound(err) {
		return nil, err
	}
	if err == nil && existing != nil {
		return s.convergeFleetNodeCertUser(ctx, existing, displayName, roleID, binding)
	}

	user := &User{
		TenantID: tenantID,
		Username: opts.username(),
		Name:     displayName,
		Roles:    []string{roleID},
		// No email. A fleet node daemon has no mailbox and must not occupy a
		// tenant's email login-key index; qrdb's user store carries an
		// explicit no-email-login-key mode for exactly this.
		AuthMethods:  []string{AuthMethodMTLS},
		Lifecycle:    LifecycleActive,
		CertBindings: []qrdbsdk.CertBinding{binding},
		Created:      now,
		Updated:      now,
	}
	if createErr := s.userStorage.CreateUser(ctx, user); createErr != nil {
		// Lost a race with a concurrent registration of the same daemon: the
		// record now exists and is this identity's own, so converge onto it.
		if isAlreadyExistsError(createErr) {
			return s.convergeExistingFleetNodeCertUser(ctx, tenantID, roleID, displayName, binding, opts)
		}
		return nil, createErr
	}
	return user, nil
}

// convergeExistingFleetNodeCertUser re-reads the record a concurrent enrollment
// committed and applies this enrollment's state to it.
func (s *IdentityStore) convergeExistingFleetNodeCertUser(
	ctx context.Context,
	tenantID, roleID, displayName string,
	binding qrdbsdk.CertBinding,
	opts FleetNodeCertIdentityOptions,
) (*User, error) {
	existing, err := s.userStorage.GetUserByUsername(ctx, tenantID, opts.username())
	if err != nil {
		return nil, err
	}
	return s.convergeFleetNodeCertUser(ctx, existing, displayName, roleID, binding)
}

// convergeFleetNodeCertUser brings an existing record to the state this enrollment
// asserts and commits it. It is the ONE place that state is written, so the
// ordinary re-registration and the lost-race path cannot drift apart.
//
// The binding list is REPLACED rather than appended to: a daemon holds exactly
// one certificate, and leaving the superseded thumbprint bound would keep a
// rotated-out credential naming this identity for as long as the record lives.
func (s *IdentityStore) convergeFleetNodeCertUser(
	ctx context.Context,
	existing *User,
	displayName, roleID string,
	binding qrdbsdk.CertBinding,
) (*User, error) {
	if err := assertFleetNodeIdentityMayBeConverged(existing); err != nil {
		return nil, err
	}
	existing.Name = displayName
	existing.Roles = withRole(existing.Roles, roleID)
	existing.AuthMethods = []string{AuthMethodMTLS}
	existing.Lifecycle = LifecycleActive
	existing.CertBindings = []qrdbsdk.CertBinding{binding}
	existing.Updated = time.Now().UTC()
	if err := s.userStorage.UpdateUser(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// assertFleetNodeIdentityMayBeConverged refuses to re-activate an identity an
// administrator (or a lockout) took out of service.
//
// A registration is authenticated by a CA-issued agent certificate alone, so
// letting it flip a disabled, suspended or locked record back to active would
// make "disable this agent" undone by the agent itself at its next restart.
// Pending is converged, not refused: that is a record awaiting activation, not
// one withdrawn from service.
func assertFleetNodeIdentityMayBeConverged(existing *User) error {
	switch existing.Lifecycle {
	case LifecycleActive, LifecyclePending, "":
		return nil
	default:
		return &AgentIdentityNotInServiceError{
			Username:  existing.Username,
			TenantID:  existing.TenantID,
			Lifecycle: string(existing.Lifecycle),
		}
	}
}

// AgentIdentityNotInServiceError reports a registration whose certificate
// principal exists but has been taken out of service. Registration fails
// rather than resurrecting it.
type AgentIdentityNotInServiceError struct {
	// Username is the certificate subject.
	Username string
	// TenantID is the tenant the identity belongs to.
	TenantID string
	// Lifecycle is the state that refused the convergence.
	Lifecycle string
}

// Error implements the error interface.
func (e *AgentIdentityNotInServiceError) Error() string {
	return "iam: agent identity " + strconv.Quote(e.Username) + " in tenant " +
		strconv.Quote(e.TenantID) + " is " + e.Lifecycle + " and will not be re-enrolled"
}

// validateFleetNodeGrant returns the trimmed, de-duplicated grant, refusing an
// empty set and a wildcard. An identity enrolled with no permissions would be
// authenticated and refused everything, which is the defect this exists to fix
// rather than a state worth writing; a wildcard would hand a daemon the
// platform that runs it.
func validateFleetNodeGrant(permissions []string) ([]string, error) {
	out := make([]string, 0, len(permissions))
	seen := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		value := strings.TrimSpace(permission)
		if value == "" {
			return nil, fieldValidationError("permissions", "a permission cannot be blank")
		}
		if strings.Contains(value, "*") {
			return nil, fieldValidationError("permissions",
				"an agent runtime grant cannot contain a wildcard: "+value)
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, fieldValidationError("permissions",
			"an agent certificate identity must be enrolled with at least one permission")
	}
	return out, nil
}

// withRole returns roles with roleID present exactly once, preserving order.
func withRole(roles []string, roleID string) []string {
	for _, held := range roles {
		if held == roleID {
			return roles
		}
	}
	return append(append([]string(nil), roles...), roleID)
}

// samePermissionSet reports whether two permission lists carry the same
// members, ignoring order and duplicates.
func samePermissionSet(a, b []string) bool {
	left := dedupeSorted(a)
	right := dedupeSorted(b)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// dedupeSorted returns the sorted, de-duplicated members of in.
func dedupeSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// isAlreadyExistsError reports whether err is a storage conflict — the class a
// concurrent enrollment of the same agent produces.
//
// The CODE is the question, deliberately: the conflict arrives as a different
// sentinel from each store this enrollment writes through (a duplicate user, a
// duplicate role name, a duplicate role id), every one of them means the record
// this enrollment wanted already exists, and every one of them is converged the
// same way. Matching sentinels one at a time would make the set a list to keep
// in step with three stores.
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	var qbErr *dberrors.QBError
	if errors.As(err, &qbErr) {
		return qbErr.Code == dberrors.ErrAlreadyExists
	}
	return false
}
