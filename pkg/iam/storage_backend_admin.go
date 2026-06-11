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
	"strings"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Key prefixes for delegated administration data. All admin-related state lives
// under the canonical __iam: namespace so it cleans up alongside the IAM data
// on tenant delete (where applicable).
const (
	keyPrefixAdminAssignment   = KeyPrefixIAM + "admin:assignment:"
	keyPrefixImpersonationSess = KeyPrefixIAM + "admin:impersonation:"
)

// -----------------------------------------------------------------------------
// BackendAdminStorage — AdminStorage on top of interfaces.KVStore.
// -----------------------------------------------------------------------------

// BackendAdminStorage implements AdminStorage backed by a KVStore. Admin
// assignments are the linchpin of the first-boot bootstrap idempotency check:
// firstBootBootstrap calls ListAssignments to detect a prior init, so this
// store MUST persist across daemon restarts.
type BackendAdminStorage struct {
	kv interfaces.KVStore
}

// NewBackendAdminStorage constructs a persistent AdminStorage using the given
// KVStore. The store is shared across the IAM subsystem; admin assignments are
// keyed under a dedicated prefix to keep scans tight.
func NewBackendAdminStorage(kv interfaces.KVStore) *BackendAdminStorage {
	return &BackendAdminStorage{kv: kv}
}

// Compile-time check.
var _ AdminStorage = (*BackendAdminStorage)(nil)

func adminAssignmentKey(id string) string {
	return keyPrefixAdminAssignment + id
}

// CreateAssignment persists a new admin assignment. Rejects duplicates by
// performing a check-then-write — the operation is single-tenant and the
// surrounding AdminManager already serialises mutations behind its own mutex.
func (s *BackendAdminStorage) CreateAssignment(ctx context.Context, assignment *AdminAssignment) error {
	if assignment == nil || assignment.ID == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.create_assignment",
			Err:  ErrAdminAssignmentInvalid,
		}
	}
	key := adminAssignmentKey(assignment.ID)
	if _, err := s.kv.Get(ctx, key); err == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.admin_storage.create_assignment",
		}
	}
	data, err := json.Marshal(assignment)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.admin_storage.create_assignment", Err: err}
	}
	return s.kv.Put(ctx, key, data)
}

// GetAssignment retrieves an admin assignment by ID.
func (s *BackendAdminStorage) GetAssignment(ctx context.Context, id string) (*AdminAssignment, error) {
	if id == "" {
		return nil, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.admin_storage.get_assignment"}
	}
	data, err := s.kv.Get(ctx, adminAssignmentKey(id))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrAdminAssignmentNotFound
		}
		return nil, err
	}
	var a AdminAssignment
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.admin_storage.get_assignment", Err: err}
	}
	return &a, nil
}

// UpdateAssignment overwrites an existing assignment.
func (s *BackendAdminStorage) UpdateAssignment(ctx context.Context, assignment *AdminAssignment) error {
	if assignment == nil || assignment.ID == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.update_assignment",
			Err:  ErrAdminAssignmentInvalid,
		}
	}
	if _, err := s.kv.Get(ctx, adminAssignmentKey(assignment.ID)); err != nil {
		if dberrors.IsNotFound(err) {
			return ErrAdminAssignmentNotFound
		}
		return err
	}
	data, err := json.Marshal(assignment)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.admin_storage.update_assignment", Err: err}
	}
	return s.kv.Put(ctx, adminAssignmentKey(assignment.ID), data)
}

// DeleteAssignment removes an admin assignment.
func (s *BackendAdminStorage) DeleteAssignment(ctx context.Context, id string) error {
	if id == "" {
		return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.admin_storage.delete_assignment"}
	}
	if _, err := s.kv.Get(ctx, adminAssignmentKey(id)); err != nil {
		if dberrors.IsNotFound(err) {
			return ErrAdminAssignmentNotFound
		}
		return err
	}
	return s.kv.Delete(ctx, adminAssignmentKey(id))
}

// ListAssignments returns the assignments matching opts. Filtering and
// pagination are applied in memory after a prefix scan; the assignment set is
// bounded (one row per admin) so a full scan is acceptable.
func (s *BackendAdminStorage) ListAssignments(ctx context.Context, opts ListAdminAssignmentsOptions) ([]*AdminAssignment, error) {
	all, err := s.scanAssignments(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]*AdminAssignment, 0, len(all))
	for _, a := range all {
		if opts.UserID != "" && a.UserID != opts.UserID {
			continue
		}
		if opts.AdminRole != "" && a.AdminRole != opts.AdminRole {
			continue
		}
		if opts.TenantID != "" && len(a.TenantIDs) > 0 {
			match := false
			for _, tid := range a.TenantIDs {
				if tid == opts.TenantID {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if opts.ActiveOnly {
			if !a.Active {
				continue
			}
			if a.ExpiresAt != nil && now.After(*a.ExpiresAt) {
				continue
			}
		}
		out = append(out, a)
	}

	if opts.Offset > 0 {
		if opts.Offset >= len(out) {
			return []*AdminAssignment{}, nil
		}
		out = out[opts.Offset:]
	}
	if opts.Limit > 0 && opts.Limit < len(out) {
		out = out[:opts.Limit]
	}
	return out, nil
}

// GetUserAssignments returns the active assignments for the given user.
func (s *BackendAdminStorage) GetUserAssignments(ctx context.Context, userID string) ([]*AdminAssignment, error) {
	if userID == "" {
		return nil, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.admin_storage.get_user_assignments"}
	}
	all, err := s.scanAssignments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*AdminAssignment, 0)
	for _, a := range all {
		if a.UserID != userID {
			continue
		}
		if !a.Active {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// scanAssignments enumerates every persisted assignment record. The result is
// a fresh slice — callers can mutate it freely.
func (s *BackendAdminStorage) scanAssignments(ctx context.Context) ([]*AdminAssignment, error) {
	var out []*AdminAssignment
	err := s.kv.Scan(ctx, keyPrefixAdminAssignment, func(_ string, value []byte) error {
		var a AdminAssignment
		if err := json.Unmarshal(value, &a); err != nil {
			// Skip malformed rows rather than aborting the scan — surfacing a
			// fatal error here would brick the daemon on any corrupt record.
			return nil
		}
		out = append(out, &a)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// BackendImpersonationStorage — ImpersonationStorage on top of KVStore.
// -----------------------------------------------------------------------------

// BackendImpersonationStorage implements ImpersonationStorage using a KVStore.
// Impersonation sessions are short-lived but must survive across daemon
// restarts so an in-flight admin investigation isn't dropped on the floor.
type BackendImpersonationStorage struct {
	kv interfaces.KVStore
}

// NewBackendImpersonationStorage constructs a persistent ImpersonationStorage.
func NewBackendImpersonationStorage(kv interfaces.KVStore) *BackendImpersonationStorage {
	return &BackendImpersonationStorage{kv: kv}
}

// Compile-time check.
var _ ImpersonationStorage = (*BackendImpersonationStorage)(nil)

func impersonationKey(id string) string {
	return keyPrefixImpersonationSess + id
}

// CreateImpersonationSession persists a new impersonation session.
func (s *BackendImpersonationStorage) CreateImpersonationSession(ctx context.Context, session *ImpersonationSession) error {
	if session == nil || session.ID == "" {
		return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.impersonation_storage.create_session"}
	}
	key := impersonationKey(session.ID)
	if _, err := s.kv.Get(ctx, key); err == nil {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.impersonation_storage.create_session"}
	}
	data, err := json.Marshal(session)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.impersonation_storage.create_session", Err: err}
	}
	return s.kv.Put(ctx, key, data)
}

// GetImpersonationSession retrieves a session by ID.
func (s *BackendImpersonationStorage) GetImpersonationSession(ctx context.Context, id string) (*ImpersonationSession, error) {
	if id == "" {
		return nil, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.impersonation_storage.get_session"}
	}
	data, err := s.kv.Get(ctx, impersonationKey(id))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.impersonation_storage.get_session"}
		}
		return nil, err
	}
	var sess ImpersonationSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.impersonation_storage.get_session", Err: err}
	}
	return &sess, nil
}

// EndImpersonationSession marks a session as ended via a read-modify-write.
// The surrounding ImpersonationManager already serialises mutations per
// session ID; concurrent end calls degrade to "last write wins" which matches
// the in-memory implementation's behaviour.
func (s *BackendImpersonationStorage) EndImpersonationSession(ctx context.Context, id string) error {
	if id == "" {
		return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.impersonation_storage.end_session"}
	}
	sess, err := s.GetImpersonationSession(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	sess.EndedAt = &now
	data, err := json.Marshal(sess)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.impersonation_storage.end_session", Err: err}
	}
	return s.kv.Put(ctx, impersonationKey(id), data)
}

// IncrementOperationCount atomically increments the operation count.
func (s *BackendImpersonationStorage) IncrementOperationCount(ctx context.Context, id string) (int64, error) {
	if id == "" {
		return 0, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.impersonation_storage.increment_operation_count"}
	}
	sess, err := s.GetImpersonationSession(ctx, id)
	if err != nil {
		return 0, err
	}
	sess.OperationCount++
	data, err := json.Marshal(sess)
	if err != nil {
		return 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.impersonation_storage.increment_operation_count", Err: err}
	}
	if err := s.kv.Put(ctx, impersonationKey(id), data); err != nil {
		return 0, err
	}
	return sess.OperationCount, nil
}

// ListActiveImpersonations returns the active sessions for an admin user.
func (s *BackendImpersonationStorage) ListActiveImpersonations(ctx context.Context, adminUserID string) ([]*ImpersonationSession, error) {
	if adminUserID == "" {
		return nil, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.impersonation_storage.list_active"}
	}
	all, err := s.scanSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*ImpersonationSession, 0)
	for _, sess := range all {
		if sess.AdminUserID != adminUserID {
			continue
		}
		if sess.EndedAt != nil {
			continue
		}
		out = append(out, sess)
	}
	return out, nil
}

// ListImpersonations returns sessions with optional filters.
func (s *BackendImpersonationStorage) ListImpersonations(ctx context.Context, activeOnly bool, adminUserID string) ([]*ImpersonationSession, error) {
	all, err := s.scanSessions(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]*ImpersonationSession, 0)
	for _, sess := range all {
		if adminUserID != "" && sess.AdminUserID != adminUserID {
			continue
		}
		if activeOnly {
			if sess.EndedAt != nil {
				continue
			}
			if now.After(sess.ExpiresAt) {
				continue
			}
		}
		out = append(out, sess)
	}
	return out, nil
}

// DeleteExpiredSessions purges sessions whose expiry has passed or that were
// explicitly ended. Mirrors the MemoryImpersonationStorage semantics so the
// AdminManager GC loop behaves identically across backends.
func (s *BackendImpersonationStorage) DeleteExpiredSessions(ctx context.Context) error {
	all, err := s.scanSessions(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, sess := range all {
		if now.After(sess.ExpiresAt) || sess.EndedAt != nil {
			_ = s.kv.Delete(ctx, impersonationKey(sess.ID))
		}
	}
	return nil
}

// scanSessions enumerates every persisted impersonation session.
func (s *BackendImpersonationStorage) scanSessions(ctx context.Context) ([]*ImpersonationSession, error) {
	var out []*ImpersonationSession
	err := s.kv.Scan(ctx, keyPrefixImpersonationSess, func(_ string, value []byte) error {
		var sess ImpersonationSession
		if err := json.Unmarshal(value, &sess); err != nil {
			return nil
		}
		out = append(out, &sess)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// BackendInviteStore — InviteStore on top of KVStore.
// -----------------------------------------------------------------------------

// keyPrefixInvite namespaces the WebAuthn / activation invite records.
const keyPrefixInvite = KeyPrefixIAM + "invite:"

func inviteKey(tokenHash string) string {
	return keyPrefixInvite + strings.ToLower(tokenHash)
}

// BackendInviteStore implements InviteStore using a KVStore. The first-boot
// bootstrap stores an invite that must outlive the daemon process: an operator
// might activate hours after the cluster comes up.
type BackendInviteStore struct {
	kv interfaces.KVStore
}

// NewBackendInviteStore constructs a persistent InviteStore.
func NewBackendInviteStore(kv interfaces.KVStore) *BackendInviteStore {
	return &BackendInviteStore{kv: kv}
}

// Compile-time check.
var _ InviteStore = (*BackendInviteStore)(nil)

// Create stores a new invite record keyed by its token hash.
func (s *BackendInviteStore) Create(ctx context.Context, record *InviteRecord) error {
	if record == nil || record.TokenHash == "" {
		return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "iam.invite.create"}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.invite.create", Err: err}
	}
	return s.kv.Put(ctx, inviteKey(record.TokenHash), data)
}

// GetByToken returns the record for the supplied plaintext token. The
// plaintext is hashed before the lookup; the raw token is never persisted.
func (s *BackendInviteStore) GetByToken(ctx context.Context, token string) (*InviteRecord, error) {
	hash := tokenHash(token)
	data, err := s.kv.Get(ctx, inviteKey(hash))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrInviteNotFound
		}
		return nil, err
	}
	var rec InviteRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.invite.get", Err: err}
	}
	return &rec, nil
}

// MarkUsed atomically flips the Used bit. Bootstrap invites are single-use
// and the surrounding handler already validates the call sequence, so the
// read-modify-write below is safe under single-writer usage.
func (s *BackendInviteStore) MarkUsed(ctx context.Context, token string) error {
	hash := tokenHash(token)
	data, err := s.kv.Get(ctx, inviteKey(hash))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrInviteNotFound
		}
		return err
	}
	var rec InviteRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.invite.mark_used", Err: err}
	}
	if rec.Used {
		return ErrInviteUsed
	}
	rec.Used = true
	out, err := json.Marshal(&rec)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.invite.mark_used", Err: err}
	}
	return s.kv.Put(ctx, inviteKey(hash), out)
}

// GC drops expired or already-consumed records.
func (s *BackendInviteStore) GC(ctx context.Context) error {
	now := time.Now()
	return s.kv.Scan(ctx, keyPrefixInvite, func(key string, value []byte) error {
		var rec InviteRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			// Drop malformed rows so corruption does not block GC.
			_ = s.kv.Delete(ctx, key)
			return nil
		}
		if rec.Used || now.After(rec.ExpiresAt) {
			_ = s.kv.Delete(ctx, key)
		}
		return nil
	})
}
