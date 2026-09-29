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

package quic

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_GetSpendReportOverHTTP3 drives the spend-report verb over a real
// HTTP/3 server. QUIC is the REST surface on HTTP/3, so this proves the SDK
// method reaches the same /orchestrator/cost route the REST transport builds
// and decodes into the SDK type rather than a bare map.
func TestRequest_GetSpendReportOverHTTP3(t *testing.T) {
	gotPath := make(chan string, 1)
	gotProject := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		gotProject <- r.URL.Query().Get("project_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"level":"project","tenant_id":"automatethethings","project_id":"p1",
			"entries":2,"input_tokens":20,"output_tokens":10,"total_tokens":30,
			"equivalent_micros":0,"priced_entries":0,"child_level":"task",
			"children":[{"level":"task","unattributed":true,"total_tokens":30}],
			"accounts":[{"account_id":"a1","total_tokens":30,"policy_status":"unknown"}]}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
		&orchestratorclient.GetSpendReportRequest{TenantID: "automatethethings", ProjectID: "p1"})
	require.NoError(t, err)

	report, ok := resp.(*orchestratorclient.GetSpendReportResponse)
	require.True(t, ok, "transport returned %T", resp)
	assert.Equal(t, orchestratorclient.SpendLevelProject, report.Level)
	assert.Equal(t, int64(30), report.TotalTokens)
	assert.Zero(t, report.PricedEntries, "an unpriced report must not claim a measured cost")
	require.Len(t, report.Children, 1)
	assert.True(t, report.Children[0].Unattributed)
	require.Len(t, report.Accounts, 1)
	assert.Equal(t, orchestratorclient.SpendPolicyUnknown, report.Accounts[0].PolicyStatus)

	assert.Equal(t, "/api/v1/orchestrator/cost", <-gotPath)
	assert.Equal(t, "p1", <-gotProject)
}

// TestRequest_GetSpendReportRefusalOverHTTP3 proves a server refusal survives
// the QUIC hop as an error, never as a decoded report that reads as a tenant
// which has spent nothing.
func TestRequest_GetSpendReportRefusalOverHTTP3(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"consumption ledger not available"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
		&orchestratorclient.GetSpendReportRequest{TenantID: "automatethethings"})
	require.Error(t, err)
}
