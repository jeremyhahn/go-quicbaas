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
	"sort"
	"strconv"
)

// This file is the pure decision: no store, no context, no clock. Every input
// is passed in, which is what lets the filter and score rules be tested
// exhaustively in memory and lets the store re-run the decision verbatim
// after a lost guarded write.

// candidate is one live node with everything the filters read.
type candidate struct {
	facts     NodeFacts
	status    NodeStatus
	allocated map[string]int64
}

// groupCounts maps a workload group to the number of its placements on each
// node: group -> node ID -> count.
type groupCounts map[string]map[string]int64

// hosts reports whether node hosts at least one placement of group.
func (g groupCounts) hosts(group, nodeID string) bool {
	return g[group][nodeID] > 0
}

// scored is a feasible node with its score.
type scored struct {
	node  candidate
	score float64
}

// decision is the outcome of evaluate.
type decision struct {
	// ranked holds the feasible nodes, best first.
	ranked []scored

	// rejections maps every infeasible node to the first filter it failed.
	rejections map[string]string
}

// filterFunc returns "" when the node passes, else the rejection reason.
type filterFunc func(req *PlacementRequest, node candidate, groups groupCounts) string

// nodeFilters are the per-node hard filters, in the order they are reported.
// Spread is not here: it depends on the set of nodes that pass these, so it
// runs as a second pass.
var nodeFilters = []filterFunc{
	filterSchedulable,
	filterConstraints,
	filterCapacity,
	filterRequiredAffinity,
	filterRequiredAntiAffinity,
}

// filterSchedulable rejects cordoned and draining nodes.
func filterSchedulable(_ *PlacementRequest, node candidate, _ groupCounts) string {
	if node.status.Draining {
		return "node is draining"
	}
	if node.status.Unschedulable {
		return "node is cordoned"
	}
	return ""
}

// operatorMatch evaluates a constraint operator against a node label.
var operatorMatch = map[Operator]func(value string, present bool, values []string) bool{
	OperatorIn: func(value string, present bool, values []string) bool {
		return present && contains(values, value)
	},
	OperatorNotIn: func(value string, present bool, values []string) bool {
		return !present || !contains(values, value)
	},
	OperatorExists: func(_ string, present bool, _ []string) bool {
		return present
	},
	OperatorDoesNotExist: func(_ string, present bool, _ []string) bool {
		return !present
	},
}

// contains reports whether values holds v.
func contains(values []string, v string) bool {
	for _, candidate := range values {
		if candidate == v {
			return true
		}
	}
	return false
}

// filterConstraints rejects a node whose labels fail any constraint.
func filterConstraints(req *PlacementRequest, node candidate, _ groupCounts) string {
	for _, c := range req.Constraints {
		match, known := operatorMatch[c.Operator]
		value, present := node.facts.Labels[c.Key]
		if !known || !match(value, present, c.Values) {
			return "label constraint " + c.Key + " " + string(c.Operator) + " not satisfied"
		}
	}
	return ""
}

// filterCapacity rejects a node lacking a requested resource or the free
// amount of it.
func filterCapacity(req *PlacementRequest, node candidate, _ groupCounts) string {
	for _, name := range sortedKeys(req.Resources) {
		want := req.Resources[name]
		capacity, offered := node.facts.Capacity[name]
		if !offered {
			return "node does not offer resource " + name
		}
		free := capacity - node.allocated[name]
		if want > free {
			return "insufficient " + name + ": requested " + strconv.FormatInt(want, 10) +
				", free " + strconv.FormatInt(free, 10)
		}
	}
	return ""
}

// filterRequiredAffinity rejects a node that does not host every group a
// required affinity term names.
func filterRequiredAffinity(req *PlacementRequest, node candidate, groups groupCounts) string {
	for _, term := range req.Affinity {
		if term.Required && !groups.hosts(term.Group, node.facts.NodeID) {
			return "required affinity: node hosts no workload of group " + term.Group
		}
	}
	return ""
}

// filterRequiredAntiAffinity rejects a node hosting any group a required
// anti-affinity term names.
func filterRequiredAntiAffinity(req *PlacementRequest, node candidate, groups groupCounts) string {
	for _, term := range req.AntiAffinity {
		if term.Required && groups.hosts(term.Group, node.facts.NodeID) {
			return "required anti-affinity: node already hosts a workload of group " + term.Group
		}
	}
	return ""
}

// spreadDomains counts the request group's placements per topology domain
// across the eligible nodes. Only eligible nodes define domains, so a domain
// no workload could ever land in does not hold the minimum down forever.
func spreadDomains(spread *SpreadConstraint, group string, eligible []candidate, groups groupCounts) map[string]int64 {
	domains := make(map[string]int64)
	for _, node := range eligible {
		domain, ok := node.facts.Labels[spread.TopologyKey]
		if !ok {
			continue
		}
		domains[domain] += groups[group][node.facts.NodeID]
	}
	return domains
}

// skewAfter is the skew of the domain set if one more workload lands in
// domain: its new count minus the smallest domain count.
func skewAfter(domains map[string]int64, domain string) int64 {
	minimum := int64(-1)
	for _, count := range domains {
		if minimum < 0 || count < minimum {
			minimum = count
		}
	}
	if minimum < 0 {
		minimum = 0
	}
	return domains[domain] + 1 - minimum
}

// strategyScore ranks a node by its utilisation after placement, in [0,100].
var strategyScore = map[Strategy]func(utilisation float64) float64{
	StrategyBinPack: func(u float64) float64 { return u },
	StrategySpread:  func(u float64) float64 { return 100 - u },
}

// utilisation is the node's mean utilisation, in percent, across the
// resources the request names — or across every resource the node offers
// when the request names none — counting the request itself.
func utilisation(req *PlacementRequest, node candidate) float64 {
	names := sortedKeys(req.Resources)
	if len(names) == 0 {
		names = sortedKeys(node.facts.Capacity)
	}
	var sum float64
	var counted int
	for _, name := range names {
		capacity := node.facts.Capacity[name]
		if capacity <= 0 {
			continue
		}
		used := node.allocated[name] + req.Resources[name]
		sum += float64(used) / float64(capacity) * 100
		counted++
	}
	if counted == 0 {
		return 0
	}
	return sum / float64(counted)
}

// spreadPenalty is the score a preferred spread subtracts per unit of skew.
const spreadPenalty = 10

// score combines the strategy score with the preferred terms.
func score(req *PlacementRequest, node candidate, groups groupCounts, domains map[string]int64) float64 {
	strategy := req.Strategy
	if strategy == "" {
		strategy = StrategyBinPack
	}
	total := strategyScore[strategy](utilisation(req, node))
	for _, term := range req.Affinity {
		if !term.Required && groups.hosts(term.Group, node.facts.NodeID) {
			total += float64(term.Weight)
		}
	}
	for _, term := range req.AntiAffinity {
		if !term.Required && groups.hosts(term.Group, node.facts.NodeID) {
			total -= float64(term.Weight)
		}
	}
	if req.Spread != nil && !req.Spread.Required {
		if domain, ok := node.facts.Labels[req.Spread.TopologyKey]; ok {
			total -= float64(skewAfter(domains, domain)) * spreadPenalty
		}
	}
	return total
}

// evaluate filters and ranks nodes for req. It is deterministic: equal
// scores are ordered by node ID.
func evaluate(req *PlacementRequest, nodes []candidate, groups groupCounts) decision {
	d := decision{rejections: make(map[string]string)}

	eligible := make([]candidate, 0, len(nodes))
	for _, node := range nodes {
		if reason := firstRejection(req, node, groups); reason != "" {
			d.rejections[node.facts.NodeID] = reason
			continue
		}
		eligible = append(eligible, node)
	}

	var domains map[string]int64
	if req.Spread != nil {
		domains = spreadDomains(req.Spread, req.Group, eligible, groups)
		if req.Spread.Required {
			eligible = filterSpread(req.Spread, eligible, domains, d.rejections)
		}
	}

	for _, node := range eligible {
		d.ranked = append(d.ranked, scored{node: node, score: score(req, node, groups, domains)})
	}
	sort.SliceStable(d.ranked, func(i, j int) bool {
		if d.ranked[i].score != d.ranked[j].score {
			return d.ranked[i].score > d.ranked[j].score
		}
		return d.ranked[i].node.facts.NodeID < d.ranked[j].node.facts.NodeID
	})
	return d
}

// firstRejection runs the per-node filters in order.
func firstRejection(req *PlacementRequest, node candidate, groups groupCounts) string {
	for _, filter := range nodeFilters {
		if reason := filter(req, node, groups); reason != "" {
			return reason
		}
	}
	return ""
}

// filterSpread enforces a required spread's skew bound, recording each
// rejected node.
func filterSpread(spread *SpreadConstraint, eligible []candidate, domains map[string]int64, rejections map[string]string) []candidate {
	kept := eligible[:0]
	for _, node := range eligible {
		domain, ok := node.facts.Labels[spread.TopologyKey]
		if !ok {
			rejections[node.facts.NodeID] = "required spread: node has no " + spread.TopologyKey + " label"
			continue
		}
		if skew := skewAfter(domains, domain); skew > int64(spread.MaxSkew) {
			rejections[node.facts.NodeID] = "required spread: placing in " + spread.TopologyKey + "=" + domain +
				" would make the skew " + strconv.FormatInt(skew, 10) + ", above " + strconv.Itoa(spread.MaxSkew)
			continue
		}
		kept = append(kept, node)
	}
	return kept
}

// sortedKeys returns a map's keys in ascending order, so every loop over a
// resource map visits the same order and reports the same first failure.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
