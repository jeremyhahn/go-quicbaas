// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.
package embedded

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

func TestSpendLimitsOrchestrationConversions(t *testing.T) {
	limits := spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 5, MaxEquivalentMicros: 7}}}
	back := spendLimitsFromOrchestration(spendLimitsToOrchestration(limits))
	if len(back.Windows) != 1 || back.Windows[0] != limits.Windows[0] {
		t.Fatalf("round trip = %+v, want %+v", back, limits)
	}
	if optionalSpendLimitsToOrchestration(nil) != nil {
		t.Fatal("nil must leave the limits unchanged")
	}
	if cleared := optionalSpendLimitsToOrchestration(&spend.Limits{}); cleared == nil || cleared.Metered() {
		t.Fatalf("an empty update = %+v, want a present clear", cleared)
	}
	if got := spendLimitsFromOrchestration(orchestration.SpendLimits{}); got.Metered() {
		t.Fatalf("empty = %+v", got)
	}
}
