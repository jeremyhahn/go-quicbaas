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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// spendLimitsToProto renders SDK limits; limits that meter nothing are
// absent.
func spendLimitsToProto(limits spend.Limits) *pb.SpendLimits {
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

// optionalSpendLimitsToProto renders an update's limits: nil stays absent
// (unchanged) and a non-nil empty value is a present empty message (clear).
func optionalSpendLimitsToProto(limits *spend.Limits) *pb.SpendLimits {
	if limits == nil {
		return nil
	}
	if out := spendLimitsToProto(*limits); out != nil {
		return out
	}
	return &pb.SpendLimits{}
}

// spendLimitsFromProto converts wire limits; absent meters nothing.
func spendLimitsFromProto(limits *pb.SpendLimits) spend.Limits {
	var out spend.Limits
	for _, w := range limits.GetWindows() {
		out.Windows = append(out.Windows, spend.Window{
			Name:                w.GetName(),
			Size:                time.Duration(w.GetSizeNanos()),
			MaxTokens:           w.GetMaxTokens(),
			MaxEquivalentMicros: w.GetMaxEquivalentMicros(),
		})
	}
	return out
}

// accountSpendPolicyToProto renders an SDK policy; nil stays absent.
func accountSpendPolicyToProto(policy *spend.AccountPolicy) *pb.AccountSpendPolicy {
	if policy == nil {
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

// accountSpendPolicyFromProto converts a wire policy; absent is unmetered.
func accountSpendPolicyFromProto(policy *pb.AccountSpendPolicy) spend.AccountPolicy {
	out := spend.AccountPolicy{Quota: spendLimitsFromProto(policy.GetQuota())}
	if breaker := policy.GetBreaker(); breaker != nil {
		out.Breaker = &spend.BreakerPolicy{
			Window:           time.Duration(breaker.GetWindowNanos()),
			FailureThreshold: int(breaker.GetFailureThreshold()),
			Cooldown:         time.Duration(breaker.GetCooldownNanos()),
		}
	}
	return out
}
