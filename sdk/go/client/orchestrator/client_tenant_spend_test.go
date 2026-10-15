// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.
package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// TestTenantSpendPolicy_HappyPath is go-quicbaas#1058 at the SDK client: both
// verbs dispatch by name and return the typed policy.
func TestTenantSpendPolicy_HappyPath(t *testing.T) {
	want := &TenantSpendPolicy{TenantID: "t1", Limits: spend.Limits{Windows: []spend.Window{
		{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 5},
	}}}
	got, err := NewClient(okTransport(t, "GetTenantSpendPolicy", want)).
		GetTenantSpendPolicy(context.Background(), &GetTenantSpendPolicyRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, want, got)

	set, err := NewClient(okTransport(t, "SetTenantSpendPolicy", want)).
		SetTenantSpendPolicy(context.Background(), &SetTenantSpendPolicyRequest{TenantID: "t1", Limits: want.Limits})
	require.NoError(t, err)
	assert.Equal(t, want, set)
}

// TestTenantSpendPolicy_Refusals asserts an unusable request, a transport
// failure and a wrongly typed response are all errors.
func TestTenantSpendPolicy_Refusals(t *testing.T) {
	c := NewClient(failTransport())
	var nilCtx context.Context
	_, err := c.GetTenantSpendPolicy(nilCtx, &GetTenantSpendPolicyRequest{TenantID: "t1"})
	assert.ErrorContains(t, err, "context is required")
	_, err = c.GetTenantSpendPolicy(context.Background(), nil)
	assert.ErrorContains(t, err, "request is required")
	_, err = c.SetTenantSpendPolicy(context.Background(), nil)
	assert.ErrorContains(t, err, "request is required")
	_, err = c.GetTenantSpendPolicy(context.Background(), &GetTenantSpendPolicyRequest{TenantID: "t1"})
	assert.Error(t, err, "a transport failure must surface")

	_, err = NewClient(mismatchTransport()).SetTenantSpendPolicy(context.Background(), &SetTenantSpendPolicyRequest{TenantID: "t1"})
	assert.ErrorContains(t, err, "invalid response type")
}
