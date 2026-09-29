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

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// TestOrchSpendPolicySet_SendsTheWindows is go-quicbaas#1058 at the CLI: set
// sends the tenant's windows, and --clear-spend sends an explicit empty set.
func TestOrchSpendPolicySet_SendsTheWindows(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	mock := &mockOrchestratorClient{}
	setMockOrchestratorFactory(mock)
	t.Cleanup(func() { clearSessionOverrides(mock) })
	globalTenantID = "acme"
	var sent *orchestratorSDK.SetTenantSpendPolicyRequest
	overridesFor(mock).setPolicy = func(_ context.Context, req *orchestratorSDK.SetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error) {
		sent = req
		return &orchestratorSDK.TenantSpendPolicy{TenantID: req.TenantID, Limits: req.Limits}, nil
	}

	set := newOrchSpendPolicyCommand().Commands()
	var setCmd = set[0]
	for _, c := range set {
		if c.Name() == "set" {
			setCmd = c
		}
	}
	spendWindowValues, spendClear = nil, false
	require.NoError(t, setCmd.Flags().Set("spend-window", "name=monthly,size=720h,usd=2000"))
	require.NoError(t, runOrchSpendPolicySet(setCmd, nil))
	require.NotNil(t, sent)
	assert.Equal(t, "acme", sent.TenantID)
	require.Len(t, sent.Limits.Windows, 1)
	assert.Equal(t, int64(2_000_000_000), sent.Limits.Windows[0].MaxEquivalentMicros)
}

func TestOrchSpendPolicySet_RefusesNoLimits(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	spendWindowValues, spendClear = nil, false
	cmd := newOrchSpendPolicyCommand()
	for _, c := range cmd.Commands() {
		if c.Name() == "set" {
			err := runOrchSpendPolicySet(c, nil)
			var required *SpendPolicyFlagsRequiredError
			assert.True(t, errors.As(err, &required) || strings.Contains(err.Error(), "--spend-window"), "err = %v", err)
		}
	}
}

func TestOrchSpendPolicyGet_ReportsAFactoryFailure(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	setErrorOrchestratorFactory()
	assert.Error(t, runOrchSpendPolicyGet(newOrchSpendPolicyCommand(), nil))
}

func TestFormatTenantSpendPolicy(t *testing.T) {
	updated := time.Date(2026, 9, 23, 6, 0, 0, 0, time.UTC)
	policy := &orchestratorSDK.TenantSpendPolicy{
		TenantID:  "acme",
		Limits:    spend.Limits{Windows: []spend.Window{{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 2_000_000_000}}},
		UpdatedAt: &updated,
		Windows: []orchestratorSDK.SpendWindowStatus{{
			Name: "monthly", MaxEquivalentMicros: 2_000_000_000, UsedEquivalentMicros: 1_500_000,
		}},
	}
	var table bytes.Buffer
	require.NoError(t, formatTenantSpendPolicy(policy, "table", &table))
	assert.Contains(t, table.String(), "$1.50 / $2000.00")
	assert.Contains(t, table.String(), "2026-09-23T06:00:00Z")

	var js bytes.Buffer
	require.NoError(t, formatTenantSpendPolicy(policy, "json", &js))
	assert.Contains(t, js.String(), `"tenant_id": "acme"`)

	assert.Error(t, formatTenantSpendPolicy(policy, "xml", &js))
	assert.Equal(t, "-", windowAxis(5, 0, false))
	assert.Equal(t, "5 / 10", windowAxis(5, 10, false))
}
