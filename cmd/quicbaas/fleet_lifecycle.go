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
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// undeployPurgeWorkspaces backs `fleet undeploy --purge-workspaces`.
var undeployPurgeWorkspaces bool

// lifecycleAction runs one lifecycle verb against the resolved spec.
type lifecycleAction func(ctx context.Context, svc service.FleetDeployService, spec *deploy.Spec) (*deploy.LifecycleResult, error)

// newFleetUndeployCommand builds `quicbaas fleet undeploy`.
func newFleetUndeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "undeploy",
		Short: "Remove a deployed agent from a node and from the fleet registry",
		Long: `Stop and disable the agent on a node, remove everything the deploy
installed (unit, config file, binary, start script, mTLS identity, the
daemon's persisted credentials), and remove it from the fleet registry.

Workspaces — the projects root and the daemon's work directory — are kept
unless --purge-workspaces is given: they hold job checkouts a pinned job may
still need.

Examples:
  quicbaas fleet undeploy --spec deployments/fleet/agent-01.yaml
  quicbaas fleet undeploy --ssh agent@agent-01 --ssh-key-name fleet-deploy --purge-workspaces`,
		PreRunE: relaxRequiredFlagsForSpecFile("ssh"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFleetLifecycle(cmd, func(ctx context.Context, svc service.FleetDeployService, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
				return svc.Undeploy(ctx, spec, service.UndeployOptions{PurgeWorkspaces: undeployPurgeWorkspaces})
			})
		},
	}
	registerDeployTargetFlags(cmd)
	cmd.Flags().BoolVar(&undeployPurgeWorkspaces, "purge-workspaces", false, "Also remove the projects root and the daemon's work directory")
	markLifecycleTargetRequired(cmd)
	return cmd
}

// newFleetStopCommand builds `quicbaas fleet stop`.
func newFleetStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a deployed agent and remove it from the fleet registry",
		Long: `Stop the agent on a node and remove it from the fleet registry, so a
stopped node never reads "running" there. The installation is kept;
"quicbaas fleet start" brings it back and it re-registers on its own.

Examples:
  quicbaas fleet stop --spec deployments/fleet/agent-01.yaml`,
		PreRunE: relaxRequiredFlagsForSpecFile("ssh"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFleetLifecycle(cmd, func(ctx context.Context, svc service.FleetDeployService, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
				return svc.Stop(ctx, spec)
			})
		},
	}
	registerDeployTargetFlags(cmd)
	markLifecycleTargetRequired(cmd)
	return cmd
}

// newFleetStartCommand builds `quicbaas fleet start`.
func newFleetStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a deployed agent",
		Long: `Start the agent installed on a node and, unless --wait-online 0, wait
for it to come back online in the fleet registry.

Examples:
  quicbaas fleet start --spec deployments/fleet/agent-01.yaml`,
		PreRunE: relaxRequiredFlagsForSpecFile("ssh"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFleetLifecycle(cmd, func(ctx context.Context, svc service.FleetDeployService, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
				return svc.Start(ctx, spec)
			})
		},
	}
	registerDeployTargetFlags(cmd)
	registerWaitOnlineFlag(cmd)
	markLifecycleTargetRequired(cmd)
	return cmd
}

// markLifecycleTargetRequired requires --ssh unless --spec names the node.
func markLifecycleTargetRequired(cmd *cobra.Command) {
	if err := cmd.MarkFlagRequired("ssh"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark ssh required: %v\n", err)
	}
}

// runFleetLifecycle resolves the node's spec, runs action, and prints the
// result. A partial result is printed before the error, so an operator sees
// what was done (stopped) as well as what was not (deregistered).
func runFleetLifecycle(cmd *cobra.Command, action lifecycleAction) error {
	spec, err := resolveDeploySpec(commandFlags(cmd))
	if err != nil {
		return formatError(err)
	}

	svc, err := fleetDeployServiceFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout+spec.WaitOnline)
	defer cancel()

	result, err := action(ctx, svc, spec)
	if result != nil {
		if fmtErr := formatLifecycleResult(result, fleetOutputFmt, os.Stdout); fmtErr != nil {
			return fmtErr
		}
	}
	if err != nil {
		return formatError(err)
	}
	return nil
}

// formatLifecycleResult prints a stop / start / undeploy result.
func formatLifecycleResult(result *deploy.LifecycleResult, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(result)

	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Host:\t%s\n", result.Host)
		fmt.Fprintf(tw, "Action:\t%s\n", result.Action)
		if result.Supervisor != deploy.SupervisorNone {
			fmt.Fprintf(tw, "Supervisor:\t%s\n", result.Supervisor)
		}
		if result.NodeID != "" {
			fmt.Fprintf(tw, "Agent ID:\t%s\n", result.NodeID)
		}
		if result.Action != "start" {
			fmt.Fprintf(tw, "Deregistered:\t%t\n", result.Deregistered)
		}
		if result.OnlineAt != "" {
			fmt.Fprintf(tw, "Online At:\t%s\n", result.OnlineAt)
		}
		if len(result.Removed) > 0 {
			fmt.Fprintf(tw, "Removed:\t%s\n", strings.Join(result.Removed, "\n\t"))
		}
		if result.Note != "" {
			fmt.Fprintf(tw, "Note:\t%s\n", result.Note)
		}
		return tw.Flush()
	}
}
