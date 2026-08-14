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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// quicbaas orchestrator rotate-account
// =============================================================================

func TestOrchestratorRotateAccountCommandStructure(t *testing.T) {
	cmd := newOrchRotateAccountCommand()

	assert.Equal(t, "rotate-account <sessionID>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	require.NotNil(t, cmd.Args, "the command must require exactly one session id")
	assert.Error(t, cmd.Args(cmd, []string{}), "no session id must be rejected")
	assert.Error(t, cmd.Args(cmd, []string{"a", "b"}), "two session ids must be rejected")
	assert.NoError(t, cmd.Args(cmd, []string{"sess_123"}))

	assert.NotNil(t, cmd.Flags().Lookup("reason"))
	assert.NotNil(t, cmd.Flags().Lookup("hold-outgoing"))
}

// TestOrchestratorRotateAccountIsRegistered keeps the verb reachable: a
// subcommand that exists but is never added to the tree is unusable.
func TestOrchestratorRotateAccountIsRegistered(t *testing.T) {
	found := false
	for _, sub := range newOrchestratorCommand().Commands() {
		if strings.HasPrefix(sub.Use, "rotate-account") {
			found = true
			break
		}
	}
	assert.True(t, found, "orchestrator rotate-account is not registered on the command tree")
}

func TestOrchestratorRunOrchRotateAccount(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		var got *orchestratorSDK.RotateSessionAccountRequest
		mock := &mockOrchestratorClient{
			rotateAccountFn: func(_ context.Context, req *orchestratorSDK.RotateSessionAccountRequest) (*orchestratorSDK.RotateSessionAccountResponse, error) {
				got = req
				return &orchestratorSDK.RotateSessionAccountResponse{
					SessionID:         req.SessionID,
					PreviousAccountID: "acct-a",
					AccountID:         "acct-b",
					Strategy:          "least_usage",
					Mode:              "resume",
				}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		// Flags are bound to package globals, and constructing the command
		// writes their defaults back — so set them through the flag set, after
		// construction, exactly as cobra would.
		cmd := newOrchRotateAccountCommand()
		require.NoError(t, cmd.Flags().Set("reason", "seat retired"))
		require.NoError(t, cmd.Flags().Set("hold-outgoing", "true"))
		require.NoError(t, cmd.RunE(cmd, []string{"sess_123"}))

		require.NotNil(t, got)
		assert.Equal(t, "sess_123", got.SessionID)
		assert.Equal(t, "seat retired", got.Reason)
		assert.True(t, got.HoldOutgoing)
	})

	t.Run("FactoryError", func(t *testing.T) {
		setErrorOrchestratorFactory()
		cmd := newOrchRotateAccountCommand()
		assert.Error(t, cmd.RunE(cmd, []string{"sess_123"}))
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			rotateAccountFn: func(context.Context, *orchestratorSDK.RotateSessionAccountRequest) (*orchestratorSDK.RotateSessionAccountResponse, error) {
				return nil, errors.New(errors.ErrConflict, "rotate", "session is not rotatable")
			},
		}
		setMockOrchestratorFactory(mock)

		cmd := newOrchRotateAccountCommand()
		assert.Error(t, cmd.RunE(cmd, []string{"sess_123"}))
	})
}

func TestFormatOrchRotateAccountResponse(t *testing.T) {
	resp := &orchestratorSDK.RotateSessionAccountResponse{
		SessionID:         "sess_123",
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              "in_place",
	}

	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, formatOrchRotateAccountResponse(resp, "json", &buf))
		assert.Contains(t, buf.String(), `"previous_account_id": "acct-a"`)
		assert.Contains(t, buf.String(), `"account_id": "acct-b"`)
	})

	t.Run("table", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, formatOrchRotateAccountResponse(resp, "table", &buf))
		out := buf.String()
		assert.Contains(t, out, "acct-a")
		assert.Contains(t, out, "acct-b")
		assert.Contains(t, out, "in_place")
	})

	t.Run("yaml", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, formatOrchRotateAccountResponse(resp, "yaml", &buf))
		assert.Contains(t, buf.String(), "acct-b")
	})

	t.Run("invalid format", func(t *testing.T) {
		var buf bytes.Buffer
		assert.Error(t, formatOrchRotateAccountResponse(resp, "not-a-format", &buf))
	})
}

// =============================================================================
// --account-allow on spawn
// =============================================================================

func TestOrchestratorSpawnCommandAccountFlags(t *testing.T) {
	cmd := newOrchSpawnCommand()

	for _, name := range []string{"account-id", "account-auto", "account-strategy", "account-allow"} {
		assert.NotNilf(t, cmd.Flags().Lookup(name), "spawn is missing --%s", name)
	}
	assert.Equal(t, "stringSlice", cmd.Flags().Lookup("account-allow").Value.Type(),
		"--account-allow must be repeatable")
}

// TestOrchestratorSpawnCarriesAccountAllow is the CLI half of gap 2: an
// operator that enumerates a rotation pool must actually get a rotatable
// session, and a bare --account-id must stay a deliberate pin.
func TestOrchestratorSpawnCarriesAccountAllow(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	var got *orchestratorSDK.SpawnSessionRequest
	mock := &mockOrchestratorClient{
		spawnSessionFn: func(_ context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			got = req
			return &orchestratorSDK.SpawnSessionResponse{SessionID: "sess_new", Backend: req.Backend}, nil
		},
	}
	setMockOrchestratorFactory(mock)
	orchOutputFmt = "json"

	cmd := newOrchSpawnCommand()
	require.NoError(t, cmd.Flags().Set("backend", "claude"))
	require.NoError(t, cmd.Flags().Set("account-id", "acct-a"))
	require.NoError(t, cmd.Flags().Set("account-allow", "acct-a,acct-b"))
	require.NoError(t, cmd.RunE(cmd, []string{}))

	require.NotNil(t, got)
	assert.Equal(t, "acct-a", got.AccountID)
	assert.Equal(t, []string{"acct-a", "acct-b"}, got.AccountAllow)
	assert.False(t, got.AccountAuto, "the CLI must not invent account_auto")
}

// TestOrchestratorSpawnWithoutAccountFlagsStaysUnset is the negative case: a
// spawn that names no account leaves every account field zero, preserving the
// pre-account behavior exactly.
func TestOrchestratorSpawnWithoutAccountFlagsStaysUnset(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	var got *orchestratorSDK.SpawnSessionRequest
	mock := &mockOrchestratorClient{
		spawnSessionFn: func(_ context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			got = req
			return &orchestratorSDK.SpawnSessionResponse{SessionID: "sess_new", Backend: req.Backend}, nil
		},
	}
	setMockOrchestratorFactory(mock)
	orchOutputFmt = "json"

	cmd := newOrchSpawnCommand()
	require.NoError(t, cmd.Flags().Set("backend", "claude"))
	require.NoError(t, cmd.RunE(cmd, []string{}))

	require.NotNil(t, got)
	assert.Empty(t, got.AccountID)
	assert.Empty(t, got.AccountAllow)
	assert.False(t, got.AccountAuto)
	assert.Empty(t, got.AccountStrategy)
}
