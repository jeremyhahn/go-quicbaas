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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// quicbaas orchestrator cost — one scope of the consumption ledger
// =============================================================================

// Cost report flags.
const (
	costProjectFlag = "project"
	costTaskFlag    = "task"
	costJobFlag     = "job"
	costSessionFlag = "session"
	costRoundFlag   = "round"
	costFromFlag    = "from"
	costToFlag      = "to"
	costHostFlag    = "host"
	costAgentFlag   = "agent"
)

// CostFlagError is a cost report flag the command cannot read.
type CostFlagError struct {
	Flag    string
	Value   string
	Message string
}

// Error implements the error interface.
func (e *CostFlagError) Error() string {
	return "invalid --" + e.Flag + " " + strconv.Quote(e.Value) + ": " + e.Message
}

// newOrchCostCommand creates the cost command.
func newOrchCostCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show what a scope of the factory spent, by host, model and the level below",
		Long: `Show one scope of the consumption ledger: the tokens spent under it, their
list-price equivalent and billed cost, the energy local turns drew (and how
much of it was measured), and the breakdown by host, by model and by the level
below. The scope is a PATH — project, task, job, session, round — so each flag
needs every one above it. --host narrows the report to the turns that ran on
one host ("-" for turns that name none). --agent narrows it to the turns of the
sessions one tenant Agent ran ("-" for raw-backend sessions) and shows that
Agent's own spend windows, which a spawn of it is checked against.

Examples:
  quicbaas orchestrator cost
  quicbaas orchestrator cost --host gpu-host:9091
  quicbaas orchestrator cost --agent claude-coder
  quicbaas orchestrator cost --project p1 --from 2026-09-01T00:00:00Z`,
		Args: cobra.NoArgs,
		RunE: runOrchCost,
	}
	for _, flag := range []struct{ name, usage string }{
		{costProjectFlag, "Narrow to one project"},
		{costTaskFlag, "Narrow to one task (requires --project)"},
		{costJobFlag, "Narrow to one job (requires --task)"},
		{costSessionFlag, "Narrow to one session (requires --job)"},
		{costFromFlag, "Only turns at or after this RFC 3339 instant"},
		{costToFlag, "Only turns before this RFC 3339 instant (requires --from)"},
		{costHostFlag, `Only turns that ran on this host ("-" for none)`},
		{costAgentFlag, `Only turns of sessions that ran as this tenant Agent ("-" for raw-backend sessions)`},
	} {
		cmd.Flags().String(flag.name, "", flag.usage)
	}
	cmd.Flags().Int(costRoundFlag, 0, "Narrow to one round (requires --session)")
	return cmd
}

// runOrchCost executes cost.
func runOrchCost(cmd *cobra.Command, _ []string) error {
	req, err := costRequestFromFlags(cmd)
	if err != nil {
		return formatError(err)
	}
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()
	ctx, cancel := orchContext()
	defer cancel()

	report, err := client.GetSpendReport(ctx, req)
	if err != nil {
		return formatError(err)
	}
	return formatSpendReport(report, orchOutputFmt, os.Stdout)
}

// costRequestFromFlags reads the command's flags into a request.
func costRequestFromFlags(cmd *cobra.Command) (*orchestratorSDK.GetSpendReportRequest, error) {
	req := &orchestratorSDK.GetSpendReportRequest{TenantID: globalTenantID}
	for _, field := range []struct {
		flag string
		into *string
	}{
		{costProjectFlag, &req.ProjectID},
		{costTaskFlag, &req.TaskID},
		{costJobFlag, &req.JobID},
		{costSessionFlag, &req.SessionID},
		{costHostFlag, &req.Host},
		{costAgentFlag, &req.Agent},
	} {
		value, err := cmd.Flags().GetString(field.flag)
		if err != nil {
			return nil, err
		}
		*field.into = value
	}
	if cmd.Flags().Changed(costRoundFlag) {
		round, err := cmd.Flags().GetInt(costRoundFlag)
		if err != nil {
			return nil, err
		}
		req.Round = round
		req.RoundSet = true
	}
	for _, bound := range []struct {
		flag string
		into **time.Time
	}{{costFromFlag, &req.From}, {costToFlag, &req.To}} {
		raw, err := cmd.Flags().GetString(bound.flag)
		if err != nil {
			return nil, err
		}
		if raw == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, &CostFlagError{Flag: bound.flag, Value: raw, Message: "must be an RFC 3339 instant"}
		}
		utc := at.UTC()
		*bound.into = &utc
	}
	return req, nil
}

// formatSpendReport renders a spend report.
func formatSpendReport(report *orchestratorSDK.GetSpendReportResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(report)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Scope:\t%s %s\n", report.Level, report.TenantID)
		if report.Host != "" {
			fmt.Fprintf(tw, "Host:\t%s\n", report.Host)
		}
		if report.Agent != "" {
			fmt.Fprintf(tw, "Agent:\t%s\n", report.Agent)
		}
		fmt.Fprintf(tw, "Turns:\t%d (%d failed)\n", report.Entries, report.Failures)
		fmt.Fprintf(tw, "Tokens:\t%d\n", report.TotalTokens)
		fmt.Fprintf(tw, "List price:\t%s\n", costMoney(report.EquivalentMicros, report.PricedEntries, report.UnpricedEntries))
		fmt.Fprintf(tw, "Energy:\t%s\n", costEnergy(report.SpendTotals))
		if len(report.Hosts) > 0 {
			fmt.Fprintf(tw, "\nHOST\tTURNS\tTOKENS\tLIST PRICE\tENERGY\n")
			for _, host := range report.Hosts {
				name := host.Host
				if host.Unattributed {
					name = "(no host)"
				}
				fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", name, host.Entries, host.TotalTokens,
					costMoney(host.EquivalentMicros, host.PricedEntries, host.UnpricedEntries), costEnergy(host.SpendTotals))
			}
		}
		if len(report.Agents) > 0 {
			fmt.Fprintf(tw, "\nAGENT\tTURNS\tTOKENS\tLIST PRICE\tENERGY\n")
			for _, agent := range report.Agents {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", costAgentLabel(agent), agent.Entries, agent.TotalTokens,
					costMoney(agent.EquivalentMicros, agent.PricedEntries, agent.UnpricedEntries), costEnergy(agent.SpendTotals))
			}
		}
		if report.AgentPolicyStatus != "" {
			fmt.Fprintf(tw, "\nAgent limits:\t%s\n", report.AgentPolicyStatus)
			for _, window := range report.AgentWindows {
				fmt.Fprintf(tw, "  %s\t%s\n", window.Name, costWindowLabel(window))
			}
		}
		return tw.Flush()
	}
}

// costAgentLabel names an Agent row: its name and ID, or what an
// unattributed row is.
func costAgentLabel(agent orchestratorSDK.SpendAgent) string {
	switch {
	case agent.Unattributed:
		return "(no agent: raw backend)"
	case agent.Name != "":
		return agent.Name + " (" + agent.AgentID + ")"
	default:
		return agent.AgentID
	}
}

// costWindowLabel renders one spend window's usage against its allowances.
func costWindowLabel(window orchestratorSDK.SpendWindowStatus) string {
	var parts []string
	if window.MaxTokens > 0 {
		parts = append(parts, strconv.FormatInt(window.UsedTokens, 10)+" of "+strconv.FormatInt(window.MaxTokens, 10)+" tokens")
	}
	if window.MaxEquivalentMicros > 0 {
		parts = append(parts, "$"+strconv.FormatFloat(float64(window.UsedEquivalentMicros)/1e6, 'f', 2, 64)+
			" of $"+strconv.FormatFloat(float64(window.MaxEquivalentMicros)/1e6, 'f', 2, 64))
	}
	label := strings.Join(parts, ", ")
	if window.Exhausted {
		label += " — spent"
		if window.FreesAt != nil {
			label += ", frees up at " + window.FreesAt.UTC().Format(time.RFC3339)
		}
	}
	return label
}

// costMoney renders a list-price figure honestly: nothing priced is a dash,
// never a zero, and a figure beside unpriced turns is a floor.
func costMoney(micros int64, priced, unpriced int) string {
	if priced == 0 {
		return "-"
	}
	figure := "$" + strconv.FormatFloat(float64(micros)/1e6, 'f', 4, 64)
	if unpriced > 0 {
		figure += "+"
	}
	return figure
}

// costEnergy renders the energy local turns drew and how much of it was
// measured rather than estimated.
func costEnergy(totals orchestratorSDK.SpendTotals) string {
	if totals.ElectricityEntries == 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(totals.EnergyMilliWh)/1000, 'f', 2, 64) + " Wh (" +
		strconv.Itoa(totals.MeasuredEnergyEntries) + " of " + strconv.Itoa(totals.ElectricityEntries) + " measured)"
}
