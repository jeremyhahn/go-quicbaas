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

// Package main: command-handler coverage for agent_account.go.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// =============================================================================
// Mock
// =============================================================================

// mockAccountClient implements accountSDK.Client for command-handler tests.
// Unset function fields return benign zero-value responses so each test only
// wires the call it exercises.
type mockAccountClient struct {
	createFn     func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error)
	getFn        func(ctx context.Context, tenantID, accountID string) (*accountSDK.AccountInfo, error)
	listFn       func(ctx context.Context, req *accountSDK.ListAccountsRequest) (*accountSDK.ListAccountsResponse, error)
	updateFn     func(ctx context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error)
	deleteFn     func(ctx context.Context, tenantID, accountID string) (*accountSDK.DeleteAccountResponse, error)
	rotateFn     func(ctx context.Context, req *accountSDK.RotateCredentialRequest) (*accountSDK.AccountAckResponse, error)
	revokeFn     func(ctx context.Context, req *accountSDK.RevokeAccountRequest) (*accountSDK.AccountInfo, error)
	setDefaultFn func(ctx context.Context, tenantID, accountID string) (*accountSDK.AccountAckResponse, error)
	usageFn      func(ctx context.Context, tenantID, accountID string) (*accountSDK.UsageReport, error)
}

func (m *mockAccountClient) Create(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
	if m.createFn != nil {
		return m.createFn(ctx, req)
	}
	return &accountSDK.AccountInfo{ID: "acct_1", TenantID: req.TenantID, Provider: req.Provider}, nil
}

func (m *mockAccountClient) Get(ctx context.Context, tenantID, accountID string) (*accountSDK.AccountInfo, error) {
	if m.getFn != nil {
		return m.getFn(ctx, tenantID, accountID)
	}
	return &accountSDK.AccountInfo{ID: accountID, TenantID: tenantID}, nil
}

func (m *mockAccountClient) List(ctx context.Context, req *accountSDK.ListAccountsRequest) (*accountSDK.ListAccountsResponse, error) {
	if m.listFn != nil {
		return m.listFn(ctx, req)
	}
	return &accountSDK.ListAccountsResponse{}, nil
}

func (m *mockAccountClient) Update(ctx context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, req)
	}
	return &accountSDK.AccountInfo{ID: req.AccountID, TenantID: req.TenantID}, nil
}

func (m *mockAccountClient) Delete(ctx context.Context, tenantID, accountID string) (*accountSDK.DeleteAccountResponse, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, tenantID, accountID)
	}
	return &accountSDK.DeleteAccountResponse{AccountID: accountID, Deleted: true}, nil
}

func (m *mockAccountClient) RotateCredential(ctx context.Context, req *accountSDK.RotateCredentialRequest) (*accountSDK.AccountAckResponse, error) {
	if m.rotateFn != nil {
		return m.rotateFn(ctx, req)
	}
	return &accountSDK.AccountAckResponse{AccountID: req.AccountID}, nil
}

func (m *mockAccountClient) Revoke(ctx context.Context, req *accountSDK.RevokeAccountRequest) (*accountSDK.AccountInfo, error) {
	if m.revokeFn != nil {
		return m.revokeFn(ctx, req)
	}
	return &accountSDK.AccountInfo{ID: req.AccountID, TenantID: req.TenantID, Status: accountSDK.StatusRevoked}, nil
}

func (m *mockAccountClient) SetDefault(ctx context.Context, tenantID, accountID string) (*accountSDK.AccountAckResponse, error) {
	if m.setDefaultFn != nil {
		return m.setDefaultFn(ctx, tenantID, accountID)
	}
	return &accountSDK.AccountAckResponse{AccountID: accountID}, nil
}

func (m *mockAccountClient) Usage(ctx context.Context, tenantID, accountID string) (*accountSDK.UsageReport, error) {
	if m.usageFn != nil {
		return m.usageFn(ctx, tenantID, accountID)
	}
	return &accountSDK.UsageReport{}, nil
}

// Materialize satisfies the client interface. The CLI has no release verb —
// materializing a credential is what an enrolled agent daemon does on a worker
// host, not what an operator does at a terminal — so the double answers with an
// EMPTY envelope rather than a plausible-looking token: a CLI test that somehow
// reached this must not accidentally assert that a credential was printed.
func (m *mockAccountClient) Materialize(_ context.Context, req *accountSDK.MaterializeCredentialRequest) (*accountSDK.MaterializedCredential, error) {
	return &accountSDK.MaterializedCredential{
		AccountID:  req.AccountID,
		Credential: &accountSDK.ReleasedCredential{},
	}, nil
}

// =============================================================================
// Helpers
// =============================================================================

// withMockAccount installs an accountClientFactory returning the supplied mock
// and restores the previous factory, timeout, and flag state on cleanup.
func withMockAccount(t *testing.T, mc *mockAccountClient) {
	t.Helper()
	origFactory := accountClientFactory
	origTimeout := globalTimeout
	origTenant := globalTenantID
	origOutput := accountOutputFmt

	accountClientFactory = func() (accountSDK.Client, error) { return mc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	globalTenantID = "acme"
	accountOutputFmt = "table"

	t.Cleanup(func() {
		accountClientFactory = origFactory
		globalTimeout = origTimeout
		globalTenantID = origTenant
		accountOutputFmt = origOutput
		resetAccountFlags()
	})
}

// withFailingAccountFactory installs an accountClientFactory that always fails.
func withFailingAccountFactory(t *testing.T, err error) {
	t.Helper()
	orig := accountClientFactory
	accountClientFactory = func() (accountSDK.Client, error) { return nil, err }
	t.Cleanup(func() { accountClientFactory = orig })
}

// resetAccountFlags clears every package-level account flag.
func resetAccountFlags() {
	accountProvider = ""
	accountProject = ""
	accountDisplayName = ""
	accountDefault = false
	accountRefreshPolicy = ""
	accountClearProject = false
	accountCredentialFile = ""
	accountRevokeReason = ""
}

// credentialFile writes a credential JSON document to a temp file and returns
// its path.
func credentialFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credential.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// stdinCommand returns a cobra command whose stdin yields body, so the '-'
// credential source can be exercised without touching the process's stdin.
func stdinCommand(body string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(body))
	return cmd
}

const sampleCredentialJSON = `{"access_token":"SECRET-ACCESS-TOKEN","refresh_token":"SECRET-REFRESH-TOKEN"}`

// =============================================================================
// Vocabulary validation
// =============================================================================

func TestParseAccountProvider(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "  CLAUDE  "} {
		provider, err := parseAccountProvider(name)
		require.NoError(t, err, name)
		assert.NotEmpty(t, provider)
	}
}

func TestParseAccountProvider_Invalid(t *testing.T) {
	_, err := parseAccountProvider("gemini")
	require.Error(t, err)
	var typed *InvalidAccountProviderError
	require.True(t, stderrors.As(err, &typed))
	assert.Contains(t, err.Error(), "claude")
}

func TestParseAccountRefreshPolicy(t *testing.T) {
	policy, err := parseAccountRefreshPolicy("")
	require.NoError(t, err)
	assert.Empty(t, policy, "an empty policy must stay unset so the server default applies")

	policy, err = parseAccountRefreshPolicy("OAuth2")
	require.NoError(t, err)
	assert.Equal(t, accountSDK.RefreshOAuth2, policy)
}

func TestParseAccountRefreshPolicy_Invalid(t *testing.T) {
	_, err := parseAccountRefreshPolicy("magic")
	require.Error(t, err)
	var typed *InvalidAccountRefreshPolicyError
	require.True(t, stderrors.As(err, &typed))
}

// =============================================================================
// Credential input
// =============================================================================

func TestReadAccountCredential_FromFile(t *testing.T) {
	t.Cleanup(resetAccountFlags)
	accountCredentialFile = credentialFile(t, sampleCredentialJSON)

	credential, err := readAccountCredential(&cobra.Command{}, "account.create")
	require.NoError(t, err)
	assert.Equal(t, "SECRET-ACCESS-TOKEN", credential.AccessToken)
	assert.Equal(t, "SECRET-REFRESH-TOKEN", credential.RefreshToken)
}

func TestReadAccountCredential_FromStdin(t *testing.T) {
	t.Cleanup(resetAccountFlags)
	accountCredentialFile = "-"

	credential, err := readAccountCredential(stdinCommand(sampleCredentialJSON), "account.create")
	require.NoError(t, err)
	assert.Equal(t, "SECRET-ACCESS-TOKEN", credential.AccessToken)
}

func TestReadAccountCredential_ErrorPaths(t *testing.T) {
	t.Cleanup(resetAccountFlags)

	t.Run("no source", func(t *testing.T) {
		accountCredentialFile = ""
		_, err := readAccountCredential(&cobra.Command{}, "account.create")
		require.Error(t, err)
		var typed *AccountCredentialSourceRequiredError
		assert.True(t, stderrors.As(err, &typed))
	})

	t.Run("missing file", func(t *testing.T) {
		accountCredentialFile = filepath.Join(t.TempDir(), "absent.json")
		_, err := readAccountCredential(&cobra.Command{}, "account.create")
		require.Error(t, err)
		var typed *AccountCredentialReadError
		assert.True(t, stderrors.As(err, &typed))
	})

	t.Run("empty stdin", func(t *testing.T) {
		accountCredentialFile = "-"
		_, err := readAccountCredential(stdinCommand(""), "account.create")
		require.Error(t, err)
		var typed *AccountCredentialSourceRequiredError
		assert.True(t, stderrors.As(err, &typed))
	})

	t.Run("malformed json", func(t *testing.T) {
		accountCredentialFile = credentialFile(t, "{not-json")
		_, err := readAccountCredential(&cobra.Command{}, "account.create")
		require.Error(t, err)
		var typed *AccountCredentialParseError
		assert.True(t, stderrors.As(err, &typed))
	})
}

// =============================================================================
// Command handlers — success
// =============================================================================

func TestRunAgentAccountCreate(t *testing.T) {
	var captured *accountSDK.CreateAccountRequest
	mc := &mockAccountClient{
		createFn: func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
			captured = req
			return &accountSDK.AccountInfo{ID: "acct_1", TenantID: req.TenantID, Provider: req.Provider}, nil
		},
	}
	withMockAccount(t, mc)

	accountProvider = "claude"
	accountDisplayName = "alice-work"
	accountRefreshPolicy = "oauth2"
	accountDefault = true
	accountCredentialFile = "-"

	require.NoError(t, runAgentAccountCreate(stdinCommand(sampleCredentialJSON), nil))
	require.NotNil(t, captured)
	assert.Equal(t, "acme", captured.TenantID)
	assert.Equal(t, accountSDK.ProviderClaude, captured.Provider)
	assert.Equal(t, accountSDK.RefreshOAuth2, captured.RefreshPolicy)
	assert.True(t, captured.Default)
	require.NotNil(t, captured.Credential)
	assert.Equal(t, "SECRET-ACCESS-TOKEN", captured.Credential.AccessToken)
}

func TestRunAgentAccountListGetUpdate(t *testing.T) {
	var listReq *accountSDK.ListAccountsRequest
	var updateReq *accountSDK.UpdateAccountRequest
	mc := &mockAccountClient{
		listFn: func(ctx context.Context, req *accountSDK.ListAccountsRequest) (*accountSDK.ListAccountsResponse, error) {
			listReq = req
			return &accountSDK.ListAccountsResponse{
				Accounts: []*accountSDK.AccountInfo{{ID: "acct_1", Provider: accountSDK.ProviderClaude}},
				Total:    1,
			}, nil
		},
		updateFn: func(ctx context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
			updateReq = req
			return &accountSDK.AccountInfo{ID: req.AccountID}, nil
		},
	}
	withMockAccount(t, mc)

	accountProvider = "claude"
	accountProject = "proj_1"
	require.NoError(t, runAgentAccountList(&cobra.Command{}, nil))
	require.NotNil(t, listReq)
	assert.Equal(t, accountSDK.ProviderClaude, listReq.Provider)
	assert.Equal(t, "proj_1", listReq.ProjectID)

	resetAccountFlags()
	require.NoError(t, runAgentAccountGet(&cobra.Command{}, []string{"acct_1"}))

	accountClearProject = true
	accountDisplayName = "renamed"
	require.NoError(t, runAgentAccountUpdate(&cobra.Command{}, []string{"acct_1"}))
	require.NotNil(t, updateReq)
	assert.True(t, updateReq.ClearProjectID)
	assert.Equal(t, "renamed", updateReq.DisplayName)
}

func TestRunAgentAccountLifecycleVerbs(t *testing.T) {
	var rotateReq *accountSDK.RotateCredentialRequest
	var revokeReq *accountSDK.RevokeAccountRequest
	mc := &mockAccountClient{
		rotateFn: func(ctx context.Context, req *accountSDK.RotateCredentialRequest) (*accountSDK.AccountAckResponse, error) {
			rotateReq = req
			return &accountSDK.AccountAckResponse{AccountID: req.AccountID}, nil
		},
		revokeFn: func(ctx context.Context, req *accountSDK.RevokeAccountRequest) (*accountSDK.AccountInfo, error) {
			revokeReq = req
			return &accountSDK.AccountInfo{ID: req.AccountID, Status: accountSDK.StatusRevoked}, nil
		},
		usageFn: func(ctx context.Context, tenantID, accountID string) (*accountSDK.UsageReport, error) {
			return &accountSDK.UsageReport{TotalTokens: 4242, ByAgent: map[string]int64{"a": 42}}, nil
		},
	}
	withMockAccount(t, mc)

	accountCredentialFile = "-"
	require.NoError(t, runAgentAccountRotate(stdinCommand(sampleCredentialJSON), []string{"acct_1"}))
	require.NotNil(t, rotateReq)
	require.NotNil(t, rotateReq.Credential)
	assert.Equal(t, "SECRET-REFRESH-TOKEN", rotateReq.Credential.RefreshToken)

	resetAccountFlags()
	accountRevokeReason = "leaked"
	require.NoError(t, runAgentAccountRevoke(&cobra.Command{}, []string{"acct_1"}))
	require.NotNil(t, revokeReq)
	assert.Equal(t, "leaked", revokeReq.Reason)

	require.NoError(t, runAgentAccountSetDefault(&cobra.Command{}, []string{"acct_1"}))
	require.NoError(t, runAgentAccountDelete(&cobra.Command{}, []string{"acct_1"}))
	require.NoError(t, runAgentAccountUsage(&cobra.Command{}, []string{"acct_1"}))
}

// =============================================================================
// Command handlers — error paths
// =============================================================================

func TestRunAgentAccountCommands_FactoryFailure(t *testing.T) {
	withMockAccount(t, &mockAccountClient{})
	withFailingAccountFactory(t, stderrors.New("no transport"))

	accountProvider = "claude"
	accountCredentialFile = "-"
	assert.Error(t, runAgentAccountCreate(stdinCommand(sampleCredentialJSON), nil))

	resetAccountFlags()
	assert.Error(t, runAgentAccountList(&cobra.Command{}, nil))
	assert.Error(t, runAgentAccountGet(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountUpdate(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountDelete(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountRevoke(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountSetDefault(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountUsage(&cobra.Command{}, []string{"acct_1"}))

	accountCredentialFile = "-"
	assert.Error(t, runAgentAccountRotate(stdinCommand(sampleCredentialJSON), []string{"acct_1"}))
}

func TestRunAgentAccountCommands_ServiceErrors(t *testing.T) {
	boom := stderrors.New("boom")
	mc := &mockAccountClient{
		createFn: func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
			return nil, boom
		},
		listFn: func(ctx context.Context, req *accountSDK.ListAccountsRequest) (*accountSDK.ListAccountsResponse, error) {
			return nil, boom
		},
		getFn: func(ctx context.Context, tenantID, accountID string) (*accountSDK.AccountInfo, error) {
			return nil, boom
		},
		updateFn: func(ctx context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
			return nil, boom
		},
		deleteFn: func(ctx context.Context, tenantID, accountID string) (*accountSDK.DeleteAccountResponse, error) {
			return nil, boom
		},
		rotateFn: func(ctx context.Context, req *accountSDK.RotateCredentialRequest) (*accountSDK.AccountAckResponse, error) {
			return nil, boom
		},
		revokeFn: func(ctx context.Context, req *accountSDK.RevokeAccountRequest) (*accountSDK.AccountInfo, error) {
			return nil, boom
		},
		setDefaultFn: func(ctx context.Context, tenantID, accountID string) (*accountSDK.AccountAckResponse, error) {
			return nil, boom
		},
		usageFn: func(ctx context.Context, tenantID, accountID string) (*accountSDK.UsageReport, error) {
			return nil, boom
		},
	}
	withMockAccount(t, mc)

	accountProvider = "claude"
	accountCredentialFile = "-"
	assert.Error(t, runAgentAccountCreate(stdinCommand(sampleCredentialJSON), nil))

	resetAccountFlags()
	assert.Error(t, runAgentAccountList(&cobra.Command{}, nil))
	assert.Error(t, runAgentAccountGet(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountUpdate(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountDelete(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountRevoke(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountSetDefault(&cobra.Command{}, []string{"acct_1"}))
	assert.Error(t, runAgentAccountUsage(&cobra.Command{}, []string{"acct_1"}))
}

func TestRunAgentAccountCreate_InvalidInput(t *testing.T) {
	withMockAccount(t, &mockAccountClient{
		createFn: func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
			t.Fatal("client must not be called for invalid input")
			return nil, nil
		},
	})

	accountProvider = "gemini"
	accountCredentialFile = "-"
	assert.Error(t, runAgentAccountCreate(stdinCommand(sampleCredentialJSON), nil), "unknown provider must fail")

	accountProvider = "claude"
	accountRefreshPolicy = "magic"
	assert.Error(t, runAgentAccountCreate(stdinCommand(sampleCredentialJSON), nil), "unknown refresh policy must fail")

	accountRefreshPolicy = ""
	accountCredentialFile = ""
	assert.Error(t, runAgentAccountCreate(&cobra.Command{}, nil), "missing credential source must fail")
}

func TestRunAgentAccountList_InvalidProvider(t *testing.T) {
	withMockAccount(t, &mockAccountClient{
		listFn: func(ctx context.Context, req *accountSDK.ListAccountsRequest) (*accountSDK.ListAccountsResponse, error) {
			t.Fatal("client must not be called for an invalid provider filter")
			return nil, nil
		},
	})
	accountProvider = "gemini"
	assert.Error(t, runAgentAccountList(&cobra.Command{}, nil))
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatAccountDetail_AllFormats(t *testing.T) {
	info := &accountSDK.AccountInfo{
		ID:            "acct_1",
		TenantID:      "acme",
		ProjectID:     "proj_1",
		Provider:      accountSDK.ProviderClaude,
		DisplayName:   "alice-work",
		Default:       true,
		Status:        accountSDK.StatusActive,
		RefreshPolicy: accountSDK.RefreshOAuth2,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
		LastError:     "previous refresh failed",
	}
	for _, format := range []string{"table", "json", "yaml"} {
		var buf bytes.Buffer
		require.NoError(t, formatAccountDetail(info, format, &buf), format)
		assert.Contains(t, buf.String(), "acct_1", format)
	}
}

func TestFormatAccountDetail_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	assert.Error(t, formatAccountDetail(&accountSDK.AccountInfo{ID: "a"}, "toml", &buf))
}

func TestFormatAccountList_EmptyAndPopulated(t *testing.T) {
	var empty bytes.Buffer
	require.NoError(t, formatAccountList(&accountSDK.ListAccountsResponse{}, "table", &empty))
	assert.Contains(t, empty.String(), "No accounts found")

	resp := &accountSDK.ListAccountsResponse{
		Accounts: []*accountSDK.AccountInfo{
			{ID: "acct_1", Provider: accountSDK.ProviderClaude, DisplayName: "alice", Status: accountSDK.StatusActive},
			nil, // a nil entry must not panic the renderer
		},
		Total: 1,
	}
	for _, format := range []string{"table", "json", "yaml"} {
		var buf bytes.Buffer
		require.NoError(t, formatAccountList(resp, format, &buf), format)
		assert.Contains(t, buf.String(), "acct_1", format)
	}
}

func TestFormatAccountUsage_AllFormats(t *testing.T) {
	report := &accountSDK.UsageReport{TotalTokens: 4242, BudgetLimit: 10000, PercentUsed: 42.42,
		ByAgent: map[string]int64{"b": 2, "a": 1}}
	for _, format := range []string{"table", "json", "yaml"} {
		var buf bytes.Buffer
		require.NoError(t, formatAccountUsage("acct_1", report, format, &buf), format)
		assert.Contains(t, buf.String(), "4242", format)
	}

	// A nil report renders zeros rather than panicking.
	var buf bytes.Buffer
	require.NoError(t, formatAccountUsage("acct_1", nil, "table", &buf))
	assert.Contains(t, buf.String(), "acct_1")
}

func TestFormatAccountAck_AllFormats(t *testing.T) {
	payload := map[string]interface{}{"account_id": "acct_1", "deleted": true}
	var table bytes.Buffer
	require.NoError(t, formatAccountAck(payload, "Account acct_1 deleted\n", "table", &table))
	assert.Contains(t, table.String(), "deleted")

	for _, format := range []string{"json", "yaml"} {
		var buf bytes.Buffer
		require.NoError(t, formatAccountAck(payload, "", format, &buf), format)
		assert.Contains(t, buf.String(), "acct_1", format)
	}

	var bad bytes.Buffer
	assert.Error(t, formatAccountAck(payload, "", "toml", &bad))
}

// =============================================================================
// Command tree + credential-leak guard
// =============================================================================

// TestAgentAccountCommandTree pins that every verb is mounted under
// `quicbaas agent account`, so a missing AddCommand is a build-breaking test
// failure rather than a silently absent subcommand.
func TestAgentAccountCommandTree(t *testing.T) {
	cmd := newAgentAccountCommand()
	got := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range []string{"create", "list", "get", "update", "delete", "rotate", "revoke", "set-default", "usage"} {
		assert.True(t, got[want], "missing subcommand %q", want)
	}
}

// TestAgentAccountCommandIsMountedUnderAgent proves the group is reachable as
// `quicbaas agent account`.
func TestAgentAccountCommandIsMountedUnderAgent(t *testing.T) {
	agent := newAgentCommand()
	for _, sub := range agent.Commands() {
		if sub.Name() == "account" {
			return
		}
	}
	t.Fatal("`agent account` is not mounted under the agent command")
}

// TestAgentAccountOutputCarriesNoCredential is the CLI-side leak guard: the
// client is handed a fully populated inbound credential, and no rendered
// output may contain any of it.
func TestAgentAccountOutputCarriesNoCredential(t *testing.T) {
	mc := &mockAccountClient{
		createFn: func(ctx context.Context, req *accountSDK.CreateAccountRequest) (*accountSDK.AccountInfo, error) {
			require.NotNil(t, req.Credential, "test setup: credential did not reach the client")
			require.Equal(t, "SECRET-ACCESS-TOKEN", req.Credential.AccessToken)
			return &accountSDK.AccountInfo{ID: "acct_1", TenantID: req.TenantID, Provider: req.Provider}, nil
		},
	}
	withMockAccount(t, mc)

	for _, format := range []string{"table", "json", "yaml"} {
		info, err := mc.Create(context.Background(), &accountSDK.CreateAccountRequest{
			TenantID: "acme",
			Provider: accountSDK.ProviderClaude,
			Credential: &accountSDK.Credential{
				AccessToken:  "SECRET-ACCESS-TOKEN",
				RefreshToken: "SECRET-REFRESH-TOKEN",
				Env:          map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
			},
		})
		require.NoError(t, err)

		var buf bytes.Buffer
		require.NoError(t, formatAccountDetail(info, format, &buf))
		rendered := buf.String()
		for _, marker := range []string{"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN", "SECRET-ENV-VALUE", "access_token"} {
			assert.NotContains(t, rendered, marker, "format %s leaked %s", format, marker)
		}
	}
}
