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

// Package qrdbadapter exposes go-quicbaas's IAM (IdentityStore + TenantManager)
// as the narrow TenantCreator / RoleCreator / UserCreator surfaces that
// go-qrdb/pkg/firstboot.Initialize consumes. The point is to make qrdb's
// canonical first-boot bootstrap the SOLE source of truth for the
// "system tenant + built-in roles + admin user" trio across the stack,
// even though quicbaas has its own richer IAM model on top.
//
// Wire quicbaas IAM into qrdbfirstboot like this:
//
//	deps := qrdbfirstboot.Dependencies{
//	    Tenants: qrdbadapter.WrapTenant(quicbaasTenantManager),
//	    Roles:   qrdbadapter.WrapRole(quicbaasIdentityStore),
//	    Users:   qrdbadapter.WrapUser(quicbaasIdentityStore),
//	    CA:      certProvider,
//	    Logger:  log.Slog(),
//	}
//	_, err := qrdbfirstboot.Initialize(ctx, deps)
//
// The adapter translates qrdb's narrow types (e.g., user with just
// Username + TenantID + RoleIDs) into quicbaas's richer
// CreateUserOptions / CreateRoleOptions / CreateTenantOptions, defaulting
// the missing fields (e.g., synthesizes an email of the form
// "<username>@<tenant>.local" for the admin so quicbaas's validator
// accepts it).
//
// Error mapping: idempotent-skip relies on the create call returning
// the precise sentinel that qrdbfirstboot's errors.Is checks recognize.
// The adapter inspects quicbaas's *dberrors.QBError instances and emits
// the corresponding qrdb-package error type.
package qrdbadapter

import (
	"context"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Tenant adapter
// =============================================================================

// TenantAdapter exposes a quicbaas iam.TenantManager as a qrdb
// TenantCreator. Use WrapTenant to construct.
type TenantAdapter struct {
	manager iam.TenantManager
}

// WrapTenant adapts a quicbaas TenantManager to the qrdb TenantCreator
// surface qrdbfirstboot.Initialize consumes.
func WrapTenant(m iam.TenantManager) *TenantAdapter { return &TenantAdapter{manager: m} }

// CreateTenant satisfies qrdbfirstboot.TenantCreator. The isolation
// argument is forwarded to quicbaas's TenantManager (via
// CreateTenantOptions.IsolationType) so a barrier-isolation request
// reaches qrdb's engine-side CreateTenant, which gives the tenant its own
// DEK and "{tenantID}/" namespace. An empty isolation defers to qrdb's
// default (prefix). The reserved system tenant is forced to prefix here
// because qrdb rejects a barrier system tenant with
// ErrSystemTenantMustBePrefix. Returns qrdb.tenant.ErrTenantAlreadyExists
// when the underlying store reports a duplicate so qrdbfirstboot's
// idempotent re-run path triggers.
// The variadic opts match qrdb's firstboot.TenantCreator seam; this
// adapter translates to quicbaas's own iam.CreateTenantOptions and does
// not forward them (first-boot does not pass any).
func (a *TenantAdapter) CreateTenant(ctx context.Context, id, displayName string, isolation qrdbsdk.TenantIsolationType, _ ...qrdbsdk.CreateTenantOption) (*qrdbsdk.ServiceTenantInfo, error) {
	if a == nil || a.manager == nil {
		return nil, &NilDependencyError{Name: "TenantManager"}
	}
	// The system tenant must use prefix isolation; never request barrier
	// for it regardless of what the caller passed.
	if id == qrdbsdk.SystemTenantID {
		isolation = ""
	}
	opts := iam.CreateTenantOptions{
		ID:            id,
		Name:          displayName,
		IsolationType: isolation,
	}
	_, err := a.manager.CreateTenant(ctx, opts)
	if err != nil {
		if isAlreadyExists(err) {
			return nil, qrdbsdk.ErrTenantExists
		}
		return nil, err
	}
	// qrdbfirstboot.Initialize ignores the returned tenant value (only
	// the error is checked) so we return a minimal TenantInfo carrying
	// the just-created identifiers.
	return &qrdbsdk.ServiceTenantInfo{ID: id, Name: displayName}, nil
}

// =============================================================================
// Role adapter
// =============================================================================

// RoleAdapter exposes quicbaas IdentityStore role operations as a qrdb
// RoleCreator and role name resolver. Use WrapRole to construct.
type RoleAdapter struct {
	store *iam.IdentityStore
}

// WrapRole adapts a quicbaas IdentityStore to the qrdb RoleCreator
// surface qrdbfirstboot.Initialize consumes.
func WrapRole(store *iam.IdentityStore) *RoleAdapter { return &RoleAdapter{store: store} }

// Compile-time assertion: the role adapter answers "which role is named N in
// tenant T", so it can be wired as qrdbfirstboot's Dependencies.RoleResolver.
// Without a resolver, first-boot re-run against an already-seeded store cannot
// tell which identifier the system roles are persisted under and refuses to
// guess (UnresolvableSystemRoleError).
var (
	_ qrdbsdk.RoleNameResolver     = (*RoleAdapter)(nil)
	_ qrdbsdk.FirstBootRoleCreator = (*RoleAdapter)(nil)
)

// GetRoleByName satisfies qrdb's role.NameResolver: it answers which role
// carries name in tenantID, reading the store's name index rather than paging
// a listing.
//
// The absence of a role is reported as qrdb's own *RoleNotFoundError, and that
// exact type is load-bearing: ResolveSystemRoleID matches it with errors.As to
// decide "the cluster holds no role of this name yet, use the derived
// identifier", and treats every OTHER error as a failure to resolve. A
// broker-shaped not-found would therefore be read as an outage, and any error
// reported as an absence would bind a grant to an identifier the store may not
// use.
func (a *RoleAdapter) GetRoleByName(ctx context.Context, tenantID, name string) (*qrdbsdk.Role, error) {
	if a == nil || a.store == nil {
		return nil, &NilDependencyError{Name: "IdentityStore"}
	}
	role, err := a.store.GetRoleByName(ctx, tenantID, name)
	if err != nil {
		if iam.IsRoleNotFound(err) {
			return nil, &qrdbsdk.RoleNotFoundError{ID: name}
		}
		return nil, err
	}
	core := FromIAMRole(role)
	// A built-in role name resolves as System so first-boot's resolver can
	// tell a canonical role from a tenant-defined one of the same name
	// (mirrors qrdbsdk's own projection — go-qrdb firstboot).
	core.System = qrdbsdk.IsSystemRoleName(core.Name)
	return &core, nil
}

// CreateRole satisfies qrdbfirstboot.RoleCreator. Permissions are
// converted from qrdb's strongly-typed qrdbsdk.Permission slice to
// quicbaas's string-typed permission list. Returns
// qrdb.role.RoleNameExistsError when the underlying store reports a
// duplicate so qrdbfirstboot's idempotent re-run path triggers.
func (a *RoleAdapter) CreateRole(ctx context.Context, req *qrdbsdk.CreateRoleRequest) (*qrdbsdk.Role, error) {
	if a == nil || a.store == nil {
		return nil, &NilDependencyError{Name: "IdentityStore"}
	}
	if req == nil {
		return nil, &NilDependencyError{Name: "CreateRoleRequest"}
	}
	opts := iam.CreateRoleOptions{
		ID:          req.ID,
		Name:        req.Name,
		Permissions: convertPermissions(req.Permissions),
	}
	_, err := a.store.CreateRole(ctx, req.TenantID, opts)
	if err != nil {
		if isAlreadyExists(err) {
			return nil, &qrdbsdk.RoleNameExistsError{Name: req.Name, TenantID: req.TenantID}
		}
		return nil, err
	}
	return &qrdbsdk.Role{
		ID:          req.ID,
		Name:        req.Name,
		TenantID:    req.TenantID,
		Permissions: req.Permissions,
		System:      true,
	}, nil
}

// Compile-time assertion: the role adapter is the seam iam.CreateTenant seeds
// a new tenant's built-in roles through.
var _ iam.TenantRoleSeeder = (*RoleAdapter)(nil)

// SeedBuiltInRoles materializes qrdb's canonical built-in roles inside
// tenantID, mirroring what qrdbfirstboot.Initialize does for the system
// tenant — same qrdbsdk.BuiltInRoles projection, same CreateRole path, same
// deterministic role IDs — so a tenant created at runtime is seeded exactly
// as the one created at bootstrap.
//
// Permission sets are read from the registry at call time, not at package
// init, so every permission a higher layer contributed (pkg/permission's
// Register, called from server.NewServer) is present in the seeded roles.
//
// A role that already exists is skipped: seeding is idempotent, so a re-run
// over a tenant seeded by an earlier version converges instead of failing.
// qrdb's Owner role is deliberately not seeded here — it is the non-strict
// bootstrap admin's convenience superuser, seeded only by first-boot.
//
// The SYSTEM tenant is excluded, and the exclusion is load-bearing rather than
// a tidy-up. first-boot creates the system tenant through this very manager
// and only then registers the built-in roles itself, using whether they
// already existed to decide how to resolve the admin's role ID: roles it
// seeded in that run are addressable by their derived identifier, roles it
// found are not, and with no role.NameResolver wired it refuses to guess
// (UnresolvableSystemRoleError). Seeding the system tenant here would make
// first-boot find its own roles preexisting and fail the platform admin's
// creation — bootstrap would break on a fresh install. First-boot owns the
// system tenant's roles; this seam owns every tenant created after it.
func (a *RoleAdapter) SeedBuiltInRoles(ctx context.Context, tenantID string) error {
	if a == nil || a.store == nil {
		return &NilDependencyError{Name: "IdentityStore"}
	}
	if tenantID == "" || tenantID == qrdbsdk.SystemTenantID {
		return nil
	}
	// Roles written by THIS run, so a failure part-way can undo them. qrdb's
	// tenant deletion drops tenant metadata and barrier state but does not
	// purge role records, and CreateTenant's rollback removes the tenant
	// rather than the roles — so without this, a seed that failed half-way
	// would leave exactly the orphans the name check below has to refuse.
	var created []string

	for _, r := range qrdbsdk.BuiltInRoles(tenantID) {
		_, err := a.CreateRole(ctx, &qrdbsdk.CreateRoleRequest{
			ID:          r.ID,
			Name:        r.Name,
			TenantID:    r.TenantID,
			Permissions: r.Permissions,
		})
		if err == nil {
			created = append(created, r.ID)
			continue
		}

		var nameExists *qrdbsdk.RoleNameExistsError
		if errors.As(err, &nameExists) {
			// A role of this name is already here. Idempotency means "the
			// canonical role is already present", NOT "something wearing the
			// name is present": qrdb does not purge roles when a tenant is
			// deleted, so recreating a tenant can meet a leftover `admin`
			// from its previous life — with a hand-edited permission set, or
			// an identifier this seeding never chose. Accepting it would hand
			// every future `--roles admin` user those stale grants under a
			// name that promises the canonical ones.
			//
			// So verify before trusting, and fail closed on a mismatch. An
			// operator resolving a genuine conflict is a deliberate act; a
			// silent one is how the wrong permissions become permanent.
			if verifyErr := a.verifyCanonicalRole(ctx, tenantID, r); verifyErr != nil {
				a.deleteRoles(ctx, tenantID, created)
				return verifyErr
			}
			continue
		}

		a.deleteRoles(ctx, tenantID, created)
		return &RoleSeedError{Role: r.Name, TenantID: tenantID, Cause: err}
	}
	return nil
}

// verifyCanonicalRole reports whether the role already carrying want's name in
// tenantID IS want — same deterministic identifier, same permission set.
//
// It asks the store by NAME. The earlier form listed the tenant's roles and
// scanned the page, which the qrdb-backed store caps at its default page size:
// a tenant holding more roles than one page could report the name as taken and
// then not show the role, sending this into the "reported as existing but not
// listed" refusal for a role that is present and canonical.
func (a *RoleAdapter) verifyCanonicalRole(ctx context.Context, tenantID string, want *qrdbsdk.Role) error {
	found, err := a.store.GetRoleByName(ctx, tenantID, want.Name)
	if err != nil {
		if iam.IsRoleNotFound(err) {
			// The store reported the name as taken and then did not resolve
			// it. That is a store inconsistency, not an idempotent re-run, and
			// it must not be read as "the canonical role is present".
			return &RoleConflictError{Role: want.Name, TenantID: tenantID, Reason: "reported as existing but not resolvable by name"}
		}
		return &RoleSeedError{Role: want.Name, TenantID: tenantID, Cause: err}
	}
	if found == nil {
		return &RoleConflictError{Role: want.Name, TenantID: tenantID, Reason: "reported as existing but not resolvable by name"}
	}
	if found.ID != want.ID {
		return &RoleConflictError{
			Role:     want.Name,
			TenantID: tenantID,
			Reason:   "existing role has id " + found.ID + ", not the canonical " + want.ID,
		}
	}
	if !samePermissions(found.Permissions, convertPermissions(want.Permissions)) {
		return &RoleConflictError{
			Role:     want.Name,
			TenantID: tenantID,
			Reason:   "existing role's permissions differ from the canonical set",
		}
	}
	return nil
}

// deleteRoles removes roles this seeding run created, best-effort, so a failed
// seed does not leave partial state behind for the next attempt to trip over.
// Errors are deliberately dropped: the caller is already failing, and a
// cleanup failure must not mask the reason.
func (a *RoleAdapter) deleteRoles(ctx context.Context, tenantID string, ids []string) {
	for _, id := range ids {
		_ = a.store.DeleteRole(ctx, tenantID, id)
	}
}

// samePermissions reports whether two permission lists hold the same set,
// independent of order and duplicates.
//
// Both sides are reduced to sets first. Consuming one side against the other
// as it walks would make a repeated entry on either side read as a mismatch,
// which would refuse a role whose grants are in fact canonical — a false
// conflict that no operator could resolve, since the two sets ARE equal.
func samePermissions(a, b []string) bool {
	setOf := func(in []string) map[string]struct{} {
		out := make(map[string]struct{}, len(in))
		for _, p := range in {
			out[p] = struct{}{}
		}
		return out
	}

	left, right := setOf(a), setOf(b)
	if len(left) != len(right) {
		return false
	}
	for p := range left {
		if _, ok := right[p]; !ok {
			return false
		}
	}
	return true
}

// =============================================================================
// User adapter
// =============================================================================

// UserAdapter exposes quicbaas IdentityStore user operations as a qrdb
// UserCreator AND as the admin-mTLS CertBinder seam. Use WrapUser to construct.
//
// The two roles belong on one type because the binder must address the very
// user the creator just wrote: qrdb's CertBinder is handed qrdb's numeric user
// id, while quicbaas's IdentityStore is keyed by its own string UUID. The
// adapter remembers the record it created so the binding lands on the right
// user without a second, lossy id translation.
type UserAdapter struct {
	store *iam.IdentityStore

	// created is the user CreateUser most recently wrote. firstboot.Initialize
	// calls Binder.BindAdminCert only in the same, single-threaded run that
	// created the admin, so a plain field is the whole correlation needed.
	created *iam.User
}

// Compile-time proof that the adapter satisfies both first-boot seams.
var (
	_ qrdbsdk.FirstBootUserCreator = (*UserAdapter)(nil)
	_ qrdbsdk.CertBinder           = (*UserAdapter)(nil)
)

// WrapUser adapts a quicbaas IdentityStore to the qrdb UserCreator and
// CertBinder surfaces qrdbfirstboot.Initialize consumes.
func WrapUser(store *iam.IdentityStore) *UserAdapter { return &UserAdapter{store: store} }

// CreateUser satisfies qrdbfirstboot.UserCreator. Synthesizes the email
// quicbaas's validator requires (qrdb's CreateUserRequest has no Email
// field) by combining the username and tenant id; the synthesized
// address is stable across runs so idempotent re-creates land on the
// same record. Returns qrdb.user.UsernameExistsError when the
// underlying store reports a duplicate so qrdbfirstboot's idempotent
// re-run path triggers.
//
// Identifier ownership: qrdb's User.ID is a server-assigned uint64
// (qrdb's Snowflake generator) and is NOT caller-supplied — CreateUser
// ignores any ID on the request. quicbaas keeps its own string UUID
// (iam.User.ID) as the authoritative identity for its OIDC / PEP /
// session / SPKI-binding layers, so the adapter does NOT cram the
// quicbaas string id into qrdb's numeric ID field. The returned
// qrdbsdk.User therefore carries no ID (zero value). quicbaas wires this
// same adapter as qrdbfirstboot's CertBinder, and BindAdminCert addresses
// the admin through the record remembered here rather than through qrdb's
// numeric id. The stable cross-store correlation key is the username, which
// is preserved verbatim on both sides.
func (a *UserAdapter) CreateUser(ctx context.Context, req *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	if a == nil || a.store == nil {
		return nil, &NilDependencyError{Name: "IdentityStore"}
	}
	if req == nil {
		return nil, &NilDependencyError{Name: "CreateUserRequest"}
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = qrdbsdk.SystemTenantID
	}
	// State is stated rather than defaulted. The default describes an account
	// awaiting an invite, and the bootstrap admin is not one: enrollment here
	// is CERT-BASED — first-boot issues the certificate and binds its SPKI
	// thumbprint, and there is no invite token in the flow at all. Leaving the
	// default would mark the one account that is already enrolled as pending
	// activation.
	opts := iam.CreateUserOptions{
		Username:  req.Username,
		Email:     syntheticEmail(req.Username, tenantID),
		Name:      req.Username,
		Roles:     req.RoleIDs,
		Lifecycle: iam.LifecycleActive,
	}
	u, err := a.store.CreateUser(ctx, tenantID, opts)
	if err != nil {
		// isAlreadyExists alone. It matches the ErrAlreadyExists CODE, and
		// iam.ErrEmailAlreadyExists — the duplicate-synthetic-email answer this
		// path also has to treat as "already exists" — carries that code, so it
		// is already covered. The second predicate this line used to call read
		// as a narrower sentinel check but was errors.Is against a QBError,
		// which compares codes: it matched exactly the same set and could never
		// add a case (go-quicbaas#434).
		if isAlreadyExists(err) {
			return nil, &qrdbsdk.UsernameExistsError{Username: req.Username, TenantID: tenantID}
		}
		return nil, err
	}
	a.created = u
	return &qrdbsdk.User{
		Username: u.Username,
		TenantID: u.TenantID,
		RoleIDs:  u.Roles,
		Enabled:  true,
	}, nil
}

// CreatedUser returns the quicbaas user record CreateUser most recently wrote,
// or nil when this adapter has not created one. First-boot callers use it to
// reach the admin they just provisioned without re-querying the store.
func (a *UserAdapter) CreatedUser() *iam.User {
	if a == nil {
		return nil
	}
	return a.created
}

// BindAdminCert satisfies qrdbfirstboot.CertBinder: it pins the admin
// certificate's canonical SPKI thumbprint (base64url SHA-256 over
// SubjectPublicKeyInfo, computed by firstboot via qrdb's
// user.ComputeSPKIThumbprint) onto the admin user, so a subsequent mTLS
// handshake presenting that certificate authenticates as the admin.
//
// The qrdbUserID argument is qrdb's server-assigned numeric id. quicbaas's
// UserAdapter.CreateUser deliberately does not surface that id (see its doc
// comment) — the authoritative handle on this side is the string UUID of the
// record this adapter just created, which is what the binding is written
// against.
func (a *UserAdapter) BindAdminCert(ctx context.Context, tenantID string, qrdbUserID uint64, thumbprint string) error {
	if a == nil || a.store == nil {
		return &NilDependencyError{Name: "IdentityStore"}
	}
	if a.created == nil {
		return &AdminBindTargetError{TenantID: tenantID, QRDBUserID: qrdbUserID}
	}
	if thumbprint == "" {
		return &AdminBindThumbprintError{TenantID: tenantID, UserID: a.created.ID}
	}
	if tenantID == "" {
		tenantID = a.created.TenantID
	}
	updated, err := a.store.UpdateUser(ctx, tenantID, a.created.ID, iam.UpdateUserOptions{
		SPKICertThumbprint: &thumbprint,
	})
	if err != nil {
		return err
	}
	a.created = updated
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// AdminBindTargetError reports that BindAdminCert was called on an adapter
// that never created a user, so there is no admin record to bind the
// certificate to.
type AdminBindTargetError struct {
	// TenantID is the tenant first-boot asked to bind within.
	TenantID string
	// QRDBUserID is qrdb's numeric id for the admin it created.
	QRDBUserID uint64
}

// Error implements the error interface.
func (e *AdminBindTargetError) Error() string {
	return "qrdbadapter: cannot bind admin certificate in tenant " + e.TenantID +
		": this adapter created no user"
}

// AdminBindThumbprintError reports that BindAdminCert was handed an empty SPKI
// thumbprint. Binding an empty value would silently clear the admin's mTLS
// identity, so the seam fails closed instead.
type AdminBindThumbprintError struct {
	// TenantID owns the admin user.
	TenantID string
	// UserID is the quicbaas string id of the admin user.
	UserID string
}

// Error implements the error interface.
func (e *AdminBindThumbprintError) Error() string {
	return "qrdbadapter: refusing to bind an empty SPKI thumbprint to admin user " +
		e.UserID + " in tenant " + e.TenantID
}

// NilDependencyError reports that a required adapter dependency (the wrapped
// manager/store, or a request argument) was nil. Name identifies the missing
// dependency.
type NilDependencyError struct {
	Name string
}

// Error implements the error interface.
func (e *NilDependencyError) Error() string {
	return "qrdbadapter: nil " + e.Name
}

// RoleConflictError reports that a role already carrying a built-in name in
// this tenant is NOT the canonical one — a different identifier, or a
// different permission set.
//
// Seeding fails closed on it rather than adopting the impostor: the name
// promises qrdb's canonical grants, and a role that merely wears the name
// would confer whatever it happens to hold to every user granted it.
type RoleConflictError struct {
	Role     string
	TenantID string
	Reason   string
}

// Error implements the error interface.
func (e *RoleConflictError) Error() string {
	return "qrdbadapter: role " + e.Role + " in tenant " + e.TenantID +
		" is not the canonical built-in: " + e.Reason
}

// RoleSeedError reports which built-in role failed to seed, and in which
// tenant. Naming the role matters: seeding walks the whole built-in set, so
// the bare cause alone leaves the failing role ambiguous.
type RoleSeedError struct {
	Role     string
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *RoleSeedError) Error() string {
	return "qrdbadapter: seed built-in role " + e.Role + " in tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RoleSeedError) Unwrap() error {
	return e.Cause
}

// isAlreadyExists reports whether err is (or wraps) a quicbaas
// ErrAlreadyExists code. Used to translate quicbaas duplicate errors
// into the specific qrdb sentinel types qrdbfirstboot checks via
// errors.Is / errors.As.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var qb *dberrors.QBError
	if errors.As(err, &qb) {
		return qb.Code == dberrors.ErrAlreadyExists
	}
	return false
}

// syntheticEmail returns "<username>@<tenant>.qrdb.local" — a
// deterministic synthetic email so quicbaas's email-format validator
// accepts the bootstrap admin entry. Stable across runs so idempotent
// re-creates land on the same record.
func syntheticEmail(username, tenantID string) string {
	return username + "@" + tenantID + ".qrdb.local"
}

// convertPermissions maps qrdb qrdbsdk.Permission slices to the
// string slice quicbaas's CreateRoleOptions expects. qrdb's
// qrdbsdk.Permission satisfies fmt.Stringer; we marshal via its
// String() method so verb-resource pairs round-trip cleanly.
func convertPermissions(in []qrdbsdk.Permission) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = p.String()
	}
	return out
}
