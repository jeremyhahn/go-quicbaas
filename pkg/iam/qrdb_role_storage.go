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
// no second conversion path. Group persistence is intentionally NOT routed here
// (qrdb has no Group type); it stays on the broker's BackendStorage.
type QRDBRoleStorage struct {
	resolver RoleClientResolver
}

// Compile-time interface check: QRDBRoleStorage is a RoleStorage.
var _ RoleStorage = (*QRDBRoleStorage)(nil)

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
// model. Pagination is translated from the broker's offset/limit to qrdb's
// page/pageSize: the broker callers use Limit only (Offset is rare for roles),
// so a non-zero Limit maps to a single page of that size; an Offset advances
// the page accordingly when both are set.
func (s *QRDBRoleStorage) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	rc, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := rc.ListRoles(ctx, &qrdbtransport.ListRolesRequest{
		TenantID: tenantID,
		Page:     pageFromOffset(opts.Offset, opts.Limit),
		PageSize: opts.Limit,
	})
	if err != nil {
		return nil, mapQRDBRoleError("list", err)
	}
	out := make([]*Role, 0, len(resp.Roles))
	for _, tr := range resp.Roles {
		out = append(out, transportRoleToIAM(tr))
	}
	return out, nil
}

// pageFromOffset converts a (offset, limit) pair into qrdb's 1-based page
// number. With a zero limit there is no paging, so page 1 is returned; with a
// limit and an offset that is an exact multiple, the corresponding page is
// computed (offset/limit + 1).
func pageFromOffset(offset, limit int) int {
	if limit <= 0 || offset <= 0 {
		return 1
	}
	return offset/limit + 1
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
	return &Role{
		ID:          tr.ID,
		Name:        tr.Name,
		TenantID:    tr.TenantID,
		Permissions: perms,
	}
}

// mapQRDBRoleError translates qrdb's role errors into the broker's typed IAM
// sentinels so callers that branch on errors.Is(err, ErrRoleNotFound) /
// ErrRoleAlreadyExists keep working unchanged across the storage swap. Unknown
// errors are wrapped (never swallowed) with the originating op.
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
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.role.qrdb." + op,
		Err:  err,
	}
}
