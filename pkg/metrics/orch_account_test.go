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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordOrchAccountPoolExhausted_CountsByReason(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordOrchAccountPoolExhausted("tenant-a", "claude", "saturated")
	m.RecordOrchAccountPoolExhausted("tenant-a", "claude", "saturated")
	m.RecordOrchAccountPoolExhausted("tenant-a", "claude", "cooling_down")

	saturated := testutil.ToFloat64(m.OrchAccountPoolExhausted.With(prometheus.Labels{
		"tenant": "tenant-a", "provider": "claude", "reason": "saturated"}))
	cooling := testutil.ToFloat64(m.OrchAccountPoolExhausted.With(prometheus.Labels{
		"tenant": "tenant-a", "provider": "claude", "reason": "cooling_down"}))
	assert.Equal(t, float64(2), saturated)
	assert.Equal(t, float64(1), cooling, "each reason is counted on its own series")
}

func TestRecordOrchAccountLimitHold_CountsPerProvider(t *testing.T) {
	Reset()
	m := InitMetrics()

	m.RecordOrchAccountLimitHold("tenant-a", "claude")
	m.RecordOrchAccountLimitHold("tenant-a", "codex")

	claude := testutil.ToFloat64(m.OrchAccountLimitHolds.With(prometheus.Labels{"tenant": "tenant-a", "provider": "claude"}))
	codex := testutil.ToFloat64(m.OrchAccountLimitHolds.With(prometheus.Labels{"tenant": "tenant-a", "provider": "codex"}))
	assert.Equal(t, float64(1), claude)
	assert.Equal(t, float64(1), codex)
}
