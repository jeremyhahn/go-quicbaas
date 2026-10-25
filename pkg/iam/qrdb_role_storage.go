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
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// RoleClientResolver returns the qrdb RoleClient that owns the canonical role
// store. QuicBaaS routes every role mutation through qrdb so the broker and the
// qrdb policy decision point (PDP) share ONE role ID space — a role created by
// the broker is the same object the PDP resolves to permissions. The resolver
// indirection lets the wiring layer (server.iam_init) hand the platform-shard
// qrdb client to the IdentityStore without the iam package taking a dependency
// on the full QRDBClientProvider surface.
type RoleClientResolver interface {
	// RoleClient returns the qrdb RoleClient for the platform (IAM) shard.
	RoleClient(ctx context.Context) (qrdbtransport.RoleClient, error)
}

// QRDBRoleStorage is a RoleStorage that delegates every operation to qrdb's
// canonical RoleServicer (reached via the SDK RoleClient). It is the seam that
// makes "quicbaas roles ARE qrdb roles": the broker's string role ID is passed
// straight through to qrdb on create (the deterministic-ID SDK seam), so a
// later GetRole/UpdateRole/DeleteRole by that SAME id resolves the same record,
// and the PDP — which reads roles from the same qrdb store — recognizes the
// role names a token carries.
//
// The decorator preserves the RoleStorage contract exactly (no public-signature
// churn): IdentityStore continues to call CreateRole/GetRole/UpdateRole/
// DeleteRole/ListRoles unchanged. The qrdb <-> broker model translation reuses
// the C1-B converters in pkg/iam/qrdbadapter (ToIAMRole/FromIAMRole) — there is
// no second conversion path.
type QRDBRoleStorage struct {
	resolver RoleClientResolver
}

// Compile-time interface check: QRDBRoleStorage is a RoleStorage.
var _ RoleStorage = (*QRDBRoleStorage)(nil)

// Compile-time interface check: QRDBRoleStorage reconciles built-in roles.
// qrdb owns the built-in role definitions, so this is the one role storage in
// the process that can honour the contract.
var _ BuiltInRoleReconciler = (*QRDBRoleStorage)(nil)

// NewQRDBRoleStorage builds a qrdb-backed RoleStorage. The resolver must be
// non-nil; a nil resolver yields a storage that fails every call with a typed
// configuration error rather than panicking.
func NewQRDBRoleStorage(resolver RoleClientResolver) *QRDBRoleStorage {
	return &QRDBRoleStorage{resolver: resolver}
}

// client resolves the qrdb RoleClient or returns a typed configuration error.
func (s *QRDBRoleStorage) client(ctx context.Context) (qrdbtransport.RoleClient, error) {
	if s == nil || s.resolver == nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrConfiguration,
			Op:   "iam.role.qrdb",
			Err:  &IAMMessageError{Message: "qrdb role client resolver not configured"},
		}
	}
	rc, err := s.resolver.RoleClient(ctx)
	if err != nil {
		return nil, mapQRDBRoleError("resolve", err)
	}
	if rc == nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrConfiguration,
			Op:   "iam.role.qrdb",
			Err:  &IAMMessageError{Message: "qrdb role client unavailable"},
		}
	}
	return rc, nil
}

// CreateRole stores a new role in qrdb under the broker-supplied ID. Passing the
// ID through is what keeps the two stores on a single ID space; qrdb honours a
// non-empty CreateRoleRequest.ID instead of minting its own UUID.
func (s *QRDBRoleStorage) CreateRole(ctx context.Context, r *Role) error {
	rc, err := s.client(ctx)
	if err != nil {
		return err
	}
	if _, err := rc.CreateRole(ctx, &qrdbtransport.CreateRoleRequest{
		ID:          r.ID,
		Name:        r.Name,
		TenantID:    r.TenantID,
		Permissions: r.Permissions,
	}); err != nil {
		return mapQRDBRoleError("create", err)
	}
	return nil
}

// GetRole retrieves a role by its globally unique ID from qrdb and projects it
// back into the broker model via the C1-B converter.
func (s *QRDBRoleStorage) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	rc, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	tr, err := rc.GetRole(ctx, roleID)
	if err != nil {
		return nil, mapQRDBRoleError("get", err)
	}
	return transportRoleToIAM(tr), nil
}

// GetRoleByName retrieves a role by its tenant-scoped name from qrdb's OWN name
// index, which is the whole point of routing the lookup here: qrdb answers it
// with an index read, so it neither pages nor scans, and the answer is the
// canonical store's — the same record the policy decision point resolves.
//
// An absent role arrives as qrdb's RoleNotFoundError and leaves as
// ErrRoleNotFound (mapQRDBRoleError), so a caller branching on
// IsRoleNotFound tells "no such role" apart from "could not ask".
func (s *QRDBRoleStorage) GetRoleByName(ctx context.Context, tenantID, name string) (*Role, error) {
	rc, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	tr, err := rc.GetRoleByName(ctx, &qrdbtransport.GetRoleByNameRequest{
		TenantID: tenantID,
		Name:     name,
	})
	if err != nil {
		return nil, mapQRDBRoleError("get_by_name", err)
	}
	if tr == nil {
		// Neither a role nor an error breaks the RoleServicer contract.
		// Refusing it beats dereferencing nil, and beats reading it as an
		// absence: qrdb's ResolveSystemRoleID falls back to a DERIVED
		// identifier on not-found, so a silent nil here would bind a grant to
		// an identifier that may name no role at all.
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.role.qrdb.get_by_name",
			Err:  &IAMMessageError{Message: "qrdb returned no role and no error for name " + name},
		}
	}
	return transportRoleToIAM(tr), nil
}

// UpdateRole applies a full replacement of name + permissions to the qrdb role
// identified by r.ID. qrdb's UpdateRole is partial (empty fields are skipped),
// but the IdentityStore always passes the fully-merged role, so sending both
// fields reproduces the broker's replace semantics.
func (s *QRDBRoleStorage) UpdateRole(ctx context.Context, r *Role) error {
	rc, err := s.client(ctx)
	if err != nil {
		return err
	}
	if _, err := rc.UpdateRole(ctx, &qrdbtransport.UpdateRoleRequest{
		ID:          r.ID,
		Name:        r.Name,
		Permissions: r.Permissions,
	}); err != nil {
		return mapQRDBRoleError("update", err)
	}
	return nil
}

// DeleteRole removes the qrdb role by ID. qrdb rejects deletion of System
// (built-in) roles; that rejection is surfaced verbatim as a typed error.
func (s *QRDBRoleStorage) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	rc, err := s.client(ctx)
	if err != nil {
		return err
	}
	if err := rc.DeleteRole(ctx, roleID); err != nil {
		return mapQRDBRoleError("delete", err)
	}
	return nil
}

// ListRoles returns the qrdb roles for a tenant, projected into the broker
// model. qrdb lists by page/pageSize, so OffsetWindow reads the page or two
// that hold the broker's exact offset/limit window.
func (s *QRDBRoleStorage) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	rc, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	transportRoles, err := OffsetWindow(opts.Offset, opts.Limit, func(page, size int) ([]*qrdbtransport.Role, error) {
		resp, err := rc.ListRoles(ctx, &qrdbtransport.ListRolesRequest{
			TenantID: tenantID,
			Page:     page,
			PageSize: size,
		})
		if err != nil {
			return nil, mapQRDBRoleError("list", err)
		}
		return resp.Roles, nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]*Role, 0, len(transportRoles))
	for _, tr := range transportRoles {
		out = append(out, transportRoleToIAM(tr))
	}
	return out, nil
}

// ReconcileBuiltInRoles converges tenantID's stored built-in roles onto the
// definitions the running build carries, through qrdb's reconcile seam.
//
// This is the ONLY write path that may re-permission a built-in role: qrdb
// refuses UpdateRole on a system role by design (SystemRoleImmutableError, and
// mapQRDBRoleError keeps surfacing that refusal to every hand edit), so a
// tenant seeded before a permission rename could otherwise never gain the new
// verbs. Reconciliation is the one caller whose target is not arbitrary — it
// IS the build's built-in definition — which is why qrdb exempts it and
// nothing else.
//
// qrdb resolves those definitions through the permission registry at call
// time, so a caller that contributes its own permissions must have registered
// them first or every tenant converges onto a set missing its verbs.
func (s *QRDBRoleStorage) ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*RoleReconcileReport, error) {
	rc, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := rc.ReconcileBuiltInRoles(ctx, &qrdbtransport.ReconcileRolesRequest{TenantID: tenantID})
	if err != nil {
		return nil, mapQRDBRoleError("reconcile", err)
	}
	return transportReconcileToIAM(tenantID, resp), nil
}

// transportReconcileToIAM projects qrdb's reconcile response onto the broker
// report. The response's TenantID is preferred when set; the requested tenant
// is the fallback, so a report always names the tenant it describes even when
// the caller addressed its own effective tenant with an empty string.
func transportReconcileToIAM(tenantID string, resp *qrdbtransport.ReconcileRolesResponse) *RoleReconcileReport {
	report := &RoleReconcileReport{TenantID: tenantID}
	if resp == nil {
		return report
	}
	if resp.TenantID != "" {
		report.TenantID = resp.TenantID
	}
	report.Roles = make([]RoleReconcileResult, 0, len(resp.Roles))
	for _, r := range resp.Roles {
		report.Roles = append(report.Roles, RoleReconcileResult{
			RoleID:  r.RoleID,
			Name:    r.Name,
			Created: r.Created,
			Added:   append([]string(nil), r.Added...),
			Removed: append([]string(nil), r.Removed...),
		})
	}
	return report
}

// transportRoleToIAM projects a qrdb transport Role onto the broker Role. The
// transport Role already carries string permissions (qrdb stringifies its typed
// permissions at the transport boundary), so it maps directly onto the broker
// model. Timestamps are left zero here: the transport encodes them as RFC3339
// strings and the broker callers that surface role timestamps re-read them from
// the role record, not from the storage projection.
func transportRoleToIAM(tr *qrdbtransport.Role) *Role {
	if tr == nil {
		return nil
	}
	perms := make([]string, len(tr.Permissions))
	copy(perms, tr.Permissions)
	// RetiredPermissions is derived by qrdb on every read from the live
	// permission registry and never stored, so carrying it through is what
	// puts the drift in front of an operator on an ordinary listing instead
	// of only after a reconcile run.
	retired := make([]string, len(tr.RetiredPermissions))
	copy(retired, tr.RetiredPermissions)
	return &Role{
		ID:                 tr.ID,
		Name:               tr.Name,
		TenantID:           tr.TenantID,
		Permissions:        perms,
		RetiredPermissions: retired,
	}
}

// mapQRDBRoleError translates qrdb's role errors into the broker's typed IAM
// sentinels so callers that branch on errors.Is(err, ErrRoleNotFound) /
// ErrRoleAlreadyExists keep working unchanged across the storage swap.
// Validation failures raised by qrdb (an unregistered permission or a
// malformed role request) are client-input errors and are classified as
// ErrInvalidInput so the transports surface 400 / InvalidArgument instead of
// a 500. Unknown errors are wrapped (never swallowed) with the originating op.
func mapQRDBRoleError(op string, err error) error {
	if err == nil {
		return nil
	}
	var notFound *qrdbsdk.RoleNotFoundError
	if errors.As(err, &notFound) {
		return ErrRoleNotFound
	}
	var nameExists *qrdbsdk.RoleNameExistsError
	if errors.As(err, &nameExists) {
		return ErrRoleAlreadyExists
	}
	// A caller-supplied role id that is already taken. Without this branch the
	// conflict fell through to ErrInternal and every transport reported a
	// server fault for an ordinary client error (#287) — the same defect the
	// user store fixed for duplicate emails.
	var idExists *qrdbsdk.RoleIDExistsError
	if errors.As(err, &idExists) {
		return ErrRoleIDAlreadyExists
	}
	// Renaming, re-permissioning or deleting a built-in role. Refused for
	// everyone, always, so it is forbidden rather than a conflict to resolve.
	var systemImmutable *qrdbsdk.SystemRoleImmutableError
	if errors.As(err, &systemImmutable) {
		return ErrSystemRoleImmutable
	}
	// A create that would take a system role's name. Refused by policy rather
	// than by an existing row, so it is invalid input rather than a conflict.
	var reservedName *qrdbsdk.ReservedRoleNameError
	if errors.As(err, &reservedName) {
		return ErrReservedRoleName
	}
	var invalidPerm *qrdbsdk.RoleInvalidPermissionError
	if errors.As(err, &invalidPerm) {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.role.qrdb." + op,
			Err:  err,
		}
	}
	var validation *qrdbsdk.RoleValidationError
	if errors.As(err, &validation) {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.role.qrdb." + op,
			Err:  err,
		}
	}
	// A caller without the permission, e.g. a tenant admin running the
	// built-in role reconcile: an ordinary refusal, not a server fault.
	var permissionDenied *qrdbsdk.PermissionDeniedError
	if errors.As(err, &permissionDenied) {
		return &dberrors.QBError{
			Code: dberrors.ErrForbidden,
			Op:   "iam.role.qrdb." + op,
			Err:  err,
		}
	}
	// A tenant-scoped caller naming another tenant's roles. Refused by
	// go-qrdb before any permission is resolved.
	var crossTenant *qrdbsdk.CrossTenantDeniedError
	if errors.As(err, &crossTenant) {
		return &dberrors.QBError{
			Code: dberrors.ErrForbidden,
			Op:   "iam.role.qrdb." + op,
			Err:  err,
		}
	}
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.role.qrdb." + op,
		Err:  err,
	}
}
