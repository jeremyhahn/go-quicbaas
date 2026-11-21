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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Key errors
// =============================================================================

// FleetNodeKeyError reports an identifier that cannot address a fleet node record.
//
// It unwraps to two sentinels: ErrFleetRegistryInvalidInput, the registry's
// caller-facing validation contract, and the underlying *KeySegmentError, which
// names the keyspace rule that was violated. Callers may therefore match either
// the registry contract or the precise cause.
type FleetNodeKeyError struct {
	// Cause is the *KeySegmentError describing the offending segment.
	Cause error
}

// Error implements the error interface.
func (e *FleetNodeKeyError) Error() string {
	return ErrFleetRegistryInvalidInput.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both sentinels for errors.Is support.
func (e *FleetNodeKeyError) Unwrap() []error {
	return []error{ErrFleetRegistryInvalidInput, e.Cause}
}

// errFleetScanDone stops a prefix scan early once a caller-supplied limit has
// been satisfied. It never escapes the method that raises it.
var errFleetScanDone = errors.New("agent registry: scan complete")

// =============================================================================
// Registry
// =============================================================================

// fleetRegistry is the one and only FleetRegistry implementation. Durability is
// a property of the injected interfaces.KVStore, not of this type: injecting
// kvstore.NewMemory() yields a fast in-process registry for tests and embedded
// use, and injecting the server's qrdb-backed store makes the very same code
// durable, replicated and encrypted at rest. The registry holds no record state
// of its own, so a restart — or a second registry over the same store — sees
// exactly the same fleet.
//
// Records live at orch/agent/{tenantID}/{nodeID}. Because the tenant is the
// first variable key segment, a point read is confined to the caller's tenant
// and a list is a prefix scan of that tenant's subtree; there is no key shape
// under which one tenant can address another tenant's fleet node.
type fleetRegistry struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// creator is kv's guarded-create capability, asserted once at construction.
	// Nil when the backing store cannot decide a contested create, in which
	// case RegisterFleetNode refuses rather than probing and hoping.
	creator guardedCreator

	// locks serializes read-modify-write sequences per fleet node key. interfaces.KVStore
	// carries no guarded-swap verb, so update/remove/heartbeat read the current
	// record and write its successor under this lock. Writers to different
	// fleet nodes never contend.
	//
	// IT DOES NOT MAKE REGISTRATION EXCLUSIVE, and must never be relied on for
	// that: a mutex is process-local, so two nodes each hold their own. The
	// registration below is decided by the store instead.
	locks keyedMutex
}

// Compile-time interface satisfaction check.
var _ FleetRegistry = (*fleetRegistry)(nil)

// NewFleetRegistry returns a FleetRegistry backed by kv.
func NewFleetRegistry(kv interfaces.KVStore, opts ...StoreOption) FleetRegistry {
	cfg := newStoreConfig(opts...)
	registry := &fleetRegistry{kv: kv, auditor: cfg.auditor}
	// Asserted once: the capability is a static property of the injected store,
	// so probing it per registration would ask the same question forever.
	if creator, ok := kv.(guardedCreator); ok {
		registry.creator = creator
	}
	return registry
}

// NewInMemoryFleetRegistry returns a registry bound to qrdb's in-memory storage
// engine. It is a memory-backed *binding* of the single implementation above —
// not a second implementation — so behaviour is identical to a durable
// deployment in every respect except that the records vanish with the process.
func NewInMemoryFleetRegistry() FleetRegistry {
	// go-qrdb's memory KVStore rather than kvstore.NewMemory(): registration is
	// a guarded create, and that adapter wraps a raw storage engine with no
	// conditional write to delegate to. Its atomicity is a mutex rather than
	// consensus, which is honest and sufficient for one process.
	return NewFleetRegistry(NewMemoryTaskKV())
}

// =============================================================================
// Keys
// =============================================================================

// fleetNodeKey addresses one fleet node record. Both segments are validated, so an
// identifier that is empty or contains the key separator is rejected before it
// can collide with, or escape into, another tenant's subtree.
func fleetNodeKey(tenantID, nodeID string) (string, error) {
	key, err := entityKey(fleetNodeKeyspace, tenantID, "agent_id", nodeID)
	if err != nil {
		return "", &FleetNodeKeyError{Cause: err}
	}
	return key, nil
}

// fleetNodePrefix covers every fleet node record owned by tenantID.
func fleetNodePrefix(tenantID string) (string, error) {
	prefix, err := tenantPrefix(fleetNodeKeyspace, tenantID)
	if err != nil {
		return "", &FleetNodeKeyError{Cause: err}
	}
	return prefix, nil
}

// =============================================================================
// FleetRegistry
// =============================================================================

// fleetTenantContext binds tenantID to ctx when the context names no tenant.
//
// The store underneath this registry routes every read and write by the tenant
// bound to the CONTEXT, never by the tenant segment in the key (pkg/server
// tenantRoutingKVStore -> go-qrdb tenant.StoreResolver), and a barrier-isolated
// tenant's records live in that tenant's own store. A record therefore has to
// be addressed under its OWN tenant on both sides, or the write and the read
// pick different namespaces.
//
// That divergence is go-quicbaas#663: POST /api/v1/fleet/enroll is a public
// route -- the mTLS handshake IS the authentication -- so it carries no
// identity and its context named no tenant, while every read of the fleet
// registry is made under the tenant the request names. The registration landed
// in the system namespace and every tenant-scoped read looked in the tenant's,
// so "agent already exists" and "agent registry: not found" were both true of
// the same fleet node at the same time, and no heartbeat could ever succeed. The
// same shape was fixed for OAuth2 applications in #644, for pooled accounts in
// #535 and for projects in #634.
//
// A context that already names a tenant is left exactly as it is. Which tenant
// a caller may act as is an admission decision made above this layer, and this
// helper must never be the thing that redirects a write into a tenant the
// caller was not admitted to. go-qrdb folds the empty tenant onto the system
// tenant (tenant.IdentityNamespace), so a system-tenant record -- the embedded
// fleet node's -- occupies the namespace an unscoped write always used.
func fleetTenantContext(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" || TenantFromContext(ctx) != "" {
		return ctx
	}
	return ContextWithTenant(ctx, tenantID)
}

// RegisterFleetNode registers a new fleet node in the fleet registry. The fleet node's
// registration timestamps, initial version and default status are stamped onto
// the supplied record. Returns FleetNodeAlreadyExistsError if the fleet node ID is
// already taken within the tenant namespace.
func (r *fleetRegistry) RegisterFleetNode(ctx context.Context, agent *FleetNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}
	if agent == nil {
		// A nil record carries no tenant, so the failure cannot be attributed
		// to a tenant's audit trail.
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}

	ctx = fleetTenantContext(ctx, agent.TenantID)

	key, err := fleetNodeKey(agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}

	now := time.Now().UnixNano()
	agent.RegisteredAt = now
	agent.LastSeen = now
	agent.Version = 1
	if agent.Status == "" {
		agent.Status = FleetNodeStatusRegistered
	}

	data, err := marshalFleetNode(agent, "failed to marshal agent")
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}

	// REGISTRATION MUST NEVER OVERWRITE AN EXISTING RECORD, and the store is
	// what enforces that. It was previously an Exists probe followed by an
	// unconditional Put, held under the key lock — which is one atomic step in
	// this process and nothing at all across a fleet, because a mutex on one
	// node is invisible to another. Two nodes registering one fleet node ID could
	// both find the key empty, and the loser's write landed on top of the
	// winner's, replacing a live registration with a fresh one.
	//
	// The guarded create is decided inside go-qrdb's state machine, so of N
	// nodes registering one ID exactly one succeeds and the rest are told the
	// fleet node already exists.
	if r.creator == nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryRequired,
			Message:  "registration needs a store that can decide a create; this one cannot guard one",
		}
	}

	unlock := r.locks.lock(key)
	defer unlock()

	stored, err := r.creator.PutIfAbsent(ctx, key, data)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}
	if !stored {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return &FleetNodeAlreadyExistsError{NodeID: agent.ID}
	}

	r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, true)
	return nil
}

// GetFleetNode retrieves a fleet node by ID within a tenant namespace.
func (r *fleetRegistry) GetFleetNode(ctx context.Context, tenantID, nodeID string) (*FleetNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.checkStore(); err != nil {
		return nil, err
	}

	ctx = fleetTenantContext(ctx, tenantID)

	key, err := fleetNodeKey(tenantID, nodeID)
	if err != nil {
		return nil, err
	}

	agent, err := r.loadFleetNode(ctx, key, tenantID, nodeID)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// ListFleetNodes lists fleet nodes matching the filter within a tenant. The scan is
// bounded to the tenant's subtree, so the result can only ever contain that
// tenant's fleet nodes. Fleet nodes are returned in lexical agent-ID order; a positive
// filter.Limit truncates the result and stops the scan early.
func (r *fleetRegistry) ListFleetNodes(ctx context.Context, tenantID string, filter *FleetNodeFilter) ([]*FleetNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.checkStore(); err != nil {
		return nil, err
	}

	ctx = fleetTenantContext(ctx, tenantID)

	prefix, err := fleetNodePrefix(tenantID)
	if err != nil {
		return nil, err
	}
	agents := make([]*FleetNode, 0)
	err = r.kv.Scan(ctx, prefix, func(_ string, value []byte) error {
		agent := &FleetNode{}
		if unmarshalErr := json.Unmarshal(value, agent); unmarshalErr != nil {
			// One unreadable record must not hide the rest of the fleet from
			// an operator; skip it and keep listing. A KV-level failure below
			// still aborts, because that means the listing is incomplete in
			// ways the caller cannot see.
			return nil
		}
		// Defence in depth: the key already scopes this scan to tenantID, so a
		// record naming a different owner means the key was corrupted or
		// written out of band. Drop it rather than leak a foreign record.
		if agent.TenantID != tenantID {
			return nil
		}
		if !filter.Matches(agent) {
			return nil
		}

		agents = append(agents, agent)
		if filter != nil && filter.Limit > 0 && len(agents) >= filter.Limit {
			return errFleetScanDone
		}
		return nil
	})
	if err != nil && !errors.Is(err, errFleetScanDone) {
		return nil, err
	}

	return agents, nil
}

// UpdateFleetNode updates an existing fleet node registration. The supplied Version must
// match the stored version; on success the record's version is incremented and
// its last-seen timestamp refreshed.
func (r *fleetRegistry) UpdateFleetNode(ctx context.Context, agent *FleetNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}
	if agent == nil {
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}

	ctx = fleetTenantContext(ctx, agent.TenantID)
	// Update reports an unusable fleet node ID before an unusable tenant ID, which
	// is the order this method has always validated in.
	if err := validateKeySegment("agent_id", agent.ID); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return &FleetNodeKeyError{Cause: err}
	}

	key, err := fleetNodeKey(agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	current, err := r.loadFleetNode(ctx, key, agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	if agent.Version != current.Version {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryInvalidInput,
			Message:  "version conflict: expected=" + uitoa(agent.Version) + " actual=" + uitoa(current.Version),
		}
	}

	agent.Version = current.Version + 1
	agent.LastSeen = time.Now().UnixNano()

	data, err := marshalFleetNode(agent, "failed to marshal updated agent")
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	if err := r.kv.Put(ctx, key, data); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, true)
	return nil
}

// RefreshFleetNode converges an EXISTING record onto what a daemon registering
// again reports now, and is the second half of the registration contract:
// RegisterFleetNode creates and never overwrites, this one updates and never
// creates.
//
// A daemon re-registers on every restart, on certificate renewal and whenever
// its cached credentials are discarded, and each of those registrations
// carries the current truth about it -- the address a dispatcher must dial,
// the backends its runtime constructed, the OAuth2 clients an operator
// revokes through. Before this, all of it was thrown away: the guarded create
// answered "agent already exists", the register handler logged the refusal,
// and the record kept the values of the FIRST registration forever. A daemon
// that moved, changed port, or was upgraded from the pre-#681 transport
// vocabulary was dispatched to an address it had left or matched on backend
// names it did not serve (go-quicbaas#646).
//
// Refreshing is bounded to the record's OWNER. The fleet node ID is a sanitised
// derivation of the certificate CommonName, so distinct subjects can address
// one ID; a registration whose principal is not the one the record names is
// refused rather than applied, and nothing is written. A record naming no
// principal at all -- an operator-created registration -- is owned by no
// certificate and is equally untouchable.
//
// It converges what the registration reports and preserves what it does not:
// the record's identity (ID, TenantID) and its history (RegisteredAt) are
// kept, so an operator's view of when a daemon joined the fleet survives
// every re-enrollment.
func (r *fleetRegistry) RefreshFleetNode(ctx context.Context, agent *FleetNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}
	if agent == nil {
		// A nil record carries no tenant, so the failure cannot be attributed
		// to a tenant's audit trail.
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}

	ctx = fleetTenantContext(ctx, agent.TenantID)

	key, err := fleetNodeKey(agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, false)
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	current, err := r.loadFleetNode(ctx, key, agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, false)
		return err
	}

	if err := assertFleetNodePrincipal(current, agent); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, false)
		return err
	}

	// A fleet node an operator DISABLED is never resurrected by a registration --
	// the same rule #674 established for the identity enrollment. Disabling is
	// how a fleet member is taken out of service, and a daemon that keeps
	// restarting must not be able to undo it by re-enrolling.
	if current.Status == FleetNodeStatusDisabled || current.DisabledAt != 0 {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, false)
		return &FleetNodeDisabledError{TenantID: agent.TenantID, NodeID: agent.ID}
	}

	refreshed := *current
	refreshed.Name = agent.Name
	refreshed.Host = agent.Host
	refreshed.AdvertisedAddress = agent.AdvertisedAddress
	refreshed.Reachability = agent.Reachability
	refreshed.Backend = agent.Backend
	refreshed.Capabilities = agent.Capabilities
	refreshed.NodeType = agent.NodeType
	refreshed.Kind = agent.Kind
	refreshed.Metadata = agent.Metadata
	// Wholesale, not merged: a merge keeps a key nothing writes any more
	// forever, which is the staleness this method exists to end.
	refreshed.Status = FleetNodeStatusRegistered
	refreshed.LastSeen = time.Now().UnixNano()
	refreshed.Version = current.Version + 1

	data, err := marshalFleetNode(&refreshed, "failed to marshal refreshed agent")
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, false)
		return err
	}

	if err := r.kv.Put(ctx, key, data); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, false)
		return err
	}

	// The caller's record is left describing what was actually stored, so a
	// registration that reported nothing about a preserved column does not
	// hand its caller back a zero for it.
	*agent = refreshed

	r.audit(ctx, agent.TenantID, agent.ID, AuditActionReregister, true)
	return nil
}

// assertFleetNodePrincipal reports whether the registration presented may rewrite
// the record current. Both must name a principal and it must be the same one.
func assertFleetNodePrincipal(current, presented *FleetNode) error {
	recorded := FleetNodePrincipal(current)
	offered := FleetNodePrincipal(presented)
	if recorded != "" && recorded == offered {
		return nil
	}
	return &FleetNodePrincipalMismatchError{
		TenantID:  current.TenantID,
		NodeID:    current.ID,
		Recorded:  recorded,
		Presented: offered,
	}
}

// RemoveFleetNode removes a fleet node from the registry.
func (r *fleetRegistry) RemoveFleetNode(ctx context.Context, tenantID, nodeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}

	ctx = fleetTenantContext(ctx, tenantID)

	key, err := fleetNodeKey(tenantID, nodeID)
	if err != nil {
		r.audit(ctx, tenantID, nodeID, AuditActionDelete, false)
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	// Delete is idempotent in the KV contract, so presence is confirmed first:
	// removing a fleet node that is not there is reported, not silently accepted.
	if _, err := r.loadFleetNode(ctx, key, tenantID, nodeID); err != nil {
		r.audit(ctx, tenantID, nodeID, AuditActionDelete, false)
		return err
	}

	if err := r.kv.Delete(ctx, key); err != nil {
		r.audit(ctx, tenantID, nodeID, AuditActionDelete, false)
		return err
	}

	r.audit(ctx, tenantID, nodeID, AuditActionDelete, true)
	return nil
}

// Heartbeat updates the last-seen timestamp for a fleet node, transitions the
// fleet node status to running if it is currently registered or idle, and
// reconciles the record's backend axis against what the daemon reports it is
// serving.
//
// RECONCILIATION IS WHY THE OBSERVATION IS CARRIED. A registration is a claim
// made once; the served list on a heartbeat is the running truth, and the two
// diverge whenever a daemon restarts with a different backend set or is
// upgraded in place from an older vocabulary. Without this the record was
// written at enrollment and never corrected, so dispatch matched on names the
// daemon no longer served and missed ones it did (go-quicbaas#699).
//
// A nil observation reconciles nothing: the gRPC and MCP heartbeat verbs
// carry no served list, and a caller that reports nothing must not be read as
// reporting emptiness. An observation whose list MATCHES the record changes
// no backend column and emits no audit event -- only the liveness columns
// every heartbeat writes.
//
// Heartbeats are high-frequency liveness telemetry rather than a state change
// an operator would review, so the liveness write is deliberately not
// audited; a reconciliation is, because it rewrites what the fleet is
// dispatchable for. The path is one locked read plus one write -- never a
// scan.
func (r *fleetRegistry) Heartbeat(ctx context.Context, tenantID, nodeID string, observed *FleetNodeObservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}
	if err := observed.Validate(); err != nil {
		return err
	}

	ctx = fleetTenantContext(ctx, tenantID)

	key, err := fleetNodeKey(tenantID, nodeID)
	if err != nil {
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	agent, err := r.loadFleetNode(ctx, key, tenantID, nodeID)
	if err != nil {
		return err
	}

	agent.LastSeen = time.Now().UnixNano()
	if agent.Status == FleetNodeStatusRegistered || agent.Status == FleetNodeStatusIdle {
		agent.Status = FleetNodeStatusRunning
	}
	agent.Version++
	reconciled := reconcileFleetNodeBackends(agent, observed)
	if reconcileFleetNodeCapacity(agent, observed) {
		reconciled = true
	}
	if reconcileFleetNodeImages(agent, observed) {
		reconciled = true
	}
	// A fresh sample every beat is telemetry, not a change to the record's
	// identity or capacity, so it is not audited as an update.
	recordFleetNodeHostMetrics(agent, observed)

	data, err := marshalFleetNode(agent, "failed to marshal heartbeat agent")
	if err != nil {
		return err
	}

	if err := r.kv.Put(ctx, key, data); err != nil {
		if reconciled {
			r.audit(ctx, tenantID, nodeID, AuditActionUpdate, false)
		}
		return err
	}

	if reconciled {
		r.audit(ctx, tenantID, nodeID, AuditActionUpdate, true)
	}
	return nil
}

// reconcileFleetNodeBackends rewrites fleet node's backend axis to the set observed
// reports, reporting whether anything changed.
//
// The axis is the primary Backend plus the backend names in Capabilities. Host
// TRAITS in Capabilities -- "gpu" -- are preserved verbatim, because the
// served list a daemon heartbeats is Runtime.Backends() and carries backends
// only; taking it wholesale would un-advertise a GPU on the first heartbeat.
//
// The primary is the first served name. pkg/orchestration/fleet sorts the served set, so the
// primary a record settles on is stable across restarts rather than a function
// of map iteration order.
func reconcileFleetNodeBackends(agent *FleetNode, observed *FleetNodeObservation) bool {
	if agent == nil || observed == nil {
		return false
	}

	traits := make([]string, 0, len(agent.Capabilities))
	for _, capability := range agent.Capabilities {
		_, isTrait := fleetNodeCapabilityTraits[capability]
		// A signing-agent trait survives only an observation that says
		// nothing about it; one that reports the list replaces it below.
		keepSigning := isSigningAgentTrait(capability) && observed.SigningAgents == nil
		// The relay trait likewise survives only an observation silent on it.
		if capability == FleetNodeTraitSigningRelay && observed.SigningRelay != nil {
			continue
		}
		if isTrait || keepSigning {
			traits = append(traits, capability)
		}
	}
	for _, id := range observed.SigningAgents {
		traits = append(traits, SigningAgentTrait(id))
	}
	if observed.SigningRelay != nil && *observed.SigningRelay {
		traits = append(traits, FleetNodeTraitSigningRelay)
	}

	capabilities := make([]string, 0, len(observed.Backends)+len(traits))
	capabilities = append(capabilities, observed.Backends...)
	capabilities = append(capabilities, traits...)

	primary := observed.Backends[0]
	if agent.Backend == primary && sameStringSlice(agent.Capabilities, capabilities) {
		return false
	}

	agent.Backend = primary
	agent.Capabilities = capabilities
	return true
}

// reconcileFleetNodeCapacity rewrites the slot count the fleet sum reads from a
// record, reporting whether it changed.
//
// AN UNREPORTED COUNT LEAVES THE RECORD ALONE. Zero means "the daemon said
// nothing about its capacity" -- an older build, or one whose operator set no
// limit -- and taking it as an instruction would drop a host out of the fleet
// sum on its first heartbeat from that build. The count a record already
// carries stays until a daemon reports a different one, which is the same
// asymmetry the backend axis has for a nil observation.
//
// AN OPERATOR LOWERS A HOST'S CAPACITY BY REPORTING THE LOWER NUMBER, not by
// reporting none: draining a host is reported as the daemon leaving (its
// heartbeat stops, and FleetNodeAlive stops counting it), which is a different
// fact from a host that is up and can serve fewer jobs.
func reconcileFleetNodeCapacity(agent *FleetNode, observed *FleetNodeObservation) bool {
	if agent == nil || observed == nil || observed.MaxConcurrentJobs <= 0 {
		return false
	}
	if agent.MaxConcurrentJobs == observed.MaxConcurrentJobs {
		return false
	}
	agent.MaxConcurrentJobs = observed.MaxConcurrentJobs
	return true
}

// reconcileFleetNodeImages replaces the images a registration records with the
// ones an observation reports. An observation silent on images changes
// nothing: a daemon that could not list them this time has not lost them.
func reconcileFleetNodeImages(agent *FleetNode, observed *FleetNodeObservation) bool {
	if agent == nil || observed == nil || observed.Images == nil {
		return false
	}
	images := append([]string{}, *observed.Images...)
	sort.Strings(images)
	if agent.Images != nil && sameStringSlice(agent.Images, images) {
		return false
	}
	agent.Images = images
	return true
}

// recordFleetNodeHostMetrics replaces the host-metrics sample a registration
// records with the one an observation reports. An observation silent on host
// metrics keeps the previous sample, which then ages out at the stale bound
// (FleetHealthJudge).
func recordFleetNodeHostMetrics(agent *FleetNode, observed *FleetNodeObservation) {
	if agent == nil || observed == nil || observed.HostMetrics == nil {
		return
	}
	sample := *observed.HostMetrics
	agent.HostMetrics = &sample
}

// FleetNodeHasImage reports whether a registration can run a spawn that names
// image: true when no image is named, when the fleet node has never reported its
// images (unknown, an older build), or when the image is among them.
func FleetNodeHasImage(agent *FleetNode, image string) bool {
	if image == "" || agent == nil || agent.Images == nil {
		return true
	}
	for _, have := range agent.Images {
		if have == image {
			return true
		}
	}
	return false
}

// sameStringSlice reports whether two slices carry the same values in the same
// order. Order matters here: the fleet listing renders Capabilities as the
// operator sees it, so a reordering is a change worth writing once and then
// never again.
func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DeleteTenantFleetNodes deletes ALL fleet nodes for a tenant (cascade delete). Deleting
// a tenant that owns no fleet nodes succeeds.
func (r *fleetRegistry) DeleteTenantFleetNodes(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}

	ctx = fleetTenantContext(ctx, tenantID)

	prefix, err := fleetNodePrefix(tenantID)
	if err != nil {
		r.audit(ctx, tenantID, "", AuditActionDelete, false)
		return err
	}

	// The scan streams the tenant's subtree and collects keys only — never
	// record bodies, and never a key outside this tenant. Deletion runs after
	// the scan because mutating a store while one of its iterators is open is
	// outside the KVStore contract.
	keys := make([]string, 0)
	if err := r.kv.Scan(ctx, prefix, func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		r.audit(ctx, tenantID, "", AuditActionDelete, false)
		return err
	}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		nodeID := idFromKey(key, prefix)

		unlock := r.locks.lock(key)
		err := r.kv.Delete(ctx, key)
		unlock()

		if err != nil {
			r.audit(ctx, tenantID, nodeID, AuditActionDelete, false)
			return err
		}
		r.audit(ctx, tenantID, nodeID, AuditActionDelete, true)
	}

	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// checkStore rejects a registry that was constructed without a KV store. The
// registry fails closed rather than panicking on the first request.
func (r *fleetRegistry) checkStore() error {
	if r.kv == nil {
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryRequired,
			Message:  "kv store must not be nil",
		}
	}
	return nil
}

// loadFleetNode reads the record at key. A missing key, a nil value and a record
// owned by another tenant are all reported as not-found.
func (r *fleetRegistry) loadFleetNode(ctx context.Context, key, tenantID, nodeID string) (*FleetNode, error) {
	data, err := r.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &FleetNodeNotFoundError{TenantID: tenantID, NodeID: nodeID}
		}
		return nil, err
	}
	if data == nil {
		return nil, &FleetNodeNotFoundError{TenantID: tenantID, NodeID: nodeID}
	}

	agent := &FleetNode{}
	if err := json.Unmarshal(data, agent); err != nil {
		return nil, &FleetRegistryError{
			Sentinel: ErrFleetRegistryMarshal,
			Message:  "failed to unmarshal agent: " + err.Error(),
		}
	}

	// Defence in depth: the key already scopes the read to tenantID, so a
	// record naming a different owner means the key was corrupted or written
	// out of band. Report not-found rather than hand back a foreign record.
	if agent.TenantID != tenantID {
		return nil, &FleetNodeNotFoundError{TenantID: tenantID, NodeID: nodeID}
	}

	return agent, nil
}

// marshalFleetNode serializes a record, reporting failures under the registry's
// marshal sentinel prefixed with the caller's description of the operation.
func marshalFleetNode(agent *FleetNode, what string) ([]byte, error) {
	data, err := json.Marshal(agent)
	if err != nil {
		return nil, &FleetRegistryError{
			Sentinel: ErrFleetRegistryMarshal,
			Message:  what + ": " + err.Error(),
		}
	}
	return data, nil
}

// Matches reports whether fleet node satisfies every populated field of the
// filter. An empty field matches everything; a nil filter matches every
// fleet node.
//
// It is a method on the filter, exported, for the same reason
// JobFilter.Matches is: "does this record satisfy this narrowing" is the one
// answer the registry, the paged listing and every transport double must
// share, and a second copy of it is how a filter comes to mean two things.
func (filter *FleetNodeFilter) Matches(agent *FleetNode) bool {
	if agent == nil {
		return false
	}
	if filter == nil {
		return true
	}
	if filter.Host != "" && agent.Host != filter.Host {
		return false
	}
	if filter.Status != "" && agent.Status != filter.Status {
		return false
	}
	if filter.NodeType != "" && agent.NodeType != filter.NodeType {
		return false
	}
	if filter.Backend != "" && !FleetNodeServesBackend(agent, filter.Backend) {
		return false
	}
	if filter.Model != "" && agent.Model != filter.Model {
		return false
	}
	return true
}

// FleetNodeServesBackend reports whether the fleet node can run the named backend,
// as its primary Backend or as one of its Capabilities.
//
// ONE DAEMON SERVES SEVERAL BACKENDS FROM ONE RECORD. A daemon started with
// --backends acp serves claude, codex and opencode; the registrar records the
// first as the primary Backend and the whole list in Capabilities. Matching
// only the primary made a backend filter answer for one of the three and hide
// the daemon from the other two — which is how the remote backend resolver,
// whose candidate list comes from ListFleetNodes(&FleetNodeFilter{Backend: name}),
// found no daemon for a task routed on "codex" and dispatched it in-process
// instead (go-quicbaas#681).
//
// This is membership, not a redefinition of Backend: the primary keeps its
// meaning.
//
// Exported because it is THE answer to "can this fleet node run this backend", and
// it had three copies — this filter, findFleetNodeByBackend in control_loop.go,
// and pickDaemon in pkg/server/lifecycle_remote_resolver.go. Three copies of a
// vocabulary is how a vocabulary drifts, and only one of the three was
// consulted on the path that broke. There is now one.
func FleetNodeServesBackend(agent *FleetNode, backend string) bool {
	if agent.Backend == backend {
		return true
	}
	for _, capability := range agent.Capabilities {
		if capability == backend {
			return true
		}
	}
	return false
}

// audit records one store mutation. Reads and heartbeats never call it.
func (r *fleetRegistry) audit(ctx context.Context, tenantID, nodeID, action string, success bool) {
	emitStoreAudit(ctx, r.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceFleetNode,
		ResourceID: nodeID,
		Action:     action,
		Success:    success,
	})
}

// uitoa converts a uint64 to its decimal string representation
// without importing strconv.
func uitoa(val uint64) string {
	if val == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf) - 1
	for val > 0 {
		buf[i] = byte('0' + val%10)
		val /= 10
		i--
	}
	return string(buf[i+1:])
}
