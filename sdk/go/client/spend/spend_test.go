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
package spend

import (
	"testing"
	"time"
)

func TestMetered(t *testing.T) {
	if (Limits{}).Metered() {
		t.Fatal("empty limits metered")
	}
	if !(Limits{Windows: []Window{{Name: "w", Size: time.Hour, MaxTokens: 1}}}).Metered() {
		t.Fatal("a window did not meter")
	}
	if (AccountPolicy{}).Metered() {
		t.Fatal("an empty policy metered")
	}
	if !(AccountPolicy{Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute}}).Metered() {
		t.Fatal("a breaker-only policy did not meter")
	}
}
