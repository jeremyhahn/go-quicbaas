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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// runUpdate executes `account update` through the real command, so the
// flag parsing that decides whether the cap was passed is the one under test.
func runUpdate(t *testing.T, args ...string) *accountSDK.UpdateAccountRequest {
	t.Helper()
	resetAccountFlags()
	var captured *accountSDK.UpdateAccountRequest
	withMockAccount(t, &mockAccountClient{
		updateFn: func(_ context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
			captured = req
			return &accountSDK.AccountInfo{ID: req.AccountID}, nil
		},
	})
	cmd := newAccountUpdateCommand()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
	require.NotNil(t, captured)
	return captured
}

func TestAgentAccountUpdate_SessionCapIsSentOnlyWhenPassed(t *testing.T) {
	unchanged := runUpdate(t, "acct_1", "--display-name", "seat")
	assert.Nil(t, unchanged.MaxConcurrentSessions, "a cap nobody passed must not be sent")

	capped := runUpdate(t, "acct_1", "--max-concurrent-sessions", "2")
	require.NotNil(t, capped.MaxConcurrentSessions)
	assert.Equal(t, 2, *capped.MaxConcurrentSessions)

	unmetered := runUpdate(t, "acct_1", "--max-concurrent-sessions", "0")
	require.NotNil(t, unmetered.MaxConcurrentSessions, "zero is a value: unmetered")
	assert.Equal(t, 0, *unmetered.MaxConcurrentSessions)
}

func TestAgentAccountUpdate_ANonNumericCapIsRejected(t *testing.T) {
	resetAccountFlags()
	withMockAccount(t, &mockAccountClient{})
	cmd := newAccountUpdateCommand()
	cmd.SetArgs([]string{"acct_1", "--max-concurrent-sessions", "many"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	assert.Error(t, cmd.Execute())
}

func TestSessionCapLabel(t *testing.T) {
	assert.Equal(t, "unmetered", sessionCapLabel(0))
	assert.Equal(t, "unmetered", sessionCapLabel(-1))
	assert.Equal(t, "3", sessionCapLabel(3))
}

func TestFormatAccountUsage_ShowsLiveSessions(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, formatAccountUsage("acct_1", &accountSDK.UsageReport{TotalTokens: 10, LiveSessions: 2}, "table", &out))
	assert.Contains(t, out.String(), "Live Sessions:")
	assert.Contains(t, out.String(), "2")
}

func TestSpendVerdictLine(t *testing.T) {
	freesAt := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, "not metered by this server", spendVerdictLine(nil))
	assert.Equal(t, "within its spend policy", spendVerdictLine(&accountSDK.SpendHeadroom{Admits: true}))
	assert.Equal(t, `refused (quota_exhausted): window "daily" 12 of 10 tokens; frees up at 2026-09-25T00:00:00Z`,
		spendVerdictLine(&accountSDK.SpendHeadroom{Refusal: "quota_exhausted", Window: "daily", Used: 12, Limit: 10, Axis: "tokens", FreesAt: &freesAt}))
	assert.Equal(t, "refused (breaker_open)", spendVerdictLine(&accountSDK.SpendHeadroom{Refusal: "breaker_open"}))
}

func TestFormatAccountUsage_ShowsTheSpendVerdict(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, formatAccountUsage("acct_1", &accountSDK.UsageReport{
		Spend: &accountSDK.SpendHeadroom{Refusal: "breaker_open"},
	}, "table", &out))
	assert.Contains(t, out.String(), "refused (breaker_open)")
}
