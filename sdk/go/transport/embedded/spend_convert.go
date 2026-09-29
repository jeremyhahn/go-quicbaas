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

package embedded

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// spendLimitsToOrchestration converts SDK limits for the service layer.
func spendLimitsToOrchestration(limits spend.Limits) orchestration.SpendLimits {
	var out orchestration.SpendLimits
	for _, w := range limits.Windows {
		out.Windows = append(out.Windows, orchestration.QuotaWindow(w))
	}
	return out
}

// optionalSpendLimitsToOrchestration converts an update's limits to the
// service's "nil means unchanged" pointer.
func optionalSpendLimitsToOrchestration(limits *spend.Limits) *orchestration.SpendLimits {
	if limits == nil {
		return nil
	}
	out := spendLimitsToOrchestration(*limits)
	return &out
}

// spendLimitsFromOrchestration renders stored limits for the SDK.
func spendLimitsFromOrchestration(limits orchestration.SpendLimits) spend.Limits {
	var out spend.Limits
	for _, w := range limits.Windows {
		out.Windows = append(out.Windows, spend.Window(w))
	}
	return out
}

// accountSpendPolicyToOrchestration converts an SDK policy; nil is unmetered.
func accountSpendPolicyToOrchestration(policy *spend.AccountPolicy) orchestration.AccountSpendPolicy {
	if policy == nil {
		return orchestration.AccountSpendPolicy{}
	}
	out := orchestration.AccountSpendPolicy{Quota: spendLimitsToOrchestration(policy.Quota)}
	if policy.Breaker != nil {
		breaker := orchestration.BreakerPolicy(*policy.Breaker)
		out.Breaker = &breaker
	}
	return out
}

// optionalAccountSpendPolicyToOrchestration converts an update's policy to the
// service's "nil means unchanged" pointer.
func optionalAccountSpendPolicyToOrchestration(policy *spend.AccountPolicy) *orchestration.AccountSpendPolicy {
	if policy == nil {
		return nil
	}
	out := accountSpendPolicyToOrchestration(policy)
	return &out
}

// accountSpendPolicyFromOrchestration renders a stored policy for the SDK.
func accountSpendPolicyFromOrchestration(policy orchestration.AccountSpendPolicy) spend.AccountPolicy {
	out := spend.AccountPolicy{Quota: spendLimitsFromOrchestration(policy.Quota)}
	if policy.Breaker != nil {
		breaker := spend.BreakerPolicy(*policy.Breaker)
		out.Breaker = &breaker
	}
	return out
}
