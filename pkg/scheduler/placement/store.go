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

package placement

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sort"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Storage layout.
//
// RAW KV, data-access.md §3: placement records and node facts are case 5
// ("internal cluster metadata keyed by name, not by generated ID — placement
// records" is named there outright), and every record's identity is a
// caller-supplied name (case 6). They are raw rather than a DAO because the
// placement decision is ONE go-qrdb transaction comparing several records of
// different kinds at once — the node's facts, its status, its allocation,
// every group the request refers to, and the workload's own slot — which a
// per-entity DAO's conditional save cannot express.
//
// Everything lives under go-qrdb's reserved "__sys:" namespace, which keeps
// it out of user-facing List and Scan and out of MVCC restore. As with
// pkg/registry, a non-empty tenant isolates its records under
// "__sys:placement-t/{tenant}/"; the empty tenant (single-tenant mode) uses
// "__sys:placement/".
const (
	keyPrefix       = "__sys:placement/"
	tenantKeyPrefix = "__sys:placement-t/"

	nodesDir     = "nodes/"     // leased node facts, one per node
	statusDir    = "status/"    // cordon / drain state, one per node
	allocDir     = "alloc/"     // allocated totals, one per node
	groupsDir    = "groups/"    // per-node placement counts, one per group
	workloadsDir = "workloads/" // placement records, one per workload
)

// maxAttempts bounds how many times Place and Release re-run after losing a
// guarded write, mirroring pkg/cas.MaxAttempts.
const maxAttempts = 16

// Backend is the go-qrdb surface placement is built on. NewClientBackend
// adapts a qrdb SDK client; tests may supply another implementation.
type Backend interface {
	// Grant creates a lease with the given TTL in seconds.
	Grant(ctx context.Context, ttlSeconds int64) (int64, error)

	// KeepAlive refreshes a lease's TTL. A lease that no longer exists is
	// reported as go-qrdb's *transport.LeaseNotFoundError or
	// *transport.LeaseExpiredError; any other error is operational and says
	// nothing about the lease.
	KeepAlive(ctx context.Context, leaseID int64) error

	// Revoke expires a lease, deleting the keys attached to it.
	Revoke(ctx context.Context, leaseID int64) error

	// PutWithLease stores a key attached to a lease.
	PutWithLease(ctx context.Context, key string, value []byte, leaseID int64) error

	// Get returns a key's value, or nil with no error when it is absent.
	Get(ctx context.Context, key string) ([]byte, error)

	// Put stores a key with no lease.
	Put(ctx context.Context, key string, value []byte) error

	// Delete removes a key. Deleting an absent key is not an error.
	Delete(ctx context.Context, key string) error

	// Scan returns every key-value pair under a prefix.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)

	// Commit executes a go-qrdb transaction and reports whether its
	// compares held, in which case its success operations were applied.
	Commit(ctx context.Context, req *transport.TransactionRequest) (bool, error)
}

// clientBackend adapts a qrdb SDK client to Backend.
type clientBackend struct {
	client *qrdbsdk.Client
	binder qrdbservice.Binder
}

// NewClientBackend returns a Backend over a qrdb SDK client. The placement
// store is quicbaasd's own system-tenant state (every key sits under the
// system namespace, the tenant only names a sub-prefix), so every call runs
// as the system service principal binder binds. A nil binder leaves the
// caller's context as it is: a client with no embedded node authenticates
// through its own transport.
func NewClientBackend(client *qrdbsdk.Client, binder qrdbservice.Binder) Backend {
	return &clientBackend{client: client, binder: binder}
}

// bind runs ctx as the system service principal.
func (b *clientBackend) bind(ctx context.Context) (context.Context, error) {
	if b.binder == nil {
		return ctx, nil
	}
	// The service principal is a system identity, and qrdb's self-scope
	// check refuses a system identity acting for another tenant, so the
	// backend call runs as the system tenant. The caller's tenant is not
	// lost: it is already in every placement key's prefix.
	return b.binder.ServiceContext(tenantctx.WithSystem(ctx))
}

func (b *clientBackend) Grant(ctx context.Context, ttlSeconds int64) (int64, error) {
	ctx, err := b.bind(ctx)
	if err != nil {
		return 0, err
	}
	info, err := b.client.Lease().LeaseGrant(ctx, ttlSeconds)
	if err != nil {
		return 0, err
	}
	return info.ID, nil
}

func (b *clientBackend) KeepAlive(ctx context.Context, leaseID int64) error {
	ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	_, err = b.client.Lease().LeaseKeepAlive(ctx, leaseID)
	return err
}

func (b *clientBackend) Revoke(ctx context.Context, leaseID int64) error {
	ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	return b.client.Lease().LeaseRevoke(ctx, leaseID)
}

func (b *clientBackend) PutWithLease(ctx context.Context, key string, value []byte, leaseID int64) error {
	ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	_, err = b.client.MVCC().PutWithLease(ctx, key, value, leaseID)
	return err
}

func (b *clientBackend) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, err := b.bind(ctx)
	if err != nil {
		return nil, err
	}
	value, err := b.client.KV().Get(ctx, key)
	if err != nil {
		var notFound *transport.KeyNotFoundError
		if stderrors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}
	return value, nil
}

func (b *clientBackend) Put(ctx context.Context, key string, value []byte) error {
	ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	return b.client.KV().Put(ctx, key, value)
}

func (b *clientBackend) Delete(ctx context.Context, key string) error {
	ctx, err := b.bind(ctx)
	if err != nil {
		return err
	}
	err = b.client.KV().Delete(ctx, key)
	var notFound *transport.KeyNotFoundError
	if stderrors.As(err, &notFound) {
		return nil
	}
	return err
}

func (b *clientBackend) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	ctx, err := b.bind(ctx)
	if err != nil {
		return nil, err
	}
	return b.client.KV().Scan(ctx, prefix)
}

func (b *clientBackend) Commit(ctx context.Context, req *transport.TransactionRequest) (bool, error) {
	ctx, err := b.bind(ctx)
	if err != nil {
		return false, err
	}
	resp, err := b.client.Transaction().ExecuteTransaction(ctx, req)
	if err != nil {
		return false, err
	}
	return resp.Succeeded, nil
}

// nodeRecord is the leased node record.
//
// Incarnation identifies one continuous life of the node: it is the lease
// granted when the node registered while it had no live record, and it is
// carried across every re-registration that finds the record still live
// (a heartbeat-style refresh, a TTL change). A node whose lease expired, or
// that deregistered, comes back with a new incarnation, so nothing recorded
// against its previous life is mistaken for state of the new one.
type nodeRecord struct {
	Facts        NodeFacts `json:"facts"`
	LeaseID      int64     `json:"lease_id"`
	Incarnation  int64     `json:"incarnation"`
	TTLSeconds   int64     `json:"ttl_seconds"`
	RegisteredAt time.Time `json:"registered_at"`
}

// allocation is a node's allocated totals for one incarnation. Totals
// recorded against an earlier incarnation are stale: the workloads they
// counted died with that incarnation, so they neither constrain a new
// placement nor show in NodeInfo.
type allocation struct {
	Incarnation int64            `json:"incarnation"`
	Allocated   map[string]int64 `json:"allocated,omitempty"`
	Placements  int64            `json:"placements"`
}

// current returns the allocation as it applies to incarnation: itself when
// it was recorded against that incarnation, empty otherwise.
func (a allocation) current(incarnation int64) allocation {
	if a.Incarnation != incarnation {
		return allocation{Incarnation: incarnation}
	}
	return a
}

// placementRecord is the stored form of a placement: the public Placement
// plus the node incarnation it was placed against. A placement is live only
// while its node's current incarnation is that one.
type placementRecord struct {
	Placement
	NodeIncarnation int64 `json:"node_incarnation"`
}

// groupRecord is a group's stored placement counts keyed by groupMember, so
// counts from a node's earlier incarnation never count toward affinity or
// spread for its new one.
type groupRecord map[string]int64

// groupMember is a groupRecord key: the node ID and its incarnation. Node
// IDs never contain '/' (validateID), so the key is unambiguous.
func groupMember(nodeID string, incarnation int64) string {
	return nodeID + "/" + strconv.FormatInt(incarnation, 10)
}

// liveCounts projects a group's stored counts onto the live nodes of a
// snapshot: node ID -> count, keeping only entries of each node's current
// incarnation.
func (g groupRecord) liveCounts(snap *snapshotState) map[string]int64 {
	out := make(map[string]int64, len(g))
	for member, n := range g {
		nodeID, inc, ok := strings.Cut(member, "/")
		if !ok {
			continue
		}
		node, live := snap.nodes[nodeID]
		if !live || strconv.FormatInt(node.record.Incarnation, 10) != inc {
			continue
		}
		out[nodeID] += n
	}
	return out
}

// Placer is the placement engine bound to a store. Every method takes the
// tenant explicitly; the service layer resolves it from the authenticated
// request, so this package carries no identity logic of its own.
type Placer struct {
	backend Backend
	now     func() time.Time
}

// New returns a Placer over backend.
func New(backend Backend) *Placer {
	return &Placer{backend: backend, now: func() time.Time { return time.Now().UTC() }}
}

// NewFromClient returns a Placer over a qrdb SDK client whose calls run as
// the system service principal binder binds (see NewClientBackend).
func NewFromClient(client *qrdbsdk.Client, binder qrdbservice.Binder) *Placer {
	return New(NewClientBackend(client, binder))
}

// prefix returns the tenant's record namespace.
func prefix(tenant string) string {
	if tenant == "" {
		return keyPrefix
	}
	return tenantKeyPrefix + tenant + "/"
}

// key builds a record key.
func key(tenant, dir, id string) string {
	return prefix(tenant) + dir + id
}

// decode unmarshals a stored record, failing closed on corruption.
func decode(k string, raw []byte, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return &CorruptRecordError{Key: k, Err: err}
	}
	return nil
}

// encode marshals a record. Every type stored here marshals infallibly, so
// a failure is a programming error surfaced as a StoreError.
func encode(op string, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, &StoreError{Op: op + ": encode", Err: err}
	}
	return raw, nil
}

// get reads and decodes one record. found is false when it is absent.
func (p *Placer) get(ctx context.Context, op, k string, into any) (raw []byte, found bool, err error) {
	raw, err = p.backend.Get(ctx, k)
	if err != nil {
		return nil, false, &StoreError{Op: op, Err: err}
	}
	if raw == nil {
		return nil, false, nil
	}
	if err := decode(k, raw, into); err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// ---------------------------------------------------------------------------
// Nodes
// ---------------------------------------------------------------------------

// RegisterNode registers or refreshes a node's facts under a lease of ttl
// (rounded down to whole seconds, at least one). A live node re-registering
// keeps its lease and its incarnation; it must use the TTL it registered
// with, because go-qrdb cannot move a key to a new lease without the old
// lease's expiry deleting it, so a TTL change could only be made by
// dropping the node. A node with no live record — new, expired or
// deregistered — starts a new incarnation. The node's cordon and drain
// status survive re-registration.
func (p *Placer) RegisterNode(ctx context.Context, tenant string, facts *NodeFacts, ttl time.Duration) (*NodeInfo, error) {
	if err := ValidateNodeFacts(facts); err != nil {
		return nil, err
	}
	ttlSeconds := int64(ttl / time.Second)
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}
	nodeKey := key(tenant, nodesDir, facts.NodeID)

	var existing nodeRecord
	_, found, err := p.get(ctx, "register: read node", nodeKey, &existing)
	if err != nil {
		return nil, err
	}

	// continuing is set while the node's previous life is still going: its
	// record was live when read and the refresh found the lease alive.
	// Only then does the node keep its incarnation.
	leaseID := int64(0)
	continuing := false
	if found {
		if existing.TTLSeconds != ttlSeconds {
			return nil, &InvalidRequestError{Reason: "node " + facts.NodeID + " is live with ttl_seconds " +
				strconv.FormatInt(existing.TTLSeconds, 10) + "; a live node re-registers with the TTL it has " +
				"(deregister it, which orphans its placements, to change the TTL)"}
		}
		err := p.backend.KeepAlive(ctx, existing.LeaseID)
		switch {
		case err == nil:
			leaseID = existing.LeaseID
			continuing = true
		case leaseGone(err):
			// The lease expired between the read and now: the node's
			// previous life ended, and a fresh lease and incarnation are
			// granted below.
		default:
			// The store did not answer. Treating that as an expiry would
			// start a new incarnation and orphan workloads that are still
			// running, so the refresh fails instead.
			return nil, &StoreError{Op: "register: keep-alive", Err: err}
		}
	}

	granted := false
	if leaseID == 0 {
		leaseID, err = p.backend.Grant(ctx, ttlSeconds)
		if err != nil {
			return nil, &StoreError{Op: "register: grant lease", Err: err}
		}
		granted = true
	}
	incarnation := leaseID
	if continuing {
		incarnation = existing.Incarnation
	}

	record := nodeRecord{Facts: *facts, LeaseID: leaseID, Incarnation: incarnation, TTLSeconds: ttlSeconds, RegisteredAt: p.now()}
	raw, err := encode("register", record)
	if err != nil {
		return nil, err
	}
	if err := p.backend.PutWithLease(ctx, nodeKey, raw, leaseID); err != nil {
		putErr := &StoreError{Op: "register: put node", Err: err}
		if granted {
			// A lease granted for a record that was never written would
			// only expire; revoking it now is tidier, and a failure to do
			// so is reported beside the cause rather than discarded.
			if revokeErr := p.backend.Revoke(context.WithoutCancel(ctx), leaseID); revokeErr != nil {
				return nil, stderrors.Join(putErr, &StoreError{Op: "register: revoke orphaned lease", Err: revokeErr})
			}
		}
		return nil, putErr
	}
	return p.nodeInfo(ctx, tenant, record)
}

// leaseGone reports whether a KeepAlive error says the lease no longer
// exists, as opposed to the store failing to answer.
func leaseGone(err error) bool {
	var notFound *transport.LeaseNotFoundError
	var expired *transport.LeaseExpiredError
	return stderrors.As(err, &notFound) || stderrors.As(err, &expired)
}

// revokeLease revokes a node's lease. A revoke that fails because the lease
// already expired is not an error: the record it guarded is gone either way.
func (p *Placer) revokeLease(ctx context.Context, nodeKey string, leaseID int64) error {
	err := p.backend.Revoke(ctx, leaseID)
	if err == nil {
		return nil
	}
	raw, getErr := p.backend.Get(ctx, nodeKey)
	if getErr != nil {
		return &StoreError{Op: "revoke lease", Err: stderrors.Join(err, getErr)}
	}
	if raw == nil {
		return nil
	}
	return &StoreError{Op: "revoke lease", Err: err}
}

// RenewNode heartbeats a node's lease.
func (p *Placer) RenewNode(ctx context.Context, tenant, nodeID string) error {
	if err := validateID("node_id", nodeID); err != nil {
		return err
	}
	var record nodeRecord
	_, found, err := p.get(ctx, "renew: read node", key(tenant, nodesDir, nodeID), &record)
	if err != nil {
		return err
	}
	if !found {
		return &NodeNotFoundError{NodeID: nodeID}
	}
	if err := p.backend.KeepAlive(ctx, record.LeaseID); err != nil {
		if leaseGone(err) {
			return &NodeNotFoundError{NodeID: nodeID}
		}
		return &StoreError{Op: "renew: keep-alive", Err: err}
	}
	return nil
}

// DeregisterNode removes a node: its lease is revoked, so its facts are
// deleted, and its cordon/drain status is cleared. Placements on it are
// kept and reported orphaned until they are released.
func (p *Placer) DeregisterNode(ctx context.Context, tenant, nodeID string) error {
	if err := validateID("node_id", nodeID); err != nil {
		return err
	}
	nodeKey := key(tenant, nodesDir, nodeID)
	var record nodeRecord
	_, found, err := p.get(ctx, "deregister: read node", nodeKey, &record)
	if err != nil {
		return err
	}
	if !found {
		return &NodeNotFoundError{NodeID: nodeID}
	}
	if err := p.revokeLease(ctx, nodeKey, record.LeaseID); err != nil {
		return err
	}
	if err := p.backend.Delete(ctx, key(tenant, statusDir, nodeID)); err != nil {
		return &StoreError{Op: "deregister: delete status", Err: err}
	}
	return nil
}

// GetNode returns a live node.
func (p *Placer) GetNode(ctx context.Context, tenant, nodeID string) (*NodeInfo, error) {
	if err := validateID("node_id", nodeID); err != nil {
		return nil, err
	}
	var record nodeRecord
	_, found, err := p.get(ctx, "get node", key(tenant, nodesDir, nodeID), &record)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &NodeNotFoundError{NodeID: nodeID}
	}
	return p.nodeInfo(ctx, tenant, record)
}

// nodeInfo assembles a NodeInfo from a node record plus its status and
// allocation records.
func (p *Placer) nodeInfo(ctx context.Context, tenant string, record nodeRecord) (*NodeInfo, error) {
	var status NodeStatus
	if _, _, err := p.get(ctx, "read status", key(tenant, statusDir, record.Facts.NodeID), &status); err != nil {
		return nil, err
	}
	var stored allocation
	if _, _, err := p.get(ctx, "read allocation", key(tenant, allocDir, record.Facts.NodeID), &stored); err != nil {
		return nil, err
	}
	alloc := stored.current(record.Incarnation)
	return &NodeInfo{
		Facts:        record.Facts,
		Status:       status,
		LeaseID:      record.LeaseID,
		TTLSeconds:   record.TTLSeconds,
		Allocated:    alloc.Allocated,
		Placements:   alloc.Placements,
		RegisteredAt: record.RegisteredAt,
	}, nil
}

// ListNodes returns every live node, ordered by node ID.
func (p *Placer) ListNodes(ctx context.Context, tenant string) ([]*NodeInfo, error) {
	snap, err := p.snapshot(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := make([]*NodeInfo, 0, len(snap.nodes))
	for _, id := range sortedKeys(snap.nodes) {
		n := snap.nodes[id]
		alloc := n.alloc.current(n.record.Incarnation)
		out = append(out, &NodeInfo{
			Facts:        n.record.Facts,
			Status:       n.status,
			LeaseID:      n.record.LeaseID,
			TTLSeconds:   n.record.TTLSeconds,
			Allocated:    alloc.Allocated,
			Placements:   alloc.Placements,
			RegisteredAt: n.record.RegisteredAt,
		})
	}
	return out, nil
}

// setStatus writes a live node's status, or clears it when it is the zero
// status. Place compares the status record inside its transaction, so a
// cordon or drain written here is never overtaken by a concurrent Place.
func (p *Placer) setStatus(ctx context.Context, op, tenant, nodeID string, mutate func(*NodeStatus)) (*NodeInfo, error) {
	if err := validateID("node_id", nodeID); err != nil {
		return nil, err
	}
	var record nodeRecord
	_, found, err := p.get(ctx, op+": read node", key(tenant, nodesDir, nodeID), &record)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &NodeNotFoundError{NodeID: nodeID}
	}
	statusKey := key(tenant, statusDir, nodeID)
	var status NodeStatus
	if _, _, err := p.get(ctx, op+": read status", statusKey, &status); err != nil {
		return nil, err
	}
	mutate(&status)
	if status == (NodeStatus{}) {
		if err := p.backend.Delete(ctx, statusKey); err != nil {
			return nil, &StoreError{Op: op + ": clear status", Err: err}
		}
	} else {
		raw, err := encode(op, status)
		if err != nil {
			return nil, err
		}
		if err := p.backend.Put(ctx, statusKey, raw); err != nil {
			return nil, &StoreError{Op: op + ": write status", Err: err}
		}
	}
	return p.nodeInfo(ctx, tenant, record)
}

// CordonNode marks a node unschedulable. Placements on it stay.
func (p *Placer) CordonNode(ctx context.Context, tenant, nodeID string) (*NodeInfo, error) {
	return p.setStatus(ctx, "cordon", tenant, nodeID, func(s *NodeStatus) { s.Unschedulable = true })
}

// UncordonNode returns a node to service, clearing both cordon and drain.
func (p *Placer) UncordonNode(ctx context.Context, tenant, nodeID string) (*NodeInfo, error) {
	return p.setStatus(ctx, "uncordon", tenant, nodeID, func(s *NodeStatus) { *s = NodeStatus{} })
}

// DrainNode marks a node draining (and cordoned) and returns the placements
// on it, which the caller moves: it stops each workload, releases it and
// places it again. Draining does not release anything itself.
func (p *Placer) DrainNode(ctx context.Context, tenant, nodeID string) ([]*Placement, error) {
	if _, err := p.setStatus(ctx, "drain", tenant, nodeID, func(s *NodeStatus) {
		s.Unschedulable = true
		s.Draining = true
	}); err != nil {
		return nil, err
	}
	return p.ListPlacements(ctx, tenant, &PlacementFilter{NodeID: nodeID})
}

// ---------------------------------------------------------------------------
// Placements
// ---------------------------------------------------------------------------

// liveNode is one live node in a snapshot, with the raw bytes Place guards.
type liveNode struct {
	record    nodeRecord
	recordRaw []byte
	status    NodeStatus
	statusRaw []byte
	alloc     allocation
	allocRaw  []byte
}

// snapshotState is the store state one Place attempt decides over.
type snapshotState struct {
	nodes map[string]*liveNode
}

// snapshot reads every live node with its status and allocation.
func (p *Placer) snapshot(ctx context.Context, tenant string) (*snapshotState, error) {
	base := prefix(tenant)
	nodesRaw, err := p.backend.Scan(ctx, base+nodesDir)
	if err != nil {
		return nil, &StoreError{Op: "scan nodes", Err: err}
	}
	statusRaw, err := p.backend.Scan(ctx, base+statusDir)
	if err != nil {
		return nil, &StoreError{Op: "scan status", Err: err}
	}
	allocRaw, err := p.backend.Scan(ctx, base+allocDir)
	if err != nil {
		return nil, &StoreError{Op: "scan allocations", Err: err}
	}

	snap := &snapshotState{nodes: make(map[string]*liveNode, len(nodesRaw))}
	for k, raw := range nodesRaw {
		n := &liveNode{recordRaw: raw}
		if err := decode(k, raw, &n.record); err != nil {
			return nil, err
		}
		id := strings.TrimPrefix(k, base+nodesDir)
		if s, ok := statusRaw[base+statusDir+id]; ok {
			n.statusRaw = s
			if err := decode(base+statusDir+id, s, &n.status); err != nil {
				return nil, err
			}
		}
		if a, ok := allocRaw[base+allocDir+id]; ok {
			n.allocRaw = a
			if err := decode(base+allocDir+id, a, &n.alloc); err != nil {
				return nil, err
			}
		}
		snap.nodes[id] = n
	}
	return snap, nil
}

// referencedGroups lists every group a request reads or writes.
func referencedGroups(req *PlacementRequest) []string {
	set := make(map[string]struct{})
	if req.Group != "" {
		set[req.Group] = struct{}{}
	}
	for _, t := range req.Affinity {
		set[t.Group] = struct{}{}
	}
	for _, t := range req.AntiAffinity {
		set[t.Group] = struct{}{}
	}
	return sortedKeys(set)
}

// guard is a compare on one record: equal to the bytes read, or absent when
// nothing was read.
func guard(k string, held []byte) transport.Compare {
	if held == nil {
		return transport.Compare{
			Key:            []byte(k),
			Target:         transport.CompareCreateRevision,
			Result:         transport.CompareEqual,
			CreateRevision: 0,
		}
	}
	return transport.Compare{
		Key:    []byte(k),
		Target: transport.CompareValue,
		Result: transport.CompareEqual,
		Value:  held,
	}
}

// Place places one workload. It is idempotent per WorkloadID: a workload
// already placed on a live, non-draining node gets its existing placement
// back with Existing set. A workload placed on a node whose lease expired,
// or that is draining, is refused with PlacementConflictError — Place never
// moves a running workload implicitly; the caller releases it first.
//
// The decision commits in one go-qrdb transaction guarded on the chosen
// node's facts, status and allocation, on every group the request refers
// to, and on the workload's slot being empty. Any concurrent change to
// those — another placement on the node or in a referenced group, a
// cordon, a drain, an expired lease, re-registered facts — fails the
// guard, and the decision is re-run against fresh state.
func (p *Placer) Place(ctx context.Context, tenant string, req *PlacementRequest) (*PlaceResult, error) {
	if err := ValidatePlacementRequest(req); err != nil {
		return nil, err
	}
	groupNames := referencedGroups(req)
	workloadKey := key(tenant, workloadsDir, req.WorkloadID)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var existing placementRecord
		_, placed, err := p.get(ctx, "place: read workload", workloadKey, &existing)
		if err != nil {
			return nil, err
		}

		snap, err := p.snapshot(ctx, tenant)
		if err != nil {
			return nil, err
		}

		if placed {
			return p.existingPlacement(&existing, snap)
		}

		groups := make(groupCounts, len(groupNames))
		stored := make(map[string]groupRecord, len(groupNames))
		groupRaw := make(map[string][]byte, len(groupNames))
		for _, g := range groupNames {
			record := make(groupRecord)
			raw, _, err := p.get(ctx, "place: read group", key(tenant, groupsDir, g), &record)
			if err != nil {
				return nil, err
			}
			groups[g] = record.liveCounts(snap)
			stored[g] = record
			groupRaw[g] = raw
		}

		candidates := make([]candidate, 0, len(snap.nodes))
		for _, id := range sortedKeys(snap.nodes) {
			n := snap.nodes[id]
			candidates = append(candidates, candidate{
				facts:     n.record.Facts,
				status:    n.status,
				allocated: n.alloc.current(n.record.Incarnation).Allocated,
			})
		}

		d := evaluate(req, candidates, groups)
		if len(d.ranked) == 0 {
			return nil, &NoFeasibleNodeError{WorkloadID: req.WorkloadID, Rejections: d.rejections}
		}
		best := d.ranked[0]
		node := snap.nodes[best.node.facts.NodeID]

		record := &placementRecord{
			Placement: Placement{
				WorkloadID: req.WorkloadID,
				Group:      req.Group,
				NodeID:     best.node.facts.NodeID,
				Resources:  req.Resources,
				Labels:     req.Labels,
				Score:      best.score,
				CreatedAt:  p.now(),
			},
			NodeIncarnation: node.record.Incarnation,
		}
		committed, err := p.commitPlace(ctx, tenant, record, node, stored, groupRaw, groupNames)
		if err != nil {
			return nil, err
		}
		if committed {
			placement := record.Placement
			return &PlaceResult{Placement: &placement}, nil
		}
	}
	return nil, &ContendedError{Op: "place", Attempts: maxAttempts}
}

// liveIncarnation reports whether a placement's node is live in its
// recorded incarnation. A node that expired or deregistered and came back
// under the same ID is a new incarnation, and does not revive the placement.
func liveIncarnation(record *placementRecord, node *liveNode) bool {
	return node != nil && node.record.Incarnation == record.NodeIncarnation
}

// existingPlacement returns an already-recorded placement, or the conflict
// that keeps it from being returned.
func (p *Placer) existingPlacement(existing *placementRecord, snap *snapshotState) (*PlaceResult, error) {
	node := snap.nodes[existing.NodeID]
	if node == nil {
		return nil, &PlacementConflictError{WorkloadID: existing.WorkloadID, NodeID: existing.NodeID, Reason: "the node's lease has expired"}
	}
	if !liveIncarnation(existing, node) {
		return nil, &PlacementConflictError{WorkloadID: existing.WorkloadID, NodeID: existing.NodeID, Reason: "the node has re-registered since the workload was placed on it"}
	}
	if node.status.Draining {
		return nil, &PlacementConflictError{WorkloadID: existing.WorkloadID, NodeID: existing.NodeID, Reason: "the node is draining"}
	}
	placement := existing.Placement
	return &PlaceResult{Placement: &placement, Existing: true}, nil
}

// commitPlace writes one placement decision in a single guarded transaction.
func (p *Placer) commitPlace(
	ctx context.Context,
	tenant string,
	record *placementRecord,
	node *liveNode,
	groups map[string]groupRecord,
	groupRaw map[string][]byte,
	groupNames []string,
) (bool, error) {
	nodeID := record.NodeID
	allocKey := key(tenant, allocDir, nodeID)

	held := node.alloc.current(record.NodeIncarnation)
	next := allocation{
		Incarnation: record.NodeIncarnation,
		Allocated:   make(map[string]int64, len(held.Allocated)),
		Placements:  held.Placements + 1,
	}
	for name, amount := range held.Allocated {
		next.Allocated[name] = amount
	}
	for name, amount := range record.Resources {
		next.Allocated[name] += amount
	}
	allocRaw, err := encode("place", next)
	if err != nil {
		return false, err
	}
	placementRaw, err := encode("place", record)
	if err != nil {
		return false, err
	}

	req := &transport.TransactionRequest{
		Compares: []transport.Compare{
			guard(key(tenant, nodesDir, nodeID), node.recordRaw),
			guard(key(tenant, statusDir, nodeID), node.statusRaw),
			guard(allocKey, node.allocRaw),
			guard(key(tenant, workloadsDir, record.WorkloadID), nil),
		},
		SuccessOps: []transport.Op{
			{Type: transport.OpPut, Key: []byte(allocKey), Value: allocRaw},
			{Type: transport.OpPut, Key: []byte(key(tenant, workloadsDir, record.WorkloadID)), Value: placementRaw},
		},
	}
	for _, g := range groupNames {
		req.Compares = append(req.Compares, guard(key(tenant, groupsDir, g), groupRaw[g]))
	}
	if record.Group != "" {
		counts := make(groupRecord, len(groups[record.Group])+1)
		for member, n := range groups[record.Group] {
			counts[member] = n
		}
		counts[groupMember(nodeID, record.NodeIncarnation)]++
		raw, err := encode("place", counts)
		if err != nil {
			return false, err
		}
		req.SuccessOps = append(req.SuccessOps, transport.Op{Type: transport.OpPut, Key: []byte(key(tenant, groupsDir, record.Group)), Value: raw})
	}

	ok, err := p.backend.Commit(ctx, req)
	if err != nil {
		return false, &StoreError{Op: "place: commit", Err: err}
	}
	return ok, nil
}

// Release removes a workload's placement and returns its resources to the
// node. It is how a caller frees a workload it stopped, and how it moves a
// workload off a draining or dead node before placing it again.
func (p *Placer) Release(ctx context.Context, tenant, workloadID string) error {
	if err := validateID("workload_id", workloadID); err != nil {
		return err
	}
	workloadKey := key(tenant, workloadsDir, workloadID)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var record placementRecord
		placementRaw, found, err := p.get(ctx, "release: read workload", workloadKey, &record)
		if err != nil {
			return err
		}
		if !found {
			return &PlacementNotFoundError{WorkloadID: workloadID}
		}
		committed, err := p.commitRelease(ctx, tenant, &record, placementRaw)
		if err != nil {
			return err
		}
		if committed {
			return nil
		}
	}
	return &ContendedError{Op: "release", Attempts: maxAttempts}
}

// commitRelease removes one placement in a single guarded transaction. Its
// resources come off the node's allocation only when that allocation is
// still the one the placement was recorded against: an allocation of a
// later incarnation never counted it.
func (p *Placer) commitRelease(ctx context.Context, tenant string, record *placementRecord, placementRaw []byte) (bool, error) {
	workloadKey := key(tenant, workloadsDir, record.WorkloadID)
	allocKey := key(tenant, allocDir, record.NodeID)

	var alloc allocation
	allocRaw, _, err := p.get(ctx, "release: read allocation", allocKey, &alloc)
	if err != nil {
		return false, err
	}

	req := &transport.TransactionRequest{
		Compares: []transport.Compare{
			guard(workloadKey, placementRaw),
			guard(allocKey, allocRaw),
		},
		SuccessOps: []transport.Op{{Type: transport.OpDelete, Key: []byte(workloadKey)}},
	}

	if allocRaw != nil && alloc.Incarnation == record.NodeIncarnation {
		next := allocation{Incarnation: alloc.Incarnation, Allocated: make(map[string]int64), Placements: alloc.Placements - 1}
		for name, amount := range alloc.Allocated {
			if remaining := amount - record.Resources[name]; remaining > 0 {
				next.Allocated[name] = remaining
			}
		}
		if next.Placements <= 0 && len(next.Allocated) == 0 {
			req.SuccessOps = append(req.SuccessOps, transport.Op{Type: transport.OpDelete, Key: []byte(allocKey)})
		} else {
			if next.Placements < 0 {
				next.Placements = 0
			}
			raw, err := encode("release", next)
			if err != nil {
				return false, err
			}
			req.SuccessOps = append(req.SuccessOps, transport.Op{Type: transport.OpPut, Key: []byte(allocKey), Value: raw})
		}
	}

	if record.Group != "" {
		groupKey := key(tenant, groupsDir, record.Group)
		counts := make(groupRecord)
		groupRaw, _, err := p.get(ctx, "release: read group", groupKey, &counts)
		if err != nil {
			return false, err
		}
		req.Compares = append(req.Compares, guard(groupKey, groupRaw))
		member := groupMember(record.NodeID, record.NodeIncarnation)
		counts[member]--
		if counts[member] <= 0 {
			delete(counts, member)
		}
		if len(counts) == 0 {
			req.SuccessOps = append(req.SuccessOps, transport.Op{Type: transport.OpDelete, Key: []byte(groupKey)})
		} else {
			raw, err := encode("release", counts)
			if err != nil {
				return false, err
			}
			req.SuccessOps = append(req.SuccessOps, transport.Op{Type: transport.OpPut, Key: []byte(groupKey), Value: raw})
		}
	}

	ok, err := p.backend.Commit(ctx, req)
	if err != nil {
		return false, &StoreError{Op: "release: commit", Err: err}
	}
	return ok, nil
}

// GetPlacement returns a workload's placement, marked orphaned when its
// node's lease has expired or the node has re-registered since.
func (p *Placer) GetPlacement(ctx context.Context, tenant, workloadID string) (*Placement, error) {
	if err := validateID("workload_id", workloadID); err != nil {
		return nil, err
	}
	var record placementRecord
	_, found, err := p.get(ctx, "get placement", key(tenant, workloadsDir, workloadID), &record)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &PlacementNotFoundError{WorkloadID: workloadID}
	}
	var node nodeRecord
	_, live, err := p.get(ctx, "get placement: read node", key(tenant, nodesDir, record.NodeID), &node)
	if err != nil {
		return nil, err
	}
	placement := record.Placement
	placement.Orphaned = !live || node.Incarnation != record.NodeIncarnation
	return &placement, nil
}

// ListPlacements returns the placements matching filter, ordered by
// workload ID, each marked orphaned when its node's lease has expired or the
// node has re-registered since.
func (p *Placer) ListPlacements(ctx context.Context, tenant string, filter *PlacementFilter) ([]*Placement, error) {
	if filter == nil {
		filter = &PlacementFilter{}
	}
	base := prefix(tenant)
	raws, err := p.backend.Scan(ctx, base+workloadsDir)
	if err != nil {
		return nil, &StoreError{Op: "scan placements", Err: err}
	}
	snap, err := p.snapshot(ctx, tenant)
	if err != nil {
		return nil, err
	}

	out := make([]*Placement, 0, len(raws))
	for k, raw := range raws {
		var record placementRecord
		if err := decode(k, raw, &record); err != nil {
			return nil, err
		}
		if filter.NodeID != "" && record.NodeID != filter.NodeID {
			continue
		}
		if filter.Group != "" && record.Group != filter.Group {
			continue
		}
		placement := record.Placement
		placement.Orphaned = !liveIncarnation(&record, snap.nodes[record.NodeID])
		out = append(out, &placement)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkloadID < out[j].WorkloadID })
	return out, nil
}
