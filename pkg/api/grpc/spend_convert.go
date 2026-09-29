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

package grpc

import (
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Spend limits on the wire (go-quicbaas#1058, ruling F5)
// =============================================================================
//
// One limits shape is carried by an account, an Agent, a project and a tenant; these
// are its one conversion each way, so the three surfaces cannot drift.

// spendLimitsToProto renders limits; limits that meter nothing render as
// absent.
func spendLimitsToProto(limits orchestration.SpendLimits) *pb.SpendLimits {
	if !limits.Metered() {
		return nil
	}
	out := &pb.SpendLimits{Windows: make([]*pb.SpendLimitWindow, 0, len(limits.Windows))}
	for _, w := range limits.Windows {
		out.Windows = append(out.Windows, &pb.SpendLimitWindow{
			Name:                w.Name,
			SizeNanos:           int64(w.Size),
			MaxTokens:           w.MaxTokens,
			MaxEquivalentMicros: w.MaxEquivalentMicros,
		})
	}
	return out
}

// spendLimitsFromProto converts limits; absent meters nothing.
func spendLimitsFromProto(limits *pb.SpendLimits) orchestration.SpendLimits {
	var out orchestration.SpendLimits
	for _, w := range limits.GetWindows() {
		out.Windows = append(out.Windows, orchestration.QuotaWindow{
			Name:                w.GetName(),
			Size:                time.Duration(w.GetSizeNanos()),
			MaxTokens:           w.GetMaxTokens(),
			MaxEquivalentMicros: w.GetMaxEquivalentMicros(),
		})
	}
	return out
}

// optionalSpendLimits converts an update's limits to the service's "nil means
// unchanged" pointer; a present empty message clears them.
func optionalSpendLimits(limits *pb.SpendLimits) *orchestration.SpendLimits {
	if limits == nil {
		return nil
	}
	out := spendLimitsFromProto(limits)
	return &out
}

// accountSpendPolicyToProto renders an account's spend policy; an unmetered
// one is absent.
func accountSpendPolicyToProto(policy orchestration.AccountSpendPolicy) *pb.AccountSpendPolicy {
	if !policy.Metered() {
		return nil
	}
	out := &pb.AccountSpendPolicy{Quota: spendLimitsToProto(policy.Quota)}
	if policy.Breaker != nil {
		out.Breaker = &pb.SpendBreakerPolicy{
			WindowNanos:      int64(policy.Breaker.Window),
			FailureThreshold: int32(policy.Breaker.FailureThreshold),
			CooldownNanos:    int64(policy.Breaker.Cooldown),
		}
	}
	return out
}

// accountSpendPolicyFromProto converts a policy; absent is unmetered.
func accountSpendPolicyFromProto(policy *pb.AccountSpendPolicy) orchestration.AccountSpendPolicy {
	out := orchestration.AccountSpendPolicy{Quota: spendLimitsFromProto(policy.GetQuota())}
	if breaker := policy.GetBreaker(); breaker != nil {
		out.Breaker = &orchestration.BreakerPolicy{
			Window:           time.Duration(breaker.GetWindowNanos()),
			FailureThreshold: int(breaker.GetFailureThreshold()),
			Cooldown:         time.Duration(breaker.GetCooldownNanos()),
		}
	}
	return out
}

// optionalAccountSpendPolicy converts an update's policy to the service's "nil
// means unchanged" pointer.
func optionalAccountSpendPolicy(policy *pb.AccountSpendPolicy) *orchestration.AccountSpendPolicy {
	if policy == nil {
		return nil
	}
	out := accountSpendPolicyFromProto(policy)
	return &out
}
