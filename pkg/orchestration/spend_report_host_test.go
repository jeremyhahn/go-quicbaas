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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedHostFixture records turns on two hosts and one that names none.
func seedHostFixture(t *testing.T) ConsumptionLedger {
	t.Helper()
	ledger := NewInMemoryConsumptionLedger()
	hosts := map[string]string{"h1": "gpu-host:9091", "h2": "gpu-host:9091", "h3": "vm-host:9091", "h4": ""}
	tokens := map[string]int64{"h1": 10, "h2": 20, "h3": 5, "h4": 1}
	i := 0
	for _, id := range []string{"h1", "h2", "h3", "h4"} {
		entry := spendEntry(id, "p1", "task-a", "job-1", "s1", i, "acct-1", tokens[id], ConsumptionOutcomeSuccess, spendBase.Add(time.Duration(i)*time.Minute))
		entry.Host = hosts[id]
		require.NoError(t, ledger.Record(context.Background(), entry))
		i++
	}
	return ledger
}

func TestReadSpendReport_BreaksSpendDownByHost(t *testing.T) {
	t.Parallel()
	ledger := seedHostFixture(t)
	report, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger}, &SpendReportRequest{TenantID: "t1"})
	require.NoError(t, err)

	require.Len(t, report.Hosts, 3)
	assert.Equal(t, "gpu-host:9091", report.Hosts[0].Host)
	assert.Equal(t, int64(60), report.Hosts[0].TotalTokens)
	assert.Equal(t, "vm-host:9091", report.Hosts[1].Host)
	assert.True(t, report.Hosts[2].Unattributed, "an entry naming no host is labelled, and sorts last")
	assert.Empty(t, report.Hosts[2].Host)

	var sum int64
	for _, h := range report.Hosts {
		sum += h.TotalTokens
	}
	assert.Equal(t, report.TotalTokens, sum, "the host rows sum to the scope")
}

func TestReadSpendReport_DrillsIntoOneHost(t *testing.T) {
	t.Parallel()
	ledger := seedHostFixture(t)
	ctx := context.Background()

	tests := map[string]struct {
		host    string
		entries int
		tokens  int64
	}{
		"a named host":          {"gpu-host:9091", 2, 60},
		"the unattributed host": {SpendUnattributed, 1, 2},
		"a host with no turns":  {"nowhere:1", 0, 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, req := range []*SpendReportRequest{
				{TenantID: "t1", Host: tc.host},
				{TenantID: "t1", ProjectID: "p1", TaskID: "task-a", Host: tc.host},
				{TenantID: "t1", Host: tc.host, From: spendBase.Add(-time.Hour), To: spendBase.Add(time.Hour)},
			} {
				report, err := ReadSpendReport(ctx, SpendSources{Ledger: ledger}, req)
				require.NoError(t, err)
				assert.Equal(t, tc.entries, report.Entries)
				assert.Equal(t, tc.tokens, report.TotalTokens)
				assert.Equal(t, tc.host, report.Host)
				if tc.entries > 0 {
					require.Len(t, report.Hosts, 1, "a narrowed report breaks down only its own host")
				}
				for _, child := range report.Children {
					assert.LessOrEqual(t, child.TotalTokens, tc.tokens, "children are narrowed too")
				}
			}
		})
	}
}

func TestReadSpendReport_RefusesABlankHost(t *testing.T) {
	t.Parallel()
	ledger := seedHostFixture(t)
	_, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger}, &SpendReportRequest{TenantID: "t1", Host: "  "})
	assert.ErrorIs(t, err, ErrConsumptionInvalid)
}

func TestConsumptionScope_Admits(t *testing.T) {
	t.Parallel()
	named := ConsumptionEntry{Host: "h:1"}
	none := ConsumptionEntry{}
	assert.True(t, ConsumptionScope{}.admits(named))
	assert.True(t, ConsumptionScope{Host: "h:1"}.admits(named))
	assert.False(t, ConsumptionScope{Host: "h:2"}.admits(named))
	assert.True(t, ConsumptionScope{Host: consumptionUnattributed}.admits(none))
	assert.False(t, ConsumptionScope{Host: consumptionUnattributed}.admits(named))
}
