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
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// The scheduler's workload placement commands. They go through the SDK's
// Async().Scheduler() on the client the global --transport selects, so the
// CLI reaches placement exactly as an SDK consumer does, on every transport.

// placement command flags.
var (
	placementCapacity []string
	placementLabels   []string
	placementTTL      int64
	placementGroup    string
	placementResource []string
	placementStrategy string
	placementSpec     string
	placementNodeID   string
)

// placementScheduler returns the SDK scheduler client.
func placementScheduler() (scheduler.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Async().Scheduler(), nil
}

// parseInt64Pairs parses repeated key=value flags with integer values.
func parseInt64Pairs(flag string, pairs []string) (map[string]int64, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]int64, len(pairs))
	for _, pair := range pairs {
		key, raw, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, errors.Newf(errors.ErrInvalidInput, "scheduler", "--%s %q must be name=amount", flag, pair)
		}
		amount, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, errors.Newf(errors.ErrInvalidInput, "scheduler", "--%s %q: amount must be an integer", flag, pair)
		}
		out[key] = amount
	}
	return out, nil
}

// parseStringPairs parses repeated key=value flags.
func parseStringPairs(flag string, pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, errors.Newf(errors.ErrInvalidInput, "scheduler", "--%s %q must be key=value", flag, pair)
		}
		out[key] = value
	}
	return out, nil
}

// writePlacementOutput renders v as JSON, YAML, or a table via table.
func writePlacementOutput(w io.Writer, v interface{}, table func(*tabwriter.Writer)) error {
	format, err := cli.ParseOutputFormat(schedulerOutputFmt)
	if err != nil {
		return err
	}
	switch format {
	case cli.OutputFormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case cli.OutputFormatYAML:
		// The placement types carry JSON tags only; going through their
		// JSON form gives YAML the same snake_case keys as --output json.
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var generic interface{}
		if err := json.Unmarshal(raw, &generic); err != nil {
			return err
		}
		return yaml.NewEncoder(w).Encode(generic)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		table(tw)
		return tw.Flush()
	}
}

// formatAmounts renders a resource map as "a=1,b=2" in key order.
func formatAmounts(m map[string]int64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.FormatInt(m[k], 10))
	}
	return strings.Join(parts, ",")
}

// writeNodes renders nodes.
func writeNodes(w io.Writer, v interface{}, nodes ...*quicbaas.PlacementNodeInfo) error {
	return writePlacementOutput(w, v, func(tw *tabwriter.Writer) {
		fmt.Fprintln(tw, "NODE\tCAPACITY\tALLOCATED\tPLACEMENTS\tCORDONED\tDRAINING")
		for _, n := range nodes {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%t\t%t\n", n.Facts.NodeID, formatAmounts(n.Facts.Capacity),
				formatAmounts(n.Allocated), n.Placements, n.Status.Unschedulable, n.Status.Draining)
		}
	})
}

// writePlacements renders placements.
func writePlacements(w io.Writer, v interface{}, placements ...*quicbaas.Placement) error {
	return writePlacementOutput(w, v, func(tw *tabwriter.Writer) {
		fmt.Fprintln(tw, "WORKLOAD\tGROUP\tNODE\tRESOURCES\tORPHANED")
		for _, p := range placements {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\n", p.WorkloadID, p.Group, p.NodeID, formatAmounts(p.Resources), p.Orphaned)
		}
	})
}

// newSchedulerNodeCommand creates "scheduler node".
func newSchedulerNodeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage the node facts workload placement decides over",
	}

	register := &cobra.Command{
		Use:   "register <node-id>",
		Short: "Register or refresh a node's facts under a lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capacity, err := parseInt64Pairs("capacity", placementCapacity)
			if err != nil {
				return err
			}
			labels, err := parseStringPairs("label", placementLabels)
			if err != nil {
				return err
			}
			client, err := placementScheduler()
			if err != nil {
				return err
			}
			node, err := client.RegisterNode(cmd.Context(), &scheduler.RegisterNodeRequest{
				Facts:      scheduler.NodeFacts{NodeID: args[0], Capacity: capacity, Labels: labels},
				TTLSeconds: placementTTL,
			})
			if err != nil {
				return err
			}
			return writeNodes(cmd.OutOrStdout(), node, node)
		},
	}
	register.Flags().StringArrayVar(&placementCapacity, "capacity", nil, "Resource capacity name=amount (repeatable)")
	register.Flags().StringArrayVar(&placementLabels, "label", nil, "Node label key=value (repeatable)")
	register.Flags().Int64Var(&placementTTL, "ttl", 30, "Lease time-to-live in seconds")

	cmd.AddCommand(register,
		nodeIDCommand("renew", "Heartbeat a node's lease", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			if err := c.RenewNode(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Node %s renewed\n", id)
			return nil
		}),
		nodeIDCommand("deregister", "Revoke a node's lease and clear its status", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			if err := c.DeregisterNode(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Node %s deregistered\n", id)
			return nil
		}),
		nodeIDCommand("get", "Show a live node", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			node, err := c.GetNode(cmd.Context(), id)
			if err != nil {
				return err
			}
			return writeNodes(cmd.OutOrStdout(), node, node)
		}),
		nodeIDCommand("cordon", "Exclude a node from new placements", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			node, err := c.CordonNode(cmd.Context(), id)
			if err != nil {
				return err
			}
			return writeNodes(cmd.OutOrStdout(), node, node)
		}),
		nodeIDCommand("uncordon", "Return a node to service", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			node, err := c.UncordonNode(cmd.Context(), id)
			if err != nil {
				return err
			}
			return writeNodes(cmd.OutOrStdout(), node, node)
		}),
		nodeIDCommand("drain", "Drain a node and list the placements to move", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			moved, err := c.DrainNode(cmd.Context(), id)
			if err != nil {
				return err
			}
			return writePlacements(cmd.OutOrStdout(), moved, moved...)
		}),
		&cobra.Command{
			Use:   "list",
			Short: "List live nodes",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				client, err := placementScheduler()
				if err != nil {
					return err
				}
				nodes, err := client.ListNodes(cmd.Context())
				if err != nil {
					return err
				}
				return writeNodes(cmd.OutOrStdout(), nodes, nodes...)
			},
		},
	)
	return cmd
}

// nodeIDCommand builds a command taking one ID argument.
func nodeIDCommand(use, short string, run func(*cobra.Command, scheduler.Client, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := placementScheduler()
			if err != nil {
				return err
			}
			return run(cmd, client, args[0])
		},
	}
}

// buildPlacementRequest assembles a request from --spec and the flags; a
// flag overrides the same field in --spec.
func buildPlacementRequest(workloadID string) (*scheduler.PlacementRequest, error) {
	req := &scheduler.PlacementRequest{}
	if placementSpec != "" {
		if err := json.Unmarshal([]byte(placementSpec), req); err != nil {
			return nil, errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.place", "--spec is not a JSON placement request")
		}
	}
	req.WorkloadID = workloadID
	if placementGroup != "" {
		req.Group = placementGroup
	}
	if placementStrategy != "" {
		req.Strategy = scheduler.Strategy(placementStrategy)
	}
	resources, err := parseInt64Pairs("resource", placementResource)
	if err != nil {
		return nil, err
	}
	if resources != nil {
		req.Resources = resources
	}
	labels, err := parseStringPairs("label", placementLabels)
	if err != nil {
		return nil, err
	}
	if labels != nil {
		req.Labels = labels
	}
	return req, nil
}

// newSchedulerPlacementCommand creates "scheduler placement".
func newSchedulerPlacementCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "placement",
		Short: "Place workloads on nodes and manage the decisions",
	}

	place := &cobra.Command{
		Use:   "place <workload-id>",
		Short: "Place a workload on a node",
		Long: `Place a workload: live nodes are filtered by label constraints, free
capacity, affinity, anti-affinity and spread, then scored by bin-packing or
spreading. Constraints and affinity terms are given as a JSON request in
--spec; --group, --resource, --label and --strategy override it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildPlacementRequest(args[0])
			if err != nil {
				return err
			}
			client, err := placementScheduler()
			if err != nil {
				return err
			}
			res, err := client.Place(cmd.Context(), req)
			if err != nil {
				return err
			}
			return writePlacements(cmd.OutOrStdout(), res, res.Placement)
		},
	}
	place.Flags().StringVar(&placementGroup, "group", "", "Workload group")
	place.Flags().StringArrayVar(&placementResource, "resource", nil, "Requested resource name=amount (repeatable)")
	place.Flags().StringArrayVar(&placementLabels, "label", nil, "Placement label key=value (repeatable)")
	place.Flags().StringVar(&placementStrategy, "strategy", "", "binpack (default) or spread")
	place.Flags().StringVar(&placementSpec, "spec", "", "JSON placement request (constraints, affinity, anti_affinity, spread)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List placements",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := placementScheduler()
			if err != nil {
				return err
			}
			out, err := client.ListPlacements(cmd.Context(), &scheduler.PlacementFilter{NodeID: placementNodeID, Group: placementGroup})
			if err != nil {
				return err
			}
			return writePlacements(cmd.OutOrStdout(), out, out...)
		},
	}
	list.Flags().StringVar(&placementNodeID, "node", "", "Only placements on this node")
	list.Flags().StringVar(&placementGroup, "group", "", "Only placements of this group")

	cmd.AddCommand(place, list,
		nodeIDCommand("get", "Show a workload's placement", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			p, err := c.GetPlacement(cmd.Context(), id)
			if err != nil {
				return err
			}
			return writePlacements(cmd.OutOrStdout(), p, p)
		}),
		nodeIDCommand("release", "Release a workload's placement", func(cmd *cobra.Command, c scheduler.Client, id string) error {
			if err := c.Release(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workload %s released\n", id)
			return nil
		}),
	)
	return cmd
}
