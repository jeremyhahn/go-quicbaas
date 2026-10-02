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

import "strings"

const (
	// maxIDLen bounds node, workload and group identifiers.
	maxIDLen = 253

	// minWeight and maxWeight bound a preferred affinity term's weight.
	minWeight = 1
	maxWeight = 100
)

// validateID rejects empty, oversized and separator-bearing identifiers. A
// '/' would let an identifier cross into another record's key space during a
// prefix scan, exactly as pkg/registry guards its names.
func validateID(field, id string) error {
	if id == "" {
		return &InvalidRequestError{Reason: field + " is required"}
	}
	if len(id) > maxIDLen {
		return &InvalidRequestError{Reason: field + " exceeds 253 bytes"}
	}
	if strings.ContainsRune(id, '/') {
		return &InvalidRequestError{Reason: field + " must not contain '/'"}
	}
	return nil
}

// validateAmounts rejects negative resource amounts and empty resource names.
func validateAmounts(field string, amounts map[string]int64) error {
	for name, amount := range amounts {
		if name == "" {
			return &InvalidRequestError{Reason: field + " has an empty resource name"}
		}
		if amount < 0 {
			return &InvalidRequestError{Reason: field + "[" + name + "] is negative"}
		}
	}
	return nil
}

// ValidateNodeFacts checks facts before they are registered.
func ValidateNodeFacts(facts *NodeFacts) error {
	if facts == nil {
		return &InvalidRequestError{Reason: "node facts are required"}
	}
	if err := validateID("node_id", facts.NodeID); err != nil {
		return err
	}
	if err := validateAmounts("capacity", facts.Capacity); err != nil {
		return err
	}
	for key := range facts.Labels {
		if key == "" {
			return &InvalidRequestError{Reason: "labels has an empty key"}
		}
	}
	return nil
}

// operatorArity says whether an operator takes values. It doubles as the set
// of known operators, so an unknown one is rejected in the same lookup.
var operatorArity = map[Operator]bool{
	OperatorIn:           true,
	OperatorNotIn:        true,
	OperatorExists:       false,
	OperatorDoesNotExist: false,
}

// validateConstraint checks one label constraint.
func validateConstraint(c Constraint) error {
	if c.Key == "" {
		return &InvalidRequestError{Reason: "constraint key is required"}
	}
	takesValues, known := operatorArity[c.Operator]
	if !known {
		return &InvalidRequestError{Reason: "constraint on " + c.Key + " has unknown operator " + string(c.Operator)}
	}
	if takesValues && len(c.Values) == 0 {
		return &InvalidRequestError{Reason: "constraint on " + c.Key + " with operator " + string(c.Operator) + " needs at least one value"}
	}
	if !takesValues && len(c.Values) > 0 {
		return &InvalidRequestError{Reason: "constraint on " + c.Key + " with operator " + string(c.Operator) + " takes no values"}
	}
	return nil
}

// validateTerms checks affinity or anti-affinity terms.
func validateTerms(field string, terms []AffinityTerm) error {
	for _, term := range terms {
		if err := validateID(field+" group", term.Group); err != nil {
			return err
		}
		if !term.Required && (term.Weight < minWeight || term.Weight > maxWeight) {
			return &InvalidRequestError{Reason: field + " term for group " + term.Group + " needs a weight from 1 to 100"}
		}
	}
	return nil
}

// strategies is the set of known strategies.
var strategies = map[Strategy]bool{
	StrategyBinPack: true,
	StrategySpread:  true,
}

// ValidatePlacementRequest checks a request before any store access.
func ValidatePlacementRequest(req *PlacementRequest) error {
	if req == nil {
		return &InvalidRequestError{Reason: "placement request is required"}
	}
	if err := validateID("workload_id", req.WorkloadID); err != nil {
		return err
	}
	if req.Group != "" {
		if err := validateID("group", req.Group); err != nil {
			return err
		}
	}
	if err := validateAmounts("resources", req.Resources); err != nil {
		return err
	}
	for _, c := range req.Constraints {
		if err := validateConstraint(c); err != nil {
			return err
		}
	}
	if err := validateTerms("affinity", req.Affinity); err != nil {
		return err
	}
	if err := validateTerms("anti_affinity", req.AntiAffinity); err != nil {
		return err
	}
	if req.Spread != nil {
		if req.Group == "" {
			return &InvalidRequestError{Reason: "spread requires a group"}
		}
		if req.Spread.TopologyKey == "" {
			return &InvalidRequestError{Reason: "spread topology_key is required"}
		}
		if req.Spread.MaxSkew < 1 {
			return &InvalidRequestError{Reason: "spread max_skew must be at least 1"}
		}
	}
	if req.Strategy != "" && !strategies[req.Strategy] {
		return &InvalidRequestError{Reason: "unknown strategy " + string(req.Strategy)}
	}
	return nil
}
