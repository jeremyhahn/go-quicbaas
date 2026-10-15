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
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// factorySeries reads one factory series from the global registry. Each test
// uses its own tenant, so parallel tests never share a series.
func factorySeries(vec *prometheus.CounterVec, tenant, project, kind, provider, backend, model, last string) float64 {
	return promtestutil.ToFloat64(vec.WithLabelValues(tenant, project, kind, provider, backend, model, last))
}

// TestFactoryTurnLabelsAreBounded asserts every dimension that could grow
// without bound collapses to a fixed value: an unlisted model to "other", a
// turn with no project or kind to "none", an unpriced vendor to "unknown".
func TestFactoryTurnLabelsAreBounded(t *testing.T) {
	entry := ConsumptionEntry{TenantID: "t1", ProjectID: consumptionUnattributed, Engine: "ollama", Model: "some-new-model:latest"}
	got := factoryTurnLabels(entry, false)
	want := metrics.FactoryTurnLabels{Tenant: "t1", Project: "none", Kind: "none", Provider: "unknown", Backend: "ollama", Model: "other"}
	if got != want {
		t.Fatalf("labels = %+v, want %+v", got, want)
	}

	entry = ConsumptionEntry{TenantID: "t1", ProjectID: "p1", Kind: "review", Provider: "anthropic", Engine: "claude", Model: "claude-opus-4-5"}
	got = factoryTurnLabels(entry, true)
	want = metrics.FactoryTurnLabels{Tenant: "t1", Project: "p1", Kind: "review", Provider: "anthropic", Backend: "claude", Model: "claude-opus-4-5"}
	if got != want {
		t.Fatalf("labels = %+v, want %+v", got, want)
	}
}

// TestCountFactoryTurnCountsEachDirectionAndKnownBasis asserts the tokens are
// split by direction and money is added only for a figure that is known.
func TestCountFactoryTurnCountsEachDirectionAndKnownBasis(t *testing.T) {
	m := metrics.GetMetrics()
	const tenant = "factory-count"
	// The registry is process-global, so every assertion is on the change a
	// turn makes, never on the absolute value: -count=N re-runs this test
	// against counters the earlier runs already moved.
	tokens := func(project, kind, provider, engine, model, direction string) float64 {
		return factorySeries(m.FactoryTokensTotal, tenant, project, kind, provider, engine, model, direction)
	}
	cost := func(project, kind, provider, engine, model, basis string) float64 {
		return factorySeries(m.FactoryCostMicrosTotal, tenant, project, kind, provider, engine, model, basis)
	}

	priced := ConsumptionEntry{
		TenantID: tenant, ProjectID: "p1", Kind: "sdlc", Provider: "anthropic", Engine: "claude", Model: "opus",
		InputTokens: 10, OutputTokens: 20, CacheReadTokens: 300,
		EquivalentMicros: 5000, PriceRef: "v1:default:*/opus", Billing: BillingSubscription,
	}
	wantTokens := map[string]float64{"input": 10, "output": 20, "cache_read": 300, "cache_write": 0}
	tokensBefore := make(map[string]float64, len(wantTokens))
	for direction := range wantTokens {
		tokensBefore[direction] = tokens("p1", "sdlc", "anthropic", "claude", "opus", direction)
	}
	equivalentBefore := cost("p1", "sdlc", "anthropic", "claude", "opus", "equivalent")
	billedBefore := cost("p1", "sdlc", "anthropic", "claude", "opus", "billed")

	countFactoryTurn(m, priced, true)
	for direction, want := range wantTokens {
		if got := tokens("p1", "sdlc", "anthropic", "claude", "opus", direction) - tokensBefore[direction]; got != want {
			t.Errorf("%s tokens grew by %v, want %v", direction, got, want)
		}
	}
	if got := cost("p1", "sdlc", "anthropic", "claude", "opus", "equivalent") - equivalentBefore; got != 5000 {
		t.Errorf("equivalent grew by %v, want 5000", got)
	}
	if got := cost("p1", "sdlc", "anthropic", "claude", "opus", "billed") - billedBefore; got != 0 {
		t.Errorf("billed grew by %v, want 0: a subscription turn bills nothing", got)
	}

	// An unpriced turn whose bill is unknown adds tokens and no money.
	unpriced := ConsumptionEntry{TenantID: tenant, Engine: "ollama", Model: "x", InputTokens: 7, Billing: BillingUnpooled}
	unpricedTokensBefore := tokens("none", "none", "unknown", "ollama", "other", "input")
	unpricedEquivalentBefore := cost("none", "none", "unknown", "ollama", "other", "equivalent")
	countFactoryTurn(m, unpriced, false)
	if got := tokens("none", "none", "unknown", "ollama", "other", "input") - unpricedTokensBefore; got != 7 {
		t.Errorf("unpriced tokens grew by %v, want 7", got)
	}
	if got := cost("none", "none", "unknown", "ollama", "other", "equivalent") - unpricedEquivalentBefore; got != 0 {
		t.Errorf("an unpriced turn added %v to the equivalent", got)
	}
}

// TestPriceBookListsOnlyWhatTheTableNames asserts the model bound: listed
// names and aliases, nothing else, and nothing from a nil book.
func TestPriceBookListsOnlyWhatTheTableNames(t *testing.T) {
	book, err := NewPriceBook(config.PricingConfig{
		Version: "v1",
		Models: []config.ModelPriceConfig{{
			Model: "claude-opus-4-5-20251101", Aliases: []string{"claude-opus-4-5"}, Provider: "anthropic",
			InputPerMTok: 5, OutputPerMTok: 25,
		}},
	})
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	if !book.Lists("t1", "claude", "claude-opus-4-5-20251101") || !book.Lists("t1", "claude", "claude-opus-4-5") {
		t.Fatal("a listed model or alias is not listed")
	}
	if book.Lists("t1", "claude", "claude-opus-9") {
		t.Fatal("an unlisted model is listed; the metric's model label would be unbounded")
	}
	var none *PriceBook
	if none.Lists("t1", "claude", "claude-opus-4-5") {
		t.Fatal("a nil price book lists a model")
	}
}

// TestRecordedTurnReachesTheFactoryMetrics asserts the one recording site
// counts a turn once the ledger holds it.
func TestRecordedTurnReachesTheFactoryMetrics(t *testing.T) {
	const tenant = "factory-recorded"
	svc, _, _ := newConsumptionRecorder(t)
	pooledSession(svc, "sess-1", tenant, "proj-1", "")
	series := func() float64 {
		return factorySeries(metrics.GetMetrics().FactoryTokensTotal, tenant, "proj-1", "none", "unknown", "openvino", "other", "input")
	}
	before := series()
	svc.recordTurnUsage(ContextWithTenant(context.Background(), tenant), completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "openvino", model: "qwen3-8b",
	}, &TokenUsage{InputTokens: 11})
	if got := series() - before; got != 11 {
		t.Fatalf("the recorded turn added %v input tokens to the factory metric, want 11", got)
	}
}
