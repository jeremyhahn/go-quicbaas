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

package orchestration

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// =============================================================================
// Bounded factory metrics (go-quicbaas#1058, ruling F7)
// =============================================================================
//
// Every recorded turn is counted on quicbaas_factory_tokens_total and, when
// priced, quicbaas_factory_cost_micros_total. They replace
// quicbaas_orch_agent_tokens_total, whose job_id label grew a series per job
// for ever.
//
// EVERY LABEL IS BOUNDED, and each bound is stated here:
//
//	tenant    the tenant count
//	project   the project count, or "none"
//	kind      the runner names (sdlc, review, triage, workflow), or "none"
//	provider  the price table's vendors plus "local", or "unknown"
//	backend   the configured engines
//	model     the names the price table lists, or "other"
//
// The host, agent, job, task and session are never labels. They are on the
// ledger entry and in the spend report, where a question about one of them is
// a lookup rather than a new time series.

// Label values for a dimension a turn has no bounded value for.
const (
	factoryLabelNone    = "none"
	factoryLabelUnknown = "unknown"
	factoryLabelOther   = "other"
)

// Metric directions and bases.
const (
	factoryDirectionInput      = "input"
	factoryDirectionOutput     = "output"
	factoryDirectionCacheRead  = "cache_read"
	factoryDirectionCacheWrite = "cache_write"

	factoryBasisEquivalent = "equivalent"
	factoryBasisBilled     = "billed"
)

// factoryTurnLabels bounds a recorded entry's labels. listed says whether the
// price table names the entry's model.
func factoryTurnLabels(entry ConsumptionEntry, listed bool) metrics.FactoryTurnLabels {
	model := factoryLabelOther
	if listed {
		model = entry.Model
	}
	return metrics.FactoryTurnLabels{
		Tenant:   entry.TenantID,
		Project:  labelOr(entry.ProjectID, factoryLabelNone),
		Kind:     labelOr(entry.Kind, factoryLabelNone),
		Provider: labelOr(entry.Provider, factoryLabelUnknown),
		Backend:  entry.Engine,
		Model:    model,
	}
}

// labelOr is value, or fallback when value is empty or the ledger's no-value
// sentinel.
func labelOr(value, fallback string) string {
	if value == "" || value == consumptionUnattributed {
		return fallback
	}
	return value
}

// countFactoryTurn adds one recorded entry to the factory metrics: each token
// direction it consumed, and each money basis it has a known figure for.
func countFactoryTurn(m *metrics.Metrics, entry ConsumptionEntry, listed bool) {
	labels := factoryTurnLabels(entry, listed)
	for _, d := range []struct {
		direction string
		tokens    int64
	}{
		{factoryDirectionInput, entry.InputTokens},
		{factoryDirectionOutput, entry.OutputTokens},
		{factoryDirectionCacheRead, entry.CacheReadTokens},
		{factoryDirectionCacheWrite, entry.CacheWriteTokens},
	} {
		if d.tokens > 0 {
			m.RecordFactoryTokens(labels, d.direction, float64(d.tokens))
		}
	}
	if entry.Priced() {
		m.RecordFactoryCostMicros(labels, factoryBasisEquivalent, float64(entry.EquivalentMicros))
	}
	if entry.BilledKnown() {
		m.RecordFactoryCostMicros(labels, factoryBasisBilled, float64(entry.BilledMicros))
	}
}
