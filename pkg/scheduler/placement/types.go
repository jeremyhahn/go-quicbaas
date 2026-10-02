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

// Package placement is the scheduler's workload placement engine: it decides
// which node a workload runs on by filtering and scoring caller-supplied node
// facts over abstract resources, label constraints, affinity, anti-affinity
// and topology spread, and it records the decision race-free on go-qrdb.
//
// The engine knows nothing about what a node or a workload IS. A node is an
// identifier, a map of abstract capacities ("cpu_millis", "memory_bytes",
// "gpu", ...) and a map of opaque labels; a workload is an identifier, a
// group and a map of requested resources. Everything domain-specific — a CPU
// architecture, an attestation verdict, an isolation tier, a routing domain —
// reaches the engine as a label the caller chose, and is matched by a
// constraint the caller wrote. That is what makes placement a scheduler
// capability rather than a platform one (handbook doc 00, L1 amendment of
// 2026-10-01): a product built on QuicBaaS that is not a trusted-computing
// platform wants exactly this, and the platform's node facts are simply
// labels to it.
//
// Liveness rides go-qrdb leases. A node registers its facts under a lease and
// heartbeats it; when the node stops, the lease expires, qrdb deletes the
// facts, and the node drops out of every filter. Placements outlive their
// node on purpose: a placement whose node lease has expired is reported as
// orphaned so the caller can move it, rather than being silently forgotten.
//
// Executing a placement — starting the workload on the chosen node — is the
// caller's job. The engine decides and records; it never runs anything.
package placement

import "time"

// NodeFacts are the caller-supplied facts a node registers. Capacity is the
// total of each abstract resource the node offers; Labels are opaque
// key-value facts that constraints, affinity and spread match against.
type NodeFacts struct {
	// NodeID uniquely identifies the node within its tenant.
	NodeID string `json:"node_id"`

	// Capacity is the total amount of each abstract resource the node
	// offers, e.g. {"cpu_millis": 8000, "memory_bytes": 34359738368}.
	Capacity map[string]int64 `json:"capacity,omitempty"`

	// Labels are opaque facts about the node, e.g. {"arch": "arm64",
	// "zone": "rack-2"}. The engine attaches no meaning to any of them.
	Labels map[string]string `json:"labels,omitempty"`
}

// NodeStatus is the scheduling state an operator controls. It is held apart
// from the facts so a node re-registering its facts never clears a cordon
// an operator set.
type NodeStatus struct {
	// Unschedulable (cordoned) excludes the node from new placements.
	// Existing placements on it are unaffected.
	Unschedulable bool `json:"unschedulable"`

	// Draining marks a node whose placements are being moved off. A
	// draining node is always also unschedulable.
	Draining bool `json:"draining"`
}

// NodeInfo is a live node as the engine sees it: its facts, its scheduling
// status, its lease and what is currently allocated on it.
type NodeInfo struct {
	// Facts are the node's registered facts.
	Facts NodeFacts `json:"facts"`

	// Status is the operator-controlled scheduling state.
	Status NodeStatus `json:"status"`

	// LeaseID is the qrdb lease the facts are attached to.
	LeaseID int64 `json:"lease_id"`

	// TTLSeconds is the lease time-to-live the node registered with.
	TTLSeconds int64 `json:"ttl_seconds"`

	// Allocated is the sum of the resources of every placement on the node.
	Allocated map[string]int64 `json:"allocated,omitempty"`

	// Placements is the number of workloads placed on the node.
	Placements int64 `json:"placements"`

	// RegisteredAt is when the node last registered its facts.
	RegisteredAt time.Time `json:"registered_at"`
}

// Operator is a label-constraint operator.
type Operator string

// Label-constraint operators.
const (
	// OperatorIn matches a node whose label Key is set to one of Values.
	OperatorIn Operator = "In"

	// OperatorNotIn matches a node whose label Key is unset or set to a
	// value outside Values.
	OperatorNotIn Operator = "NotIn"

	// OperatorExists matches a node that carries label Key, any value.
	OperatorExists Operator = "Exists"

	// OperatorDoesNotExist matches a node that does not carry label Key.
	OperatorDoesNotExist Operator = "DoesNotExist"
)

// Constraint is a hard requirement on a node's labels.
type Constraint struct {
	// Key is the label key the constraint reads.
	Key string `json:"key"`

	// Operator is how the label is matched.
	Operator Operator `json:"operator"`

	// Values are the values In and NotIn match against. Exists and
	// DoesNotExist take none.
	Values []string `json:"values,omitempty"`
}

// AffinityTerm relates a workload to the nodes already hosting another
// workload group. In Affinity it attracts; in AntiAffinity it repels.
type AffinityTerm struct {
	// Group names the workload group the term refers to.
	Group string `json:"group"`

	// Required makes the term a filter: affinity then demands a node that
	// already hosts the group, anti-affinity a node that hosts none of it.
	// Otherwise the term only adjusts the score by Weight.
	Required bool `json:"required,omitempty"`

	// Weight is the score adjustment of a preferred term, 1 to 100.
	// Ignored when Required is set.
	Weight int `json:"weight,omitempty"`
}

// SpreadConstraint spreads a group's workloads evenly across the values of
// a node label (a topology domain, e.g. "zone").
type SpreadConstraint struct {
	// TopologyKey is the node label whose distinct values are the domains.
	TopologyKey string `json:"topology_key"`

	// MaxSkew is the largest permitted difference between the most- and
	// least-populated domains after placement. At least 1.
	MaxSkew int `json:"max_skew"`

	// Required makes the skew bound a filter (and excludes nodes without
	// the topology label). Otherwise skew only lowers the score.
	Required bool `json:"required,omitempty"`
}

// Strategy selects how feasible nodes are ranked.
type Strategy string

// Placement strategies.
const (
	// StrategyBinPack prefers the most-utilised node that still fits,
	// packing workloads densely and leaving whole nodes free.
	StrategyBinPack Strategy = "binpack"

	// StrategySpread prefers the least-utilised node, spreading load.
	StrategySpread Strategy = "spread"
)

// PlacementRequest asks the engine to place one workload.
type PlacementRequest struct {
	// WorkloadID uniquely identifies the workload within its tenant.
	// Placement is idempotent per WorkloadID.
	WorkloadID string `json:"workload_id"`

	// Group is the workload group, the unit affinity, anti-affinity and
	// spread count. Optional.
	Group string `json:"group,omitempty"`

	// Labels are carried on the placement record for the caller's use.
	Labels map[string]string `json:"labels,omitempty"`

	// Resources is the amount of each abstract resource the workload
	// consumes. Every requested resource must exist on the chosen node.
	Resources map[string]int64 `json:"resources,omitempty"`

	// Constraints are hard requirements on node labels.
	Constraints []Constraint `json:"constraints,omitempty"`

	// Affinity attracts the workload to nodes hosting other groups.
	Affinity []AffinityTerm `json:"affinity,omitempty"`

	// AntiAffinity repels the workload from nodes hosting other groups.
	AntiAffinity []AffinityTerm `json:"anti_affinity,omitempty"`

	// Spread spreads the workload's group across a topology domain.
	// Requires Group.
	Spread *SpreadConstraint `json:"spread,omitempty"`

	// Strategy ranks feasible nodes. Empty selects StrategyBinPack.
	Strategy Strategy `json:"strategy,omitempty"`
}

// Placement is a recorded placement decision.
type Placement struct {
	// WorkloadID identifies the placed workload.
	WorkloadID string `json:"workload_id"`

	// Group is the workload's group, when it has one.
	Group string `json:"group,omitempty"`

	// NodeID is the node the workload was placed on.
	NodeID string `json:"node_id"`

	// Resources is the amount of each resource reserved on the node.
	Resources map[string]int64 `json:"resources,omitempty"`

	// Labels are the request's labels.
	Labels map[string]string `json:"labels,omitempty"`

	// Score is the node's score when the placement was decided.
	Score float64 `json:"score"`

	// CreatedAt is when the placement was recorded.
	CreatedAt time.Time `json:"created_at"`

	// Orphaned is computed on every read and never stored: true when the
	// node's lease has expired, so the workload must be moved.
	Orphaned bool `json:"orphaned,omitempty"`
}

// PlaceResult is the outcome of a Place call.
type PlaceResult struct {
	// Placement is the recorded placement.
	Placement *Placement `json:"placement"`

	// Existing reports that the workload was already placed and the
	// existing placement was returned unchanged.
	Existing bool `json:"existing"`
}

// PlacementFilter narrows ListPlacements. Empty fields match everything.
type PlacementFilter struct {
	// NodeID restricts the result to placements on one node.
	NodeID string `json:"node_id,omitempty"`

	// Group restricts the result to one workload group.
	Group string `json:"group,omitempty"`
}
