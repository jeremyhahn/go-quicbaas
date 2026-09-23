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
	stderrors "errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestRunAgentAccountCreate_SecretRef proves `account create
// --credential-secret-ref` sends the reference and NO inline credential, and
// reads nothing from stdin.
func TestRunAgentAccountCreate_SecretRef(t *testing.T) {
	var captured *accountSDK.CreateAccountRequest
	withMockAccount(t, &mockAccountClient{
		createFn: func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
			captured = req
			return &accountSDK.AccountInfo{ID: "acct_1", CredentialSecretRef: req.CredentialSecretRef}, nil
		},
	})
	accountProvider = "codex"
	accountCredentialSecretRef = "agents/codex/reviewer/auth_json"
	accountCredentialSecretFile = ".codex/auth.json"

	require.NoError(t, runAgentAccountCreate(stdinCommand("must-not-be-read"), nil))
	require.NotNil(t, captured)
	assert.Nil(t, captured.Credential)
	assert.Equal(t, "agents/codex/reviewer/auth_json", captured.CredentialSecretRef)
	assert.Equal(t, ".codex/auth.json", captured.CredentialSecretFile)
}

func TestRunAgentAccountCreate_SecretRefAndCredentialFileConflict(t *testing.T) {
	called := false
	withMockAccount(t, &mockAccountClient{
		createFn: func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
			called = true
			return &accountSDK.AccountInfo{}, nil
		},
	})
	accountProvider = "claude"
	accountCredentialSecretRef = "agents/claude/claude1/oauth_token"
	accountCredentialFile = "-"

	err := runAgentAccountCreate(stdinCommand(sampleCredentialJSON), nil)
	require.Error(t, err)
	var typed *AccountCredentialSourceConflictError
	assert.True(t, stderrors.As(err, &typed), "error %v is not an AccountCredentialSourceConflictError", err)
	assert.False(t, called, "a conflicting request must not reach the server")
}

func TestRunAgentAccountUpdate_SecretRef(t *testing.T) {
	var captured *accountSDK.UpdateAccountRequest
	withMockAccount(t, &mockAccountClient{
		updateFn: func(ctx context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
			captured = req
			return &accountSDK.AccountInfo{ID: req.AccountID, CredentialSecretRef: req.CredentialSecretRef}, nil
		},
	})
	accountCredentialSecretRef = "agents/claude/claude1/oauth_token"

	require.NoError(t, runAgentAccountUpdate(&cobra.Command{}, []string{"acct_1"}))
	require.NotNil(t, captured)
	assert.Equal(t, "agents/claude/claude1/oauth_token", captured.CredentialSecretRef)
}

func TestFormatAccountDetail_ShowsSecretReference(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, formatAccountDetail(&accountSDK.AccountInfo{
		ID: "acct_1", CredentialSecretRef: "agents/codex/reviewer/auth_json", CredentialSecretFile: ".codex/auth.json",
	}, "table", &out))
	assert.Contains(t, out.String(), "agents/codex/reviewer/auth_json")
	assert.Contains(t, out.String(), ".codex/auth.json")
}

func TestAgentAccountCommands_RegisterSecretRefFlags(t *testing.T) {
	for _, cmd := range []*cobra.Command{newAgentAccountCreateCommand(), newAgentAccountUpdateCommand()} {
		assert.NotNil(t, cmd.Flags().Lookup("credential-secret-ref"), cmd.Name())
		assert.NotNil(t, cmd.Flags().Lookup("credential-secret-file"), cmd.Name())
	}
}
