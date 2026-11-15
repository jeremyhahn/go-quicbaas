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
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// Fleet placement specs on the CLI (SPEC row declarative-placement,
// go-quicbaas#1681): quicbaas fleet placement declare|get|list|withdraw|drift.

// fleetPlacementClient is the placement surface of the orchestrator client.
// It is asserted, not added to OrchestratorClient, so the CLI's many
// orchestrator fakes need not grow five methods each.
type fleetPlacementClient interface {
	DeclareFleetPlacementSpec(ctx context.Context, req *orchestratorSDK.DeclareFleetPlacementSpecRequest) (*orchestratorSDK.FleetPlacementSpec, error)
	GetFleetPlacementSpec(ctx context.Context, req *orchestratorSDK.FleetPlacementSpecRequest) (*orchestratorSDK.FleetPlacementSpec, error)
	ListFleetPlacementSpecs(ctx context.Context, req *orchestratorSDK.ListFleetPlacementSpecsRequest) (*orchestratorSDK.ListFleetPlacementSpecsResponse, error)
	WithdrawFleetPlacementSpec(ctx context.Context, req *orchestratorSDK.FleetPlacementSpecRequest) (*orchestratorSDK.WithdrawFleetPlacementSpecResponse, error)
	GetFleetPlacementDrift(ctx context.Context, req *orchestratorSDK.FleetPlacementSpecRequest) (*orchestratorSDK.FleetPlacementDrift, error)
}

var (
	placementRole         string
	placementHosts        []string
	placementHostCount    int
	placementCapabilities []string
	placementActuate      bool
	placementGeneration   uint64
	placementFile         string
)

func newFleetPlacementCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "placement",
		Short: "Declare where a role's work may run across the fleet's hosts",
		Long: `Placement specs say where a role's work may run across the fleet's hosts.
The drift report compares the role's work with the hosts a spec admits; it is
read-only. Design: docs/scheduler/.

Examples:
  quicbaas fleet placement declare agents --role agent --hosts h1,h2
  quicbaas fleet placement declare ci --role ci --host-count 2 --capability gpu --generation 3
  quicbaas fleet placement declare agents --file agents.json
  quicbaas fleet placement list
  quicbaas fleet placement drift agents --output json
  quicbaas fleet placement withdraw agents`,
	}
	declare := &cobra.Command{
		Use:   "declare NAME",
		Short: "Create or replace a placement spec (generation 0 creates)",
		Args:  cobra.ExactArgs(1),
		RunE:  runFleetPlacementDeclare,
	}
	declare.Flags().StringVar(&placementRole, "role", "", "Workload role the spec places (agent, ci, review, ...)")
	declare.Flags().StringSliceVar(&placementHosts, "hosts", nil, "Named hosts that carry the role")
	declare.Flags().IntVar(&placementHostCount, "host-count", 0, "How many admissible hosts carry the role (instead of --hosts)")
	declare.Flags().StringSliceVar(&placementCapabilities, "capability", nil, "Capability every host must have (repeatable)")
	declare.Flags().BoolVar(&placementActuate, "actuate", false, "Let the actuator move work (reported only until it exists)")
	declare.Flags().Uint64Var(&placementGeneration, "generation", 0, "Generation read; 0 creates")
	declare.Flags().StringVar(&placementFile, "file", "", "Read the spec from a JSON or YAML file; flags given override it")

	named := func(use, short string, run func(*cobra.Command, []string) error) *cobra.Command {
		return &cobra.Command{Use: use + " NAME", Short: short, Args: cobra.ExactArgs(1), RunE: run}
	}
	cmd.AddCommand(declare,
		named("get", "Show a placement spec", runFleetPlacementGet),
		&cobra.Command{Use: "list", Short: "List the tenant's placement specs", Args: cobra.NoArgs, RunE: runFleetPlacementList},
		named("withdraw", "Withdraw a placement spec", runFleetPlacementWithdraw),
		named("drift", "Report a spec's role work against the hosts it admits", runFleetPlacementDrift),
	)
	return cmd
}

// withFleetPlacement opens the orchestrator client and runs fn on its
// placement surface.
func withFleetPlacement(fn func(context.Context, fleetPlacementClient) (interface{}, error)) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()
	placement, ok := client.(fleetPlacementClient)
	if !ok {
		return formatError(errors.New(errors.ErrConfiguration, "fleet placement", "the orchestrator client has no fleet placement verbs"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	out, err := fn(ctx, placement)
	if err != nil {
		return formatError(err)
	}
	return formatFleetPlacement(out, fleetOutputFmt, os.Stdout)
}

// fleetPlacementSpecFromFlags builds the spec from --file, then the flags the
// caller set.
func fleetPlacementSpecFromFlags(cmd *cobra.Command, name string) (*orchestratorSDK.FleetPlacementSpec, error) {
	spec := &orchestratorSDK.FleetPlacementSpec{}
	if placementFile != "" {
		data, err := os.ReadFile(placementFile) //nolint:gosec // the operator names the file
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrInvalidInput, "fleet placement: read --file")
		}
		// YAML is a superset of JSON; decode through JSON so the spec's json
		// names (required_capabilities, host_count) are the file's names.
		var doc interface{}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, errors.Wrap(err, errors.ErrInvalidInput, "fleet placement: parse --file")
		}
		if data, err = json.Marshal(doc); err == nil {
			err = json.Unmarshal(data, spec)
		}
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrInvalidInput, "fleet placement: parse --file")
		}
		if spec.Name != "" && spec.Name != name {
			return nil, errors.New(errors.ErrInvalidInput, "fleet placement", fmt.Sprintf("--file names spec %q, not %q", spec.Name, name))
		}
	}
	spec.Name = name
	flags := cmd.Flags()
	if flags.Changed("role") {
		spec.Role = placementRole
	}
	if flags.Changed("hosts") {
		spec.Hosts = placementHosts
	}
	if flags.Changed("host-count") {
		spec.HostCount = placementHostCount
	}
	if flags.Changed("capability") {
		spec.RequiredCapabilities = placementCapabilities
	}
	if flags.Changed("actuate") {
		spec.Actuate = placementActuate
	}
	if flags.Changed("generation") {
		spec.Generation = placementGeneration
	}
	return spec, nil
}

func runFleetPlacementDeclare(cmd *cobra.Command, args []string) error {
	spec, err := fleetPlacementSpecFromFlags(cmd, args[0])
	if err != nil {
		return formatError(err)
	}
	return withFleetPlacement(func(ctx context.Context, c fleetPlacementClient) (interface{}, error) {
		return c.DeclareFleetPlacementSpec(ctx, &orchestratorSDK.DeclareFleetPlacementSpecRequest{TenantID: globalTenantID, Spec: spec})
	})
}

func runFleetPlacementGet(_ *cobra.Command, args []string) error {
	return withFleetPlacement(func(ctx context.Context, c fleetPlacementClient) (interface{}, error) {
		return c.GetFleetPlacementSpec(ctx, &orchestratorSDK.FleetPlacementSpecRequest{TenantID: globalTenantID, Name: args[0]})
	})
}

func runFleetPlacementList(_ *cobra.Command, _ []string) error {
	return withFleetPlacement(func(ctx context.Context, c fleetPlacementClient) (interface{}, error) {
		return c.ListFleetPlacementSpecs(ctx, &orchestratorSDK.ListFleetPlacementSpecsRequest{TenantID: globalTenantID})
	})
}

func runFleetPlacementWithdraw(_ *cobra.Command, args []string) error {
	return withFleetPlacement(func(ctx context.Context, c fleetPlacementClient) (interface{}, error) {
		return c.WithdrawFleetPlacementSpec(ctx, &orchestratorSDK.FleetPlacementSpecRequest{TenantID: globalTenantID, Name: args[0]})
	})
}

func runFleetPlacementDrift(_ *cobra.Command, args []string) error {
	return withFleetPlacement(func(ctx context.Context, c fleetPlacementClient) (interface{}, error) {
		return c.GetFleetPlacementDrift(ctx, &orchestratorSDK.FleetPlacementSpecRequest{TenantID: globalTenantID, Name: args[0]})
	})
}

func formatFleetPlacement(v interface{}, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(v)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	specRow := func(s *orchestratorSDK.FleetPlacementSpec) {
		target := strings.Join(s.Hosts, ",")
		if s.HostCount > 0 {
			target = fmt.Sprintf("%d hosts", s.HostCount)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%d\n", s.Name, s.Role, target, strings.Join(s.RequiredCapabilities, ","), s.Actuate, s.Generation)
	}
	const specHeader = "NAME\tROLE\tHOSTS\tCAPABILITIES\tACTUATE\tGENERATION\n"
	switch out := v.(type) {
	case *orchestratorSDK.FleetPlacementSpec:
		fmt.Fprint(tw, specHeader)
		specRow(out)
	case *orchestratorSDK.ListFleetPlacementSpecsResponse:
		fmt.Fprint(tw, specHeader)
		for _, s := range out.Specs {
			specRow(s)
		}
	case *orchestratorSDK.WithdrawFleetPlacementSpecResponse:
		fmt.Fprintf(tw, "Withdrawn:\t%s\n", out.Name)
	case *orchestratorSDK.FleetPlacementDrift:
		fmt.Fprintf(tw, "Spec:\t%s (role %s, generation %d)\n", out.Spec, out.Role, out.Generation)
		fmt.Fprintf(tw, "Disposition:\t%s\n", out.Disposition)
		fmt.Fprintf(tw, "Observed work:\t%d\n", out.ObservedWork)
		fmt.Fprintf(tw, "Admissible hosts:\t%s\n", strings.Join(out.AdmissibleHosts, ","))
		if len(out.Findings) > 0 {
			fmt.Fprint(tw, "\nKIND\tHOST\tWORK\tREMEDY\tDESTINATIONS\n")
			for _, f := range out.Findings {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", f.Kind, f.Host, f.WorkID, f.Remedy.Action, strings.Join(f.Remedy.Destinations, ","))
			}
		}
	default:
		return errors.New(errors.ErrInternal, "fleet placement", fmt.Sprintf("no table for %T", v))
	}
	return tw.Flush()
}
