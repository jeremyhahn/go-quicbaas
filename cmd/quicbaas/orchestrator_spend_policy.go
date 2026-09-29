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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// quicbaas orchestrator spend-policy — a tenant's own limits (go-quicbaas#1058)
// =============================================================================

// SpendPolicyFlagsRequiredError is a set command that names no limits.
type SpendPolicyFlagsRequiredError struct{}

// Error implements the error interface.
func (e *SpendPolicyFlagsRequiredError) Error() string {
	return "spend-policy set: give --" + spendWindowFlag + " (repeatable) or --" + clearSpendFlag
}

// newOrchSpendPolicyCommand creates the spend-policy command group.
func newOrchSpendPolicyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spend-policy",
		Short: "Show or set the tenant's own spend limits",
		Long: `Show or set the spend limits of the tenant named by --tenant: sliding
windows, in tokens and/or list-price dollars, over every turn charged to the
tenant — every project, pooled and local. The limits are stored and reported
against the tenant's usage; admission does not enforce them yet.

Examples:
  quicbaas orchestrator spend-policy get
  quicbaas orchestrator spend-policy set --spend-window name=monthly,size=720h,usd=2000
  quicbaas orchestrator spend-policy set --clear-spend`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Show the tenant's spend limits and its usage against each window",
		Args:  cobra.NoArgs,
		RunE:  runOrchSpendPolicyGet,
	})
	set := &cobra.Command{
		Use:   "set",
		Short: "Replace the tenant's spend limits",
		Args:  cobra.NoArgs,
		RunE:  runOrchSpendPolicySet,
	}
	addSpendFlags(set)
	cmd.AddCommand(set)
	return cmd
}

// runOrchSpendPolicyGet executes spend-policy get.
func runOrchSpendPolicyGet(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()
	ctx, cancel := orchContext()
	defer cancel()

	policy, err := client.GetTenantSpendPolicy(ctx, &orchestratorSDK.GetTenantSpendPolicyRequest{TenantID: globalTenantID})
	if err != nil {
		return formatError(err)
	}
	return formatTenantSpendPolicy(policy, orchOutputFmt, os.Stdout)
}

// runOrchSpendPolicySet executes spend-policy set.
func runOrchSpendPolicySet(cmd *cobra.Command, args []string) error {
	limits, err := spendLimitsFromFlags(cmd)
	if err != nil {
		return formatError(err)
	}
	if limits == nil {
		return formatError(&SpendPolicyFlagsRequiredError{})
	}
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()
	ctx, cancel := orchContext()
	defer cancel()

	policy, err := client.SetTenantSpendPolicy(ctx, &orchestratorSDK.SetTenantSpendPolicyRequest{
		TenantID: globalTenantID,
		Limits:   *limits,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTenantSpendPolicy(policy, orchOutputFmt, os.Stdout)
}

// formatTenantSpendPolicy renders a tenant's limits and window usage.
func formatTenantSpendPolicy(policy *orchestratorSDK.TenantSpendPolicy, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(policy)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(policy)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Tenant:\t%s\n", policy.TenantID)
		fmt.Fprintf(tw, "Limits:\t%s\n", formatSpendLimits(policy.Limits))
		if policy.UpdatedAt != nil {
			fmt.Fprintf(tw, "Updated:\t%s\n", policy.UpdatedAt.Format(time.RFC3339))
		}
		if len(policy.Windows) > 0 {
			fmt.Fprintf(tw, "\nWINDOW\tTOKENS\tLIST-PRICE $\tEXHAUSTED\n")
			for _, window := range policy.Windows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%t\n", window.Name,
					windowAxis(window.UsedTokens, window.MaxTokens, false),
					windowAxis(window.UsedEquivalentMicros, window.MaxEquivalentMicros, true),
					window.Exhausted)
			}
		}
		return tw.Flush()
	}
}

// windowAxis renders one axis of a window as "used / max", or "-" when the
// window does not meter it.
func windowAxis(used, limit int64, micros bool) string {
	if limit <= 0 {
		return "-"
	}
	if micros {
		return fmt.Sprintf("$%.2f / $%.2f", float64(used)/1_000_000, float64(limit)/1_000_000)
	}
	return fmt.Sprintf("%d / %d", used, limit)
}
