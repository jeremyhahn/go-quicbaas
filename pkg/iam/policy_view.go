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

// PolicyLoadErrorView is the wire form of a flagged policy record's load
// error: why the stored record was not applied and the unknown key paths
// it carries. It never carries the record's values.
type PolicyLoadErrorView struct {
	Reason      string   `json:"reason"`
	UnknownKeys []string `json:"unknown_keys,omitempty"`
}

// PolicyView is the JSON shape a transport returns for a policy: the policy
// document plus load_error when the stored record is flagged and not
// applied. Policy.LoadError is never part of the stored encoding, so the
// view, not the policy, carries it on the wire. An operator repairs a
// flagged record with Update (a corrected document) or Delete.
type PolicyView struct {
	*Policy
	LoadError *PolicyLoadErrorView `json:"load_error,omitempty"`
}

// NewPolicyView wraps a policy for a transport response.
func NewPolicyView(policy *Policy) PolicyView {
	view := PolicyView{Policy: policy}
	if policy != nil && policy.LoadError != nil {
		view.LoadError = &PolicyLoadErrorView{
			Reason:      policy.LoadError.Reason,
			UnknownKeys: policy.LoadError.UnknownKeys,
		}
	}
	return view
}

// NewPolicyViews wraps a policy list for a transport response.
func NewPolicyViews(policies []*Policy) []PolicyView {
	views := make([]PolicyView, len(policies))
	for i, policy := range policies {
		views[i] = NewPolicyView(policy)
	}
	return views
}
