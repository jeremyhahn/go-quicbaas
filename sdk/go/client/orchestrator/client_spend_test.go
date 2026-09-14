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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// GetSpendReport
// =============================================================================

// TestGetSpendReport_HappyPath asserts the client returns the report the
// transport produced, typed — including the two fields a renderer must not
// lose: CostMeasured and the unattributed child row.
func TestGetSpendReport_HappyPath(t *testing.T) {
	want := &GetSpendReportResponse{
		Level:        SpendLevelProject,
		TenantID:     "t1",
		ProjectID:    "p1",
		Entries:      3,
		InputTokens:  120,
		OutputTokens: 80,
		TotalTokens:  200,
		CostMeasured: false,
		ChildLevel:   SpendLevelTask,
		Children: []SpendChild{
			{ID: "w1", Level: SpendLevelTask, TotalTokens: 150},
			{Level: SpendLevelTask, Unattributed: true, TotalTokens: 50},
		},
		Accounts: []SpendAccount{
			{AccountID: "a1", TotalTokens: 200, PolicyStatus: SpendPolicyNone},
		},
	}
	c := NewClient(okTransport(t, "GetSpendReport", want))

	resp, err := c.GetSpendReport(context.Background(),
		&GetSpendReportRequest{TenantID: "t1", ProjectID: "p1"})
	require.NoError(t, err)
	assert.Equal(t, SpendLevelProject, resp.Level)
	assert.Equal(t, int64(200), resp.TotalTokens)
	assert.False(t, resp.CostMeasured, "an unpriced report must not claim a measured cost")
	require.Len(t, resp.Children, 2)
	assert.True(t, resp.Children[1].Unattributed)
	require.Len(t, resp.Accounts, 1)
	assert.Equal(t, SpendPolicyNone, resp.Accounts[0].PolicyStatus)
}

// TestGetSpendReport_Validation asserts every unusable request is refused
// before it reaches a transport.
func TestGetSpendReport_Validation(t *testing.T) {
	c := NewClient(failTransport())

	//nolint:staticcheck // intentionally nil ctx
	_, err := c.GetSpendReport(nil, &GetSpendReportRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")

	_, err = c.GetSpendReport(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is required")
}

// TestGetSpendReport_TransportError asserts a transport failure is returned
// rather than swallowed into a report reading as no spend.
func TestGetSpendReport_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetSpendReport(context.Background(), &GetSpendReportRequest{TenantID: "t1"})
	require.Error(t, err)
}

// TestGetSpendReport_TypeMismatch asserts a transport that returns the wrong
// shape is an error, not a nil-dereference at the call site.
func TestGetSpendReport_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetSpendReport(context.Background(), &GetSpendReportRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid response type")
}
