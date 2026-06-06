//go:build iam

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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// tenantLister enumerates the tenants known to the system so cross-tenant
// maintenance sweeps (CleanupExpired / DeleteExpired*) can iterate per tenant —
// injecting each tenant into the operation context — instead of performing a
// single global scan that would defeat barrier isolation. It is satisfied by
// iam.TenantManager (ListTenants). When nil, the sweep degrades to a
// best-effort current-context-tenant cleanup (see each CleanupExpired doc).
type tenantLister interface {
	ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error)
}

// sweepTenantIDs returns the tenants a maintenance sweep must visit: every known
// tenant when a tenantLister is configured, otherwise just the context tenant.
// Sweeping per tenant (binding each id into ctx) keeps each tenant's barrier
// store isolated; a nil lister limits the sweep to the current context tenant.
func sweepTenantIDs(ctx context.Context, tenants tenantLister) ([]string, error) {
	if tenants == nil {
		return []string{TenantFromContext(ctx)}, nil
	}
	all, err := tenants.ListTenants(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(all))
	for _, t := range all {
		ids = append(ids, t.ID)
	}
	return ids, nil
}

// ensureTenantEncoded returns id unchanged when it already carries the tenant
// prefix, otherwise tenant-encodes it. This keeps Save* idempotent across
// re-saves of an already-encoded record (e.g. attempt-count updates) so the id
// does not accumulate prefixes.
func ensureTenantEncoded(tenantID, id string) string {
	if tenantID == "" {
		return id
	}
	decoded, _ := decodeTenantID(id)
	if decoded == tenantID {
		return id
	}
	return encodeTenantID(tenantID, id)
}

// MFA storage key helpers. All MFA state lives under the canonical __iam:
// namespace so it is encrypted-at-rest in the owning tenant's barrier when the
// tenant is barrier-isolated (the routing KVStore selects that tenant's DEK).
//
// Secrets / backup codes are keyed by the explicit (userID) the caller passes
// alongside an explicit tenantID, so they route directly. Enrollments and
// challenges are looked up by-ID with no tenant in context, so their ids are
// tenant-encoded (see tenant_encoded_id.go) and a per-user index lets the
// by-user verbs enumerate them within the tenant's store.
const (
	keyPrefixMFASecret      = KeyPrefixIAM + "mfa/secret/"
	keyPrefixMFABackup      = KeyPrefixIAM + "mfa/backup/"
	keyPrefixMFAEnrollment  = KeyPrefixIAM + "mfa/enroll/"
	keyPrefixMFAChallenge   = KeyPrefixIAM + "mfa/challenge/"
	keyPrefixMFAEnrollIndex = KeyPrefixIAM + "mfa/enroll_idx/"
	keyPrefixMFAChalIndex   = KeyPrefixIAM + "mfa/chal_idx/"
)

func mfaSecretKey(userID string) string { return keyPrefixMFASecret + userID }
func mfaBackupKey(userID string) string { return keyPrefixMFABackup + userID }
func mfaEnrollmentKey(id string) string { return keyPrefixMFAEnrollment + id }
func mfaChallengeKey(id string) string  { return keyPrefixMFAChallenge + id }
func mfaEnrollIndexKey(userID, id string) string {
	return keyPrefixMFAEnrollIndex + userID + "/" + id
}
func mfaEnrollIndexPrefix(userID string) string { return keyPrefixMFAEnrollIndex + userID + "/" }
func mfaChalIndexKey(userID, id string) string {
	return keyPrefixMFAChalIndex + userID + "/" + id
}
func mfaChalIndexPrefix(userID string) string { return keyPrefixMFAChalIndex + userID + "/" }

// BackendMFAStorage implements MFAStorage over a per-tenant routing KVStore.
// MFA secrets are security-critical: routing every operation through the tenant
// barrier means secrets are encrypted-at-rest with the tenant's own DEK, and a
// sealed barrier tenant fails closed (the routing KVStore surfaces
// tenant.ErrTenantSealed and never falls back to the shared store).
type BackendMFAStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendMFAStorage constructs a persistent, per-tenant MFA store. The
// tenants lister is optional and only used by CleanupExpired to sweep across
// every tenant; when nil, CleanupExpired falls back to the current context
// tenant only.
func NewBackendMFAStorage(kv interfaces.KVStore, tenants tenantLister) *BackendMFAStorage {
	return &BackendMFAStorage{kv: kv, tenants: tenants}
}

var _ MFAStorage = (*BackendMFAStorage)(nil)

// withTenant returns ctx bound to tenantID so the routing KVStore selects that
// tenant's store. An empty tenantID leaves the context's tenant in place.
func withTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return ContextWithTenant(ctx, tenantID)
}

// GetSecret routes by the explicit tenantID and reads the per-user secret.
func (s *BackendMFAStorage) GetSecret(ctx context.Context, userID, tenantID string) (*MFASecret, error) {
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, mfaSecretKey(userID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrMFANotEnrolled
		}
		return nil, err
	}
	var secret MFASecret
	if err := json.Unmarshal(data, &secret); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.secret.get", Err: err}
	}
	return &secret, nil
}

// SaveSecret routes by the secret's TenantID and writes the per-user secret.
func (s *BackendMFAStorage) SaveSecret(ctx context.Context, secret *MFASecret) error {
	if err := secret.Validate(); err != nil {
		return err
	}
	ctx = withTenant(ctx, secret.TenantID)
	data, err := json.Marshal(secret)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.secret.save", Err: err}
	}
	return s.kv.Put(ctx, mfaSecretKey(secret.UserID), data)
}

// DeleteSecret routes by the explicit tenantID and removes the per-user secret.
func (s *BackendMFAStorage) DeleteSecret(ctx context.Context, userID, tenantID string) error {
	ctx = withTenant(ctx, tenantID)
	return s.kv.Delete(ctx, mfaSecretKey(userID))
}

// GetEnrollment decodes the tenant from the enrollment ID, routes to that
// tenant's store, and reads the enrollment.
func (s *BackendMFAStorage) GetEnrollment(ctx context.Context, enrollmentID string) (*MFAEnrollment, error) {
	tenantID, fullID := decodeTenantID(enrollmentID)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, mfaEnrollmentKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrMFAEnrollmentNotFound
		}
		return nil, err
	}
	var e MFAEnrollment
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.enrollment.get", Err: err}
	}
	return &e, nil
}

// SaveEnrollment tenant-encodes the enrollment ID (mutating the record so the
// caller captures the routable id), routes by the record's TenantID, and writes
// both the record and the per-user index used by the by-user verbs.
func (s *BackendMFAStorage) SaveEnrollment(ctx context.Context, enrollment *MFAEnrollment) error {
	if err := enrollment.Validate(); err != nil {
		return err
	}
	enrollment.ID = ensureTenantEncoded(enrollment.TenantID, enrollment.ID)
	ctx = withTenant(ctx, enrollment.TenantID)
	data, err := json.Marshal(enrollment)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.enrollment.save", Err: err}
	}
	if err := s.kv.Put(ctx, mfaEnrollmentKey(enrollment.ID), data); err != nil {
		return err
	}
	return s.kv.Put(ctx, mfaEnrollIndexKey(enrollment.UserID, enrollment.ID), []byte(enrollment.ID))
}

// DeleteEnrollment decodes the tenant, routes, and removes the record plus its
// index entry. The userID for the index is recovered from the stored record.
func (s *BackendMFAStorage) DeleteEnrollment(ctx context.Context, enrollmentID string) error {
	tenantID, fullID := decodeTenantID(enrollmentID)
	tctx := withTenant(ctx, tenantID)
	// Read the record first to recover the userID for index cleanup.
	if data, err := s.kv.Get(tctx, mfaEnrollmentKey(fullID)); err == nil {
		var e MFAEnrollment
		if json.Unmarshal(data, &e) == nil && e.UserID != "" {
			_ = s.kv.Delete(tctx, mfaEnrollIndexKey(e.UserID, fullID))
		}
	}
	return s.kv.Delete(tctx, mfaEnrollmentKey(fullID))
}

// DeleteEnrollmentsByUser routes by the explicit tenantID and removes every
// enrollment listed in the per-user index.
func (s *BackendMFAStorage) DeleteEnrollmentsByUser(ctx context.Context, userID, tenantID string) error {
	ctx = withTenant(ctx, tenantID)
	ids, err := s.enrollmentIDsForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.kv.Delete(ctx, mfaEnrollmentKey(id)); err != nil {
			return err
		}
		_ = s.kv.Delete(ctx, mfaEnrollIndexKey(userID, id))
	}
	return nil
}

// HasActiveEnrollment routes by the explicit tenantID and reports whether any
// indexed enrollment for the user is non-expired.
func (s *BackendMFAStorage) HasActiveEnrollment(ctx context.Context, userID, tenantID string) (bool, error) {
	ctx = withTenant(ctx, tenantID)
	ids, err := s.enrollmentIDsForUser(ctx, userID)
	if err != nil {
		return false, err
	}
	now := time.Now()
	for _, id := range ids {
		data, err := s.kv.Get(ctx, mfaEnrollmentKey(id))
		if err != nil {
			if dberrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		var e MFAEnrollment
		if json.Unmarshal(data, &e) != nil {
			continue
		}
		if !e.IsExpired() && now.Before(e.ExpiresAt) {
			return true, nil
		}
	}
	return false, nil
}

// enrollmentIDsForUser returns the enrollment ids recorded in the per-user
// index. The caller must have already bound the tenant into ctx.
func (s *BackendMFAStorage) enrollmentIDsForUser(ctx context.Context, userID string) ([]string, error) {
	var ids []string
	err := s.kv.Scan(ctx, mfaEnrollIndexPrefix(userID), func(_ string, value []byte) error {
		ids = append(ids, string(value))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// GetChallenge decodes the tenant from the challenge ID, routes, and reads it.
func (s *BackendMFAStorage) GetChallenge(ctx context.Context, challengeID string) (*MFAChallenge, error) {
	tenantID, fullID := decodeTenantID(challengeID)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, mfaChallengeKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrMFAChallengeNotFound
		}
		return nil, err
	}
	var c MFAChallenge
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.challenge.get", Err: err}
	}
	return &c, nil
}

// SaveChallenge tenant-encodes the challenge ID, routes by the record's
// TenantID, and writes the record plus its per-user index entry.
func (s *BackendMFAStorage) SaveChallenge(ctx context.Context, challenge *MFAChallenge) error {
	if err := challenge.Validate(); err != nil {
		return err
	}
	challenge.ID = ensureTenantEncoded(challenge.TenantID, challenge.ID)
	ctx = withTenant(ctx, challenge.TenantID)
	data, err := json.Marshal(challenge)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.challenge.save", Err: err}
	}
	if err := s.kv.Put(ctx, mfaChallengeKey(challenge.ID), data); err != nil {
		return err
	}
	return s.kv.Put(ctx, mfaChalIndexKey(challenge.UserID, challenge.ID), []byte(challenge.ID))
}

// DeleteChallenge decodes the tenant, routes, and removes the record plus index.
func (s *BackendMFAStorage) DeleteChallenge(ctx context.Context, challengeID string) error {
	tenantID, fullID := decodeTenantID(challengeID)
	tctx := withTenant(ctx, tenantID)
	if data, err := s.kv.Get(tctx, mfaChallengeKey(fullID)); err == nil {
		var c MFAChallenge
		if json.Unmarshal(data, &c) == nil && c.UserID != "" {
			_ = s.kv.Delete(tctx, mfaChalIndexKey(c.UserID, fullID))
		}
	}
	return s.kv.Delete(tctx, mfaChallengeKey(fullID))
}

// DeleteChallengesByUser routes by tenantID and removes every indexed challenge.
func (s *BackendMFAStorage) DeleteChallengesByUser(ctx context.Context, userID, tenantID string) error {
	ctx = withTenant(ctx, tenantID)
	var ids []string
	if err := s.kv.Scan(ctx, mfaChalIndexPrefix(userID), func(_ string, value []byte) error {
		ids = append(ids, string(value))
		return nil
	}); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.kv.Delete(ctx, mfaChallengeKey(id)); err != nil {
			return err
		}
		_ = s.kv.Delete(ctx, mfaChalIndexKey(userID, id))
	}
	return nil
}

// GetBackupCodes routes by the explicit tenantID and reads the per-user codes.
func (s *BackendMFAStorage) GetBackupCodes(ctx context.Context, userID, tenantID string) (*BackupCodeSet, error) {
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, mfaBackupKey(userID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrMFANotEnrolled
		}
		return nil, err
	}
	var codes BackupCodeSet
	if err := json.Unmarshal(data, &codes); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.backup.get", Err: err}
	}
	return &codes, nil
}

// SaveBackupCodes routes by the set's TenantID and writes the per-user codes.
func (s *BackendMFAStorage) SaveBackupCodes(ctx context.Context, codes *BackupCodeSet) error {
	if codes.ID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "backup code set ID is required", nil)
	}
	if codes.UserID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "user ID is required", nil)
	}
	if codes.TenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "tenant ID is required", nil)
	}
	ctx = withTenant(ctx, codes.TenantID)
	data, err := json.Marshal(codes)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.mfa.backup.save", Err: err}
	}
	return s.kv.Put(ctx, mfaBackupKey(codes.UserID), data)
}

// DeleteBackupCodes routes by the explicit tenantID and removes the codes.
func (s *BackendMFAStorage) DeleteBackupCodes(ctx context.Context, userID, tenantID string) error {
	ctx = withTenant(ctx, tenantID)
	return s.kv.Delete(ctx, mfaBackupKey(userID))
}

// CleanupExpired removes expired enrollments and challenges across every
// tenant. It iterates the tenant list (binding each tenant into ctx) so each
// tenant's barrier store is swept independently — no global cross-tenant scan.
//
// When no tenantLister is configured it falls back to sweeping only the tenant
// bound to ctx; this preserves correctness for single-tenant / system callers
// but cannot reach other tenants' stores without enumeration.
func (s *BackendMFAStorage) CleanupExpired(ctx context.Context) (int, error) {
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

// cleanupTenant sweeps the expired enrollment/challenge records for the tenant
// already bound into ctx.
func (s *BackendMFAStorage) cleanupTenant(ctx context.Context) (int, error) {
	now := time.Now()
	count := 0

	type expired struct {
		id     string
		userID string
	}
	var staleEnroll []expired
	if err := s.kv.Scan(ctx, keyPrefixMFAEnrollment, func(_ string, value []byte) error {
		var e MFAEnrollment
		if json.Unmarshal(value, &e) != nil {
			return nil
		}
		if e.IsExpired() || now.After(e.ExpiresAt) {
			staleEnroll = append(staleEnroll, expired{id: e.ID, userID: e.UserID})
		}
		return nil
	}); err != nil {
		return count, err
	}
	for _, e := range staleEnroll {
		if err := s.kv.Delete(ctx, mfaEnrollmentKey(e.id)); err != nil {
			return count, err
		}
		_ = s.kv.Delete(ctx, mfaEnrollIndexKey(e.userID, e.id))
		count++
	}

	var staleChal []expired
	if err := s.kv.Scan(ctx, keyPrefixMFAChallenge, func(_ string, value []byte) error {
		var c MFAChallenge
		if json.Unmarshal(value, &c) != nil {
			return nil
		}
		if c.IsExpired() || now.After(c.ExpiresAt) {
			staleChal = append(staleChal, expired{id: c.ID, userID: c.UserID})
		}
		return nil
	}); err != nil {
		return count, err
	}
	for _, c := range staleChal {
		if err := s.kv.Delete(ctx, mfaChallengeKey(c.id)); err != nil {
			return count, err
		}
		_ = s.kv.Delete(ctx, mfaChalIndexKey(c.userID, c.id))
		count++
	}

	return count, nil
}
