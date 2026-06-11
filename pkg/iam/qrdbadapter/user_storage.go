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
)

// UserServicerResolver returns the in-process qrdb UserServicer that owns the
// canonical user CORE (Username, TenantID, RoleIDs, RoleBindings, ABAC
// Attributes, CertBindings, Factors, Enabled, System, timestamps). QuicBaaS
// routes every user mutation through this servicer so the broker and the qrdb
// policy decision point share ONE user record: the user the broker creates is
// the same object login and the PDP resolve against.
//
// The resolver indirection lets the wiring layer (server.iam_init) hand the
// embedded node's UserServicer to the IdentityStore without the iam package
// taking a dependency on the full Server surface. It mirrors the
// RoleClientResolver shape used by QRDBRoleStorage.
type UserServicerResolver interface {
	// UserServicer returns the qrdb UserServicer for the embedded engine node.
	UserServicer(ctx context.Context) (qrdbsdk.UserServicer, error)
}

// QRDBUserStorage is a UserStorage that keeps qrdb as the canonical store for
// the user CORE while persisting the broker-only fields in a side-record.
//
// The composition is:
//   - qrdb UserServicer (via the resolver) owns the core. CreateUser/GetUser/
//     UpdateUser/DeleteUser/ListUsers/GetUserByUsername/UserByCertThumbprint
//     are routed to it. The conversion is strictly via FromIAMUser/ToIAMUser
//     (convert.go) so there is exactly one model-translation path.
//   - IDMap correlates the broker's external string UUID (iam.User.ID — the
//     value the OIDC subject, PEP, sessions and SPKI bindings key on) with
//     qrdb's internal uint64 id.
//   - BrokerProfileStore persists the broker-only fields (Email/Name/Groups/
//     AuthMethods/State/MFA/Invite/LastAuth) keyed by the qrdb uint64 id.
//   - A broker-side email index resolves GetUserByEmail (qrdb has no email
//     lookup).
//   - A broker-side legacy-PIV index resolves the legacy hex-digest PIV
//     thumbprint (which is NOT an SPKI thumbprint, so qrdb's
//     UserByCertThumbprint cannot resolve it directly) for no-qrdb-index
//     deployments and the migration window.
//
// It mirrors QRDBRoleStorage's resolver-indirection + typed-error-mapping
// shape and preserves the UserStorage contract exactly: the IdentityStore
// needs no interface change beyond the new GetUserByUsername verb (which the
// local stores also gain).
type QRDBUserStorage struct {
	resolver UserServicerResolver
	profiles iam.BrokerProfileStore
	idmap    IDMap
	kv       interfaces.KVStore
}

// Compile-time interface check: QRDBUserStorage is a UserStorage.
var _ iam.UserStorage = (*QRDBUserStorage)(nil)

// NewQRDBUserStorage builds a qrdb-backed UserStorage. The resolver, profile
// store and id map must be non-nil; a nil resolver yields a storage that fails
// every core call with a typed configuration error rather than panicking. The
// kv backs the broker-side email and legacy-PIV indexes.
func NewQRDBUserStorage(resolver UserServicerResolver, profiles iam.BrokerProfileStore, idmap IDMap, kv interfaces.KVStore) *QRDBUserStorage {
	return &QRDBUserStorage{resolver: resolver, profiles: profiles, idmap: idmap, kv: kv}
}

// broker-side index keys. They live under the canonical __iam: namespace so
// they are encrypted-at-rest in the owning tenant's barrier and a sealed
// tenant fails closed via the routing KVStore. Each email/legacy-PIV index has
// a tenant-scoped key AND a global (un-scoped) key, mirroring the local
// BackendStorage dual-write/dual-read: the global key serves lookups made with
// no tenant in context (e.g. the first-boot admin read-back, which queries by a
// globally unique email before any tenant is bound). The index VALUE is
// "tenantID:uuid" so a global hit still resolves the owning tenant for GetUser.
const (
	keyPrefixUserEmailIndex     = iam.KeyPrefixIAM + "qrdbuser:email:"
	keyPrefixUserLegacyPIVIndex = iam.KeyPrefixIAM + "qrdbuser:legacypiv:"
)

func userEmailIndexKey(tenantID, email string) string {
	return keyPrefixUserEmailIndex + tenantID + ":" + email
}

func userEmailGlobalKey(email string) string {
	return keyPrefixUserEmailIndex + "_global:" + email
}

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

// CreateUser writes the qrdb core (CreateUser then UpdateUser to apply the ABAC
// attributes and cert bindings qrdb's CreateUserRequest cannot carry), records
// the string-UUID <-> uint64 correlation in the IDMap, persists the broker
// side-record, and writes the broker-side email/legacy-PIV indexes.
func (s *QRDBUserStorage) CreateUser(ctx context.Context, u *iam.User) error {
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}

	core, _, _ := FromIAMUser(u, 0)
	created, err := svc.CreateUser(ctx, &qrdbsdk.CreateUserRequest{
		Username: core.Username,
		TenantID: core.TenantID,
		RoleIDs:  core.RoleIDs,
	})
	if err != nil {
		return mapQRDBUserError("create", err)
	}

	// Apply the fields CreateUserRequest cannot carry. The update routes on the
	// context tenant, so bind it.
	tctx := qrdbsdk.ContextWithTenant(ctx, u.TenantID)
	if core.Attributes != nil || len(u.CertBindings) > 0 {
		if _, err := svc.UpdateUser(tctx, &qrdbsdk.UpdateUserRequest{
			ID:           created.ID,
			Attributes:   core.Attributes,
			CertBindings: cloneCertBindings(u.CertBindings),
		}); err != nil {
			return mapQRDBUserError("create.update", err)
		}
	}

	// Persist the broker side-record and id correlation keyed by the qrdb id.
	_, profile, _ := FromIAMUser(u, created.ID)
	if err := s.profiles.SaveProfile(ctx, profile); err != nil {
		return err
	}
	if err := s.idmap.Link(ctx, u.TenantID, u.ID, created.ID); err != nil {
		return err
	}
	return s.writeIndexes(ctx, u)
}

// GetUser resolves the qrdb uint64 from the IDMap, reads the qrdb core (routed
// on the context tenant) and the broker side-record, and composes the broker
// iam.User via ToIAMUser.
func (s *QRDBUserStorage) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	qid, err := s.idmap.ToQRDB(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, ErrIDMapNotFound) {
			return nil, iam.ErrUserNotFound
		}
		return nil, err
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	core, err := svc.GetUser(tctx, qid)
	if err != nil {
		return nil, mapQRDBUserError("get", err)
	}
	profile, err := s.profile(ctx, tenantID, qid)
	if err != nil {
		return nil, err
	}
	return ToIAMUser(*core, profile, userID), nil
}

// GetUserByEmail resolves the user via the broker-side email index, then
// delegates to GetUser. qrdb has no email lookup, so the index is broker-owned.
// It reads the tenant-scoped key when a tenant is in context and falls back to
// the global key otherwise (the first-boot admin read-back queries by a globally
// unique email before any tenant is bound).
func (s *QRDBUserStorage) GetUserByEmail(ctx context.Context, email string) (*iam.User, error) {
	owningTenant, uuid, err := s.resolveIndex(ctx, userEmailIndexKey, userEmailGlobalKey, email)
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, owningTenant, uuid)
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
	tenantID := iam.TenantFromContext(ctx)
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)

	// SPKI first.
	if core, err := svc.UserByCertThumbprint(tctx, thumbprint); err == nil {
		uuid, lerr := s.idmap.ToBroker(ctx, core.TenantID, core.ID)
		if lerr != nil {
			return nil, lerr
		}
		profile, perr := s.profile(ctx, core.TenantID, core.ID)
		if perr != nil {
			return nil, perr
		}
		return ToIAMUser(*core, profile, uuid), nil
	} else if !isQRDBUserNotFound(err) {
		return nil, mapQRDBUserError("piv.spki", err)
	}

	// Legacy fallback: broker-side legacy-PIV index (tenant-scoped, then global).
	owningTenant, uuid, err := s.resolveIndex(ctx, userLegacyPIVIndexKey, userLegacyPIVGlobalKey, thumbprint)
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, owningTenant, uuid)
}

// GetUserByUsername resolves a user by tenant-scoped username via qrdb's native
// lookup, then composes the broker iam.User. It is the seam the RBAC
// AssignRole/RevokeRole redirect uses (the broker has no username->id index of
// its own; qrdb owns it).
func (s *QRDBUserStorage) GetUserByUsername(ctx context.Context, tenantID, username string) (*iam.User, error) {
	svc, err := s.servicer(ctx)
	if err != nil {
		return nil, err
	}
	core, err := svc.GetUserByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, mapQRDBUserError("get_by_username", err)
	}
	uuid, err := s.idmap.ToBroker(ctx, core.TenantID, core.ID)
	if err != nil {
		return nil, err
	}
	profile, err := s.profile(ctx, core.TenantID, core.ID)
	if err != nil {
		return nil, err
	}
	return ToIAMUser(*core, profile, uuid), nil
}

// UpdateUser applies a full replacement of the qrdb core (RoleIDs, Attributes,
// CertBindings, Username) for the resolved uint64 and overwrites the broker
// side-record, keeping the email/legacy-PIV indexes consistent.
func (s *QRDBUserStorage) UpdateUser(ctx context.Context, u *iam.User) error {
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}
	qid, err := s.idmap.ToQRDB(ctx, u.TenantID, u.ID)
	if err != nil {
		if errors.Is(err, ErrIDMapNotFound) {
			return iam.ErrUserNotFound
		}
		return err
	}

	// Read the prior profile to reconcile the email/legacy-PIV indexes.
	prior, err := s.profile(ctx, u.TenantID, qid)
	if err != nil && !errors.Is(err, iam.ErrBrokerProfileNotFound) {
		return err
	}

	// CertBindings are a qrdb-canonical field, but the converter routes them into
	// the broker profile for round-trip fidelity, leaving core.CertBindings nil.
	// The qrdb core write therefore takes the bindings straight from the broker
	// user so qrdb's UserByCertThumbprint index stays authoritative.
	core, profile, _ := FromIAMUser(u, qid)
	tctx := qrdbsdk.ContextWithTenant(ctx, u.TenantID)
	if _, err := svc.UpdateUser(tctx, &qrdbsdk.UpdateUserRequest{
		ID:           qid,
		Username:     core.Username,
		RoleIDs:      orEmptySlice(core.RoleIDs),
		Attributes:   orEmptyMap(core.Attributes),
		CertBindings: orEmptyCertBindings(cloneCertBindings(u.CertBindings)),
	}); err != nil {
		return mapQRDBUserError("update", err)
	}
	if err := s.profiles.SaveProfile(ctx, profile); err != nil {
		return err
	}
	return s.reconcileIndexes(ctx, prior, u)
}

// DeleteUser removes the qrdb core, the broker side-record, the id correlation,
// and the broker-side indexes.
func (s *QRDBUserStorage) DeleteUser(ctx context.Context, tenantID, userID string) error {
	svc, err := s.servicer(ctx)
	if err != nil {
		return err
	}
	qid, err := s.idmap.ToQRDB(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, ErrIDMapNotFound) {
			return iam.ErrUserNotFound
		}
		return err
	}
	prior, err := s.profile(ctx, tenantID, qid)
	if err != nil && !errors.Is(err, iam.ErrBrokerProfileNotFound) {
		return err
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, tenantID)
	if err := svc.DeleteUser(tctx, qid); err != nil {
		return mapQRDBUserError("delete", err)
	}
	if err := s.profiles.DeleteProfile(ctx, tenantID, qid); err != nil {
		return err
	}
	if err := s.idmap.Unlink(ctx, tenantID, userID); err != nil {
		return err
	}
	return s.deleteIndexes(ctx, tenantID, prior)
}

// ListUsers returns the tenant's qrdb users, composing each with its broker
// side-record. The qrdb id is the IDMap reverse key, so a user missing an IDMap
// entry (never persisted through this store) is skipped rather than failing the
// whole listing.
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
		uuid, lerr := s.idmap.ToBroker(ctx, tenantID, core.ID)
		if lerr != nil {
			if errors.Is(lerr, ErrIDMapNotFound) {
				continue
			}
			return nil, lerr
		}
		profile, perr := s.profile(ctx, tenantID, core.ID)
		if perr != nil {
			return nil, perr
		}
		out = append(out, ToIAMUser(*core, profile, uuid))
	}
	return out, nil
}

// profile reads the broker side-record, returning a nil profile (not an error)
// when none exists so ToIAMUser falls back to the qrdb-core-only projection.
func (s *QRDBUserStorage) profile(ctx context.Context, tenantID string, qid uint64) (*iam.BrokerProfile, error) {
	p, err := s.profiles.GetProfile(ctx, tenantID, qid)
	if err != nil {
		if errors.Is(err, iam.ErrBrokerProfileNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

// indexValue encodes the owning tenant and broker UUID of an index entry so a
// global (un-scoped) hit still resolves the owning tenant for GetUser.
func indexValue(tenantID, uuid string) []byte {
	return []byte(tenantID + ":" + uuid)
}

// writeIndexes writes the email and legacy-PIV broker-side indexes for u. Each
// is written under a tenant-scoped key AND a global key (both routed through the
// owning tenant's store) so a lookup with no tenant in context still resolves.
func (s *QRDBUserStorage) writeIndexes(ctx context.Context, u *iam.User) error {
	tctx := iam.ContextWithTenant(ctx, u.TenantID)
	val := indexValue(u.TenantID, u.ID)
	if u.Email != "" {
		if err := s.kv.Put(tctx, userEmailIndexKey(u.TenantID, u.Email), val); err != nil {
			return err
		}
		if err := s.kv.Put(tctx, userEmailGlobalKey(u.Email), val); err != nil {
			return err
		}
	}
	for _, tp := range u.LegacyPIVThumbprints() {
		if err := s.kv.Put(tctx, userLegacyPIVIndexKey(u.TenantID, tp), val); err != nil {
			return err
		}
		if err := s.kv.Put(tctx, userLegacyPIVGlobalKey(tp), val); err != nil {
			return err
		}
	}
	return nil
}

// reconcileIndexes updates the broker-side indexes for an UpdateUser: it drops
// index entries that no longer apply and (re)writes the current ones.
func (s *QRDBUserStorage) reconcileIndexes(ctx context.Context, prior *iam.BrokerProfile, u *iam.User) error {
	tctx := iam.ContextWithTenant(ctx, u.TenantID)
	if prior != nil {
		if prior.Email != "" && prior.Email != u.Email {
			if err := s.kv.Delete(tctx, userEmailIndexKey(u.TenantID, prior.Email)); err != nil {
				return err
			}
			if err := s.kv.Delete(tctx, userEmailGlobalKey(prior.Email)); err != nil {
				return err
			}
		}
		current := make(map[string]struct{})
		for _, tp := range u.LegacyPIVThumbprints() {
			current[tp] = struct{}{}
		}
		for _, tp := range legacyPIVThumbprintsFromBindings(prior.CertBindings) {
			if _, keep := current[tp]; !keep {
				if err := s.kv.Delete(tctx, userLegacyPIVIndexKey(u.TenantID, tp)); err != nil {
					return err
				}
				if err := s.kv.Delete(tctx, userLegacyPIVGlobalKey(tp)); err != nil {
					return err
				}
			}
		}
	}
	return s.writeIndexes(ctx, u)
}

// deleteIndexes removes the broker-side indexes for a deleted user, using the
// prior profile to recover the email and legacy-PIV thumbprints.
func (s *QRDBUserStorage) deleteIndexes(ctx context.Context, tenantID string, prior *iam.BrokerProfile) error {
	if prior == nil {
		return nil
	}
	tctx := iam.ContextWithTenant(ctx, tenantID)
	if prior.Email != "" {
		if err := s.kv.Delete(tctx, userEmailIndexKey(tenantID, prior.Email)); err != nil {
			return err
		}
		if err := s.kv.Delete(tctx, userEmailGlobalKey(prior.Email)); err != nil {
			return err
		}
	}
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

// resolveIndex reads a broker-side index, returning the owning tenant and broker
// UUID. It tries the tenant-scoped key first when a tenant is in context, then
// falls back to the global key (routed through the in-context tenant, which for a
// prefix/system tenant or an empty context is the shared store). A not-found maps
// to the canonical iam.ErrUserNotFound.
func (s *QRDBUserStorage) resolveIndex(ctx context.Context, scopedKey func(tenantID, k string) string, globalKey func(k string) string, k string) (string, string, error) {
	tenantID := iam.TenantFromContext(ctx)
	if tenantID != "" {
		if owning, uuid, ok, err := s.indexLookup(ctx, scopedKey(tenantID, k)); err != nil {
			return "", "", err
		} else if ok {
			return owning, uuid, nil
		}
	}
	owning, uuid, ok, err := s.indexLookup(ctx, globalKey(k))
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", iam.ErrUserNotFound
	}
	return owning, uuid, nil
}

// indexLookup reads a single index key and splits its "tenantID:uuid" value. The
// ok return distinguishes a clean miss from an error so resolveIndex can fall
// through to the global key.
func (s *QRDBUserStorage) indexLookup(ctx context.Context, key string) (tenantID, uuid string, ok bool, err error) {
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

// splitIndexValue splits a "tenantID:uuid" index value. A UUID never contains a
// colon, and the tenant id is the first segment, so a single SplitN(2) is exact.
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
// ErrEmailAlreadyExists keep working unchanged. Unknown errors are wrapped
// (never swallowed) with the originating op.
func mapQRDBUserError(op string, err error) error {
	if err == nil {
		return nil
	}
	if isQRDBUserNotFound(err) {
		return iam.ErrUserNotFound
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
