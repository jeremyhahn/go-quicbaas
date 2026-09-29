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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

var factoryLabels = FactoryTurnLabels{
	Tenant: "acme", Project: "p1", Kind: "sdlc", Provider: "anthropic", Backend: "claude", Model: "claude-opus-4-5",
}

func factorySeries(last, value string) prometheus.Labels {
	return prometheus.Labels{
		"tenant": "acme", "project": "p1", "kind": "sdlc", "provider": "anthropic",
		"backend": "claude", "model": "claude-opus-4-5", last: value,
	}
}

func TestRecordFactoryTokensAccumulatesPerDirection(t *testing.T) {
	Reset()
	m := InitMetrics()
	m.RecordFactoryTokens(factoryLabels, "input", 100)
	m.RecordFactoryTokens(factoryLabels, "input", 50)
	m.RecordFactoryTokens(factoryLabels, "cache_read", 900)

	assert.Equal(t, 150.0, testutil.ToFloat64(m.FactoryTokensTotal.With(factorySeries("direction", "input"))))
	assert.Equal(t, 900.0, testutil.ToFloat64(m.FactoryTokensTotal.With(factorySeries("direction", "cache_read"))))
}

func TestRecordFactoryCostMicrosKeepsTheBasesApart(t *testing.T) {
	Reset()
	m := InitMetrics()
	m.RecordFactoryCostMicros(factoryLabels, "equivalent", 36_500)
	m.RecordFactoryCostMicros(factoryLabels, "billed", 0)

	assert.Equal(t, 36_500.0, testutil.ToFloat64(m.FactoryCostMicrosTotal.With(factorySeries("basis", "equivalent"))))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.FactoryCostMicrosTotal.With(factorySeries("basis", "billed"))))
}

func TestRecordFactorySpendDeferral(t *testing.T) {
	Reset()
	m := InitMetrics()
	m.RecordFactorySpendDeferral("acme", "p1", "project_spend")
	m.RecordFactorySpendDeferral("acme", "p1", "project_spend")

	assert.Equal(t, 2.0, testutil.ToFloat64(m.FactorySpendDeferrals.With(
		prometheus.Labels{"tenant": "acme", "project": "p1", "axis": "project_spend"})))
}

// TestFactoryMetricsCarryNoUnboundedLabel pins the label sets: a job, host,
// agent or session label would make each series set grow without bound.
func TestFactoryMetricsCarryNoUnboundedLabel(t *testing.T) {
	for _, labels := range [][]string{factoryTurnLabelNames("direction"), factoryTurnLabelNames("basis")} {
		for _, label := range labels {
			switch label {
			case "job_id", "job", "host", "agent_id", "session_id", "task_id":
				t.Fatalf("label %q is unbounded", label)
			}
		}
	}
}
