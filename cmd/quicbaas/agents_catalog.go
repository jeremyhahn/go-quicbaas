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
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	agentsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// =============================================================================
// quicbaas agents — the tenant AI agent catalog
// =============================================================================
//
//   quicbaas agents list
//   quicbaas agents get <agent-id>
//   quicbaas agents create -f agent.yaml
//   quicbaas agents update <agent-id> -f agent.yaml
//   quicbaas agents delete <agent-id>
//   quicbaas agents used-by <agent-id>
//   quicbaas agents pools list|get|create|update|delete|used-by
//
// An Agent is a named runtime + model + provider every role slot references;
// a pool is the ordered list of Agents a slot fails over along. The daemons
// that serve backends are `quicbaas fleet`, not this.

var (
	agentsOutputFmt string
	agentsFile      string
)

// AgentsClientFactory creates the Agents client; tests substitute a fake.
type AgentsClientFactory func() (agentsSDK.Client, error)

var agentsClientFactory AgentsClientFactory = createSDKAgentsClient

func createSDKAgentsClient() (agentsSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Agents(), nil
}

// AgentsFileRequiredError reports a create or update without a definition.
type AgentsFileRequiredError struct {
	Verb string
}

// Error implements the error interface.
func (e *AgentsFileRequiredError) Error() string {
	return "agents " + e.Verb + ": --file is required (the definition as YAML or JSON)"
}

// AgentsFileError reports a definition file that could not be read or parsed.
type AgentsFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *AgentsFileError) Error() string { return "agents: " + e.Path + ": " + e.Cause.Error() }

// Unwrap returns the cause.
func (e *AgentsFileError) Unwrap() error { return e.Cause }

func newAgentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage the tenant's AI Agents and the pools slots fail over along",
		Long: `An Agent is a named runtime (backend), model, provider endpoint and image that a
role slot references: a binding's worker, triage and review seats, a project's
default lead, a job's lead. A pool is an ordered list of Agents; a slot bound to
a pool fails over to the next Agent when one's accounts are exhausted, expired
or rate-limited, or its backend is unavailable.

An Agent in use cannot be deleted; the refusal names every user, and
'agents used-by' lists them.

Examples:
  quicbaas agents list
  quicbaas agents create -f claude-coder.yaml
  quicbaas agents used-by claude-coder
  quicbaas agents pools create -f reviewers.yaml`,
	}
	cmd.PersistentFlags().StringVarP(&agentsOutputFmt, "output", "o", "table", "Output format (table|json|yaml)")

	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List the tenant's Agents", Args: cobra.NoArgs, RunE: runAgentsList},
		&cobra.Command{Use: "get <agent-id>", Short: "Show one Agent", Args: cobra.ExactArgs(1), RunE: runAgentsGet},
		agentsFileCommand("create", "Create an Agent from a definition file", cobra.NoArgs, runAgentsCreate),
		agentsFileCommand("update <agent-id>", "Replace an Agent from a definition file", cobra.ExactArgs(1), runAgentsUpdate),
		&cobra.Command{Use: "delete <agent-id>", Short: "Delete an Agent nothing references", Args: cobra.ExactArgs(1), RunE: runAgentsDelete},
		&cobra.Command{Use: "used-by <agent-id>", Short: "List everything that references an Agent", Args: cobra.ExactArgs(1), RunE: runAgentsUsedBy},
		newAgentPoolsCommand(),
	)
	return cmd
}

func newAgentPoolsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pools",
		Short: "Manage pools: ordered failover lists of Agents",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List the tenant's pools", Args: cobra.NoArgs, RunE: runAgentPoolsList},
		&cobra.Command{Use: "get <pool-id>", Short: "Show one pool", Args: cobra.ExactArgs(1), RunE: runAgentPoolsGet},
		agentsFileCommand("create", "Create a pool from a definition file", cobra.NoArgs, runAgentPoolsCreate),
		agentsFileCommand("update <pool-id>", "Replace a pool from a definition file", cobra.ExactArgs(1), runAgentPoolsUpdate),
		&cobra.Command{Use: "delete <pool-id>", Short: "Delete a pool nothing references", Args: cobra.ExactArgs(1), RunE: runAgentPoolsDelete},
		&cobra.Command{Use: "used-by <pool-id>", Short: "List everything that references a pool", Args: cobra.ExactArgs(1), RunE: runAgentPoolsUsedBy},
	)
	return cmd
}

// agentsFileCommand is a verb that reads a definition file.
func agentsFileCommand(use, short string, args cobra.PositionalArgs, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: args, RunE: run}
	cmd.Flags().StringVarP(&agentsFile, "file", "f", "", "Definition (YAML or JSON)")
	return cmd
}

// readAgentsFile decodes a definition file into T.
func readAgentsFile[T any](verb, path string) (*T, error) {
	if path == "" {
		return nil, &AgentsFileRequiredError{Verb: verb}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &AgentsFileError{Path: path, Cause: err}
	}
	var out T
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, &AgentsFileError{Path: path, Cause: err}
	}
	return &out, nil
}

// withAgentsClient runs fn against the Agents client under the global
// timeout.
func withAgentsClient(fn func(ctx context.Context, client agentsSDK.Client) error) error {
	client, err := agentsClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	if err := fn(ctx, client); err != nil {
		return formatError(err)
	}
	return nil
}

func runAgentsList(_ *cobra.Command, _ []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		list, err := client.ListAgents(ctx, globalTenantID)
		if err != nil {
			return err
		}
		return formatAgentList(list, agentsOutputFmt, os.Stdout)
	})
}

func runAgentsGet(_ *cobra.Command, args []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		agent, err := client.GetAgent(ctx, globalTenantID, args[0])
		if err != nil {
			return err
		}
		return formatAgent(agent, agentsOutputFmt, os.Stdout)
	})
}

func runAgentsCreate(_ *cobra.Command, _ []string) error {
	agent, err := readAgentsFile[agentsSDK.Agent]("create", agentsFile)
	if err != nil {
		return formatError(err)
	}
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		created, err := client.CreateAgent(ctx, globalTenantID, agent)
		if err != nil {
			return err
		}
		return formatAgent(created, agentsOutputFmt, os.Stdout)
	})
}

func runAgentsUpdate(_ *cobra.Command, args []string) error {
	agent, err := readAgentsFile[agentsSDK.Agent]("update", agentsFile)
	if err != nil {
		return formatError(err)
	}
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		updated, err := client.UpdateAgent(ctx, globalTenantID, args[0], agent)
		if err != nil {
			return err
		}
		return formatAgent(updated, agentsOutputFmt, os.Stdout)
	})
}

func runAgentsDelete(_ *cobra.Command, args []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		if err := client.DeleteAgent(ctx, globalTenantID, args[0]); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Agent %s deleted\n", args[0])
		return nil
	})
}

func runAgentsUsedBy(_ *cobra.Command, args []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		usages, err := client.AgentUsedBy(ctx, globalTenantID, args[0])
		if err != nil {
			return err
		}
		return formatAgentUsages(usages, agentsOutputFmt, os.Stdout)
	})
}

func runAgentPoolsList(_ *cobra.Command, _ []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		pools, err := client.ListPools(ctx, globalTenantID)
		if err != nil {
			return err
		}
		return formatAgentPools(pools, agentsOutputFmt, os.Stdout)
	})
}

func runAgentPoolsGet(_ *cobra.Command, args []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		pool, err := client.GetPool(ctx, globalTenantID, args[0])
		if err != nil {
			return err
		}
		return formatAgentPools([]*agentsSDK.Pool{pool}, agentsOutputFmt, os.Stdout)
	})
}

func runAgentPoolsCreate(_ *cobra.Command, _ []string) error {
	pool, err := readAgentsFile[agentsSDK.Pool]("pools create", agentsFile)
	if err != nil {
		return formatError(err)
	}
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		created, err := client.CreatePool(ctx, globalTenantID, pool)
		if err != nil {
			return err
		}
		return formatAgentPools([]*agentsSDK.Pool{created}, agentsOutputFmt, os.Stdout)
	})
}

func runAgentPoolsUpdate(_ *cobra.Command, args []string) error {
	pool, err := readAgentsFile[agentsSDK.Pool]("pools update", agentsFile)
	if err != nil {
		return formatError(err)
	}
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		updated, err := client.UpdatePool(ctx, globalTenantID, args[0], pool)
		if err != nil {
			return err
		}
		return formatAgentPools([]*agentsSDK.Pool{updated}, agentsOutputFmt, os.Stdout)
	})
}

func runAgentPoolsDelete(_ *cobra.Command, args []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		if err := client.DeletePool(ctx, globalTenantID, args[0]); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Pool %s deleted\n", args[0])
		return nil
	})
}

func runAgentPoolsUsedBy(_ *cobra.Command, args []string) error {
	return withAgentsClient(func(ctx context.Context, client agentsSDK.Client) error {
		usages, err := client.PoolUsedBy(ctx, globalTenantID, args[0])
		if err != nil {
			return err
		}
		return formatAgentUsages(usages, agentsOutputFmt, os.Stdout)
	})
}

// =============================================================================
// Output
// =============================================================================

// writeStructured writes v as JSON or YAML, reporting whether format was one
// of them.
func writeStructured(v interface{}, format string, w io.Writer) (bool, error) {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return true, err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return true, encoder.Encode(v)
	case cli.OutputFormatYAML:
		return true, yaml.NewEncoder(w).Encode(v)
	default:
		return false, nil
	}
}

func formatAgentList(list []*agentsSDK.Agent, format string, w io.Writer) error {
	if done, err := writeStructured(list, format, w); done {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tROLES\tBACKEND\tMODEL\tIMAGE")
	for _, a := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.Name, strings.Join(a.Roles, ","), a.Backend, a.Model, orDash(a.Image))
	}
	return tw.Flush()
}

func formatAgent(a *agentsSDK.Agent, format string, w io.Writer) error {
	if done, err := writeStructured(a, format, w); done {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ID:\t%s\n", a.ID)
	fmt.Fprintf(tw, "Name:\t%s\n", a.Name)
	fmt.Fprintf(tw, "Description:\t%s\n", orDash(a.Description))
	fmt.Fprintf(tw, "Roles:\t%s\n", strings.Join(a.Roles, ", "))
	fmt.Fprintf(tw, "Backend:\t%s\n", a.Backend)
	fmt.Fprintf(tw, "Model:\t%s\n", a.Model)
	fmt.Fprintf(tw, "Image:\t%s\n", orDash(a.Image))
	fmt.Fprintf(tw, "Provider:\t%s\n", orDash(a.Provider.BaseURL))
	fmt.Fprintf(tw, "Reasoning effort:\t%s\n", orDash(a.ReasoningEffort))
	fmt.Fprintf(tw, "Account strategy:\t%s\n", orDash(a.Account.Strategy))
	fmt.Fprintf(tw, "Accounts:\t%s\n", orDash(strings.Join(a.Account.Accounts, ", ")))
	fmt.Fprintf(tw, "Context tokens:\t%s\n", intOrDash(a.ContextTokens))
	fmt.Fprintf(tw, "Max concurrent:\t%s\n", intOrDash(a.MaxConcurrent))
	return tw.Flush()
}

func formatAgentPools(pools []*agentsSDK.Pool, format string, w io.Writer) error {
	if done, err := writeStructured(pools, format, w); done {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tFAILOVER ORDER")
	for _, p := range pools {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.ID, p.Name, strings.Join(p.Agents, " -> "))
	}
	return tw.Flush()
}

func formatAgentUsages(usages []agentsSDK.Usage, format string, w io.Writer) error {
	if done, err := writeStructured(usages, format, w); done {
		return err
	}
	if len(usages) == 0 {
		_, err := fmt.Fprintln(w, "Nothing references it.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tID\tSLOT")
	for _, u := range usages {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", u.Kind, u.ID, u.Slot)
	}
	return tw.Flush()
}

// intOrDash renders a count, a dash for zero.
func intOrDash(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}
