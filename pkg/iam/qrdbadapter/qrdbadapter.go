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
// RoleCreator. Use WrapRole to construct.
type RoleAdapter struct {
	store *iam.IdentityStore
}

// WrapRole adapts a quicbaas IdentityStore to the qrdb RoleCreator
// surface qrdbfirstboot.Initialize consumes.
func WrapRole(store *iam.IdentityStore) *RoleAdapter { return &RoleAdapter{store: store} }

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

// GetRoleByName satisfies qrdbsdk.RoleNameResolver: it answers "which role is
// named N in tenant T" from the quicbaas role store so first-boot's grants
// bind to the identifier a system role is actually persisted under. On a
// re-run against a store seeded earlier — the retry after a partial first
// boot — the built-in roles already exist and first-boot refuses to derive
// their identifiers (UnresolvableSystemRoleError) unless it can resolve them
// by name here. A miss is qrdbsdk.RoleNotFoundError so first-boot's own
// not-found handling applies.
func (a *RoleAdapter) GetRoleByName(ctx context.Context, tenantID, name string) (*qrdbsdk.Role, error) {
	if a == nil || a.store == nil {
		return nil, &NilDependencyError{Name: "IdentityStore"}
	}
	// Limit 0 lists every role in the broker-backed stores and the first
	// default page (100) in the qrdb-backed one. Both cover this lookup: it
	// runs during first-boot against the system tenant, whose roles are the
	// built-in set seeded moments earlier.
	roles, err := a.store.ListRoles(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.Name != name {
			continue
		}
		return &qrdbsdk.Role{
			ID:          r.ID,
			Name:        r.Name,
			TenantID:    r.TenantID,
			Permissions: parsePermissions(r.Permissions),
			System:      qrdbsdk.IsSystemRoleName(r.Name),
		}, nil
	}
	return nil, &qrdbsdk.RoleNotFoundError{ID: tenantID + "/" + name}
}

// Compile-time proof that the adapter satisfies both first-boot role seams.
var (
	_ qrdbsdk.FirstBootRoleCreator = (*RoleAdapter)(nil)
	_ qrdbsdk.RoleNameResolver     = (*RoleAdapter)(nil)
)

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
	opts := iam.CreateUserOptions{
		Username: req.Username,
		Email:    syntheticEmail(req.Username, tenantID),
		Name:     req.Username,
		Roles:    req.RoleIDs,
	}
	u, err := a.store.CreateUser(ctx, tenantID, opts)
	if err != nil {
		if isAlreadyExists(err) || isEmailDuplicate(err) {
			return nil, &qrdbsdk.UsernameExistsError{Username: req.Username, TenantID: tenantID}
		}
		return nil, err
	}
	a.created = u
	return &qrdbsdk.User{
		Username: u.Name,
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

// isEmailDuplicate reports whether err is iam.ErrEmailAlreadyExists.
// CreateUser surfaces this when the synthesized email conflicts with
// an existing user — for the admin re-create path that's the
// "already exists" signal.
func isEmailDuplicate(err error) bool {
	return errors.Is(err, iam.ErrEmailAlreadyExists)
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
