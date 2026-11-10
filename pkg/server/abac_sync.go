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

package server

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// qrdbABACSync implements iam.ABACSync (P6.1 end-state of the unified
// login-plane plan) by recomputing the FULL cross-tenant policy set from
// storage and pushing it into the embedded qrdb node's live ABAC decorator
// via Node.ABACPolicyReloader(). It is the seam iam.PolicyEngine drives
// after every policy CRUD mutation, and once at startup
// (initABACPolicies), so qrdb stays the single authorization decision
// point instead of pkg/iam caching a stale duplicate compiled
// representation.
type qrdbABACSync struct {
	server        *Server
	tenantManager iam.TenantManager
	storage       iam.PolicyStorage
}

// newQRDBABACSync builds the ABACSync the composed PolicyEngine drives.
func newQRDBABACSync(s *Server, tenantManager iam.TenantManager, storage iam.PolicyStorage) *qrdbABACSync {
	return &qrdbABACSync{server: s, tenantManager: tenantManager, storage: storage}
}

// Compile-time interface check.
var _ iam.ABACSync = (*qrdbABACSync)(nil)

// Sync lists every tenant's policies (plus the system tenant's
// platform-wide policies), converts them to qrdb's abac.Policy wire shape,
// and atomically reloads the node's live ABAC decorator. Reload is
// all-or-nothing on the qrdb side: an invalid policy set is rejected and
// leaves the previously active set fully in force.
func (a *qrdbABACSync) Sync(ctx context.Context) error {
	node := a.server.getQRDBNode()
	if node == nil {
		return &QRDBNodeClientUnavailableError{}
	}
	reloader := node.ABACPolicyReloader()
	if reloader == nil {
		// The node was constructed without WithABACLiveReload and has no
		// policies to reload into -- nothing to do (matches qrdb's own
		// zero-overhead-when-unconfigured contract).
		return nil
	}

	policies, err := a.allPolicies(ctx)
	if err != nil {
		return err
	}

	var logger iamStorageLogger
	if a.server.logger != nil {
		logger = a.server.logger
	}
	converted, err := policiesToABAC(policies, logger)
	if err != nil {
		return err
	}

	return reloader.Reload(converted)
}

// policiesToABAC converts the stored policies for qrdb's ABAC engine. A
// flagged entry (a stored record that did not decode strictly) is never
// converted: none of its rules take effect, so what it would have allowed
// is denied. Each one is logged at WARN with its name and unknown keys.
func policiesToABAC(policies []*iam.Policy, logger iamStorageLogger) ([]*qrdbsdk.ABACPolicy, error) {
	converted := make([]*qrdbsdk.ABACPolicy, 0, len(policies))
	for _, p := range policies {
		if p.LoadError != nil {
			if logger != nil {
				logger.Warnf("abac sync: stored policy not applied, every request in its tenant is denied until it is repaired or deleted: tenant=%q name=%q unknown_keys=%q reason=%q",
					p.LoadError.TenantID, p.LoadError.Name, strings.Join(p.LoadError.UnknownKeys, ","), p.LoadError.Reason)
			}
			converted = append(converted, flaggedRecordDenyPolicy(p))
			continue
		}
		abacPolicy, err := policyToABAC(p)
		if err != nil {
			return nil, err
		}
		converted = append(converted, abacPolicy)
	}
	return converted, nil
}

// flaggedRecordDenyRule names the rule of the policy that stands in for a
// stored record that did not decode strictly.
const flaggedRecordDenyRule = "stored-record-not-applied"

// flaggedRecordDenyPolicy returns the policy that stands in for a stored
// record that did not decode strictly. Its rules are unknown, so it may have
// held denies: skipping it could let another allow policy or the RBAC
// fallback admit a request it blocked. It therefore denies every request in
// the record's tenant (its storage key names the tenant even when its body
// does not decode). A record whose tenant is unknown is scoped to the system
// tenant, whose policies apply to every tenant, so it denies everywhere.
func flaggedRecordDenyPolicy(p *iam.Policy) *qrdbsdk.ABACPolicy {
	tenant := p.LoadError.TenantID
	if tenant == "" {
		tenant = qrdbsdk.SystemTenantID
	}
	return &qrdbsdk.ABACPolicy{
		APIVersion: iam.PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata: qrdbsdk.ABACPolicyMetadata{
			Name:   p.LoadError.Name,
			Tenant: tenant,
		},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Any: true},
			Rules: []qrdbsdk.ABACRule{{
				Name:      flaggedRecordDenyRule,
				Effect:    qrdbsdk.ABACEffectDeny,
				Actions:   []string{"*"},
				Resources: []string{"**"},
			}},
		},
	}
}

// allPolicies enumerates every tenant (via TenantManager) plus the system
// tenant and lists each one's stored policies. PolicyStorage only supports
// per-tenant listing, so a global scan is composed here rather than in
// pkg/iam, which must not depend on qrdb's cross-tenant tenant.SystemTenantID
// concept. Storage routes by the context's tenant, so the catalog is read in
// the system namespace and each tenant's policies under that tenant's own
// context, whatever tenant the triggering request was bound to. A sealed
// non-system tenant is skipped with a warning; its policies load when it is
// unsealed. A tenant held by its unseal checks is read too (its hold refuses
// requests, not the reload), so a reload for one tenant never drops another's
// deny rules; only a sealed tenant is skipped.
func (a *qrdbABACSync) allPolicies(ctx context.Context) ([]*iam.Policy, error) {
	tenantIDs := map[string]struct{}{qrdbsdk.SystemTenantID: {}}

	sysCtx := tenantctx.WithSystem(ctx)
	if a.tenantManager != nil {
		tenants, err := a.tenantManager.ListTenants(sysCtx, iam.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, t := range tenants {
			tenantIDs[t.ID] = struct{}{}
		}
	}

	var all []*iam.Policy
	for tenantID := range tenantIDs {
		policies, err := a.storage.ListPolicies(tenantctx.WithPolicySyncRead(tenantctx.WithTenant(ctx, tenantID), tenantID), tenantID, iam.ListOptions{})
		if err != nil {
			if tenantID == qrdbsdk.SystemTenantID || !errors.Is(err, qrdbtenant.ErrTenantSealed) {
				return nil, err
			}
			a.warnf("abac sync: tenant %q is sealed; its policies load when it is unsealed", tenantID)
			policies = nil
		}
		if tenantID != qrdbsdk.SystemTenantID {
			legacy, err := a.storage.ListPolicies(sysCtx, tenantID, iam.ListOptions{})
			if err != nil {
				return nil, err
			}
			policies = mergeLegacyPolicies(tenantID, policies, legacy)
		}
		all = append(all, policies...)
	}
	return all, nil
}

// policyRecordName returns the stored record name of p, flagged or not.
func policyRecordName(p *iam.Policy) string {
	if p.LoadError != nil {
		return p.LoadError.Name
	}
	return p.Metadata.Name
}

// mergeLegacyPolicies keeps all of own and appends each legacy record whose name own lacks. An own record
// whose legacy copy is enforced differently is replaced by a flagged entry, denying the tenant until resolved.
func mergeLegacyPolicies(tenantID string, own, legacy []*iam.Policy) []*iam.Policy {
	index := make(map[string]int, len(own))
	for i, p := range own {
		index[policyRecordName(p)] = i
	}
	for _, p := range legacy {
		name := policyRecordName(p)
		i, ok := index[name]
		if !ok {
			own = append(own, p)
			continue
		}
		if samePolicyEnforcement(own[i], p) {
			continue
		}
		tenant := own[i].Metadata.Tenant
		if own[i].LoadError != nil {
			tenant = own[i].LoadError.TenantID
		}
		if tenant == "" {
			tenant = tenantID
		}
		flagged := &iam.Policy{LoadError: &iam.StoredPolicyDecodeError{
			TenantID: tenant,
			Name:     name,
			Reason:   "the tenant's copy and the legacy system-namespace copy of this policy differ; delete or update the policy to resolve the conflict",
		}}
		flagged.Metadata.Name = name
		own[i] = flagged
	}
	return own
}

// samePolicyEnforcement reports whether own (a) and its legacy copy (b) are enforced alike.
func samePolicyEnforcement(a, b *iam.Policy) bool {
	if a.LoadError != nil {
		return true
	}
	if b.LoadError != nil {
		return false
	}
	pa, err := policyToABAC(a)
	if err != nil {
		return false
	}
	pb, err := policyToABAC(b)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(pa, pb)
}

// migrateLegacyPolicies moves tenant policies out of the system namespace:
// an earlier release stored a system admin's tenant policies there, under
// the tenant's key. A sealed tenant is migrated by the unseal hook.
func (a *qrdbABACSync) migrateLegacyPolicies(ctx context.Context) {
	if a.tenantManager == nil {
		return
	}
	tenants, err := a.tenantManager.ListTenants(tenantctx.WithSystem(ctx), iam.ListOptions{})
	if err != nil {
		a.warnf("abac sync: listing tenants to migrate legacy policies failed: %v", err)
		return
	}
	for _, t := range tenants {
		if t.ID != qrdbsdk.SystemTenantID {
			a.migrateTenantLegacyPolicies(ctx, t.ID)
		}
	}
}

// migrateTenantLegacyPolicies copies tenantID's legacy policies from the
// system namespace into the tenant's own, and deletes each legacy copy only
// after its tenant copy was written. A prefix tenant shares the system store,
// so nothing is written for it.
func (a *qrdbABACSync) migrateTenantLegacyPolicies(ctx context.Context, tenantID string) {
	if tenantID == qrdbsdk.SystemTenantID {
		return
	}
	sysCtx := tenantctx.WithSystem(ctx)
	legacy, err := a.storage.ListPolicies(sysCtx, tenantID, iam.ListOptions{})
	if err != nil {
		a.warnf("abac sync: listing legacy policies of tenant %q failed: %v", tenantID, err)
		return
	}
	tenantCtx := tenantctx.WithTenant(ctx, tenantID)
	for _, p := range legacy {
		if p.LoadError != nil {
			continue
		}
		name := p.Metadata.Name
		if err := a.storage.CreatePolicy(tenantCtx, tenantID, p); err != nil {
			// coded-sentinel-ok: the whole already-exists class is the intent, and
			// it fails safe. Whichever layer raised it, the tenant already holds a
			// policy of this name, so the legacy copy is kept (not deleted, nothing
			// written) and the next load still merges it from the legacy namespace.
			if errors.Is(err, iam.ErrAlreadyExists) {
				continue
			}
			if errors.Is(err, qrdbtenant.ErrTenantSealed) {
				return
			}
			a.warnf("abac sync: migrating legacy policy %q of tenant %q failed: %v", name, tenantID, err)
			continue
		}
		if err := a.storage.DeletePolicy(sysCtx, tenantID, name); err != nil {
			a.warnf("abac sync: deleting migrated legacy policy %q of tenant %q failed: %v", name, tenantID, err)
		}
	}
}

// warnf logs a warning through the server's logger, when there is one.
func (a *qrdbABACSync) warnf(format string, args ...interface{}) {
	if a.server == nil || a.server.logger == nil {
		return
	}
	a.server.logger.Warnf(format, args...)
}

// resyncOnUnseal migrates the unsealed tenant's legacy policies and reloads
// the live ABAC set each time a tenant barrier is unsealed: allPolicies
// skips a sealed tenant, so its policies take effect only then. resync runs
// the migration and the reload under the policy engine's write lock, so
// neither interleaves with a policy mutation or another reload, and a
// reload that succeeds ends the tenant's policy-repair exception under that
// lock (iam.PolicyEngine.SyncABACAtUnseal), before the hold is released. It
// is an unseal gate: a failed reload fails the unseal and the tenant's
// barrier is sealed again, so the tenant is never open without its deny
// rules.
func (a *qrdbABACSync) resyncOnUnseal(hooks *service.TenantUnsealHooks, resync func(context.Context, string, func(context.Context)) error) {
	hooks.RegisterGate(service.TenantUnsealGateFunc(func(ctx context.Context, tenantID string) error {
		if err := resync(ctx, tenantID, func(ctx context.Context) {
			a.migrateTenantLegacyPolicies(ctx, tenantID)
		}); err != nil {
			a.warnf("abac sync after unsealing tenant %q failed: %v", tenantID, err)
			return fmt.Errorf("abac sync after unsealing tenant %q: %w", tenantID, err)
		}
		return nil
	}))
}

// legacyCleanupPolicyStorage deletes a tenant policy together with the legacy
// copy an earlier release kept in the system namespace, or the next sync
// would merge that copy back in. The legacy copy goes first, so a failure
// leaves the tenant copy in place; where both contexts reach the same store
// (sharesRecord), both names are the one record, which is written or deleted
// once and never through a legacy step. A tenant's legacy records,
// flagged ones included, are also read, listed and replaced through it, so a
// record the migration left can be repaired or deleted.
// A tenant copy is always reached in the tenant's own namespace, whatever tenant ctx is bound to.
type legacyCleanupPolicyStorage struct {
	iam.PolicyStorage
}

// newLegacyCleanupPolicyStorage wraps inner so DeletePolicy also removes a
// tenant policy's legacy system-namespace copy.
func newLegacyCleanupPolicyStorage(inner iam.PolicyStorage) iam.PolicyStorage {
	return &legacyCleanupPolicyStorage{PolicyStorage: inner}
}

// tenantContexts resolves the contexts of a non-system tenant's own copy and its legacy
// system-namespace copy. ctx must be allowed to act in tenantID (tenantctx.AdminTarget). Where
// both contexts reach the same store they name one record, which the callers check with sharesRecord.
func (s *legacyCleanupPolicyStorage) tenantContexts(ctx context.Context, op, tenantID string) (tenantCtx, legacyCtx context.Context, err error) {
	tenantCtx, resolved, err := tenantctx.AdminTarget(ctx, op, tenantID)
	if err != nil {
		return nil, nil, err
	}
	if resolved == qrdbsdk.SystemTenantID {
		return nil, nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   op,
			Err:  &iam.IAMMessageError{Message: "tenant policy copy resolved to the system namespace"},
		}
	}
	return tenantCtx, tenantctx.WithSystem(ctx), nil
}

// policyRecordSharer reports whether one policy key names the same physical
// record under two contexts.
type policyRecordSharer interface {
	SharesPolicyRecord(a, b context.Context) (bool, error)
}

// sharesRecord reports whether the tenant copy and the legacy copy are one
// physical record; an inner storage that cannot tell keeps them apart.
func (s *legacyCleanupPolicyStorage) sharesRecord(tenantCtx, legacyCtx context.Context) (bool, error) {
	if sharer, ok := s.PolicyStorage.(policyRecordSharer); ok {
		return sharer.SharesPolicyRecord(tenantCtx, legacyCtx)
	}
	return false, nil
}

// CreatePolicy creates a tenant policy in the tenant's namespace, whatever tenant ctx is bound to.
func (s *legacyCleanupPolicyStorage) CreatePolicy(ctx context.Context, tenantID string, policy *iam.Policy) error {
	if tenantID == qrdbsdk.SystemTenantID {
		return s.PolicyStorage.CreatePolicy(ctx, tenantID, policy)
	}
	tenantCtx, legacyCtx, err := s.tenantContexts(ctx, "iam.policy.create", tenantID)
	if err != nil {
		return err
	}
	// A record in either namespace, even one that does not decode, holds the name.
	for _, c := range []context.Context{tenantCtx, legacyCtx} {
		var held bool
		if raw, ok := s.PolicyStorage.(policyRecordStore); ok {
			_, err = raw.GetPolicyRecord(c, tenantID, policy.Metadata.Name)
			held = err == nil
		} else {
			var existing *iam.Policy
			existing, err = s.PolicyStorage.GetPolicy(c, tenantID, policy.Metadata.Name)
			var de *iam.StoredPolicyDecodeError
			held = (err == nil && existing != nil) || errors.As(err, &de)
		}
		if held {
			return &iam.PolicyExistsError{TenantID: tenantID, Name: policy.Metadata.Name}
		}
		if err != nil && !dberrors.Is(err, dberrors.ErrNotFound) {
			return err
		}
	}
	return s.PolicyStorage.CreatePolicy(tenantCtx, tenantID, policy)
}

// RevertCreatedPolicy undoes a CreatePolicy in the tenant's namespace only:
// create never writes the legacy namespace, so it is never touched.
func (s *legacyCleanupPolicyStorage) RevertCreatedPolicy(ctx context.Context, tenantID string, policy *iam.Policy) error {
	tenantCtx := ctx
	if tenantID != qrdbsdk.SystemTenantID {
		var err error
		if tenantCtx, _, err = s.tenantContexts(ctx, "iam.policy.create", tenantID); err != nil {
			return err
		}
	}
	if r, ok := s.PolicyStorage.(iam.PolicyCreateReverter); ok {
		return r.RevertCreatedPolicy(tenantCtx, tenantID, policy)
	}
	if err := s.PolicyStorage.DeletePolicy(tenantCtx, tenantID, policy.Metadata.Name); err != nil && !dberrors.Is(err, dberrors.ErrNotFound) {
		return err
	}
	return nil
}

// DeletePolicy deletes the legacy system-namespace copy of a tenant policy
// and then the tenant copy. The system tenant's policies have no legacy copy.
// A context that may not act in tenantID is refused before anything is deleted.
func (s *legacyCleanupPolicyStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	if tenantID == qrdbsdk.SystemTenantID {
		return s.PolicyStorage.DeletePolicy(ctx, tenantID, name)
	}
	tenantCtx, legacyCtx, err := s.tenantContexts(ctx, "iam.policy.delete", tenantID)
	if err != nil {
		return err
	}
	shared, err := s.sharesRecord(tenantCtx, legacyCtx)
	if err != nil {
		return err
	}
	if shared {
		return s.PolicyStorage.DeletePolicy(tenantCtx, tenantID, name)
	}
	legacyDeleted := true
	if err := s.PolicyStorage.DeletePolicy(legacyCtx, tenantID, name); err != nil {
		if !dberrors.Is(err, dberrors.ErrNotFound) {
			return err
		}
		legacyDeleted = false
	}
	err = s.PolicyStorage.DeletePolicy(tenantCtx, tenantID, name)
	if err != nil && legacyDeleted && dberrors.Is(err, dberrors.ErrNotFound) {
		return nil
	}
	return err
}

// GetPolicy reads a tenant policy, falling back to its legacy
// system-namespace copy when the tenant has none. A legacy record that does
// not decode returns its decode error, so the engine flags it.
// A context that may not act in tenantID is refused.
func (s *legacyCleanupPolicyStorage) GetPolicy(ctx context.Context, tenantID, name string) (*iam.Policy, error) {
	if tenantID == qrdbsdk.SystemTenantID {
		return s.PolicyStorage.GetPolicy(ctx, tenantID, name)
	}
	tenantCtx, legacyCtx, err := s.tenantContexts(ctx, "iam.policy.get", tenantID)
	if err != nil {
		return nil, err
	}
	policy, err := s.PolicyStorage.GetPolicy(tenantCtx, tenantID, name)
	if err == nil || !dberrors.Is(err, dberrors.ErrNotFound) {
		return policy, err
	}
	return s.PolicyStorage.GetPolicy(legacyCtx, tenantID, name)
}

// ListPolicies lists a tenant's policies together with its legacy
// system-namespace records, flagged ones included, paginated once over the
// merged set in name order. A negative offset is refused for every tenant.
// A context that may not act in tenantID is refused.
func (s *legacyCleanupPolicyStorage) ListPolicies(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.Policy, error) {
	if opts.Offset < 0 {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.policy.list",
			Err:  &iam.IAMMessageError{Message: "negative policy list offset"},
		}
	}
	if tenantID == qrdbsdk.SystemTenantID {
		return s.PolicyStorage.ListPolicies(ctx, tenantID, opts)
	}
	tenantCtx, legacyCtx, err := s.tenantContexts(ctx, "iam.policy.list", tenantID)
	if err != nil {
		return nil, err
	}
	all := iam.ListOptions{Filter: opts.Filter}
	own, err := s.PolicyStorage.ListPolicies(tenantCtx, tenantID, all)
	if err != nil {
		return nil, err
	}
	legacy, err := s.PolicyStorage.ListPolicies(legacyCtx, tenantID, all)
	if err != nil {
		return nil, err
	}
	merged := mergeLegacyPolicies(tenantID, own, legacy)
	sort.SliceStable(merged, func(i, j int) bool { return policyRecordName(merged[i]) < policyRecordName(merged[j]) })
	return paginatePolicies(merged, opts), nil
}

// paginatePolicies applies opts.Offset and opts.Limit as BackendStorage.ListPolicies does;
// a negative offset counts as 0 and a negative limit as no limit, so it never panics.
func paginatePolicies(policies []*iam.Policy, opts iam.ListOptions) []*iam.Policy {
	offset := max(opts.Offset, 0)
	if offset >= len(policies) {
		return []*iam.Policy{}
	}
	policies = policies[offset:]
	if opts.Limit > 0 && opts.Limit < len(policies) {
		policies = policies[:opts.Limit]
	}
	return policies
}

// UpdatePolicy replaces a tenant policy. A policy held only as a legacy
// system-namespace record, flagged or not, is written to the tenant namespace
// and its legacy copy deleted. A successful tenant update deletes any legacy
// copy too, resolving a conflict between the two. When that legacy delete
// fails, the tenant copy is put back as it was read before the write.
// A context that may not act in tenantID is refused before anything is written.
func (s *legacyCleanupPolicyStorage) UpdatePolicy(ctx context.Context, tenantID string, policy *iam.Policy) error {
	if tenantID == qrdbsdk.SystemTenantID {
		return s.PolicyStorage.UpdatePolicy(ctx, tenantID, policy)
	}
	tenantCtx, legacyCtx, err := s.tenantContexts(ctx, "iam.policy.update", tenantID)
	if err != nil {
		return err
	}
	shared, err := s.sharesRecord(tenantCtx, legacyCtx)
	if err != nil {
		return err
	}
	if shared {
		return s.PolicyStorage.UpdatePolicy(tenantCtx, tenantID, policy)
	}
	restoreTenant, err := s.snapshotRecord(tenantCtx, tenantID, policy.Metadata.Name, policy)
	if err != nil {
		return err
	}
	deleteLegacy := func() error {
		err := s.PolicyStorage.DeletePolicy(legacyCtx, tenantID, policy.Metadata.Name)
		if err == nil || dberrors.Is(err, dberrors.ErrNotFound) {
			return nil
		}
		if rerr := restoreTenant(tenantCtx); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	err = s.PolicyStorage.UpdatePolicy(tenantCtx, tenantID, policy)
	if err == nil {
		return deleteLegacy()
	}
	if !dberrors.Is(err, dberrors.ErrNotFound) {
		return err
	}
	_, readErr := s.PolicyStorage.GetPolicy(legacyCtx, tenantID, policy.Metadata.Name)
	var de *iam.StoredPolicyDecodeError
	switch {
	case readErr == nil || errors.As(readErr, &de):
	case dberrors.Is(readErr, dberrors.ErrNotFound):
		return err
	default:
		return readErr
	}
	if err := s.PolicyStorage.CreatePolicy(tenantCtx, tenantID, policy); err != nil {
		return err
	}
	return deleteLegacy()
}

var _ iam.PolicyWriteSnapshotter = (*legacyCleanupPolicyStorage)(nil)

// SnapshotPolicy captures a policy's tenant copy and its legacy system-namespace copy (the system
// tenant's one record), as raw bytes when the inner storage allows, so flagged records come back too.
// Both the snapshot and its restore reach the tenant copy in the tenant's namespace.
func (s *legacyCleanupPolicyStorage) SnapshotPolicy(ctx context.Context, tenantID, name string, written *iam.Policy) (func(context.Context) error, error) {
	if tenantID == qrdbsdk.SystemTenantID {
		return s.snapshotRecord(ctx, tenantID, name, written)
	}
	const op = "iam.policy.update"
	tenantCtx, legacyCtx, err := s.tenantContexts(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	shared, err := s.sharesRecord(tenantCtx, legacyCtx)
	if err != nil {
		return nil, err
	}
	restoreTenant, err := s.snapshotRecord(tenantCtx, tenantID, name, written)
	if err != nil {
		return nil, err
	}
	if shared {
		return func(ctx context.Context) error {
			tenantCtx, _, err := s.tenantContexts(ctx, op, tenantID)
			if err != nil {
				return err
			}
			return restoreTenant(tenantCtx)
		}, nil
	}
	// A write only ever deletes the legacy copy.
	restoreLegacy, err := s.snapshotRecord(legacyCtx, tenantID, name, nil)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		tenantCtx, legacyCtx, err := s.tenantContexts(ctx, op, tenantID)
		if err != nil {
			return err
		}
		return errors.Join(restoreTenant(tenantCtx), restoreLegacy(legacyCtx))
	}, nil
}

// policyRecordStore reads a policy's stored bytes as they are, and restores a write snapshot of
// them, guarded by what the write left there.
type policyRecordStore interface {
	GetPolicyRecord(ctx context.Context, tenantID, name string) ([]byte, error)
	RestorePolicyRecord(ctx context.Context, tenantID, name string, written *iam.Policy, snapshot []byte, present bool) error
}

// snapshotRecord captures the one record ctx routes tenantID's policy name to; written is the
// policy the write stores, nil for a delete. With raw access to the inner storage, its restore
// puts the record back as it was, or deletes it when it was absent, only while the record still
// holds what the write left there (iam.BackendStorage.RestorePolicyRecord). Without raw access to
// the inner storage, a record that does not decode cannot be restored, so the snapshot fails.
// The non-raw fallback restore is unguarded: it can overwrite another writer's record.
func (s *legacyCleanupPolicyStorage) snapshotRecord(ctx context.Context, tenantID, name string, written *iam.Policy) (func(context.Context) error, error) {
	inner := s.PolicyStorage
	removeIfPresent := func(ctx context.Context) error {
		if err := inner.DeletePolicy(ctx, tenantID, name); err != nil && !dberrors.Is(err, dberrors.ErrNotFound) {
			return err
		}
		return nil
	}
	if raw, ok := inner.(policyRecordStore); ok {
		data, err := raw.GetPolicyRecord(ctx, tenantID, name)
		found := err == nil
		if err != nil && !dberrors.Is(err, dberrors.ErrNotFound) {
			return nil, err
		}
		return func(ctx context.Context) error {
			return raw.RestorePolicyRecord(ctx, tenantID, name, written, data, found)
		}, nil
	}
	policy, err := inner.GetPolicy(ctx, tenantID, name)
	if err != nil {
		if dberrors.Is(err, dberrors.ErrNotFound) {
			return removeIfPresent, nil
		}
		return nil, err
	}
	if policy == nil {
		return removeIfPresent, nil
	}
	return func(ctx context.Context) error {
		err := inner.UpdatePolicy(ctx, tenantID, policy)
		if dberrors.Is(err, dberrors.ErrNotFound) {
			err = inner.CreatePolicy(ctx, tenantID, policy)
		}
		return err
	}, nil
}

// policyToABAC converts an iam.Policy to qrdb's abac.Policy wire shape
// (qrdbsdk.ABACPolicy). The two schemas are structurally identical by
// construction (qrdb's ABAC engine was ported from this package's own
// evaluator/compiler/trie, since deleted in favor of this live seam), so
// the conversion is a faithful, lossless field-by-field copy: every
// operator, condition expression, and resource-pattern syntax quicbaas's
// policy documents use is understood verbatim by qrdb's condition parser
// and resource trie. The one exception is a nested And/Or/Not condition,
// which qrdb cannot hold; it is refused (see conditionsToABAC).
func policyToABAC(p *iam.Policy) (*qrdbsdk.ABACPolicy, error) {
	if p == nil {
		return nil, nil
	}
	rules := make([]qrdbsdk.ABACRule, 0, len(p.Spec.Rules))
	for _, r := range p.Spec.Rules {
		when, err := conditionsToABAC("when", r.When)
		if err != nil {
			return nil, err
		}
		unless, err := conditionsToABAC("unless", r.Unless)
		if err != nil {
			return nil, err
		}
		rules = append(rules, qrdbsdk.ABACRule{
			Name:      r.Name,
			Effect:    qrdbsdk.ABACEffect(r.Effect),
			Priority:  r.Priority,
			Actions:   r.Actions,
			Resources: r.Resources,
			When:      when,
			Unless:    unless,
		})
	}
	return &qrdbsdk.ABACPolicy{
		APIVersion: p.APIVersion,
		Kind:       p.Kind,
		Metadata: qrdbsdk.ABACPolicyMetadata{
			Name:        p.Metadata.Name,
			Tenant:      p.Metadata.Tenant,
			Description: p.Metadata.Description,
			Labels:      p.Metadata.Labels,
			Priority:    p.Metadata.Priority,
			Created:     p.Metadata.Created,
			Updated:     p.Metadata.Updated,
		},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{
				Users:         p.Spec.Subjects.Users,
				Groups:        p.Spec.Subjects.Groups,
				Roles:         p.Spec.Subjects.Roles,
				Services:      p.Spec.Subjects.Services,
				Match:         p.Spec.Subjects.Match,
				Authenticated: p.Spec.Subjects.Authenticated,
				Any:           p.Spec.Subjects.Any,
			},
			Variables: p.Spec.Variables,
			Rules:     rules,
		},
	}, nil
}

// conditionsToABAC converts iam.Condition to qrdb's flat Expression-string
// ABAC condition DSL. iam.Condition also carries And/Or/Not sub-condition
// fields that qrdb's condition cannot hold; a condition using them is
// refused with iam.ErrConditionNested rather than dropped, because dropping
// an authorization condition would widen what the rule matches. Writes are
// already refused by iam.Rule.Validate; this check is defence in depth for
// policies stored before that validation existed. Boolean composition
// ("a and b", "a or b", "not a") is written textually WITHIN a single
// Expression string, using a parser grammar identical in both engines (verified
// operator-for-operator: ==, !=, eq, neq, >=, <=, >, <, between, endswith,
// startswith, contains, matches, like, in, notin, containsall, containsany,
// notcontains, overlaps, disjoint, exists, notexists).
func conditionsToABAC(field string, conds []iam.Condition) ([]qrdbsdk.ABACCondition, error) {
	if err := iam.ValidateConditions(field, conds); err != nil {
		return nil, err
	}
	if len(conds) == 0 {
		return nil, nil
	}
	out := make([]qrdbsdk.ABACCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, qrdbsdk.ABACCondition{Expression: c.Expression})
	}
	return out, nil
}
