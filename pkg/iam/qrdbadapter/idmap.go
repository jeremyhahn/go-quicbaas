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
	"strconv"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// IDMap is the permanent, tenant-scoped bidirectional correlation between
// quicbaas's EXTERNAL/API user identifier (a string UUID — the value the OIDC
// subject, PEP, sessions and SPKI bindings all key on) and qrdb's INTERNAL
// user identifier (a uint64 Snowflake assigned by qrdb's user store). The
// locked identity architecture keeps the string UUID as the stable external id
// forever and wraps qrdb's sdk.User; this map is the load-bearing join that
// makes the wrapper possible without rewriting either id space.
//
// The map is strictly 1:1 within a tenant: a single string UUID corresponds to
// exactly one uint64 and vice versa.
type IDMap interface {
	// Link records the bidirectional correlation uuid <-> qrdbID within tenantID.
	// It is idempotent for an identical pair and rejects a re-link that would
	// remap an already-bound id to a different counterpart (ErrIDMapConflict).
	Link(ctx context.Context, tenantID, uuid string, qrdbID uint64) error

	// ToQRDB resolves the qrdb uint64 id for the external string UUID within
	// tenantID. Returns ErrIDMapNotFound when no correlation exists.
	ToQRDB(ctx context.Context, tenantID, uuid string) (uint64, error)

	// ToBroker resolves the external string UUID for the qrdb uint64 id within
	// tenantID. Returns ErrIDMapNotFound when no correlation exists.
	ToBroker(ctx context.Context, tenantID string, qrdbID uint64) (string, error)

	// Unlink removes the correlation for the external string UUID within
	// tenantID. Removing a missing entry is a no-op.
	Unlink(ctx context.Context, tenantID, uuid string) error
}

// IDMap typed errors. Declared as *dberrors.QBError per the project's typed-error
// convention so callers can branch on the canonical code via errors.As.
var (
	// ErrIDMapNotFound is returned when no correlation exists for the requested
	// identifier in the given tenant.
	ErrIDMapNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.idmap",
		Err:  &idMapMessageError{message: "id correlation not found"},
	}

	// ErrIDMapInvalid is returned when a Link/lookup is missing a required
	// argument (non-empty tenant + uuid and a non-zero qrdb id).
	ErrIDMapInvalid = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.idmap",
		Err:  &idMapMessageError{message: "id correlation requires a tenant, a non-empty uuid and a non-zero qrdb id"},
	}

	// ErrIDMapConflict is returned when a Link would remap an already-bound id to
	// a different counterpart, which would corrupt the 1:1 invariant.
	ErrIDMapConflict = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.idmap",
		Err:  &idMapMessageError{message: "id correlation already bound to a different counterpart"},
	}
)

// idMapMessageError is the typed payload for IDMap sentinels.
type idMapMessageError struct{ message string }

func (e *idMapMessageError) Error() string { return e.message }

// IDMap canonical keys. Both directions live under the canonical __iam:
// namespace so they are encrypted-at-rest in the owning tenant's barrier when
// the tenant is barrier-isolated, and a sealed tenant fails closed (the routing
// KVStore surfaces the sealed sentinel and never falls back to the shared
// store). The tenant id is embedded in the key in addition to being bound into
// the routing context so the two directions never collide across tenants.
//
//	forward (uuid  -> uint64): __iam:idmap:fwd:{tenantID}:{uuid}
//	reverse (uint64 -> uuid):  __iam:idmap:rev:{tenantID}:{uint64}
const (
	keyPrefixIDMapForward = iam.KeyPrefixIAM + "idmap:fwd:"
	keyPrefixIDMapReverse = iam.KeyPrefixIAM + "idmap:rev:"
)

func idMapForwardKey(tenantID, uuid string) string {
	return keyPrefixIDMapForward + tenantID + ":" + uuid
}

func idMapReverseKey(tenantID string, qrdbID uint64) string {
	return keyPrefixIDMapReverse + tenantID + ":" + strconv.FormatUint(qrdbID, 10)
}

// BackendIDMap implements IDMap over the per-tenant routing KVStore. It writes
// both index directions on Link and resolves either direction in O(1). It
// inherits the routing store's fail-closed behavior for sealed tenants.
type BackendIDMap struct {
	kv interfaces.KVStore
}

// NewBackendIDMap constructs a persistent, per-tenant id correlation map over
// the routing KVStore.
func NewBackendIDMap(kv interfaces.KVStore) *BackendIDMap {
	return &BackendIDMap{kv: kv}
}

var _ IDMap = (*BackendIDMap)(nil)

// withTenant binds tenantID into ctx so the routing KVStore selects that
// tenant's store. It mirrors the pkg/iam helper; an empty tenantID leaves the
// context unchanged (the lookup guards reject that case before reaching here).
func withTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return iam.ContextWithTenant(ctx, tenantID)
}

// Link records the correlation in both directions. It is idempotent when the
// existing binding already matches and rejects a conflicting remap so the 1:1
// invariant is preserved.
func (m *BackendIDMap) Link(ctx context.Context, tenantID, uuid string, qrdbID uint64) error {
	if tenantID == "" || uuid == "" || qrdbID == 0 {
		return ErrIDMapInvalid
	}
	ctx = withTenant(ctx, tenantID)

	// Reject a forward remap to a different uint64.
	if existing, err := m.kv.Get(ctx, idMapForwardKey(tenantID, uuid)); err == nil {
		if string(existing) != strconv.FormatUint(qrdbID, 10) {
			return ErrIDMapConflict
		}
	} else if !dberrors.IsNotFound(err) {
		return err
	}

	// Reject a reverse remap to a different uuid.
	if existing, err := m.kv.Get(ctx, idMapReverseKey(tenantID, qrdbID)); err == nil {
		if string(existing) != uuid {
			return ErrIDMapConflict
		}
	} else if !dberrors.IsNotFound(err) {
		return err
	}

	if err := m.kv.Put(ctx, idMapForwardKey(tenantID, uuid), []byte(strconv.FormatUint(qrdbID, 10))); err != nil {
		return err
	}
	return m.kv.Put(ctx, idMapReverseKey(tenantID, qrdbID), []byte(uuid))
}

// ToQRDB resolves the qrdb uint64 id for an external string UUID.
func (m *BackendIDMap) ToQRDB(ctx context.Context, tenantID, uuid string) (uint64, error) {
	if tenantID == "" || uuid == "" {
		return 0, ErrIDMapInvalid
	}
	ctx = withTenant(ctx, tenantID)
	data, err := m.kv.Get(ctx, idMapForwardKey(tenantID, uuid))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return 0, ErrIDMapNotFound
		}
		return 0, err
	}
	id, perr := strconv.ParseUint(string(data), 10, 64)
	if perr != nil {
		return 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.idmap.to_qrdb", Err: perr}
	}
	return id, nil
}

// ToBroker resolves the external string UUID for a qrdb uint64 id.
func (m *BackendIDMap) ToBroker(ctx context.Context, tenantID string, qrdbID uint64) (string, error) {
	if tenantID == "" || qrdbID == 0 {
		return "", ErrIDMapInvalid
	}
	ctx = withTenant(ctx, tenantID)
	data, err := m.kv.Get(ctx, idMapReverseKey(tenantID, qrdbID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return "", ErrIDMapNotFound
		}
		return "", err
	}
	return string(data), nil
}

// Unlink removes both directions of the correlation for an external string
// UUID. Removing a missing entry is a no-op.
func (m *BackendIDMap) Unlink(ctx context.Context, tenantID, uuid string) error {
	if tenantID == "" || uuid == "" {
		return ErrIDMapInvalid
	}
	ctx = withTenant(ctx, tenantID)
	qrdbID, err := m.ToQRDB(ctx, tenantID, uuid)
	if err != nil {
		if err == ErrIDMapNotFound {
			return nil
		}
		return err
	}
	if err := m.kv.Delete(ctx, idMapForwardKey(tenantID, uuid)); err != nil {
		return err
	}
	return m.kv.Delete(ctx, idMapReverseKey(tenantID, qrdbID))
}
