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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTurnModel(t *testing.T) {
	t.Parallel()
	configured := &ACPBackend{config: ACPBackendConfig{DefaultModel: "gpt-5.5-codex"}}
	unconfigured := &ACPBackend{}
	tests := map[string]struct {
		served, planned string
		backend         AgentBackend
		want            string
	}{
		"the served model wins":               {"claude-opus-5-5", "opus", configured, "claude-opus-5-5"},
		"the plan when the backend is silent": {"", "claude-opus-5", configured, "claude-opus-5"},
		"the backend's configured model":      {"", "", configured, "gpt-5.5-codex"},
		"nothing resolves on a bare backend":  {"", "", unconfigured, ""},
		"a backend that declares no default":  {"", "", &RemoteACPBackend{}, ""},
		"no backend at all resolves the plan": {"", "m", nil, "m"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, resolveTurnModel(tc.served, tc.planned, tc.backend))
		})
	}
}

// TestRecordConsumption_AnUnnamedModelIsRecordedNotDropped is the regression
// test for the 450 "model: must not be empty" refusals on the burn-in node:
// codex reported no model, the reviewer binding named none, and the ledger
// dropped every one of those turns — their spend and their breaker evidence.
func TestRecordConsumption_AnUnnamedModelIsRecordedNotDropped(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	logger := &capturingLogger{}
	impl.logger = logger
	ledger := NewInMemoryConsumptionLedger()
	impl.SetConsumptionLedger(ledger)
	ctx := ContextWithTenant(context.Background(), "t1")

	turn := completedTurn{turnID: "turn-codex", sessionID: "ses-1", engine: "codex", duration: time.Second}
	impl.recordConsumption(ctx, turn, TurnAttribution{}, turnSession{accountID: "7eb5ea66"}, nil, ConsumptionOutcomeSuccess)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	require.NoError(t, err)
	require.Equal(t, 1, rollup.Entries, "the turn is in the ledger")
	require.Contains(t, rollup.ByModel, "codex/"+ConsumptionModelUnknown)
	assert.Equal(t, 1, rollup.UnpricedEntries, "an unknown model is unpriced, never free")

	warned := false
	for _, w := range logger.warnings() {
		if strings.Contains(w, "named no model") {
			warned = true
		}
	}
	assert.True(t, warned, "the gap is logged, not silent")
}

func TestRecordConsumption_ANamedModelIsNotRelabelled(t *testing.T) {
	t.Parallel()
	impl := newTestService(t)
	logger := &capturingLogger{}
	impl.logger = logger
	ledger := NewInMemoryConsumptionLedger()
	impl.SetConsumptionLedger(ledger)
	ctx := ContextWithTenant(context.Background(), "t1")

	turn := completedTurn{turnID: "turn-2", sessionID: "ses-2", engine: "codex", model: "gpt-5.5", duration: time.Second}
	impl.recordConsumption(ctx, turn, TurnAttribution{}, turnSession{}, nil, ConsumptionOutcomeSuccess)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	require.NoError(t, err)
	assert.Contains(t, rollup.ByModel, "codex/gpt-5.5")
	assert.Empty(t, logger.warnings())
}
