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

package qrdbadapter

import (
	"context"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// UserServicerResolver returns the in-process qrdb UserServicer that owns the
// canonical user record. QuicBaaS routes every user read and mutation through
// this servicer so the broker and the qrdb policy decision point share ONE user
// record: the user the broker creates is the same object login and the PDP
// resolve against.
//
// The resolver indirection lets the wiring layer (server.iam_init) hand the
// embedded node's UserServicer to the IdentityStore without the iam package
// taking a dependency on the full Server surface. It mirrors the
// RoleClientResolver shape used by QRDBRoleStorage.
type UserServicerResolver interface {
	// UserServicer returns the qrdb UserServicer for the embedded engine node.
	UserServicer(ctx context.Context) (qrdbsdk.UserServicer, error)
}

// QRDBUserStorage is a UserStorage backed entirely by qrdb's canonical user
// record. There is no side-record and no identifier correlation: iam.User is a
// projection of the qrdb user (see convert.go), and the external user id is the
// qrdb Snowflake in decimal.
//
// It keeps no index of its own. Every lookup, the PIV certificate thumbprint
// included, resolves through qrdb's canonical indexes, which the store writes
// in the same transaction as the record and re-checks against it on read.
type QRDBUserStorage struct {
	resolver UserServicerResolver
}

// Compile-time interface check: QRDBUserStorage is a UserStorage.
var _ iam.UserStorage = (*QRDBUserStorage)(nil)

// NewQRDBUserStorage builds a qrdb-backed UserStorage. A nil resolver yields a
// storage that fails every call with a typed configuration error rather than
// panicking.
func NewQRDBUserStorage(resolver UserServicerResolver) *QRDBUserStorage {
	return &QRDBUserStorage{resolver: resolver}
}

// servicer resolves the qrdb UserServicer or returns a typed configuration
// error. A nil servicer fails closed; production wiring never reaches login or
// user-mutation paths with a missing servicer silently.
func (s *QRDBUserStorage) servicer(ctx context.Context) (qrdbsdk.UserServicer, error) {
	if s == nil || s.resolver == nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrConfiguration,
			Op:   "iam.user.qrdb",
			Err:  &iam.IAMMessageError{Message: "qrdb user servicer resolver not configured"},
		}
	}
	svc, err := s.resolver.UserServicer(ctx)
	if err != nil {
		return nil, mapQRDBUserError("resolve", err)
	}
	if svc == nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrConfiguration,
			Op:   "iam.user.qrdb",
			Err:  &iam.IAMMessageError{Message: "qrdb user servicer unavailable"},
		}
	}
	return svc, nil
}

// CreateUser writes the qrdb user and stamps the assigned Snowflake back onto u
// as its external id.
//
// The Email is sent, and that is load-bearing rather than incidental: it is the
// canonical tenant-unique login key, and qrdb refuses a create that omits it
// unless the caller declares WithoutEmailLoginKey. A user without an email is a
// legitimate identity only when it authenticates by certificate — a fleet node
// enrolled by ensureFleetNodeCertUser, which carries its binding at creation —
// and never an accident, so the declaration is derived from the record itself
// rather than from a flag a caller might forget. The first-boot admin is not
// such a record: it is created with a synthetic email and bound afterwards. A
// record that declares no email must carry a certificate binding instead
// (iam.RequireLoginKey), or it would be an account nothing could ever log in as.
func (s *QRDBUserStorage) CreateUser(ctx context.Context, u *iam.User) error {
	if err := iam.RequireLoginKey(u); err != nil {
		return err
	}
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}
	core, err := FromIAMUser(u)
	if err != nil {
		return err
	}

	created, err := svc.CreateUser(ctx, &qrdbsdk.CreateUserRequest{
		Username:             core.Username,
		Email:                core.Email,
		WithoutEmailLoginKey: core.Email == "",
		Name:                 core.Name,
		TenantID:             core.TenantID,
		RoleIDs:              core.RoleIDs,
		AuthMethods:          core.AuthMethods,
		Lifecycle:            core.Lifecycle,
	})
	if err != nil {
		return mapQRDBUserError("create", err)
	}

	// Stamp the assigned id BEFORE anything else can fail: the record is already
	// persisted at this point, and the compensation below needs to name it.
	u.ID = FormatUserID(created.ID)

	// Everything past this point COMPENSATES on failure. qrdb has committed the
	// canonical record, so returning an error without undoing it would leave a
	// half-initialised account that the caller was told did not exist — and a
	// retry would then collide with its own email and username. The create has
	// to be all or nothing from outside.
	if err := s.finishCreate(ctx, svc, u, core, created.ID); err != nil {
		s.compensateCreate(ctx, svc, tenantOf(u), created.ID)
		u.ID = ""
		return err
	}
	return nil
}

// finishCreate applies everything CreateUserRequest could not carry.
func (s *QRDBUserStorage) finishCreate(
	ctx context.Context,
	svc qrdbsdk.UserServicer,
	u *iam.User,
	core qrdbsdk.User,
	id uint64,
) error {
	if core.Attributes != nil || len(core.CertBindings) > 0 {
		tctx := qrdbsdk.ContextWithTenant(ctx, u.TenantID)
		if _, err := svc.UpdateUser(tctx, &qrdbsdk.UpdateUserRequest{
			ID:           id,
			Attributes:   core.Attributes,
			CertBindings: core.CertBindings,
		}); err != nil {
			return mapQRDBUserError("create.update", err)
		}
	}
	return nil
}

// compensateCreate undoes a create that could not be finished.
//
// Its own errors are deliberately dropped: the caller is already being told the
// create failed, and a cleanup failure must not replace the reason it failed.
func (s *QRDBUserStorage) compensateCreate(
	ctx context.Context,
	svc qrdbsdk.UserServicer,
	tenantID string,
	id uint64,
) {
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	_ = svc.DeleteUser(tctx, id)
}

// tenantOf returns the tenant a user record belongs to.
func tenantOf(u *iam.User) string {
	if u == nil {
		return ""
	}
	return u.TenantID
}

// GetUser reads the qrdb user addressed by its Snowflake, routed on tenantID.
func (s *QRDBUserStorage) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	id, err := ParseUserID(userID)
	if err != nil {
		return nil, iam.ErrUserNotFound
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	core, err := svc.GetUser(tctx, id)
	if err != nil {
		return nil, mapQRDBUserError("get", err)
	}
	return ToIAMUser(*core), nil
}

// GetUserByEmail resolves a user through qrdb's canonical tenant-unique email
// index — the same index the login ceremony's own record lives in, which is what
// makes a verified factor and the record that says what it may do impossible to
// separate.
func (s *QRDBUserStorage) GetUserByEmail(ctx context.Context, tenantID, email string) (*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	normalized := qrdbsdk.NormalizeEmail(email)
	if normalized == "" || tenantID == "" {
		return nil, iam.ErrUserNotFound
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	core, err := svc.GetUserByEmail(tctx, tenantID, normalized)
	if err != nil {
		return nil, mapQRDBUserError("get_by_email", err)
	}
	return ToIAMUser(*core), nil
}

// GetUserByPIVThumbprint resolves a user by a PIV-attested cert binding. Both
// the SPKI thumbprint and the legacy hex digest over the certificate DER
// resolve through qrdb's tenant-scoped cert-thumbprint index
// (UserByCertThumbprint), so ctx must carry the tenant the lookup is made in.
func (s *QRDBUserStorage) GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*iam.User, error) {
	if thumbprint == "" {
		return nil, iam.ErrUserNotFound
	}
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	core, err := svc.UserByCertThumbprint(ctx, thumbprint)
	if err != nil {
		return nil, mapQRDBUserError("piv", err)
	}
	return ToIAMUser(*core), nil
}

// GetUserByUsername resolves a user by tenant-scoped username via qrdb's native
// lookup. It is the seam the RBAC AssignRole/RevokeRole redirect uses.
func (s *QRDBUserStorage) GetUserByUsername(ctx context.Context, tenantID, username string) (*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	core, err := svc.GetUserByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, mapQRDBUserError("get_by_username", err)
	}
	return ToIAMUser(*core), nil
}

// UpdateUser applies a full replacement of the qrdb user record.
//
// Lifecycle is sent as a TRANSITION, not a field write: qrdb checks the move
// against its transition table, so an illegal one (anything out of deleting,
// say) is refused by the store rather than quietly persisted. Setting the state
// the record already holds is a no-op, which is what makes an ordinary update
// that does not intend a state change safe to send unconditionally.
func (s *QRDBUserStorage) UpdateUser(ctx context.Context, u *iam.User) error {
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}
	core, err := FromIAMUser(u)
	if err != nil {
		return err
	}
	if core.ID == 0 {
		return iam.ErrUserNotFound
	}

	tctx := qrdbsdk.ContextWithTenant(ctx, u.TenantID)
	if _, err := svc.UpdateUser(tctx, &qrdbsdk.UpdateUserRequest{
		ID:           core.ID,
		Username:     core.Username,
		Email:        core.Email,
		Name:         core.Name,
		RoleIDs:      orEmptySlice(core.RoleIDs),
		AuthMethods:  orEmptySlice(core.AuthMethods),
		Attributes:   orEmptyMap(core.Attributes),
		CertBindings: orEmptyCertBindings(core.CertBindings),
		Lifecycle:    core.Lifecycle,
		LastAuth:     core.LastAuth,
	}); err != nil {
		return mapQRDBUserError("update", err)
	}
	return nil
}

// MarkUserDeleting claims the record for deletion.
//
// The request carries the lifecycle and NOTHING else. That is not tidiness: the
// store refuses any update to a terminal record except one that re-asserts the
// state it already holds, so a request carrying the rest of the user would be
// rejected the second time it ran — and the second time is precisely a retried
// deletion, the case the terminal state exists to make resumable.
func (s *QRDBUserStorage) MarkUserDeleting(ctx context.Context, tenantID, userID string) error {
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}
	id, err := ParseUserID(userID)
	if err != nil {
		return iam.ErrUserNotFound
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	if _, err := svc.UpdateUser(tctx, &qrdbsdk.UpdateUserRequest{
		ID:        id,
		Lifecycle: iam.LifecycleDeleting,
	}); err != nil {
		return mapQRDBUserError("mark_deleting", err)
	}
	return nil
}

// DeleteUser removes the qrdb user.
//
// It does NOT mark the record deleting itself: qrdb's own DeleteUser claims the
// record as its first act, and IdentityStore.DeleteUser has already claimed it
// before sweeping the credentials that outlive this call. A third marker here
// would be a redundant round trip that changes nothing.
func (s *QRDBUserStorage) DeleteUser(ctx context.Context, tenantID, userID string) error {
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}
	id, err := ParseUserID(userID)
	if err != nil {
		return iam.ErrUserNotFound
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	if err := svc.DeleteUser(tctx, id); err != nil {
		return mapQRDBUserError("delete", err)
	}
	return nil
}

// ListUsers returns the tenant's qrdb users. Every record projects, so there is
// no "skipped because it had no correlation entry" case: a user that exists in
// the tenant is a user this listing returns. qrdb lists by page/pageSize, so
// iam.OffsetWindow reads the page or two that hold the exact offset/limit
// window.
func (s *QRDBUserStorage) ListUsers(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	cores, err := iam.OffsetWindow(opts.Offset, opts.Limit, func(page, size int) ([]*qrdbsdk.User, error) {
		resp, err := svc.ListUsers(ctx, &qrdbsdk.ListUsersRequest{
			TenantID: tenantID,
			Page:     page,
			PageSize: size,
		})
		if err != nil {
			return nil, mapQRDBUserError("list", err)
		}
		return resp.Users, nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]*iam.User, 0, len(cores))
	for _, core := range cores {
		if core == nil {
			continue
		}
		out = append(out, ToIAMUser(*core))
	}
	return out, nil
}

// orEmptySlice forces a non-nil slice so qrdb's partial UpdateUser performs a
// full replacement (a nil slice is skipped by qrdb; an empty slice clears it).
func orEmptySlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// orEmptyMap forces a non-nil map for the same replace-semantics reason.
func orEmptyMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

// orEmptyCertBindings forces a non-nil slice for the same replace-semantics
// reason: an UpdateUser that clears all bindings must send an empty (not nil)
// slice so qrdb drops the secondary index.
func orEmptyCertBindings(in []qrdbsdk.CertBinding) []qrdbsdk.CertBinding {
	if in == nil {
		return []qrdbsdk.CertBinding{}
	}
	return in
}

// isQRDBUserNotFound reports whether err is qrdb's UserNotFoundError.
func isQRDBUserNotFound(err error) bool {
	var nf *qrdbsdk.UserNotFoundError
	return errors.As(err, &nf)
}

// mapQRDBUserError translates qrdb's user errors into the broker's typed IAM
// sentinels so callers that branch on errors.Is(err, ErrUserNotFound) /
// ErrAlreadyExists keep working unchanged. Unknown errors are wrapped (never
// swallowed) with the originating op.
func mapQRDBUserError(op string, err error) error {
	if err == nil {
		return nil
	}
	if isQRDBUserNotFound(err) {
		return iam.ErrUserNotFound
	}
	// The email index is tenant-unique in the STORE, so a duplicate can arrive
	// here even though IdentityStore checked first: two concurrent creates both
	// pass that check, qrdb accepts one and refuses the other. Without this the
	// loser is wrapped as an internal error and every transport reports a server
	// failure for what is an ordinary, expected conflict.
	var emailExists *qrdbsdk.EmailExistsError
	if errors.As(err, &emailExists) {
		return iam.ErrEmailAlreadyExists
	}
	var exists *qrdbsdk.UsernameExistsError
	if errors.As(err, &exists) {
		return iam.ErrAlreadyExists
	}
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.user.qrdb." + op,
		Err:  err,
	}
}
