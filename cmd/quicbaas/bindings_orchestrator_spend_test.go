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

//go:build desktop

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorBinding_GetSpendReport_ForwardsTheScope asserts the desktop
// binding forwards every chain level and returns the SDK type unreshaped.
//
// THE TYPE IS THE POINT. cost_measured is what tells the UI whether
// cost_micros is a measured figure or an unmeasured zero, and a binding that
// rebuilt the response as a map is exactly where that flag would be lost.
func TestOrchestratorBinding_GetSpendReport_ForwardsTheScope(t *testing.T) {
	mock := &mockOrchestratorClient{}
	binding := newTestOrchestratorBinding(t, mock)
	t.Cleanup(func() { clearSessionOverrides(mock) })

	var got *orchestratorSDK.GetSpendReportRequest
	setSpendReportFn(mock, func(_ context.Context, req *orchestratorSDK.GetSpendReportRequest) (*orchestratorSDK.GetSpendReportResponse, error) {
		got = req
		return &orchestratorSDK.GetSpendReportResponse{
			Level:       orchestratorSDK.SpendLevelJob,
			TenantID:    req.TenantID,
			JobID:       req.JobID,
			SpendTotals: orchestratorSDK.SpendTotals{TotalTokens: 4242},
			Accounts: []orchestratorSDK.SpendAccount{{
				AccountID:    "acct-1",
				SpendTotals:  orchestratorSDK.SpendTotals{TotalTokens: 4242},
				PolicyStatus: orchestratorSDK.SpendPolicyNone,
			}},
		}, nil
	})

	resp, err := binding.GetSpendReport(&orchestratorSDK.GetSpendReportRequest{
		TenantID: "acme", ProjectID: "proj-1", TaskID: "task-1", JobID: "job-1",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "acme", got.TenantID)
	assert.Equal(t, "job-1", got.JobID)

	assert.Equal(t, orchestratorSDK.SpendLevelJob, resp.Level)
	assert.Equal(t, int64(4242), resp.TotalTokens)
	assert.Zero(t, resp.PricedEntries, "an unmeasured cost must stay unmeasured across the binding")
	require.Len(t, resp.Accounts, 1)
	assert.Equal(t, orchestratorSDK.SpendPolicyNone, resp.Accounts[0].PolicyStatus)
}

// TestOrchestratorBinding_GetSpendReport_BadInput asserts the two refusals the
// binding owns: a missing request, and a transport that failed. Neither may
// return a report the caller would render as "nothing was spent".
func TestOrchestratorBinding_GetSpendReport_BadInput(t *testing.T) {
	mock := &mockOrchestratorClient{}
	binding := newTestOrchestratorBinding(t, mock)
	t.Cleanup(func() { clearSessionOverrides(mock) })

	resp, err := binding.GetSpendReport(nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	setSpendReportFn(mock, func(context.Context, *orchestratorSDK.GetSpendReportRequest) (*orchestratorSDK.GetSpendReportResponse, error) {
		return nil, errors.New("transport down")
	})
	resp, err = binding.GetSpendReport(&orchestratorSDK.GetSpendReportRequest{TenantID: "acme"})
	require.Error(t, err)
	assert.Nil(t, resp, "a failed read must return no report, never an empty one")
}

// TestOrchestratorBinding_TenantSpendPolicy is go-quicbaas#1058 on the desktop
// binding: both verbs forward the request and return the SDK's policy.
func TestOrchestratorBinding_TenantSpendPolicy(t *testing.T) {
	mock := &mockOrchestratorClient{}
	binding := newTestOrchestratorBinding(t, mock)
	t.Cleanup(func() { clearSessionOverrides(mock) })

	got, err := binding.GetTenantSpendPolicy(&orchestratorSDK.GetTenantSpendPolicyRequest{TenantID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "acme", got.TenantID)

	set, err := binding.SetTenantSpendPolicy(&orchestratorSDK.SetTenantSpendPolicyRequest{TenantID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "acme", set.TenantID)

	_, err = binding.GetTenantSpendPolicy(nil)
	require.Error(t, err)
	_, err = binding.SetTenantSpendPolicy(nil)
	require.Error(t, err)

	overridesFor(mock).getPolicy = func(context.Context, *orchestratorSDK.GetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error) {
		return nil, errors.New("transport down")
	}
	overridesFor(mock).setPolicy = func(context.Context, *orchestratorSDK.SetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error) {
		return nil, errors.New("transport down")
	}
	_, err = binding.GetTenantSpendPolicy(&orchestratorSDK.GetTenantSpendPolicyRequest{TenantID: "acme"})
	require.Error(t, err)
	_, err = binding.SetTenantSpendPolicy(&orchestratorSDK.SetTenantSpendPolicyRequest{TenantID: "acme"})
	require.Error(t, err)
}
