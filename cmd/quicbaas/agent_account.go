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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// =============================================================================
// `quicbaas agent account` — pooled provider account management.
//
// Every command goes through the SDK account client, so the whole verb set
// works identically on rest / grpc / quic / mcp / unix / embedded.
//
// SECURITY: no command prints credential material, because no SDK verb returns
// any. Credentials are supplied INBOUND on create and rotate, from a file or
// stdin — never as a command-line argument, which would land in shell history
// and the process table.
// =============================================================================

// =============================================================================
// Flags
// =============================================================================

var (
	accountOutputFmt      string
	accountProvider       string
	accountProject        string
	accountDisplayName    string
	accountDefault        bool
	accountRefreshPolicy  string
	accountClearProject   bool
	accountCredentialFile string
	accountRevokeReason   string
)

// =============================================================================
// Typed Errors
// =============================================================================

// AccountCredentialSourceRequiredError indicates a create/rotate command was
// run without a credential source.
type AccountCredentialSourceRequiredError struct {
	Op string
}

// Error implements the error interface.
func (e *AccountCredentialSourceRequiredError) Error() string {
	return e.Op + ": --credential-file is required (use '-' to read the credential JSON from stdin)"
}

// AccountCredentialReadError indicates the credential source could not be read.
type AccountCredentialReadError struct {
	Source string
	Err    error
}

// Error implements the error interface.
func (e *AccountCredentialReadError) Error() string {
	return "failed to read credential from " + e.Source + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *AccountCredentialReadError) Unwrap() error { return e.Err }

// AccountCredentialParseError indicates the credential source was not valid
// credential JSON.
type AccountCredentialParseError struct {
	Source string
	Err    error
}

// Error implements the error interface.
func (e *AccountCredentialParseError) Error() string {
	return "failed to parse credential JSON from " + e.Source + ": " + e.Err.Error()
}

// Unwrap exposes the underlying decode error.
func (e *AccountCredentialParseError) Unwrap() error { return e.Err }

// InvalidAccountProviderError indicates an unrecognized --provider value.
type InvalidAccountProviderError struct {
	Provider string
}

// Error implements the error interface.
func (e *InvalidAccountProviderError) Error() string {
	return "invalid provider: " + e.Provider + " (valid: " + strings.Join(accountProviderNames(), ", ") + ")"
}

// InvalidAccountRefreshPolicyError indicates an unrecognized
// --refresh-policy value.
type InvalidAccountRefreshPolicyError struct {
	Policy string
}

// Error implements the error interface.
func (e *InvalidAccountRefreshPolicyError) Error() string {
	return "invalid refresh policy: " + e.Policy + " (valid: " + strings.Join(accountRefreshPolicyNames(), ", ") + ")"
}

// =============================================================================
// Vocabulary
// =============================================================================

// accountProviders is the canonical provider lookup. Map-based dispatch keeps
// validation O(1) as the provider vocabulary grows.
var accountProviders = map[string]accountSDK.Provider{
	string(accountSDK.ProviderClaude):   accountSDK.ProviderClaude,
	string(accountSDK.ProviderCodex):    accountSDK.ProviderCodex,
	string(accountSDK.ProviderOpenCode): accountSDK.ProviderOpenCode,
}

// accountRefreshPolicies is the canonical refresh-policy lookup.
var accountRefreshPolicies = map[string]accountSDK.RefreshPolicy{
	string(accountSDK.RefreshNone):   accountSDK.RefreshNone,
	string(accountSDK.RefreshOAuth2): accountSDK.RefreshOAuth2,
}

// accountProviderNames returns the valid provider names in a stable order.
func accountProviderNames() []string {
	names := make([]string, 0, len(accountProviders))
	for name := range accountProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// accountRefreshPolicyNames returns the valid policy names in a stable order.
func accountRefreshPolicyNames() []string {
	names := make([]string, 0, len(accountRefreshPolicies))
	for name := range accountRefreshPolicies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseAccountProvider validates a --provider value.
func parseAccountProvider(s string) (accountSDK.Provider, error) {
	provider, ok := accountProviders[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return "", &InvalidAccountProviderError{Provider: s}
	}
	return provider, nil
}

// parseAccountRefreshPolicy validates a --refresh-policy value. An empty
// value is accepted and leaves the field unset (the server applies its
// default on create and leaves the stored value unchanged on update).
func parseAccountRefreshPolicy(s string) (accountSDK.RefreshPolicy, error) {
	trimmed := strings.ToLower(strings.TrimSpace(s))
	if trimmed == "" {
		return "", nil
	}
	policy, ok := accountRefreshPolicies[trimmed]
	if !ok {
		return "", &InvalidAccountRefreshPolicyError{Policy: s}
	}
	return policy, nil
}

// =============================================================================
// Command Tree
// =============================================================================

// newAgentAccountCommand creates the `quicbaas agent account` command group.
func newAgentAccountCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Pooled provider account management",
		Long: `Manage the tenant's pool of provider accounts — the Anthropic/Claude,
OpenAI/Codex, and OpenCode logins an agent session can be spawned against.

Accounts are tenant-scoped: every command operates within the tenant named by
the global --tenant flag. Credential material is supplied from a file or stdin
and is never printed back by any command.

Examples:
  claude setup-token | quicbaas agent account create --provider claude \
    --display-name alice-work --credential-file -
  quicbaas agent account list --provider claude
  quicbaas agent account get acct_abc123
  quicbaas agent account set-default acct_abc123
  quicbaas agent account rotate acct_abc123 --credential-file ./new-token.json
  quicbaas agent account usage acct_abc123
  quicbaas agent account revoke acct_abc123 --reason "token leaked"`,
	}

	cmd.PersistentFlags().StringVar(&accountOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newAgentAccountCreateCommand())
	cmd.AddCommand(newAgentAccountListCommand())
	cmd.AddCommand(newAgentAccountGetCommand())
	cmd.AddCommand(newAgentAccountUpdateCommand())
	cmd.AddCommand(newAgentAccountDeleteCommand())
	cmd.AddCommand(newAgentAccountRotateCommand())
	cmd.AddCommand(newAgentAccountRevokeCommand())
	cmd.AddCommand(newAgentAccountSetDefaultCommand())
	cmd.AddCommand(newAgentAccountUsageCommand())

	return cmd
}

// =============================================================================
// Create
// =============================================================================

func newAgentAccountCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a pooled provider account",
		Long: `Create a pooled provider account from a credential JSON document.

The credential document mirrors the account credential envelope; supply the
fields the provider needs:

  {
    "access_token": "...",
    "refresh_token": "...",
    "token_endpoint": "https://...",
    "client_id": "...",
    "access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN",
    "env": {"SOME_KEY": "value"}
  }

The FIRST account created for a provider becomes that provider's default
automatically, so a session spawned without --account uses it. Later accounts
are created non-default; pass --default to import one as the default instead,
or run 'quicbaas agent account set-default' at any time.

Examples:
  claude setup-token | quicbaas agent account create --provider claude \
    --display-name alice-work --refresh-policy oauth2 --credential-file -
  quicbaas agent account create --provider codex --display-name ci \
    --credential-file ./codex.json --default`,
		Args: cobra.NoArgs,
		RunE: runAgentAccountCreate,
	}

	cmd.Flags().StringVar(&accountProvider, "provider", "", "Provider the credential authenticates: claude, codex, or opencode (required)")
	cmd.Flags().StringVar(&accountDisplayName, "display-name", "", "Operator-chosen label for the account")
	cmd.Flags().StringVar(&accountProject, "project", "", "Scope the account to one project (empty means tenant-wide)")
	cmd.Flags().BoolVar(&accountDefault, "default", false, "Import this account AS the tenant+provider default, displacing the current one (the first account for a provider becomes the default with or without this flag)")
	cmd.Flags().StringVar(&accountRefreshPolicy, "refresh-policy", "", "Credential refresh policy: none or oauth2")
	cmd.Flags().StringVar(&accountCredentialFile, "credential-file", "", "Path to the credential JSON document; '-' reads stdin (required)")

	if err := cmd.MarkFlagRequired("provider"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark provider flag as required: %v\n", err)
	}

	return cmd
}

func runAgentAccountCreate(cmd *cobra.Command, args []string) error {
	provider, err := parseAccountProvider(accountProvider)
	if err != nil {
		return formatError(err)
	}
	policy, err := parseAccountRefreshPolicy(accountRefreshPolicy)
	if err != nil {
		return formatError(err)
	}
	credential, err := readAccountCredential(cmd, "account.create")
	if err != nil {
		return formatError(err)
	}

	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Create(ctx, &accountSDK.CreateAccountRequest{
		TenantID:      globalTenantID,
		ProjectID:     accountProject,
		Provider:      provider,
		DisplayName:   accountDisplayName,
		Default:       accountDefault,
		RefreshPolicy: policy,
		Credential:    credential,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAccountDetail(info, accountOutputFmt, os.Stdout)
}

// =============================================================================
// List
// =============================================================================

func newAgentAccountListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pooled provider accounts",
		Long: `List every pooled provider account in the tenant named by --tenant.

Examples:
  quicbaas agent account list
  quicbaas agent account list --provider claude --output json
  quicbaas agent account list --project proj_abc123`,
		Args: cobra.NoArgs,
		RunE: runAgentAccountList,
	}

	cmd.Flags().StringVar(&accountProvider, "provider", "", "Only list accounts for this provider")
	cmd.Flags().StringVar(&accountProject, "project", "", "Only list accounts scoped to this project")

	return cmd
}

func runAgentAccountList(cmd *cobra.Command, args []string) error {
	// An empty --provider means "no filter", so it is not validated here; a
	// non-empty value still must be a known provider.
	var provider accountSDK.Provider
	if accountProvider != "" {
		parsed, err := parseAccountProvider(accountProvider)
		if err != nil {
			return formatError(err)
		}
		provider = parsed
	}

	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.List(ctx, &accountSDK.ListAccountsRequest{
		TenantID:  globalTenantID,
		Provider:  provider,
		ProjectID: accountProject,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAccountList(resp, accountOutputFmt, os.Stdout)
}

// =============================================================================
// Get
// =============================================================================

func newAgentAccountGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <accountID>",
		Short: "Get pooled provider account details",
		Long: `Retrieve a single pooled provider account's metadata.

Credential material is never returned.

Examples:
  quicbaas agent account get acct_abc123
  quicbaas agent account get acct_abc123 --output yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountGet,
	}
}

func runAgentAccountGet(cmd *cobra.Command, args []string) error {
	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Get(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}

	return formatAccountDetail(info, accountOutputFmt, os.Stdout)
}

// =============================================================================
// Update
// =============================================================================

func newAgentAccountUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <accountID>",
		Short: "Update pooled provider account metadata",
		Long: `Update an account's non-secret metadata. Only the flags you pass are
applied; every other field keeps its current value. Use --clear-project to
make a project-scoped account tenant-wide.

Use 'rotate' to replace the credential itself.

Examples:
  quicbaas agent account update acct_abc123 --display-name alice-personal
  quicbaas agent account update acct_abc123 --project proj_abc123
  quicbaas agent account update acct_abc123 --clear-project`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountUpdate,
	}

	cmd.Flags().StringVar(&accountDisplayName, "display-name", "", "New operator-chosen label")
	cmd.Flags().StringVar(&accountProject, "project", "", "Scope the account to this project")
	cmd.Flags().BoolVar(&accountClearProject, "clear-project", false, "Remove the project scope, making the account tenant-wide")
	cmd.Flags().StringVar(&accountRefreshPolicy, "refresh-policy", "", "New refresh policy: none or oauth2")

	return cmd
}

func runAgentAccountUpdate(cmd *cobra.Command, args []string) error {
	policy, err := parseAccountRefreshPolicy(accountRefreshPolicy)
	if err != nil {
		return formatError(err)
	}

	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Update(ctx, &accountSDK.UpdateAccountRequest{
		TenantID:       globalTenantID,
		AccountID:      args[0],
		DisplayName:    accountDisplayName,
		ProjectID:      accountProject,
		ClearProjectID: accountClearProject,
		RefreshPolicy:  policy,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAccountDetail(info, accountOutputFmt, os.Stdout)
}

// =============================================================================
// Delete
// =============================================================================

func newAgentAccountDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <accountID>",
		Short: "Delete a pooled provider account",
		Long: `Permanently delete an account and its stored credential envelope.

Sessions already spawned with the credential are unaffected; no new spawn can
use it. Use 'revoke' to keep the audit record while excluding the account from
selection.

Examples:
  quicbaas agent account delete acct_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountDelete,
	}
}

func runAgentAccountDelete(cmd *cobra.Command, args []string) error {
	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.Delete(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}

	return formatAccountAck(map[string]interface{}{
		"account_id": resp.AccountID,
		"deleted":    resp.Deleted,
	}, fmt.Sprintf("Account %s deleted\n", resp.AccountID), accountOutputFmt, os.Stdout)
}

// =============================================================================
// Rotate
// =============================================================================

func newAgentAccountRotateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate <accountID>",
		Short: "Replace a pooled provider account's credential",
		Long: `Replace the account's stored credential envelope, leaving its metadata
unchanged. Run this after re-running the provider's login flow.

Examples:
  claude setup-token | quicbaas agent account rotate acct_abc123 --credential-file -
  quicbaas agent account rotate acct_abc123 --credential-file ./new-token.json`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountRotate,
	}

	cmd.Flags().StringVar(&accountCredentialFile, "credential-file", "", "Path to the replacement credential JSON; '-' reads stdin (required)")

	return cmd
}

func runAgentAccountRotate(cmd *cobra.Command, args []string) error {
	credential, err := readAccountCredential(cmd, "account.rotate")
	if err != nil {
		return formatError(err)
	}

	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RotateCredential(ctx, &accountSDK.RotateCredentialRequest{
		TenantID:   globalTenantID,
		AccountID:  args[0],
		Credential: credential,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAccountAck(map[string]interface{}{"account_id": resp.AccountID},
		fmt.Sprintf("Account %s credential rotated\n", resp.AccountID), accountOutputFmt, os.Stdout)
}

// =============================================================================
// Revoke
// =============================================================================

func newAgentAccountRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <accountID>",
		Short: "Revoke a pooled provider account",
		Long: `Mark an account revoked. A revoked account is excluded from every
selection strategy immediately; sessions already holding its credential are not
killed retroactively.

Examples:
  quicbaas agent account revoke acct_abc123
  quicbaas agent account revoke acct_abc123 --reason "token leaked"`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountRevoke,
	}

	cmd.Flags().StringVar(&accountRevokeReason, "reason", "", "Reason recorded with the revocation")

	return cmd
}

func runAgentAccountRevoke(cmd *cobra.Command, args []string) error {
	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Revoke(ctx, &accountSDK.RevokeAccountRequest{
		TenantID:  globalTenantID,
		AccountID: args[0],
		Reason:    accountRevokeReason,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAccountDetail(info, accountOutputFmt, os.Stdout)
}

// =============================================================================
// Set Default
// =============================================================================

func newAgentAccountSetDefaultCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <accountID>",
		Short: "Mark a pooled provider account as the default",
		Long: `Mark an account the default for its tenant+provider (or
tenant+project+provider when the account is project-scoped). The default is
what a spawn selects when it names no account explicitly.

Examples:
  quicbaas agent account set-default acct_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountSetDefault,
	}
}

func runAgentAccountSetDefault(cmd *cobra.Command, args []string) error {
	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.SetDefault(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}

	return formatAccountAck(map[string]interface{}{"account_id": resp.AccountID},
		fmt.Sprintf("Account %s is now the default\n", resp.AccountID), accountOutputFmt, os.Stdout)
}

// =============================================================================
// Usage
// =============================================================================

func newAgentAccountUsageCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "usage <accountID>",
		Short: "Report token usage for a pooled provider account",
		Long: `Report the cumulative token usage recorded against an account.

Examples:
  quicbaas agent account usage acct_abc123
  quicbaas agent account usage acct_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentAccountUsage,
	}
}

func runAgentAccountUsage(cmd *cobra.Command, args []string) error {
	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	report, err := client.Usage(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}

	return formatAccountUsage(args[0], report, accountOutputFmt, os.Stdout)
}

// =============================================================================
// Credential input
// =============================================================================

// readAccountCredential loads the credential envelope from --credential-file
// ('-' means stdin). Credentials are never accepted as a flag value: a
// command-line secret lands in shell history and the process table.
func readAccountCredential(cmd *cobra.Command, op string) (*accountSDK.Credential, error) {
	if accountCredentialFile == "" {
		return nil, &AccountCredentialSourceRequiredError{Op: op}
	}

	source := accountCredentialFile
	var raw []byte
	var err error
	if accountCredentialFile == "-" {
		source = "stdin"
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(accountCredentialFile)
	}
	if err != nil {
		return nil, &AccountCredentialReadError{Source: source, Err: err}
	}
	if len(raw) == 0 {
		return nil, &AccountCredentialSourceRequiredError{Op: op}
	}

	var credential accountSDK.Credential
	if err := json.Unmarshal(raw, &credential); err != nil {
		return nil, &AccountCredentialParseError{Source: source, Err: err}
	}
	return &credential, nil
}

// =============================================================================
// Formatting
// =============================================================================

// formatAccountDetail renders a single account. AccountInfo has no credential
// field, so no formatting path can print secret material.
func formatAccountDetail(info *accountSDK.AccountInfo, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(info)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(info)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%s\n", info.ID)
		fmt.Fprintf(tw, "Tenant ID:\t%s\n", info.TenantID)
		if info.ProjectID != "" {
			fmt.Fprintf(tw, "Project ID:\t%s\n", info.ProjectID)
		}
		fmt.Fprintf(tw, "Provider:\t%s\n", info.Provider)
		if info.DisplayName != "" {
			fmt.Fprintf(tw, "Display Name:\t%s\n", info.DisplayName)
		}
		fmt.Fprintf(tw, "Default:\t%t\n", info.Default)
		fmt.Fprintf(tw, "Status:\t%s\n", info.Status)
		fmt.Fprintf(tw, "Refresh Policy:\t%s\n", info.RefreshPolicy)
		if !info.CreatedAt.IsZero() {
			fmt.Fprintf(tw, "Created:\t%s\n", info.CreatedAt.Format(time.RFC3339))
		}
		if !info.UpdatedAt.IsZero() {
			fmt.Fprintf(tw, "Updated:\t%s\n", info.UpdatedAt.Format(time.RFC3339))
		}
		if !info.LastUsedAt.IsZero() {
			fmt.Fprintf(tw, "Last Used:\t%s\n", info.LastUsedAt.Format(time.RFC3339))
		}
		if !info.LastRefreshedAt.IsZero() {
			fmt.Fprintf(tw, "Last Refreshed:\t%s\n", info.LastRefreshedAt.Format(time.RFC3339))
		}
		if info.LastError != "" {
			fmt.Fprintf(tw, "Last Error:\t%s\n", info.LastError)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %s (%s, %s)\n", info.ID, info.DisplayName, info.Provider, info.Status)
		return nil
	}
}

// formatAccountList renders a list of accounts.
func formatAccountList(resp *accountSDK.ListAccountsResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		if resp == nil || len(resp.Accounts) == 0 {
			fmt.Fprintln(w, "No accounts found")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tPROVIDER\tDISPLAY NAME\tSTATUS\tDEFAULT\tPROJECT")
		for _, info := range resp.Accounts {
			if info == nil {
				continue
			}
			project := info.ProjectID
			if project == "" {
				project = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\n",
				info.ID, info.Provider, info.DisplayName, info.Status, info.Default, project)
		}
		return tw.Flush()

	default:
		if resp == nil {
			return nil
		}
		for _, info := range resp.Accounts {
			if info != nil {
				fmt.Fprintf(w, "%s\n", info.ID)
			}
		}
		return nil
	}
}

// formatAccountUsage renders a usage report.
func formatAccountUsage(accountID string, report *accountSDK.UsageReport, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if report == nil {
		report = &accountSDK.UsageReport{}
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(report)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Account ID:\t%s\n", accountID)
		fmt.Fprintf(tw, "Total Tokens:\t%d\n", report.TotalTokens)
		fmt.Fprintf(tw, "Budget Limit:\t%d\n", report.BudgetLimit)
		fmt.Fprintf(tw, "Percent Used:\t%.2f\n", report.PercentUsed)
		if len(report.ByAgent) > 0 {
			fmt.Fprintf(tw, "\nBy Agent:\n")
			agents := make([]string, 0, len(report.ByAgent))
			for agentID := range report.ByAgent {
				agents = append(agents, agentID)
			}
			sort.Strings(agents)
			for _, agentID := range agents {
				fmt.Fprintf(tw, "  %s:\t%d\n", agentID, report.ByAgent[agentID])
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %d tokens\n", accountID, report.TotalTokens)
		return nil
	}
}

// formatAccountAck renders the acknowledgement returned by the verbs that
// carry no account body (delete / rotate / set-default).
func formatAccountAck(payload map[string]interface{}, humanLine, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(payload)

	default:
		fmt.Fprint(w, humanLine)
		return nil
	}
}
