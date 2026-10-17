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

// Package service provides per-key authorization and key-use audit for the KMS.
//
// # Design
//
// Per-key authorization evaluates the persisted KeyPolicy (AllowedPrincipals /
// DeniedPrincipals / AllowedOperations) stored in the Raft-backed KVStore under
// the key "kms:policy:<keyID>". The Subject is extracted from the request
// context via auth.SubjectFromContext. When no policy is stored, the check is
// allow-all (key owner has full access; explicit Deny entries still apply).
//
// The authz gate is fail-closed:
//   - No authenticated Subject in context → ErrForbidden (unless auth is
//     disabled globally or this is a system context).
//   - Policy cannot be loaded from KV → deny.
//   - Policy evaluation yields deny → ErrForbidden.
//
// Internal system operations (scheduler, re-encryption daemon) mark their
// context with auth.NewSystemContext and bypass per-key authz entirely;
// this path must never be reachable from network-originated requests.
//
// Key-use audit (AU-2)
//
// KMSKeyUseAuditLogger extends KMSAuditLogger with LogKeyUse, which records
// principal, keyID, operation, allow/deny outcome, and timestamp for each
// crypto call. Emit occurs from checkKeyAuthz so both allow and deny outcomes
// are captured uniformly regardless of transport.
package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// kmsKeyPolicyPrefix is the KVStore namespace for persisted per-key policies.
const kmsKeyPolicyPrefix = "kms:policy:"

// keyUseOutcome records whether a key-use operation was allowed or denied.
type keyUseOutcome string

const (
	keyUseOutcomeAllow keyUseOutcome = "allow"
	keyUseOutcomeDeny  keyUseOutcome = "deny"
)

// KMSKeyUseAuditLogger extends KMSAuditLogger with per-operation key-use logging.
// Implementations must be safe for concurrent use.
type KMSKeyUseAuditLogger interface {
	KMSAuditLogger

	// LogKeyUse records a single key-use operation for AU-2 compliance.
	//   - principal: authenticated subject ID (username or user ID)
	//   - keyID:     the key involved in the operation
	//   - operation: one of "encrypt", "decrypt", "sign", "verify", "generate_data_key"
	//   - outcome:   "allow" or "deny"
	//   - timestamp: when the operation occurred
	LogKeyUse(ctx context.Context, principal, keyID, operation string, outcome keyUseOutcome, timestamp time.Time) error
}

// kmsKeyPolicyKey returns the KVStore key for a given keyID's policy.
func kmsKeyPolicyKey(keyID string) string {
	return kmsKeyPolicyPrefix + keyID
}

// persistKeyPolicy serialises and stores policy in the KVStore.
// It does NOT validate input; callers must validate before calling.
// When kvStore is nil (not configured) the operation is a no-op so that
// deployments without Raft storage continue to function with in-memory-only
// policy semantics (policies are not replicated across nodes).
func (k *kmsServiceImpl) persistKeyPolicy(ctx context.Context, policy *KeyPolicy) error {
	if k.kvStore == nil {
		return nil
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return NewError(errors.ErrInternal, "kms.persistKeyPolicy", "failed to marshal policy")
	}
	if putErr := k.kvStore.Put(ctx, kmsKeyPolicyKey(policy.KeyID), data); putErr != nil {
		return WrapError(putErr, errors.ErrStorage, "kms.persistKeyPolicy", "failed to store policy")
	}
	return nil
}

// loadKeyPolicy reads the stored policy for keyID from the KVStore.
// Returns (nil, nil) when no policy has been stored (first use).
func (k *kmsServiceImpl) loadKeyPolicy(ctx context.Context, keyID string) (*KeyPolicy, error) {
	if k.kvStore == nil {
		return nil, nil
	}
	data, err := k.kvStore.Get(ctx, kmsKeyPolicyKey(keyID))
	if err != nil {
		// Not found is not an error; any other error is surfaced.
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, WrapError(err, errors.ErrStorage, "kms.loadKeyPolicy", "failed to load policy")
	}
	if len(data) == 0 {
		return nil, nil
	}
	var policy KeyPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, NewError(errors.ErrInternal, "kms.loadKeyPolicy", "failed to unmarshal policy")
	}
	return &policy, nil
}

// deleteKeyPolicy removes the stored policy for keyID from the KVStore.
func (k *kmsServiceImpl) deleteKeyPolicy(ctx context.Context, keyID string) error {
	if k.kvStore == nil {
		return nil
	}
	return k.kvStore.Delete(ctx, kmsKeyPolicyKey(keyID))
}

// checkKeyAuthz enforces per-key authorization for the given operation and
// emits a key-use audit event for both allow and deny outcomes.
//
// Fail-closed semantics:
//  1. System contexts (auth.IsSystemContext) bypass authz entirely.
//  2. When authz is disabled globally (k.authzEnabled == false), allow all.
//  3. When no authenticated Subject is in ctx, deny (ErrForbidden).
//  4. When the KVStore is unavailable or the policy cannot be loaded, deny.
//  5. Default-allow when no policy has been stored for the key (any
//     authenticated principal may use it until a policy is set).
//  6. Explicit deny entries take precedence over allow entries.
func (k *kmsServiceImpl) checkKeyAuthz(ctx context.Context, keyID, operation string) error {
	// System-context bypass: scheduler / daemon internal operations.
	if auth.IsSystemContext(ctx) {
		return nil
	}

	// Global authz disable (test/dev environments).
	if !k.authzEnabled {
		return nil
	}

	// Extract authenticated principal from context.
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated {
		k.emitKeyUseAudit(ctx, "", keyID, operation, keyUseOutcomeDeny)
		return NewError(errors.ErrForbidden, "kms.checkKeyAuthz", "no authenticated principal in request context")
	}

	// Load the stored policy for this key.
	policy, err := k.loadKeyPolicy(ctx, keyID)
	if err != nil {
		k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeDeny)
		return WrapError(err, errors.ErrForbidden, "kms.checkKeyAuthz", "failed to evaluate key policy")
	}

	// No policy stored → default-allow for any authenticated principal.
	if policy == nil {
		k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeAllow)
		return nil
	}

	// Check AllowedPrincipals: if set, the subject must appear.
	if len(policy.AllowedPrincipals) > 0 && !k.principalInList(subject, policy.AllowedPrincipals) {
		k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeDeny)
		return NewError(errors.ErrForbidden, "kms.checkKeyAuthz", "principal not in allowed_principals for key "+keyID)
	}

	// Check DeniedPrincipals: explicit deny overrides everything.
	if k.principalInList(subject, policy.DeniedPrincipals) {
		k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeDeny)
		return NewError(errors.ErrForbidden, "kms.checkKeyAuthz", "principal in denied_principals for key "+keyID)
	}

	// Check AllowedOperations: if set, operation must be listed.
	if len(policy.AllowedOperations) > 0 {
		op := operationToKeyUsage(operation)
		if !k.operationAllowed(op, policy.AllowedOperations) {
			k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeDeny)
			return NewError(errors.ErrForbidden, "kms.checkKeyAuthz",
				"operation "+operation+" not permitted by key policy for "+keyID)
		}
	}

	// Check policy expiry.
	if policy.ExpiresAt != nil && time.Now().After(*policy.ExpiresAt) {
		k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeDeny)
		return NewError(errors.ErrForbidden, "kms.checkKeyAuthz", "key policy has expired for "+keyID)
	}

	k.emitKeyUseAudit(ctx, subject.ID, keyID, operation, keyUseOutcomeAllow)
	return nil
}

// principalInList checks whether the subject (by ID or Username) appears in
// the given principal list.  List entries may be user IDs or usernames.
func (k *kmsServiceImpl) principalInList(subject interfaces.Subject, list []string) bool {
	for _, p := range list {
		if p == subject.ID || p == subject.Username {
			return true
		}
	}
	return false
}

// operationAllowed checks whether op appears in the allowed list.
func (k *kmsServiceImpl) operationAllowed(op KeyUsage, allowed []KeyUsage) bool {
	for _, a := range allowed {
		if a == op {
			return true
		}
	}
	return false
}

// operationToKeyUsage maps an operation string to its KeyUsage constant.
// "generate_data_key" uses KeyUsageWrapKey because it wraps a DEK with the KEK.
var operationToKeyUsageMap = map[string]KeyUsage{
	"encrypt":           KeyUsageEncrypt,
	"decrypt":           KeyUsageDecrypt,
	"sign":              KeyUsageSign,
	"verify":            KeyUsageVerify,
	"generate_data_key": KeyUsageWrapKey,
	"generate_mac":      KeyUsageGenerateMAC,
	"verify_mac":        KeyUsageVerifyMAC,
}

func operationToKeyUsage(op string) KeyUsage {
	if ku, ok := operationToKeyUsageMap[op]; ok {
		return ku
	}
	// Unknown operations are mapped to the empty string; they will not match
	// any specific AllowedOperation and will therefore be denied when
	// AllowedOperations is non-empty.
	return ""
}

// emitKeyUseAudit emits a key-use audit event when a KMSKeyUseAuditLogger is
// configured. Audit logging is best-effort; errors are logged but never
// returned to callers so a logging failure cannot silently suppress authz.
func (k *kmsServiceImpl) emitKeyUseAudit(ctx context.Context, principal, keyID, operation string, outcome keyUseOutcome) {
	if k.auditLogger == nil {
		return
	}
	useLogger, ok := k.auditLogger.(KMSKeyUseAuditLogger)
	if !ok {
		return
	}
	if err := useLogger.LogKeyUse(ctx, principal, keyID, operation, outcome, time.Now()); err != nil {
		if k.logger != nil {
			k.logger.WithError(err).WithFields(map[string]interface{}{
				"key_id":    keyID,
				"operation": operation,
				"outcome":   string(outcome),
			}).Warn("failed to write key-use audit log")
		}
	}
}
