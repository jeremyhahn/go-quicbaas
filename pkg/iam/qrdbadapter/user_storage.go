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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
// It keeps exactly ONE index of its own, and only because qrdb cannot serve it:
// the LEGACY PIV thumbprint (hex SHA-256 over the certificate DER) is not an
// SPKI thumbprint, so qrdb's UserByCertThumbprint index does not resolve it.
// Email is NOT indexed here — qrdb owns the canonical tenant-unique email index,
// and the broker-owned copy that used to shadow it is what a verified login
// could miss while the record it named existed all along.
type QRDBUserStorage struct {
	resolver UserServicerResolver
	kv       interfaces.KVStore
}

// Compile-time interface check: QRDBUserStorage is a UserStorage.
var _ iam.UserStorage = (*QRDBUserStorage)(nil)

// NewQRDBUserStorage builds a qrdb-backed UserStorage. A nil resolver yields a
// storage that fails every call with a typed configuration error rather than
// panicking. The kv backs the legacy-PIV index.
func NewQRDBUserStorage(resolver UserServicerResolver, kv interfaces.KVStore) *QRDBUserStorage {
	return &QRDBUserStorage{resolver: resolver, kv: kv}
}

// legacy-PIV index keys. They live under the canonical __iam: namespace so they
// are encrypted-at-rest in the owning tenant's barrier and a sealed tenant fails
// closed via the routing KVStore. Each thumbprint has a tenant-scoped key AND a
// global key: the global one serves a lookup made with no tenant in context. The
// index VALUE is "tenantID:userID" so a global hit still resolves the owning
// tenant for the follow-up read.
const keyPrefixUserLegacyPIVIndex = iam.KeyPrefixIAM + "qrdbuser:legacypiv:"

func userLegacyPIVIndexKey(tenantID, thumbprint string) string {
	return keyPrefixUserLegacyPIVIndex + tenantID + ":" + thumbprint
}

func userLegacyPIVGlobalKey(thumbprint string) string {
	return keyPrefixUserLegacyPIVIndex + "_global:" + thumbprint
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
// legitimate identity (a bootstrap admin, an mTLS-only service account) but never
// an accident, so the declaration is derived from the record itself rather than
// from a flag a caller might forget.
func (s *QRDBUserStorage) CreateUser(ctx context.Context, u *iam.User) error {
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
		s.compensateCreate(ctx, svc, tenantOf(u), created.ID, u)
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
	return s.writeLegacyPIVIndex(ctx, u)
}

// compensateCreate undoes a create that could not be finished.
//
// Its own errors are deliberately dropped: the caller is already being told the
// create failed, and a cleanup failure must not replace the reason it failed.
// The index sweep runs first because it is keyed by the record this is about to
// remove.
func (s *QRDBUserStorage) compensateCreate(
	ctx context.Context,
	svc qrdbsdk.UserServicer,
	tenantID string,
	id uint64,
	u *iam.User,
) {
	_ = s.deleteLegacyPIVIndex(ctx, tenantID, u)
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

// GetUserByPIVThumbprint resolves a user by a PIV-attested cert binding. It
// tries qrdb's SPKI-thumbprint index first (UserByCertThumbprint), then falls
// back to the broker-side legacy-PIV index. This mirrors the SPKI-primary +
// legacy-fallback resolution order of the login service.
func (s *QRDBUserStorage) GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	// The tenant (if any) is already bound under go-qrdb's key, which the
	// servicer reads directly.

	// SPKI first.
	if core, err := svc.UserByCertThumbprint(ctx, thumbprint); err == nil {
		return ToIAMUser(*core), nil
	} else if !isQRDBUserNotFound(err) {
		return nil, mapQRDBUserError("piv.spki", err)
	}

	// Legacy fallback: the hex-digest index qrdb's SPKI index cannot serve.
	owningTenant, userID, err := s.resolveLegacyPIV(ctx, thumbprint)
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, owningTenant, userID)
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

	// Read the prior record to reconcile the legacy-PIV index.
	// coded-sentinel-ok: the CLASS is the intent. Any not-found means there is
	// no prior record whose index entries need reconciling.
	prior, err := s.GetUser(ctx, u.TenantID, u.ID)
	if err != nil && !errors.Is(err, iam.ErrUserNotFound) {
		return err
	}

	// Drop stale index entries BEFORE the canonical write, not after.
	//
	// The two orderings fail in opposite directions and only one of them is
	// survivable. Writing first and reconciling after means a failed reconcile
	// returns an error with the binding already removed — and a retry re-reads
	// the record, no longer sees the old thumbprint, and never removes its index
	// entry, so a revoked certificate goes on resolving the user forever.
	// Removing first fails the other way: the entry is gone while the binding is
	// still valid, which costs a legacy-index lookup and nothing else. A
	// credential that stops resolving too early is recoverable; one that never
	// stops is not.
	if err := s.deleteStaleLegacyPIVIndex(ctx, prior, u); err != nil {
		return err
	}

	tctx := qrdbsdk.ContextWithTenant(ctx, u.TenantID)
	committed, err := svc.UpdateUser(tctx, &qrdbsdk.UpdateUserRequest{
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
	})
	if err != nil {
		return mapQRDBUserError("update", err)
	}

	// Publish from what the store COMMITTED, not from the caller's in-memory
	// copy. The two differ whenever another writer got there first, and an index
	// built from the caller's copy would then advertise a binding the canonical
	// record does not hold — a thumbprint resolving a user it is no longer bound
	// to.
	//
	// This does not make concurrent updates to one user's bindings safe: two
	// racing updates can still interleave so that the loser publishes last. That
	// needs arbitration on the canonical record and is filed separately; the
	// legacy index is a migration-window fallback for hex-digest thumbprints
	// qrdb's SPKI index cannot resolve, and its SPKI-primary sibling is not
	// affected.
	return s.writeLegacyPIVIndex(ctx, ToIAMUser(*committed))
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

// DeleteUser removes the qrdb user and the legacy-PIV index entries it owned.
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
	prior, err := s.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	// Sweep the index BEFORE removing the record, for the reason UpdateUser
	// gives: once the canonical record is gone a retry cannot re-read the
	// thumbprints it owned, so an entry that outlives this call outlives every
	// chance to remove it and goes on resolving a user that no longer exists.
	if err := s.deleteLegacyPIVIndex(ctx, tenantID, prior); err != nil {
		return err
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	if err := svc.DeleteUser(tctx, id); err != nil {
		return mapQRDBUserError("delete", err)
	}
	return nil
}

// ListUsers returns the tenant's qrdb users. Every record projects, so there is
// no "skipped because it had no correlation entry" case: a user that exists in
// the tenant is a user this listing returns.
func (s *QRDBUserStorage) ListUsers(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := svc.ListUsers(ctx, &qrdbsdk.ListUsersRequest{
		TenantID: tenantID,
		Page:     pageFromOffset(opts.Offset, opts.Limit),
		PageSize: opts.Limit,
	})
	if err != nil {
		return nil, mapQRDBUserError("list", err)
	}
	out := make([]*iam.User, 0, len(resp.Users))
	for _, core := range resp.Users {
		if core == nil {
			continue
		}
		out = append(out, ToIAMUser(*core))
	}
	return out, nil
}

// indexValue encodes the owning tenant and user id of an index entry so a global
// (un-scoped) hit still resolves the owning tenant for the follow-up read.
func indexValue(tenantID, userID string) []byte {
	return []byte(tenantID + ":" + userID)
}

// writeLegacyPIVIndex writes the legacy-PIV index entries for u under a
// tenant-scoped key AND a global key (both routed through the owning tenant's
// store) so a lookup with no tenant in context still resolves.
func (s *QRDBUserStorage) writeLegacyPIVIndex(ctx context.Context, u *iam.User) error {
	thumbprints := u.LegacyPIVThumbprints()
	if len(thumbprints) == 0 {
		return nil
	}
	tctx := tenantctx.WithTenant(ctx, u.TenantID)
	val := indexValue(u.TenantID, u.ID)
	for _, tp := range thumbprints {
		if err := s.kv.Put(tctx, userLegacyPIVIndexKey(u.TenantID, tp), val); err != nil {
			return err
		}
		if err := s.kv.Put(tctx, userLegacyPIVGlobalKey(tp), val); err != nil {
			return err
		}
	}
	return nil
}

// deleteStaleLegacyPIVIndex removes the index entries an update is about to
// invalidate — the thumbprints the prior record carried and the new one does
// not. It runs BEFORE the canonical write; see UpdateUser for why that ordering
// is the survivable one.
func (s *QRDBUserStorage) deleteStaleLegacyPIVIndex(ctx context.Context, prior *iam.User, u *iam.User) error {
	if prior == nil {
		return nil
	}
	tctx := tenantctx.WithTenant(ctx, u.TenantID)
	current := make(map[string]struct{})
	for _, tp := range u.LegacyPIVThumbprints() {
		current[tp] = struct{}{}
	}
	for _, tp := range legacyPIVThumbprintsFromBindings(prior.CertBindings) {
		if _, keep := current[tp]; keep {
			continue
		}
		if err := s.kv.Delete(tctx, userLegacyPIVIndexKey(u.TenantID, tp)); err != nil {
			return err
		}
		if err := s.kv.Delete(tctx, userLegacyPIVGlobalKey(tp)); err != nil {
			return err
		}
	}
	return nil
}

// deleteLegacyPIVIndex removes the index entries a deleted user owned.
func (s *QRDBUserStorage) deleteLegacyPIVIndex(ctx context.Context, tenantID string, prior *iam.User) error {
	if prior == nil {
		return nil
	}
	tctx := tenantctx.WithTenant(ctx, tenantID)
	for _, tp := range legacyPIVThumbprintsFromBindings(prior.CertBindings) {
		if err := s.kv.Delete(tctx, userLegacyPIVIndexKey(tenantID, tp)); err != nil {
			return err
		}
		if err := s.kv.Delete(tctx, userLegacyPIVGlobalKey(tp)); err != nil {
			return err
		}
	}
	return nil
}

// resolveLegacyPIV reads the legacy-PIV index, returning the owning tenant and
// user id. It tries the tenant-scoped key first when a tenant is in context,
// then falls back to the global key. A not-found maps to iam.ErrUserNotFound.
func (s *QRDBUserStorage) resolveLegacyPIV(ctx context.Context, thumbprint string) (string, string, error) {
	if tenantID, ok := tenantctx.Lookup(ctx); ok {
		owning, userID, ok, err := s.indexLookup(ctx, userLegacyPIVIndexKey(tenantID, thumbprint))
		if err != nil {
			return "", "", err
		}
		if ok {
			return owning, userID, nil
		}
	}
	owning, userID, ok, err := s.indexLookup(ctx, userLegacyPIVGlobalKey(thumbprint))
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", iam.ErrUserNotFound
	}
	return owning, userID, nil
}

// indexLookup reads a single index key and splits its "tenantID:userID" value.
// The ok return distinguishes a clean miss from an error so the caller can fall
// through to the global key.
func (s *QRDBUserStorage) indexLookup(ctx context.Context, key string) (tenantID, userID string, ok bool, err error) {
	data, gerr := s.kv.Get(ctx, key)
	if gerr != nil {
		if dberrors.IsNotFound(gerr) {
			return "", "", false, nil
		}
		return "", "", false, gerr
	}
	parts := splitIndexValue(string(data))
	if parts[0] == "" || parts[1] == "" {
		return "", "", false, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.user.qrdb.index",
			Err:  &iam.IAMMessageError{Message: "malformed broker index entry"},
		}
	}
	return parts[0], parts[1], true, nil
}

// splitIndexValue splits a "tenantID:userID" index value. A decimal user id
// never contains a colon, and the tenant id is the first segment, so splitting
// at the first colon is exact.
func splitIndexValue(v string) [2]string {
	for i := 0; i < len(v); i++ {
		if v[i] == ':' {
			return [2]string{v[:i], v[i+1:]}
		}
	}
	return [2]string{"", ""}
}

// pageFromOffset converts an (offset, limit) pair into qrdb's 1-based page
// number. With a zero limit there is no paging, so page 1 is returned; with a
// limit and a positive offset, the corresponding page is computed.
func pageFromOffset(offset, limit int) int {
	if limit <= 0 || offset <= 0 {
		return 1
	}
	return offset/limit + 1
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
