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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// =============================================================================
// `quicbaas account` — pooled provider account management.
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

	// accountCredentialSecretRef / accountCredentialSecretFile point an
	// account's credential at a tenant secret instead of an inline document.
	accountCredentialSecretRef  string
	accountCredentialSecretFile string
	accountMaxSessions          int

	// accountListPage pages, orders and searches `account list`.
	accountListPage pageFlags
)

// maxSessionsFlag is the flag that caps an account's live sessions.
const maxSessionsFlag = "max-concurrent-sessions"

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
	return e.Op + ": a credential source is required: --credential-file (use '-' to read the credential JSON from stdin)" +
		" or, on create and update, --credential-secret-ref"
}

// AccountCredentialSourceConflictError indicates a create command was given
// both an inline credential document and a tenant secret reference.
type AccountCredentialSourceConflictError struct {
	Op string
}

// Error implements the error interface.
func (e *AccountCredentialSourceConflictError) Error() string {
	return e.Op + ": --credential-file and --credential-secret-ref are mutually exclusive; " +
		"an account holds its credential inline or reads it from a tenant secret, not both"
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

// newAccountCommand creates the `quicbaas account` command group.
func newAccountCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Pooled provider account management",
		Long: `Manage the tenant's pool of provider accounts — the Anthropic/Claude,
OpenAI/Codex, and OpenCode logins an agent session can be spawned against.

Accounts are tenant-scoped: every command operates within the tenant named by
the global --tenant flag. Credential material is supplied from a file or stdin
and is never printed back by any command.

The recommended source is a tenant SECRET the account names by reference: the
account reads it on every spawn, so writing a new version of the secret rotates
the account with no account write.

Examples:
  quicbaas secrets put agents/claude/claude1/oauth_token --from-stdin
  quicbaas account create --provider claude --display-name claude1 \
    --credential-secret-ref agents/claude/claude1/oauth_token
  claude setup-token | quicbaas account create --provider claude \
    --display-name alice-work --credential-file -
  quicbaas account list --provider claude
  quicbaas account get acct_abc123
  quicbaas account set-default acct_abc123
  quicbaas account rotate acct_abc123 --credential-file ./new-token.json
  quicbaas account usage acct_abc123
  quicbaas account revoke acct_abc123 --reason "token leaked"`,
	}

	cmd.PersistentFlags().StringVar(&accountOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newAccountCreateCommand())
	cmd.AddCommand(newAccountListCommand())
	cmd.AddCommand(newAccountGetCommand())
	cmd.AddCommand(newAccountUpdateCommand())
	cmd.AddCommand(newAccountDeleteCommand())
	cmd.AddCommand(newAccountRotateCommand())
	cmd.AddCommand(newAccountRevokeCommand())
	cmd.AddCommand(newAccountSetDefaultCommand())
	cmd.AddCommand(newAccountUsageCommand())

	return cmd
}

// =============================================================================
// Create
// =============================================================================

func newAccountCreateCommand() *cobra.Command {
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

Instead of a document, --credential-secret-ref names a tenant secret (by its
short name, in the --tenant keyspace) the credential is read from on every
spawn; the secret must already exist. For a provider whose CLI reads a file
rather than an access-token variable (codex: .codex/auth.json), add
--credential-secret-file with the path relative to the session home. A
referenced account uses --refresh-policy none.

The FIRST account created for a provider becomes that provider's default
automatically, so a session spawned without --account uses it. Later accounts
are created non-default; pass --default to import one as the default instead,
or run 'quicbaas account set-default' at any time.

Examples:
  quicbaas account create --provider claude --display-name claude1 \
    --credential-secret-ref agents/claude/claude1/oauth_token
  quicbaas account create --provider codex --display-name reviewer \
    --credential-secret-ref agents/codex/reviewer/auth_json \
    --credential-secret-file .codex/auth.json
  claude setup-token | quicbaas account create --provider claude \
    --display-name alice-work --refresh-policy oauth2 --credential-file -
  quicbaas account create --provider codex --display-name ci \
    --credential-file ./codex.json --default`,
		Args: cobra.NoArgs,
		RunE: runAccountCreate,
	}

	cmd.Flags().StringVar(&accountProvider, "provider", "", "Provider the credential authenticates: claude, codex, or opencode (required)")
	cmd.Flags().StringVar(&accountDisplayName, "display-name", "", "Operator-chosen label for the account")
	cmd.Flags().StringVar(&accountProject, "project", "", "Scope the account to one project (empty means tenant-wide)")
	cmd.Flags().BoolVar(&accountDefault, "default", false, "Import this account AS the tenant+provider default, displacing the current one (the first account for a provider becomes the default with or without this flag)")
	cmd.Flags().StringVar(&accountRefreshPolicy, "refresh-policy", "", "Credential refresh policy: none or oauth2")
	addAccountBillingFlags(cmd, false)
	addAccountSpendFlags(cmd)
	cmd.Flags().StringVar(&accountCredentialFile, "credential-file", "", "Path to the credential JSON document; '-' reads stdin")
	cmd.Flags().StringVar(&accountCredentialSecretRef, "credential-secret-ref", "", "Short name of the tenant secret the credential is read from on every spawn (instead of --credential-file)")
	cmd.Flags().StringVar(&accountCredentialSecretFile, "credential-secret-file", "", "With --credential-secret-ref: write the secret to this file relative to the session home instead of the access token")
	cmd.Flags().IntVar(&accountMaxSessions, maxSessionsFlag, 0, "How many live sessions the account may serve at once; 0 is unmetered")

	if err := cmd.MarkFlagRequired("provider"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark provider flag as required: %v\n", err)
	}

	return cmd
}

func runAccountCreate(cmd *cobra.Command, args []string) error {
	provider, err := parseAccountProvider(accountProvider)
	if err != nil {
		return formatError(err)
	}
	policy, err := parseAccountRefreshPolicy(accountRefreshPolicy)
	if err != nil {
		return formatError(err)
	}
	billing, err := accountBillingFromFlags(cmd)
	if err != nil {
		return formatError(err)
	}
	spendPolicy, err := accountSpendFromFlags(cmd, spend.AccountPolicy{})
	if err != nil {
		return formatError(err)
	}
	credential, err := readAccountCreateCredential(cmd)
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
		Billing:       billing,
		Spend:         spendPolicy,
		Credential:    credential,

		CredentialSecretRef:   accountCredentialSecretRef,
		CredentialSecretFile:  accountCredentialSecretFile,
		MaxConcurrentSessions: accountMaxSessions,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAccountDetail(info, accountOutputFmt, os.Stdout)
}

// =============================================================================
// List
// =============================================================================

func newAccountListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pooled provider accounts",
		Long: `List every pooled provider account in the tenant named by --tenant.

Examples:
  quicbaas account list
  quicbaas account list --provider claude --output json
  quicbaas account list --project proj_abc123
  quicbaas account list --search work --sort display_name --limit 50
  quicbaas account list --all`,
		Args: cobra.NoArgs,
		RunE: runAccountList,
	}
	accountListPage.bind(cmd)

	cmd.Flags().StringVar(&accountProvider, "provider", "", "Only list accounts for this provider")
	cmd.Flags().StringVar(&accountProject, "project", "", "Only list accounts scoped to this project")

	return cmd
}

func runAccountList(cmd *cobra.Command, args []string) error {
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

	accounts, env, err := fetchPages(&accountListPage, func(params page.Params) ([]*accountSDK.AccountInfo, page.Envelope, error) {
		resp, err := client.List(ctx, &accountSDK.ListAccountsRequest{
			TenantID:  globalTenantID,
			Provider:  provider,
			ProjectID: accountProject,
			Params:    params,
		})
		if err != nil || resp == nil {
			return nil, page.Envelope{}, err
		}
		return resp.Accounts, resp.Envelope, nil
	})
	if err != nil {
		return formatError(err)
	}

	resp := &accountSDK.ListAccountsResponse{Accounts: accounts, Envelope: env}
	return formatAccountList(resp, accountOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Get
// =============================================================================

func newAccountGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <accountID>",
		Short: "Get pooled provider account details",
		Long: `Retrieve a single pooled provider account's metadata.

Credential material is never returned.

Examples:
  quicbaas account get acct_abc123
  quicbaas account get acct_abc123 --output yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountGet,
	}
}

func runAccountGet(cmd *cobra.Command, args []string) error {
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

func newAccountUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <accountID>",
		Short: "Update pooled provider account metadata",
		Long: `Update an account's non-secret metadata. Only the flags you pass are
applied; every other field keeps its current value. Use --clear-project to
make a project-scoped account tenant-wide.

Use 'rotate' to replace an inline credential. --credential-secret-ref switches
the account to read its credential from a tenant secret (its inline credential
is destroyed), or re-points it; the secret must exist. Re-applying an account's
current reference is how an account put in "error" by a missing secret returns
to service once the secret is written.

Examples:
  quicbaas account update acct_abc123 --display-name alice-personal
  quicbaas account update acct_abc123 \
    --credential-secret-ref agents/claude/claude1/oauth_token
  quicbaas account update acct_abc123 --project proj_abc123
  quicbaas account update acct_abc123 --clear-project
  quicbaas account update acct_abc123 --max-concurrent-sessions 2
  quicbaas account update acct_abc123 --billing subscription --monthly-fee 200
  quicbaas account update acct_abc123 --spend-window name=five-hour,size=5h,tokens=2000000
  quicbaas account update acct_abc123 --breaker-failures 3 --breaker-window 1h --breaker-cooldown 30m`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountUpdate,
	}

	cmd.Flags().StringVar(&accountDisplayName, "display-name", "", "New operator-chosen label")
	cmd.Flags().StringVar(&accountProject, "project", "", "Scope the account to this project")
	cmd.Flags().BoolVar(&accountClearProject, "clear-project", false, "Remove the project scope, making the account tenant-wide")
	cmd.Flags().StringVar(&accountRefreshPolicy, "refresh-policy", "", "New refresh policy: none or oauth2")
	addAccountBillingFlags(cmd, true)
	addAccountSpendFlags(cmd)
	cmd.Flags().StringVar(&accountCredentialSecretRef, "credential-secret-ref", "", "Read the credential from this tenant secret from now on (switches an inline account)")
	cmd.Flags().StringVar(&accountCredentialSecretFile, "credential-secret-file", "", "With --credential-secret-ref: write the secret to this file relative to the session home")
	cmd.Flags().IntVar(&accountMaxSessions, maxSessionsFlag, 0, "New cap on live sessions; 0 makes the account unmetered (omit to leave it unchanged)")

	return cmd
}

func runAccountUpdate(cmd *cobra.Command, args []string) error {
	policy, err := parseAccountRefreshPolicy(accountRefreshPolicy)
	if err != nil {
		return formatError(err)
	}
	billing, err := accountBillingFromFlags(cmd)
	if err != nil {
		return formatError(err)
	}

	client, err := accountClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// A spend flag changes only what it names, so the policy it is applied
	// to is the account's current one.
	var spendPolicy *spend.AccountPolicy
	if accountSpendFlagsChanged(cmd) {
		current, err := client.Get(ctx, globalTenantID, args[0])
		if err != nil {
			return formatError(err)
		}
		if spendPolicy, err = accountSpendFromFlags(cmd, current.Spend); err != nil {
			return formatError(err)
		}
	}

	req := &accountSDK.UpdateAccountRequest{
		TenantID:             globalTenantID,
		AccountID:            args[0],
		DisplayName:          accountDisplayName,
		ProjectID:            accountProject,
		ClearProjectID:       accountClearProject,
		RefreshPolicy:        policy,
		Billing:              billing,
		Spend:                spendPolicy,
		CredentialSecretRef:  accountCredentialSecretRef,
		CredentialSecretFile: accountCredentialSecretFile,
	}
	// Only a flag the operator passed changes the cap: zero is a value they
	// can mean (unmetered), so "not passed" cannot be read off the number.
	if cmd.Flags().Changed(maxSessionsFlag) {
		maxSessions := accountMaxSessions
		req.MaxConcurrentSessions = &maxSessions
	}
	info, err := client.Update(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatAccountDetail(info, accountOutputFmt, os.Stdout)
}

// =============================================================================
// Delete
// =============================================================================

func newAccountDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <accountID>",
		Short: "Delete a pooled provider account",
		Long: `Permanently delete an account and its stored credential envelope.

Sessions already spawned with the credential are unaffected; no new spawn can
use it. Use 'revoke' to keep the audit record while excluding the account from
selection.

Examples:
  quicbaas account delete acct_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountDelete,
	}
}

func runAccountDelete(cmd *cobra.Command, args []string) error {
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

func newAccountRotateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate <accountID>",
		Short: "Replace a pooled provider account's credential",
		Long: `Replace the account's stored credential envelope, leaving its metadata
unchanged. Run this after re-running the provider's login flow.

Examples:
  claude setup-token | quicbaas account rotate acct_abc123 --credential-file -
  quicbaas account rotate acct_abc123 --credential-file ./new-token.json`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountRotate,
	}

	cmd.Flags().StringVar(&accountCredentialFile, "credential-file", "", "Path to the replacement credential JSON; '-' reads stdin (required)")

	return cmd
}

func runAccountRotate(cmd *cobra.Command, args []string) error {
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

func newAccountRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <accountID>",
		Short: "Revoke a pooled provider account",
		Long: `Mark an account revoked. A revoked account is excluded from every
selection strategy immediately; sessions already holding its credential are not
killed retroactively.

Examples:
  quicbaas account revoke acct_abc123
  quicbaas account revoke acct_abc123 --reason "token leaked"`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountRevoke,
	}

	cmd.Flags().StringVar(&accountRevokeReason, "reason", "", "Reason recorded with the revocation")

	return cmd
}

func runAccountRevoke(cmd *cobra.Command, args []string) error {
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

func newAccountSetDefaultCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <accountID>",
		Short: "Mark a pooled provider account as the default",
		Long: `Mark an account the default for its tenant+provider (or
tenant+project+provider when the account is project-scoped). The default is
what a spawn selects when it names no account explicitly.

Examples:
  quicbaas account set-default acct_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountSetDefault,
	}
}

func runAccountSetDefault(cmd *cobra.Command, args []string) error {
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

func newAccountUsageCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "usage <accountID>",
		Short: "Report token usage for a pooled provider account",
		Long: `Report the cumulative token usage recorded against an account.

Examples:
  quicbaas account usage acct_abc123
  quicbaas account usage acct_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runAccountUsage,
	}
}

func runAccountUsage(cmd *cobra.Command, args []string) error {
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

// readAccountCreateCredential resolves create's credential source: the inline
// document from --credential-file, or none when --credential-secret-ref names a
// tenant secret instead. Both at once is refused before anything is read.
func readAccountCreateCredential(cmd *cobra.Command) (*accountSDK.Credential, error) {
	const op = "account.create"
	if accountCredentialSecretRef == "" {
		return readAccountCredential(cmd, op)
	}
	if accountCredentialFile != "" {
		return nil, &AccountCredentialSourceConflictError{Op: op}
	}
	return nil, nil
}

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
		fmt.Fprintf(tw, "Billing:\t%s\n", accountBillingLabel(info.Billing))
		fmt.Fprintf(tw, "Spend Policy:\t%s\n", formatAccountSpend(info.Spend))
		if info.CredentialSecretRef != "" {
			fmt.Fprintf(tw, "Credential Secret:\t%s\n", info.CredentialSecretRef)
		}
		if info.CredentialSecretFile != "" {
			fmt.Fprintf(tw, "Credential File:\t%s\n", info.CredentialSecretFile)
		}
		fmt.Fprintf(tw, "Max Concurrent Sessions:\t%s\n", sessionCapLabel(info.MaxConcurrentSessions))
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
			if resp != nil {
				writePageFooter(w, 0, resp.Envelope)
			}
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
		if err := tw.Flush(); err != nil {
			return err
		}
		writePageFooter(w, len(resp.Accounts), resp.Envelope)
		return nil

	default:
		if resp == nil {
			return nil
		}
		for _, info := range resp.Accounts {
			if info != nil {
				fmt.Fprintf(w, "%s\n", info.ID)
			}
		}
		writePageFooter(w, len(resp.Accounts), resp.Envelope)
		return nil
	}
}

// sessionCapLabel renders an account's session cap, naming zero for what it
// means rather than printing a number that reads as "none allowed".
func sessionCapLabel(maxSessions int) string {
	if maxSessions <= 0 {
		return "unmetered"
	}
	return strconv.Itoa(maxSessions)
}

// spendVerdictLine renders the spend gate's verdict on an account: whether its
// own spend policy admits more work and, if not, why and until when.
func spendVerdictLine(verdict *accountSDK.SpendHeadroom) string {
	switch {
	case verdict == nil:
		return "not metered by this server"
	case verdict.Admits:
		return "within its spend policy"
	}
	line := "refused (" + verdict.Refusal + ")"
	if verdict.Window != "" {
		line += ": window " + strconv.Quote(verdict.Window) + " " +
			strconv.FormatInt(verdict.Used, 10) + " of " + strconv.FormatInt(verdict.Limit, 10) + " " + verdict.Axis
	}
	if verdict.FreesAt != nil {
		line += "; frees up at " + verdict.FreesAt.UTC().Format(time.RFC3339)
	}
	return line
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
		fmt.Fprintf(tw, "Live Sessions:\t%d\n", report.LiveSessions)
		fmt.Fprintf(tw, "Spend:\t%s\n", spendVerdictLine(report.Spend))
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
