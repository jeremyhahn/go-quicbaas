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
	"errors"
	"sync"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// RBACAuthorizer is the SINGLE decision point PolicyEngine.Evaluate
// delegates to for every request (P6.1 end-state of the unified login-plane
// plan). On a composed node the implementation wraps qrdb's own RBAC
// authorizer decorated with a live-reloaded ABAC policy set (abac.Decorator
// over auth.Authorizer): explicit ABAC deny overrides, explicit ABAC allow
// grants beyond RBAC, and "no ABAC policy matches" falls through to RBAC —
// ALL evaluated by qrdb as one atomic decision, not layered locally by
// pkg/iam. This interface carries no qrdb dependency into pkg/iam; pkg/server
// injects the real implementation (backed by the embedded qrdb node's
// RBACClient) at composition time, mirroring how NodeAuditLogger backs
// iam.AuditLogger without pkg/iam importing qrdb directly.
type RBACAuthorizer interface {
	// Authorize returns nil when subject is permitted to perform action on
	// resource, and a non-nil error (denial or evaluation failure)
	// otherwise.
	Authorize(ctx context.Context, subject *Subject, resource, action string) error
}

// ABACSync is the seam PolicyEngine drives after every policy CRUD mutation
// (Create/Update/Delete), and once at startup, to keep qrdb's live-reloaded
// ABAC decorator synchronized with the FULL current policy store across
// every tenant plus the system tenant's platform-wide policies. Sync must be
// atomic and all-or-nothing on the qrdb side: an invalid policy set is
// rejected by the underlying Reload and leaves the previously active set
// fully in force (see go-qrdb pkg/auth/abac.Decorator.Reload). This
// interface carries no qrdb dependency into pkg/iam; pkg/server injects the
// real implementation (backed by the embedded qrdb node's
// ABACPolicyReloader) at composition time.
type ABACSync interface {
	// Sync recomputes the complete cross-tenant policy set from storage and
	// atomically reloads it into the live authorizer. A failed reload
	// (invalid policy) returns its error and leaves enforcement unchanged.
	Sync(ctx context.Context) error
}

// PolicySimulator is the seam PolicyEngine.SimulatePolicy delegates to for
// evaluating a single draft policy against a check request in isolation,
// without persisting the policy or mutating the live enforcement state
// ABACSync maintains. This interface carries no qrdb dependency into
// pkg/iam; pkg/server injects the real implementation (a scratch qrdb ABAC
// decorator scoped to exactly the one draft policy) at composition time.
type PolicySimulator interface {
	// Simulate evaluates policy against req in isolation. The returned
	// CheckResponse's Allowed/MatchedPolicy/MatchedRule/Reason reflect the
	// draft policy's own rules only (no other stored policy and no RBAC
	// fallback participate).
	Simulate(ctx context.Context, policy *Policy, req *CheckRequest) (*CheckResponse, error)
}

// PolicyRepair is the seam PolicyEngine uses to let the system admin repair
// a held tenant's stored policies one at a time. A tenant is held at unseal
// while any of its stored policies fails to load into the live authorizer,
// and the sync that follows every policy write fails for as long as one such
// policy is left, so with several broken policies no single repair could
// pass it. During a repair the engine checks the one written policy on its
// own and keeps the write when the sync still fails; the tenant stays held
// until its unseal checks load the whole set. This interface carries no qrdb
// dependency into pkg/iam; pkg/server injects the real implementation at
// composition time.
type PolicyRepair interface {
	// TenantHeld reports whether tenantID is closed to requests because its
	// unseal has not passed its checks.
	TenantHeld(tenantID string) bool
	// CompilePolicy returns an error when policy alone would not load into
	// the live authorizer.
	CompilePolicy(policy *Policy) error
}

// PolicySimulatorUnavailableError is returned by SimulatePolicy when no
// PolicySimulator was wired into PolicyEngineOptions (embedded/test
// contexts with no qrdb node composed).
type PolicySimulatorUnavailableError struct{}

// Error implements the error interface.
func (e *PolicySimulatorUnavailableError) Error() string {
	return "iam: policy simulator not available"
}

// PolicyEngine provides policy management and evaluation. Evaluate/Check
// delegate every decision to opts.RBACAuthorizer (the composed qrdb
// RBAC+ABAC authorizer); PolicyEngine itself performs no local policy
// compilation or evaluation (P6.1 end-state of the unified login-plane
// plan — the duplicated local ABAC engine, previously kept as a
// live-reloadable layer, was deleted once qrdb shipped its own live-reload
// seam).
type PolicyEngine struct {
	storage PolicyStorage
	cache   *PolicyCache
	opts    PolicyEngineOptions
	mu      sync.RWMutex
}

// NewPolicyEngine creates a new policy engine. opts.RBACAuthorizer becomes
// Evaluate's single decision point for every request; opts.ABACSync (when
// set) is driven after every CRUD mutation to keep qrdb's live ABAC
// decorator synchronized with the policy store.
func NewPolicyEngine(opts PolicyEngineOptions) *PolicyEngine {
	return &PolicyEngine{
		storage: opts.Storage,
		cache:   opts.Cache,
		opts:    opts,
	}
}

// CreatePolicy creates a new policy, then synchronizes qrdb's live ABAC
// decorator with the full current policy store. A storage refusal is
// returned unchanged and compensates nothing. A failed sync rejects the
// write: a PolicyCreateReverter removes only the record this create wrote
// (without one, the policy is deleted) and the sync error is returned. A
// name held by any record, even one that does not decode, refuses the create.
func (e *PolicyEngine) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Set tenant if not already set (before validation)
	if policy != nil && policy.Metadata.Tenant == "" {
		policy.Metadata.Tenant = tenantID
	}

	// Validate policy
	if err := e.ValidatePolicy(policy); err != nil {
		return err
	}

	// Verify tenant matches
	if policy.Metadata.Tenant != tenantID {
		return &InvalidPolicyError{
			Reason: "policy tenant does not match request tenant",
		}
	}

	// Check if policy already exists
	existing, err := e.storage.GetPolicy(ctx, tenantID, policy.Metadata.Name)
	var loadErr *StoredPolicyDecodeError
	if (err == nil && existing != nil) || errors.As(err, &loadErr) {
		return &PolicyExistsError{
			TenantID: tenantID,
			Name:     policy.Metadata.Name,
		}
	}
	if err != nil && !dberrors.Is(err, dberrors.ErrNotFound) {
		return err
	}

	// Store policy; a refusal wrote nothing of ours, so nothing is compensated.
	if err := e.storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		return err
	}

	if err := e.syncABAC(ctx); err != nil {
		// Reject the write: remove only the record this create wrote.
		if r, ok := e.storage.(PolicyCreateReverter); ok {
			if rerr := r.RevertCreatedPolicy(ctx, tenantID, policy); rerr != nil {
				err = errors.Join(err, rerr)
			}
		} else {
			_ = e.storage.DeletePolicy(ctx, tenantID, policy.Metadata.Name)
		}
		if e.cache != nil {
			e.cache.InvalidateAll(tenantID)
		}
		return err
	}

	// Invalidate cache
	if e.cache != nil {
		e.cache.InvalidateAll(tenantID)
	}

	return nil
}

// GetPolicy retrieves a policy by name.
func (e *PolicyEngine) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// A record that does not decode strictly is returned as its flagged
	// entry (LoadError set, no rules), the same entry ListPolicies lists.
	policy, err := repairableRecord(e.storage.GetPolicy(ctx, tenantID, name))
	if err != nil {
		return nil, err
	}

	if policy == nil {
		return nil, &PolicyNotFoundTypeError{
			TenantID: tenantID,
			Name:     name,
		}
	}

	return policy, nil
}

// UpdatePolicy updates an existing policy, then synchronizes qrdb's live
// ABAC decorator with the full current policy store. A failed sync rejects
// the write: the previous policy value is restored in storage (best-effort
// compensation) and the sync error is returned. A PolicyWriteSnapshotter
// storage restores its snapshot instead, after a failed storage write too,
// guarded: only a record still holding what this write stored is put back.
// The system admin's repair of a held tenant's policy (see PolicyRepair) is
// the one exception: the policy must compile on its own before anything is
// written, and the write then stays stored when the sync fails, because
// another of the tenant's stored policies may still be broken.
func (e *PolicyEngine) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// A flagged entry (a fetched record that did not decode) is never
	// written back: only a corrected document repairs the record.
	if policy != nil && policy.LoadError != nil {
		return policy.LoadError
	}

	// Validate policy
	if err := e.ValidatePolicy(policy); err != nil {
		return err
	}

	// Verify tenant matches
	if policy.Metadata.Tenant != tenantID {
		return &InvalidPolicyError{
			Reason: "policy tenant does not match request tenant",
		}
	}

	// A repair of a held tenant's policy must compile on its own: the sync
	// below cannot vouch for it while another stored policy still fails.
	repair := e.repairing(ctx, tenantID)
	if repair {
		if err := e.opts.Repair.CompilePolicy(policy); err != nil {
			return &InvalidPolicyError{
				TenantID: tenantID,
				Name:     policy.Metadata.Name,
				Reason:   "policy does not compile: " + err.Error(),
			}
		}
	}

	// Check if policy exists
	existing, err := repairableRecord(e.storage.GetPolicy(ctx, tenantID, policy.Metadata.Name))
	if err != nil {
		return err
	}
	if existing == nil {
		return &PolicyNotFoundTypeError{
			TenantID: tenantID,
			Name:     policy.Metadata.Name,
		}
	}

	// Snapshot every record the write may touch; a failed snapshot writes nothing.
	var restore func(context.Context) error
	if s, ok := e.storage.(PolicyWriteSnapshotter); ok {
		if restore, err = s.SnapshotPolicy(ctx, tenantID, policy.Metadata.Name, policy); err != nil {
			return err
		}
	}
	reject := func(err error) error {
		if restore != nil {
			if rerr := restore(ctx); rerr != nil {
				err = errors.Join(err, rerr)
			}
		} else {
			// Reject the write: compensate by restoring the previous value.
			_ = e.storage.UpdatePolicy(ctx, tenantID, existing)
		}
		if e.cache != nil {
			e.cache.InvalidateAll(tenantID)
		}
		return err
	}

	// Update policy
	if err := e.storage.UpdatePolicy(ctx, tenantID, policy); err != nil {
		if restore == nil {
			return err
		}
		return reject(err)
	}

	// A held tenant's repair stays stored when the sync fails: another of
	// its stored policies may still be broken, and its unseal checks load
	// the whole set before the tenant serves a request.
	if err := e.syncABAC(ctx); err != nil && !repair {
		return reject(err)
	}

	// Invalidate cache
	if e.cache != nil {
		e.cache.InvalidateAll(tenantID)
	}

	return nil
}

// DeletePolicy deletes a policy by name, then synchronizes qrdb's live ABAC
// decorator with the full current policy store. A failed sync rejects the
// write: the deleted policy is re-created in storage (best-effort
// compensation) and the sync error is returned. A PolicyWriteSnapshotter
// storage restores its snapshot instead, after a failed storage delete too,
// guarded: only a record this delete left absent is put back. The system
// admin's delete of a held tenant's policy (see PolicyRepair) is the one
// exception: it stays deleted when the sync fails, because another of the
// tenant's stored policies may still be broken.
func (e *PolicyEngine) DeletePolicy(ctx context.Context, tenantID, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if policy exists
	existing, err := e.storage.GetPolicy(ctx, tenantID, name)
	existing, err = repairableRecord(existing, err)
	if err != nil {
		return err
	}
	if existing == nil {
		return &PolicyNotFoundTypeError{
			TenantID: tenantID,
			Name:     name,
		}
	}

	repair := e.repairing(ctx, tenantID)

	// Snapshot every record the delete may touch; a failed snapshot deletes nothing.
	var restore func(context.Context) error
	if s, ok := e.storage.(PolicyWriteSnapshotter); ok {
		if restore, err = s.SnapshotPolicy(ctx, tenantID, name, nil); err != nil {
			return err
		}
	}
	reject := func(err error) error {
		if restore != nil {
			if rerr := restore(ctx); rerr != nil {
				err = errors.Join(err, rerr)
			}
		} else {
			// Reject the write: compensate by re-creating the deleted policy.
			_ = e.storage.CreatePolicy(ctx, tenantID, existing)
		}
		if e.cache != nil {
			e.cache.InvalidateAll(tenantID)
		}
		return err
	}

	// Delete policy
	if err := e.storage.DeletePolicy(ctx, tenantID, name); err != nil {
		if restore == nil {
			return err
		}
		return reject(err)
	}

	// A held tenant's repair stays stored when the sync fails: another of
	// its stored policies may still be broken, and its unseal checks load
	// the whole set before the tenant serves a request.
	if err := e.syncABAC(ctx); err != nil && !repair {
		return reject(err)
	}

	// Invalidate cache
	if e.cache != nil {
		e.cache.InvalidateAll(tenantID)
	}

	return nil
}

// ListPolicies lists policies for a tenant.
func (e *PolicyEngine) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.storage.ListPolicies(ctx, tenantID, opts)
}

// repairableRecord lets a get, update or delete reach a stored record that
// does not decode strictly: a get reports it, and replacing or removing it
// is how an operator repairs it. The record stands in as its flagged entry,
// which applies no rules.
func repairableRecord(existing *Policy, err error) (*Policy, error) {
	var loadErr *StoredPolicyDecodeError
	if errors.As(err, &loadErr) {
		return flaggedPolicy(loadErr), nil
	}
	return existing, err
}

// ValidatePolicy validates a policy for correctness.
func (e *PolicyEngine) ValidatePolicy(policy *Policy) error {
	if policy == nil {
		return &InvalidPolicyError{Reason: "policy is nil"}
	}

	return policy.Validate()
}

// repairing reports whether a write to tenantID is the system admin's repair
// of a held tenant's policy: a PolicyRepair is wired, ctx carries the repair
// mark for exactly that tenant, and the tenant is still held. Only such a
// write may outlive a failed sync. Callers already hold e.mu.
func (e *PolicyEngine) repairing(ctx context.Context, tenantID string) bool {
	return e.opts.Repair != nil && tenantctx.InPolicyRepair(ctx, tenantID) && e.opts.Repair.TenantHeld(tenantID)
}

// syncABAC drives opts.ABACSync (when configured) to recompute the full
// cross-tenant policy set from storage and reload it into qrdb's live ABAC
// decorator. Nil-safe: a nil ABACSync is a no-op success (embedded/test
// contexts with no qrdb node composed). Callers already hold e.mu.
func (e *PolicyEngine) syncABAC(ctx context.Context) error {
	if e.opts.ABACSync == nil {
		return nil
	}
	return e.opts.ABACSync.Sync(ctx)
}

// SyncABAC performs an out-of-band ABAC sync, driving opts.ABACSync exactly
// as every CRUD mutation does. It is the composition root's hook for the
// initial load at startup (post-unseal, once the IAM policy store is
// readable) so the live decorator reflects every previously stored policy
// before the first request is evaluated. It holds the write lock so a reload
// never overlaps another reload or a policy mutation. Nil-safe.
func (e *PolicyEngine) SyncABAC(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.syncABAC(ctx)
}

// SyncABACAfter is SyncABAC with prepare run first under the same write lock:
// prepare does storage work that must not interleave with a policy mutation,
// such as a legacy-policy migration, just before the reload.
func (e *PolicyEngine) SyncABACAfter(ctx context.Context, prepare func(context.Context)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if prepare != nil {
		prepare(ctx)
	}
	return e.syncABAC(ctx)
}

// TestPolicy evaluates a check request against the CURRENT active policies
// (identical to Evaluate; kept as a distinct name for API/RPC parity with
// the historical "dry run against live state" contract).
func (e *PolicyEngine) TestPolicy(ctx context.Context, tenantID string, req *CheckRequest) (*CheckResponse, error) {
	return e.Evaluate(ctx, req)
}

// Evaluate evaluates an authorization request by delegating to
// opts.RBACAuthorizer — the single decision point for RBAC+ABAC on a
// composed node. A nil RBACAuthorizer (embedded/test contexts with no qrdb
// node composed) default-denies.
func (e *PolicyEngine) Evaluate(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	startTime := time.Now()

	if req == nil || req.Subject.ID == "" {
		return &CheckResponse{
			Allowed:          false,
			Reason:           "invalid request: subject required",
			EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
		}, nil
	}

	e.mu.RLock()
	rbac := e.opts.RBACAuthorizer
	e.mu.RUnlock()

	if rbac == nil {
		return &CheckResponse{
			Allowed:          false,
			Reason:           "no authorizer configured",
			EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
		}, nil
	}

	if authErr := rbac.Authorize(ctx, &req.Subject, req.Resource, req.Action); authErr != nil {
		return &CheckResponse{
			Allowed:          false,
			Reason:           "denied: " + authErr.Error(),
			EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
		}, nil
	}
	return &CheckResponse{
		Allowed:          true,
		Reason:           "allowed",
		EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
	}, nil
}

// ClearCache clears the policy cache. Retained for API compatibility; qrdb's
// live ABAC decorator (not this cache) is the actual decision-affecting
// state, so clearing this cache has no effect on enforcement.
func (e *PolicyEngine) ClearCache(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cache != nil {
		e.cache.Clear()
	}

	return nil
}

// SimulatePolicy evaluates a draft policy against a test request without
// persisting it, by delegating to opts.Simulator (a scratch qrdb ABAC
// decorator scoped to exactly the one draft policy). Returns
// PolicySimulatorUnavailableError when no Simulator is configured.
//
// SCOPE NOTE: unlike the deleted local ABAC engine, qrdb's Authorizer
// interface exposes no step-by-step evaluation trace and no matched-rule
// name on the ALLOW path (Decorator.Authorize returns nil on allow with no
// match metadata; only the DENY path's PolicyDeniedError carries
// Policy/Rule names). SimulatePolicy's EvaluationSteps/IncludeTrace output
// and the ALLOW-path MatchedRule are therefore always empty — this is a
// precise, reported capability gap, not an approximation of the
// allow/deny decision itself, which remains exact.
func (e *PolicyEngine) SimulatePolicy(ctx context.Context, req *PolicySimulationRequest) (*PolicySimulationResult, error) {
	if req == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.simulate", "request is nil", nil)
	}
	if req.Policy == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.simulate", "policy is nil", nil)
	}
	if req.CheckRequest == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.simulate", "check request is nil", nil)
	}

	startTime := time.Now()

	// A flagged entry is a stored record that is never applied.
	if req.Policy.LoadError != nil {
		return nil, req.Policy.LoadError
	}
	if err := req.Policy.Validate(); err != nil {
		return nil, NewIAMError(ErrPolicyInvalid, "iam.policy.simulate", "policy validation failed", err)
	}

	e.mu.RLock()
	sim := e.opts.Simulator
	e.mu.RUnlock()
	if sim == nil {
		return nil, &PolicySimulatorUnavailableError{}
	}

	resp, err := sim.Simulate(ctx, req.Policy, req.CheckRequest)
	if err != nil {
		return nil, NewIAMError(ErrPolicyEvaluationFailed, "iam.policy.simulate", "simulation failed", err)
	}

	result := &PolicySimulationResult{
		Allowed:          resp.Allowed,
		MatchedPolicy:    resp.MatchedPolicy,
		MatchedRule:      resp.MatchedRule,
		Reason:           resp.Reason,
		EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
	}
	if req.IncludeTrace {
		result.EvaluationSteps = []EvaluationStep{{
			StepNumber: 1,
			PolicyName: req.Policy.Metadata.Name,
			Action:     "evaluate",
			Result:     resp.Allowed,
			Details:    "qrdb ABAC decorator does not expose a step-by-step trace; see SimulatePolicy's scope note",
		}}
	}
	return result, nil
}

// GetEffectivePermissions calculates the effective permissions for a subject
// by delegating each resource-pattern/action combination to
// opts.RBACAuthorizer — the SAME live composed decision Evaluate uses, so
// the reported permissions can never disagree with a real Check call.
//
// SCOPE NOTE: unlike the deleted local ABAC engine, qrdb's Authorizer
// interface does not report WHICH policy/rule granted an ALLOW (only a DENY
// carries the matched policy/rule via PolicyDeniedError), so
// PermissionSet.Sources is populated on the Denied path only; the Allowed
// path's Sources is always empty. This is a precise, reported capability
// gap, not an approximation of the allow/deny decision itself.
func (e *PolicyEngine) GetEffectivePermissions(ctx context.Context, tenantID string, req *EffectivePermissionsRequest) (*EffectivePermissionsResult, error) {
	if req == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.effectivePermissions", "request is nil", nil)
	}
	if req.Subject == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.effectivePermissions", "subject is nil", nil)
	}
	if len(req.ResourcePatterns) == 0 {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.effectivePermissions", "resource patterns are required", nil)
	}

	startTime := time.Now()

	if req.Subject.TenantID == "" {
		req.Subject.TenantID = tenantID
	}

	actionsToCheck := req.Actions
	if len(actionsToCheck) == 0 {
		actionsToCheck = []string{"create", "read", "update", "delete", "list", "admin", "*"}
	}

	e.mu.RLock()
	rbac := e.opts.RBACAuthorizer
	e.mu.RUnlock()

	result := &EffectivePermissionsResult{
		Subject:     req.Subject,
		Permissions: make(map[string]*PermissionSet),
	}

	for _, resourcePattern := range req.ResourcePatterns {
		permSet := &PermissionSet{
			Allowed: make([]string, 0),
			Denied:  make([]string, 0),
			Sources: make(map[string][]string),
		}
		result.Permissions[resourcePattern] = permSet

		if rbac == nil {
			continue
		}

		for _, action := range actionsToCheck {
			err := rbac.Authorize(ctx, req.Subject, resourcePattern, action)
			if err != nil {
				permSet.Denied = append(permSet.Denied, action)
				if req.IncludeDenyDetails {
					permSet.Sources[action] = []string{err.Error()}
				}
				continue
			}
			permSet.Allowed = append(permSet.Allowed, action)
		}
	}

	result.EvaluationTimeNs = time.Since(startTime).Nanoseconds()
	return result, nil
}

// matchAction checks if the request action matches any of the rule's actions.
func matchAction(ruleActions []string, requestAction string) bool {
	for _, action := range ruleActions {
		if action == "*" || action == requestAction {
			return true
		}
	}
	return false
}

// MemoryPolicyCache is an in-memory implementation of PolicyCache.
type MemoryPolicyCache struct {
	mu      sync.RWMutex
	cache   map[string][]*Policy
	ttl     time.Duration
	expires map[string]time.Time
}

// NewMemoryPolicyCache creates a new in-memory policy cache.
func NewMemoryPolicyCache(ttl time.Duration) *MemoryPolicyCache {
	return &MemoryPolicyCache{
		cache:   make(map[string][]*Policy),
		expires: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// Get retrieves cached policies for a tenant.
func (c *MemoryPolicyCache) Get(ctx context.Context, tenantID string) ([]*Policy, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check expiration
	if expiry, ok := c.expires[tenantID]; ok {
		if time.Now().After(expiry) {
			// Expired, return nil
			return nil, nil
		}
	}

	policies, ok := c.cache[tenantID]
	if !ok {
		return nil, nil
	}

	return policies, nil
}

// Set stores policies for a tenant.
func (c *MemoryPolicyCache) Set(ctx context.Context, tenantID string, policies []*Policy) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[tenantID] = policies
	if c.ttl > 0 {
		c.expires[tenantID] = time.Now().Add(c.ttl)
	}

	return nil
}

// Invalidate removes cached policies for a tenant.
func (c *MemoryPolicyCache) Invalidate(ctx context.Context, tenantID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, tenantID)
	delete(c.expires, tenantID)

	return nil
}

// Clear removes all cached policies.
func (c *MemoryPolicyCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string][]*Policy)
	c.expires = make(map[string]time.Time)

	return nil
}
