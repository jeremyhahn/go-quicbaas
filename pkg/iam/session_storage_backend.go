// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/json"
	"errors"
	"strconv"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Session storage key helpers. Sessions are keyed by a tenant-encoded session
// id so by-ID lookups (which arrive with no tenant in context) recover the
// owning tenant; a per-user index lets the by-user verbs enumerate within the
// tenant's store; a per-tenant token index resolves GetByToken under the
// request's tenant context (the token hash carries no tenant, but a session
// token is only ever validated inside its own tenant's request scope).
const (
	keyPrefixSessionRec      = KeyPrefixIAM + "session/rec/"
	keyPrefixSessionUserIdx  = KeyPrefixIAM + "session_idx/user/"
	keyPrefixSessionTokenIdx = KeyPrefixIAM + "session_idx/token/"
)

func sessionRecKey(id string) string { return keyPrefixSessionRec + id }
func sessionTokenIdxKey(tokenHash string) string {
	return keyPrefixSessionTokenIdx + tokenHash
}
func sessionUserIdxKey(tenantID string, userID uint64, id string) string {
	return sessionUserIdxPrefix(tenantID, userID) + id
}
func sessionUserIdxPrefix(tenantID string, userID uint64) string {
	return keyPrefixSessionUserIdx + tenantID + "/" + strconv.FormatUint(userID, 10) + "/"
}

// -----------------------------------------------------------------------------
// BackendSessionStorage — SessionStorage (basic) over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendSessionStorage implements the lightweight SessionStorage interface
// (iam.Session records) over the per-tenant routing KVStore. Session ids are
// tenant-encoded so GetSession / DeleteSession route to the owning tenant.
type BackendSessionStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendSessionStorage constructs a persistent, per-tenant session store.
// The tenants lister is optional and used only by DeleteExpiredSessions.
func NewBackendSessionStorage(kv interfaces.KVStore, tenants tenantLister) *BackendSessionStorage {
	return &BackendSessionStorage{kv: kv, tenants: tenants}
}

var _ SessionStorage = (*BackendSessionStorage)(nil)

// CreateSession tenant-encodes the session ID, routes by the record's TenantID,
// and writes the record plus its per-user index.
func (s *BackendSessionStorage) CreateSession(ctx context.Context, session *Session) error {
	if session == nil || session.ID == "" {
		return NewIAMError(ErrUserInvalid, "iam.session.storage.create", "session ID is required", nil)
	}
	session.ID = ensureTenantEncoded(session.TenantID, session.ID)
	ctx = withTenant(ctx, session.TenantID)
	if _, err := s.kv.Get(ctx, sessionRecKey(session.ID)); err == nil {
		return NewIAMError(ErrUserInvalid, "iam.session.storage.create", "session already exists", nil)
	}
	data, err := json.Marshal(session)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.session.storage.create", Err: err}
	}
	if err := s.kv.Put(ctx, sessionRecKey(session.ID), data); err != nil {
		return err
	}
	return s.kv.Put(ctx, sessionUserIdxKey(session.TenantID, session.UserID, session.ID), []byte(session.ID))
}

// GetSession decodes the tenant from the session ID, routes, and reads.
func (s *BackendSessionStorage) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	tenantID, fullID := decodeTenantID(sessionID)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, sessionRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, NewIAMError(ErrUserNotFound, "iam.session.storage.get", "session not found", nil)
		}
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.session.storage.get", Err: err}
	}
	return &session, nil
}

// GetSessionsByUser routes by the explicit tenantID and returns the user's
// non-expired sessions enumerated via the per-user index.
func (s *BackendSessionStorage) GetSessionsByUser(ctx context.Context, tenantID string, userID uint64) ([]*Session, error) {
	ctx = withTenant(ctx, tenantID)
	ids, err := s.sessionIDsForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	sessions := make([]*Session, 0, len(ids))
	for _, id := range ids {
		data, err := s.kv.Get(ctx, sessionRecKey(id))
		if err != nil {
			if dberrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		var session Session
		if json.Unmarshal(data, &session) != nil {
			continue
		}
		if !session.IsExpired() {
			sessions = append(sessions, &session)
		}
	}
	return sessions, nil
}

// UpdateSession decodes the tenant, verifies existence, and overwrites.
func (s *BackendSessionStorage) UpdateSession(ctx context.Context, session *Session) error {
	if session == nil || session.ID == "" {
		return NewIAMError(ErrUserInvalid, "iam.session.storage.update", "session ID is required", nil)
	}
	tenantID, fullID := decodeTenantID(session.ID)
	ctx = withTenant(ctx, tenantID)
	if _, err := s.kv.Get(ctx, sessionRecKey(fullID)); err != nil {
		if dberrors.IsNotFound(err) {
			return NewIAMError(ErrUserNotFound, "iam.session.storage.update", "session not found", nil)
		}
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.session.storage.update", Err: err}
	}
	return s.kv.Put(ctx, sessionRecKey(fullID), data)
}

// DeleteSession decodes the tenant, routes, and removes the record plus index.
func (s *BackendSessionStorage) DeleteSession(ctx context.Context, sessionID string) error {
	tenantID, fullID := decodeTenantID(sessionID)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, sessionRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return NewIAMError(ErrUserNotFound, "iam.session.storage.delete", "session not found", nil)
		}
		return err
	}
	var session Session
	if json.Unmarshal(data, &session) == nil && session.UserID != 0 {
		_ = s.kv.Delete(ctx, sessionUserIdxKey(session.TenantID, session.UserID, fullID))
	}
	return s.kv.Delete(ctx, sessionRecKey(fullID))
}

// DeleteExpiredSessions sweeps expired sessions per tenant (binding each tenant
// into ctx) so each tenant's barrier store is cleaned independently. Without a
// tenantLister it sweeps only the context tenant.
func (s *BackendSessionStorage) DeleteExpiredSessions(ctx context.Context) (int, error) {
	tenantIDs, err := sweepTenantIDs(ctx, s.tenants)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, tid := range tenantIDs {
		n, err := s.cleanupTenant(withTenant(ctx, tid))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (s *BackendSessionStorage) cleanupTenant(ctx context.Context) (int, error) {
	var stale []*Session
	if err := s.kv.Scan(ctx, keyPrefixSessionRec, func(_ string, value []byte) error {
		var session Session
		if json.Unmarshal(value, &session) != nil {
			return nil
		}
		if session.IsExpired() {
			st := session
			stale = append(stale, &st)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	count := 0
	for _, session := range stale {
		if err := s.kv.Delete(ctx, sessionRecKey(session.ID)); err != nil {
			return count, err
		}
		_ = s.kv.Delete(ctx, sessionUserIdxKey(session.TenantID, session.UserID, session.ID))
		count++
	}
	return count, nil
}

func (s *BackendSessionStorage) sessionIDsForUser(ctx context.Context, tenantID string, userID uint64) ([]string, error) {
	var ids []string
	err := s.kv.Scan(ctx, sessionUserIdxPrefix(tenantID, userID), func(_ string, value []byte) error {
		ids = append(ids, string(value))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// -----------------------------------------------------------------------------
// BackendSessionExtStorage — SessionExtStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendSessionExtStorage implements the richer SessionExtStorage interface
// (token/refresh hashes, device info) over the per-tenant routing KVStore.
// Session ids are tenant-encoded; GetByToken routes by the request's context
// tenant via a per-tenant token index.
type BackendSessionExtStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendSessionExtStorage constructs a persistent, per-tenant extended
// session store.
func NewBackendSessionExtStorage(kv interfaces.KVStore, tenants tenantLister) *BackendSessionExtStorage {
	return &BackendSessionExtStorage{kv: kv, tenants: tenants}
}

var _ SessionExtStorage = (*BackendSessionExtStorage)(nil)

// Create tenant-encodes the session ID, routes by TenantID, and writes the
// record plus the per-user and per-token (access + refresh) indexes.
func (s *BackendSessionExtStorage) Create(ctx context.Context, session *SessionExt) error {
	if err := session.Validate(); err != nil {
		return err
	}
	session.ID = ensureTenantEncoded(session.TenantID, session.ID)
	ctx = withTenant(ctx, session.TenantID)
	if _, err := s.kv.Get(ctx, sessionRecKey(session.ID)); err == nil {
		return NewIAMError(ErrSessionInvalid, "iam.session.create", "session already exists", nil)
	}
	data, err := json.Marshal(session)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.session.create", Err: err}
	}
	if err := s.kv.Put(ctx, sessionRecKey(session.ID), data); err != nil {
		return err
	}
	if err := s.kv.Put(ctx, sessionUserIdxKey(session.TenantID, session.UserID, session.ID), []byte(session.ID)); err != nil {
		return err
	}
	if session.TokenHash != "" {
		if err := s.kv.Put(ctx, sessionTokenIdxKey(session.TokenHash), []byte(session.ID)); err != nil {
			return err
		}
	}
	if session.RefreshHash != "" {
		if err := s.kv.Put(ctx, sessionTokenIdxKey(session.RefreshHash), []byte(session.ID)); err != nil {
			return err
		}
	}
	return nil
}

// Get decodes the tenant from the session ID and reads the record.
func (s *BackendSessionExtStorage) Get(ctx context.Context, sessionID string) (*SessionExt, error) {
	if sessionID == "" {
		return nil, NewIAMError(ErrSessionInvalid, "iam.session.get", "session ID is required", nil)
	}
	tenantID, fullID := decodeTenantID(sessionID)
	ctx = withTenant(ctx, tenantID)
	return s.getRouted(ctx, fullID)
}

// GetByToken routes by the request's context tenant and resolves the token
// index. A session token is only ever presented inside its owning tenant's
// request scope, so the context tenant is the correct routing key; the index
// lives in that tenant's barrier store, never a global table.
func (s *BackendSessionExtStorage) GetByToken(ctx context.Context, tokenHash string) (*SessionExt, error) {
	if tokenHash == "" {
		return nil, NewIAMError(ErrSessionInvalid, "iam.session.get_by_token", "token hash is required", nil)
	}
	idData, err := s.kv.Get(ctx, sessionTokenIdxKey(tokenHash))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	_, fullID := decodeTenantID(string(idData))
	return s.getRouted(ctx, fullID)
}

// getRouted reads a session record assuming ctx is already tenant-bound.
func (s *BackendSessionExtStorage) getRouted(ctx context.Context, fullID string) (*SessionExt, error) {
	data, err := s.kv.Get(ctx, sessionRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	var session SessionExt
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.session.get", Err: err}
	}
	return &session, nil
}

// Update decodes the tenant, reconciles changed token indexes, and overwrites.
func (s *BackendSessionExtStorage) Update(ctx context.Context, session *SessionExt) error {
	if err := session.Validate(); err != nil {
		return err
	}
	tenantID, fullID := decodeTenantID(session.ID)
	ctx = withTenant(ctx, tenantID)
	existing, err := s.getRouted(ctx, fullID)
	if err != nil {
		return err
	}
	if existing.TokenHash != session.TokenHash {
		if existing.TokenHash != "" {
			_ = s.kv.Delete(ctx, sessionTokenIdxKey(existing.TokenHash))
		}
		if session.TokenHash != "" {
			if err := s.kv.Put(ctx, sessionTokenIdxKey(session.TokenHash), []byte(session.ID)); err != nil {
				return err
			}
		}
	}
	if existing.RefreshHash != session.RefreshHash {
		if existing.RefreshHash != "" {
			_ = s.kv.Delete(ctx, sessionTokenIdxKey(existing.RefreshHash))
		}
		if session.RefreshHash != "" {
			if err := s.kv.Put(ctx, sessionTokenIdxKey(session.RefreshHash), []byte(session.ID)); err != nil {
				return err
			}
		}
	}
	data, err := json.Marshal(session)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.session.update", Err: err}
	}
	return s.kv.Put(ctx, sessionRecKey(fullID), data)
}

// Delete decodes the tenant and removes the record plus all indexes.
func (s *BackendSessionExtStorage) Delete(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.delete", "session ID is required", nil)
	}
	tenantID, fullID := decodeTenantID(sessionID)
	ctx = withTenant(ctx, tenantID)
	existing, err := s.getRouted(ctx, fullID)
	if err != nil {
		// coded-sentinel-ok: the CLASS is the intent. The delete is idempotent,
		// and any not-found means there is no record left to remove.
		if errors.Is(err, ErrSessionNotFound) {
			return nil // Idempotent delete.
		}
		return err
	}
	s.deleteIndexes(ctx, existing)
	return s.kv.Delete(ctx, sessionRecKey(fullID))
}

// deleteIndexes removes a session's user and token index entries. ctx must
// already be tenant-bound.
func (s *BackendSessionExtStorage) deleteIndexes(ctx context.Context, session *SessionExt) {
	_ = s.kv.Delete(ctx, sessionUserIdxKey(session.TenantID, session.UserID, session.ID))
	if session.TokenHash != "" {
		_ = s.kv.Delete(ctx, sessionTokenIdxKey(session.TokenHash))
	}
	if session.RefreshHash != "" {
		_ = s.kv.Delete(ctx, sessionTokenIdxKey(session.RefreshHash))
	}
}

// ListByUser routes by the explicit tenantID and returns every indexed session.
func (s *BackendSessionExtStorage) ListByUser(ctx context.Context, userID uint64, tenantID string) ([]*SessionExt, error) {
	ctx = withTenant(ctx, tenantID)
	return s.listByUser(ctx, tenantID, userID, false)
}

// ListByUserActive routes by tenantID and returns only non-expired sessions.
func (s *BackendSessionExtStorage) ListByUserActive(ctx context.Context, userID uint64, tenantID string) ([]*SessionExt, error) {
	ctx = withTenant(ctx, tenantID)
	return s.listByUser(ctx, tenantID, userID, true)
}

func (s *BackendSessionExtStorage) listByUser(ctx context.Context, tenantID string, userID uint64, activeOnly bool) ([]*SessionExt, error) {
	var ids []string
	if err := s.kv.Scan(ctx, sessionUserIdxPrefix(tenantID, userID), func(_ string, value []byte) error {
		ids = append(ids, string(value))
		return nil
	}); err != nil {
		return nil, err
	}
	now := time.Now()
	sessions := make([]*SessionExt, 0, len(ids))
	for _, id := range ids {
		_, fullID := decodeTenantID(id)
		session, err := s.getRouted(ctx, fullID)
		if err != nil {
			// coded-sentinel-ok: the CLASS is the intent. An index entry whose
			// record the store cannot produce is skipped however it went
			// missing; the listing is of the sessions that exist.
			if errors.Is(err, ErrSessionNotFound) {
				continue
			}
			return nil, err
		}
		if activeOnly && now.After(session.ExpiresAt) {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// DeleteByUser routes by tenantID and removes every indexed session.
func (s *BackendSessionExtStorage) DeleteByUser(ctx context.Context, userID uint64, tenantID string) error {
	ctx = withTenant(ctx, tenantID)
	sessions, err := s.listByUser(ctx, tenantID, userID, false)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		s.deleteIndexes(ctx, session)
		if err := s.kv.Delete(ctx, sessionRecKey(session.ID)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAllExcept routes by tenantID and removes every session except the one
// identified by exceptSessionID.
func (s *BackendSessionExtStorage) DeleteAllExcept(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error {
	ctx = withTenant(ctx, tenantID)
	sessions, err := s.listByUser(ctx, tenantID, userID, false)
	if err != nil {
		return err
	}
	_, exceptFull := decodeTenantID(exceptSessionID)
	for _, session := range sessions {
		if session.ID == exceptFull || session.ID == exceptSessionID {
			continue
		}
		s.deleteIndexes(ctx, session)
		if err := s.kv.Delete(ctx, sessionRecKey(session.ID)); err != nil {
			return err
		}
	}
	return nil
}

// CleanupExpired sweeps expired sessions per tenant (see DeleteExpiredSessions
// on BackendSessionStorage for the cross-tenant semantics). Without a
// tenantLister it sweeps only the context tenant.
func (s *BackendSessionExtStorage) CleanupExpired(ctx context.Context) (int, error) {
	tenantIDs, err := sweepTenantIDs(ctx, s.tenants)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, tid := range tenantIDs {
		n, err := s.cleanupTenant(withTenant(ctx, tid))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (s *BackendSessionExtStorage) cleanupTenant(ctx context.Context) (int, error) {
	now := time.Now()
	var stale []*SessionExt
	if err := s.kv.Scan(ctx, keyPrefixSessionRec, func(_ string, value []byte) error {
		var session SessionExt
		if json.Unmarshal(value, &session) != nil {
			return nil
		}
		if now.After(session.ExpiresAt) {
			st := session
			stale = append(stale, &st)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	count := 0
	for _, session := range stale {
		s.deleteIndexes(ctx, session)
		if err := s.kv.Delete(ctx, sessionRecKey(session.ID)); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
