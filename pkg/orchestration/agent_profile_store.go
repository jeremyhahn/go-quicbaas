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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// AgentProfileStore — interface
// =============================================================================

// AgentProfileStore persists the agent profiles and pools in force for a
// tenant.
//
// WHY THIS IS DATA RATHER THAN CONFIGURATION. Which runtime runs a role, which
// model it thinks with and where that model is served are the three facts an
// operator changes most often and a deployment changes per tenant — a fleet
// that must keep private work on the local NPU and burst public work to a
// hosted API expresses exactly that with two profiles and one pool. Compiling
// those choices into a daemon flag makes every change a redeploy, and hiding
// them in a config file makes them invisible to the API the GUI drives.
//
// Every method is tenant-explicit, so one tenant's catalogue is unreachable
// from another's. An empty tenant is the system tenant, which is what an
// in-process embedded caller and a unit test carry.
type AgentProfileStore interface {
	// PutProfile creates or replaces one profile.
	PutProfile(ctx context.Context, tenantID string, profile *AgentProfile) error

	// GetProfile reads one profile by name.
	GetProfile(ctx context.Context, tenantID, name string) (*AgentProfile, error)

	// ListProfiles returns every profile owned by tenantID, name-ordered.
	ListProfiles(ctx context.Context, tenantID string) ([]*AgentProfile, error)

	// DeleteProfile removes one profile. It refuses while a pool still refers
	// to the profile.
	DeleteProfile(ctx context.Context, tenantID, name string) error

	// PutPool creates or replaces one pool. It refuses a pool naming a
	// profile that does not exist.
	PutPool(ctx context.Context, tenantID string, pool *AgentPool) error

	// GetPool reads one pool by name.
	GetPool(ctx context.Context, tenantID, name string) (*AgentPool, error)

	// ListPools returns every pool owned by tenantID, name-ordered.
	ListPools(ctx context.Context, tenantID string) ([]*AgentPool, error)

	// DeletePool removes one pool.
	DeletePool(ctx context.Context, tenantID, name string) error

	// Catalogue returns the tenant's whole catalogue in one read, which is
	// what a resolver needs: selection walks a pool's preference list and must
	// see every profile it can name, and reading them one at a time would let
	// the list change underneath the walk.
	Catalogue(ctx context.Context, tenantID string) (ProfileCatalogue, error)
}

// =============================================================================
// Key derivation
//
// orch/agentprofile/{tenant}/{name} -> AgentProfile (JSON)
// orch/agentpool/{tenant}/{name}    -> AgentPool (JSON)
// =============================================================================

func agentProfileKey(tenantID, name string) (string, error) {
	key, err := entityKey(agentProfileKeyspace, normalizeStoreTenant(tenantID), "profile_name", name)
	if err != nil {
		return "", profileKeyError(err)
	}
	return key, nil
}

func agentPoolKey(tenantID, name string) (string, error) {
	key, err := entityKey(agentPoolKeyspace, normalizeStoreTenant(tenantID), "pool_name", name)
	if err != nil {
		return "", profileKeyError(err)
	}
	return key, nil
}

// profileKeyError converts a key-layout violation into this store's own
// validation vocabulary, mirroring accountKeyError.
func profileKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &InputValidationError{Field: segment.Field, Message: segment.Message}
}

// =============================================================================
// agentProfileStore — the single KV-backed AgentProfileStore
// =============================================================================

// agentProfileStore holds no data of its own: durability, replication and
// storage-barrier encryption are properties of the backing KV. The record is
// METADATA ONLY — a profile names an endpoint, never a credential for it,
// which is what keeps the catalogue safe to list from the GUI.
type agentProfileStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// recordMu serializes the read-modify-write sequences that span two
	// records: a pool write validating against the profiles it names, and a
	// profile delete validating against the pools that name it. Both are held
	// on the TENANT rather than on one key, because the invariant is a
	// property of the tenant's catalogue as a whole and locking either record
	// alone would let the other side change mid-check.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ AgentProfileStore = (*agentProfileStore)(nil)

// NewAgentProfileStore returns an AgentProfileStore backed by kv.
func NewAgentProfileStore(kv interfaces.KVStore, opts ...StoreOption) AgentProfileStore {
	cfg := newStoreConfig(opts...)
	return &agentProfileStore{kv: kv, auditor: cfg.auditor}
}

// NewInMemoryAgentProfileStore returns a memory-backed binding of the store
// returned by NewAgentProfileStore — not a second implementation. It exists
// for tests and for embedded use that needs no durability.
func NewInMemoryAgentProfileStore() AgentProfileStore {
	return NewAgentProfileStore(kvstore.NewMemory())
}

func (s *agentProfileStore) ready() error {
	if s.kv == nil {
		return &AgentProfileStoreError{Message: "kv store must not be nil"}
	}
	return nil
}

// =============================================================================
// Profiles
// =============================================================================

// PutProfile creates or replaces one profile.
func (s *agentProfileStore) PutProfile(ctx context.Context, tenantID string, profile *AgentProfile) error {
	var name string
	if profile != nil {
		name = profile.Name
	}
	err := s.putProfile(ctx, tenantID, profile)
	s.audit(ctx, AuditResourceAgentProfile, AuditActionUpdate, tenantID, name, err)
	return err
}

func (s *agentProfileStore) putProfile(ctx context.Context, tenantID string, profile *AgentProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if profile == nil {
		return &InputValidationError{Field: "profile", Message: "must not be nil"}
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return err
	}
	// A PROFILE IS REFUSED AT THE WRITE, NEVER AT THE SPAWN. An endpoint with
	// no dialect or a negative concurrency limit is only detectable as a
	// defect here; by the time a spawn reads it, the operator believes the
	// profile works and the failure surfaces as a runtime the CLI rejected.
	if err := profile.Validate(); err != nil {
		return err
	}

	key, err := agentProfileKey(tenantID, profile.Name)
	if err != nil {
		return err
	}

	data, err := json.Marshal(profile)
	if err != nil {
		return &AgentProfileStoreError{Message: "failed to marshal profile: " + err.Error(), Cause: err}
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentProfileStoreError{Message: "failed to write profile: " + err.Error(), Cause: err}
	}
	return nil
}

// GetProfile reads one profile by name.
func (s *agentProfileStore) GetProfile(ctx context.Context, tenantID, name string) (*AgentProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return nil, err
	}
	key, err := agentProfileKey(tenantID, name)
	if err != nil {
		return nil, err
	}
	return s.loadProfile(ctx, key, name)
}

func (s *agentProfileStore) loadProfile(ctx context.Context, key, name string) (*AgentProfile, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &ProfileNotFoundError{Name: name}
		}
		return nil, &AgentProfileStoreError{Message: "failed to read profile: " + err.Error(), Cause: err}
	}
	if len(data) == 0 {
		return nil, &ProfileNotFoundError{Name: name}
	}
	var profile AgentProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, &AgentProfileStoreError{Message: "failed to unmarshal profile: " + err.Error(), Cause: err}
	}
	return &profile, nil
}

// ListProfiles returns every profile owned by tenantID, name-ordered.
func (s *agentProfileStore) ListProfiles(ctx context.Context, tenantID string) ([]*AgentProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return nil, err
	}
	profiles, err := s.scanProfiles(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*AgentProfile, 0, len(profiles))
	names := sortedKeys(profiles)
	for _, name := range names {
		profile := profiles[name]
		out = append(out, &profile)
	}
	return out, nil
}

func (s *agentProfileStore) scanProfiles(ctx context.Context, tenantID string) (map[string]AgentProfile, error) {
	prefix, err := tenantPrefix(agentProfileKeyspace, normalizeStoreTenant(tenantID))
	if err != nil {
		return nil, profileKeyError(err)
	}
	profiles := make(map[string]AgentProfile)
	var decodeErr error
	scanErr := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var profile AgentProfile
		if err := json.Unmarshal(value, &profile); err != nil {
			decodeErr = &AgentProfileStoreError{
				Message: "failed to unmarshal profile: " + err.Error(),
				Cause:   err,
			}
			return decodeErr
		}
		// The key is the authority on the name: a record whose body disagrees
		// would otherwise be unaddressable — listed under one name and read
		// back under another.
		profile.Name = idFromKey(key, prefix)
		profiles[profile.Name] = profile
		return nil
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if scanErr != nil {
		return nil, &AgentProfileStoreError{Message: "failed to list profiles: " + scanErr.Error(), Cause: scanErr}
	}
	return profiles, nil
}

// DeleteProfile removes one profile.
func (s *agentProfileStore) DeleteProfile(ctx context.Context, tenantID, name string) error {
	err := s.deleteProfile(ctx, tenantID, name)
	s.audit(ctx, AuditResourceAgentProfile, AuditActionDelete, tenantID, name, err)
	return err
}

func (s *agentProfileStore) deleteProfile(ctx context.Context, tenantID, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return err
	}
	key, err := agentProfileKey(tenantID, name)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	if _, err := s.loadProfile(ctx, key, name); err != nil {
		return err
	}

	// A DANGLING POOL REFERENCE IS REFUSED HERE, NOT DISCOVERED AT A SPAWN.
	// Resolve does report it — but by then the operator has deleted the
	// profile, forgotten, and is reading a dispatch failure about a pool they
	// did not touch. Naming the pools at the delete puts the reason where the
	// decision is.
	pools, err := s.scanPools(ctx, tenantID)
	if err != nil {
		return err
	}
	var referring []string
	for _, poolName := range sortedKeys(pools) {
		for _, candidate := range pools[poolName].Profiles {
			if candidate == name {
				referring = append(referring, poolName)
				break
			}
		}
	}
	if len(referring) > 0 {
		return &ProfileInUseError{Name: name, Pools: referring}
	}

	if err := s.kv.Delete(ctx, key); err != nil {
		return &AgentProfileStoreError{Message: "failed to delete profile: " + err.Error(), Cause: err}
	}
	return nil
}

// =============================================================================
// Pools
// =============================================================================

// PutPool creates or replaces one pool.
func (s *agentProfileStore) PutPool(ctx context.Context, tenantID string, pool *AgentPool) error {
	var name string
	if pool != nil {
		name = pool.Name
	}
	err := s.putPool(ctx, tenantID, pool)
	s.audit(ctx, AuditResourceAgentPool, AuditActionUpdate, tenantID, name, err)
	return err
}

func (s *agentProfileStore) putPool(ctx context.Context, tenantID string, pool *AgentPool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if pool == nil {
		return &InputValidationError{Field: "pool", Message: "must not be nil"}
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return err
	}
	if err := pool.Validate(); err != nil {
		return err
	}

	key, err := agentPoolKey(tenantID, pool.Name)
	if err != nil {
		return err
	}

	data, err := json.Marshal(pool)
	if err != nil {
		return &AgentProfileStoreError{Message: "failed to marshal pool: " + err.Error(), Cause: err}
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	// EVERY NAMED PROFILE MUST EXIST. A pool is a preference list, and a name
	// in it that resolves to nothing is not a degraded preference — it is a
	// dispatch that fails at the moment the fleet needs the fallback most,
	// which is when the preferred profile is already full.
	profiles, err := s.scanProfiles(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, name := range pool.Profiles {
		if _, ok := profiles[name]; !ok {
			return &ProfileNotFoundError{Name: name, Pool: pool.Name}
		}
	}

	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentProfileStoreError{Message: "failed to write pool: " + err.Error(), Cause: err}
	}
	return nil
}

// GetPool reads one pool by name.
func (s *agentProfileStore) GetPool(ctx context.Context, tenantID, name string) (*AgentPool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return nil, err
	}
	key, err := agentPoolKey(tenantID, name)
	if err != nil {
		return nil, err
	}
	return s.loadPool(ctx, key, name)
}

func (s *agentProfileStore) loadPool(ctx context.Context, key, name string) (*AgentPool, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &PoolNotFoundError{Name: name}
		}
		return nil, &AgentProfileStoreError{Message: "failed to read pool: " + err.Error(), Cause: err}
	}
	if len(data) == 0 {
		return nil, &PoolNotFoundError{Name: name}
	}
	var pool AgentPool
	if err := json.Unmarshal(data, &pool); err != nil {
		return nil, &AgentProfileStoreError{Message: "failed to unmarshal pool: " + err.Error(), Cause: err}
	}
	return &pool, nil
}

// ListPools returns every pool owned by tenantID, name-ordered.
func (s *agentProfileStore) ListPools(ctx context.Context, tenantID string) ([]*AgentPool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return nil, err
	}
	pools, err := s.scanPools(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*AgentPool, 0, len(pools))
	for _, name := range sortedKeys(pools) {
		pool := pools[name]
		out = append(out, &pool)
	}
	return out, nil
}

func (s *agentProfileStore) scanPools(ctx context.Context, tenantID string) (map[string]AgentPool, error) {
	prefix, err := tenantPrefix(agentPoolKeyspace, normalizeStoreTenant(tenantID))
	if err != nil {
		return nil, profileKeyError(err)
	}
	pools := make(map[string]AgentPool)
	var decodeErr error
	scanErr := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var pool AgentPool
		if err := json.Unmarshal(value, &pool); err != nil {
			decodeErr = &AgentProfileStoreError{
				Message: "failed to unmarshal pool: " + err.Error(),
				Cause:   err,
			}
			return decodeErr
		}
		pool.Name = idFromKey(key, prefix)
		pools[pool.Name] = pool
		return nil
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if scanErr != nil {
		return nil, &AgentProfileStoreError{Message: "failed to list pools: " + scanErr.Error(), Cause: scanErr}
	}
	return pools, nil
}

// DeletePool removes one pool.
func (s *agentProfileStore) DeletePool(ctx context.Context, tenantID, name string) error {
	err := s.deletePool(ctx, tenantID, name)
	s.audit(ctx, AuditResourceAgentPool, AuditActionDelete, tenantID, name, err)
	return err
}

func (s *agentProfileStore) deletePool(ctx context.Context, tenantID, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return err
	}
	key, err := agentPoolKey(tenantID, name)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	if _, err := s.loadPool(ctx, key, name); err != nil {
		return err
	}
	if err := s.kv.Delete(ctx, key); err != nil {
		return &AgentProfileStoreError{Message: "failed to delete pool: " + err.Error(), Cause: err}
	}
	return nil
}

// =============================================================================
// Catalogue
// =============================================================================

// Catalogue returns the tenant's profiles and pools in one read.
func (s *agentProfileStore) Catalogue(ctx context.Context, tenantID string) (ProfileCatalogue, error) {
	if err := ctx.Err(); err != nil {
		return ProfileCatalogue{}, err
	}
	if err := s.ready(); err != nil {
		return ProfileCatalogue{}, err
	}
	if err := scopeCheck(ctx, normalizeStoreTenant(tenantID)); err != nil {
		return ProfileCatalogue{}, err
	}
	profiles, err := s.scanProfiles(ctx, tenantID)
	if err != nil {
		return ProfileCatalogue{}, err
	}
	pools, err := s.scanPools(ctx, tenantID)
	if err != nil {
		return ProfileCatalogue{}, err
	}
	return ProfileCatalogue{Profiles: profiles, Pools: pools}, nil
}

// audit emits exactly one store event per mutating call, recording a refused
// attempt with Success false.
func (s *agentProfileStore) audit(ctx context.Context, resource, action, tenantID, name string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   normalizeStoreTenant(tenantID),
		Resource:   resource,
		ResourceID: name,
		Action:     action,
		Success:    err == nil,
	})
}

// sortedKeys returns a map's keys in ascending order. Listing is ordered so
// the GUI and the CLI render a stable catalogue rather than one that reshuffles
// on every poll.
func sortedKeys[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.Compare(names[i], names[j]) < 0 })
	return names
}
