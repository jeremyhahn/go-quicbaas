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
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	registrySDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
)

// =============================================================================
// `quicbaas registry` — the service registry.
//
// Every verb routes through the active SDK transport (rest / grpc / quic /
// mcp / unix / embedded) and is scoped to the authenticated tenant.
// =============================================================================

// RegistryClientFactory creates service registry clients.
type RegistryClientFactory func() (registrySDK.Client, error)

// registryClientFactory creates registry clients. Defaults to
// createSDKRegistryClient.
var registryClientFactory RegistryClientFactory = createSDKRegistryClient

// createSDKRegistryClient returns the active SDK client's registry.
func createSDKRegistryClient() (registrySDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Registry(), nil
}

var (
	registryOutputFmt    string
	registryService      string
	registryInstanceID   string
	registryAddress      string
	registryTTL          int64
	registryMetadata     []string
	registryFromRevision int64
	registryMaxEvents    int
)

// InvalidRegistryMetadataError reports a --meta value that is not key=value.
type InvalidRegistryMetadataError struct {
	// Value is the offending flag value.
	Value string
}

// Error returns a human-readable description.
func (e *InvalidRegistryMetadataError) Error() string {
	return "registry: --meta " + strconv.Quote(e.Value) + " must be key=value"
}

// InvalidLeaseIDError reports a lease argument that is not a positive integer.
type InvalidLeaseIDError struct {
	// Value is the offending argument.
	Value string
}

// Error returns a human-readable description.
func (e *InvalidLeaseIDError) Error() string {
	return "registry: lease ID " + strconv.Quote(e.Value) + " must be a positive integer"
}

func newRegistryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Service registry: lease-based service-instance endpoints",
		Long: `Register service instances under a lease, resolve a service's instances,
and watch its changes. An instance stays listed while its lease is renewed and
disappears when the lease lapses. Every command is scoped to your tenant.

Examples:
  quicbaas registry register --service api --id node-a --address 10.0.4.7:8443 --ttl 15 --meta zone=z1
  quicbaas registry renew 42
  quicbaas registry resolve api
  quicbaas registry watch api
  quicbaas registry deregister 42`,
	}
	cmd.PersistentFlags().StringVar(&registryOutputFmt, "output", "table", "Output format (json, yaml, table)")
	cmd.AddCommand(newRegistryRegisterCommand())
	cmd.AddCommand(newRegistryRenewCommand())
	cmd.AddCommand(newRegistryDeregisterCommand())
	cmd.AddCommand(newRegistryResolveCommand())
	cmd.AddCommand(newRegistryWatchCommand())
	return cmd
}

func newRegistryRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a service instance under a new lease",
		Args:  cobra.NoArgs,
		RunE:  runRegistryRegister,
	}
	cmd.Flags().StringVar(&registryService, "service", "", "Service name (required)")
	cmd.Flags().StringVar(&registryInstanceID, "id", "", "Instance ID, unique within the service (required)")
	cmd.Flags().StringVar(&registryAddress, "address", "", "Instance address, host:port (required)")
	cmd.Flags().Int64Var(&registryTTL, "ttl", 15, "Lease TTL in seconds (1 to 86400)")
	cmd.Flags().StringArrayVar(&registryMetadata, "meta", nil, "Metadata as key=value (repeatable)")
	return cmd
}

func runRegistryRegister(cmd *cobra.Command, _ []string) error {
	metadata, err := parseRegistryMetadata(registryMetadata)
	if err != nil {
		return formatError(err)
	}
	client, err := registryClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	lease, err := client.Register(ctx, &registrySDK.RegisterRequest{
		Instance:   registrySDK.ServiceInstance{ID: registryInstanceID, Service: registryService, Address: registryAddress, Metadata: metadata},
		TTLSeconds: registryTTL,
	})
	if err != nil {
		return formatError(err)
	}
	return writeRegistryOutput(cmd.OutOrStdout(), lease, func(tw io.Writer) {
		fmt.Fprintf(tw, "Lease ID:\t%d\n", lease.LeaseID)
		fmt.Fprintf(tw, "TTL:\t%ds\n", lease.TTLSeconds)
	})
}

func newRegistryRenewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "renew <leaseID>",
		Short: "Renew the lease backing one of your registrations",
		Args:  cobra.ExactArgs(1),
		RunE:  runRegistryRenew,
	}
}

func runRegistryRenew(cmd *cobra.Command, args []string) error {
	leaseID, err := parseLeaseID(args[0])
	if err != nil {
		return formatError(err)
	}
	client, err := registryClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	renewed, err := client.Renew(ctx, leaseID)
	if err != nil {
		return formatError(err)
	}
	return writeRegistryOutput(cmd.OutOrStdout(), renewed, func(tw io.Writer) {
		fmt.Fprintf(tw, "Lease ID:\t%d\n", renewed.LeaseID)
		fmt.Fprintf(tw, "TTL:\t%ds\n", renewed.TTLSeconds)
	})
}

func newRegistryDeregisterCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "deregister <leaseID>",
		Short: "Deregister an instance by revoking its lease",
		Args:  cobra.ExactArgs(1),
		RunE:  runRegistryDeregister,
	}
}

func runRegistryDeregister(cmd *cobra.Command, args []string) error {
	leaseID, err := parseLeaseID(args[0])
	if err != nil {
		return formatError(err)
	}
	client, err := registryClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	if err := client.Deregister(ctx, leaseID); err != nil {
		return formatError(err)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deregistered lease %d\n", leaseID)
	return err
}

func newRegistryResolveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <service>",
		Short: "List a service's registered instances",
		Args:  cobra.ExactArgs(1),
		RunE:  runRegistryResolve,
	}
}

func runRegistryResolve(cmd *cobra.Command, args []string) error {
	client, err := registryClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	resolved, err := client.Resolve(ctx, args[0])
	if err != nil {
		return formatError(err)
	}
	return writeRegistryOutput(cmd.OutOrStdout(), resolved, func(tw io.Writer) {
		fmt.Fprintf(tw, "ID\tADDRESS\tMETADATA\n")
		for _, inst := range resolved.Instances {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", inst.ID, inst.Address, formatRegistryMetadata(inst.Metadata))
		}
		fmt.Fprintf(tw, "\nRevision:\t%d\n", resolved.Revision)
	})
}

func newRegistryWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch <service>",
		Short: "Stream a service's registration changes until interrupted (or --max-events)",
		Args:  cobra.ExactArgs(1),
		RunE:  runRegistryWatch,
	}
	cmd.Flags().Int64Var(&registryFromRevision, "from-revision", 0, "First revision to deliver (0: changes from now)")
	cmd.Flags().IntVar(&registryMaxEvents, "max-events", 0, "Exit after this many changes (0: run until interrupted)")
	return cmd
}

func runRegistryWatch(cmd *cobra.Command, args []string) error {
	client, err := registryClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	events, err := client.Watch(ctx, &registrySDK.WatchRequest{Service: args[0], FromRevision: registryFromRevision})
	if err != nil {
		return formatError(err)
	}
	delivered := 0
	for ev := range events {
		if ev.Err != nil {
			return formatError(ev.Err)
		}
		if err := writeRegistryEvent(cmd.OutOrStdout(), ev); err != nil {
			return err
		}
		delivered++
		if registryMaxEvents > 0 && delivered >= registryMaxEvents {
			return nil
		}
	}
	return nil
}

// writeRegistryEvent prints one change: a JSON or YAML document per event,
// or one table row.
func writeRegistryEvent(w io.Writer, ev registrySDK.Event) error {
	format, err := cli.ParseOutputFormat(registryOutputFmt)
	if err != nil {
		return err
	}
	switch format {
	case cli.OutputFormatJSON:
		return json.NewEncoder(w).Encode(ev)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(ev)
	default:
		_, err := fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", ev.Revision, ev.Type, ev.Instance.ID, ev.Instance.Address)
		return err
	}
}

// writeRegistryOutput renders a result in the chosen format; table renders
// through the supplied row writer.
func writeRegistryOutput(w io.Writer, v interface{}, table func(io.Writer)) error {
	format, err := cli.ParseOutputFormat(registryOutputFmt)
	if err != nil {
		return err
	}
	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(v)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		table(tw)
		return tw.Flush()
	}
}

// parseRegistryMetadata turns repeated key=value flags into a map.
func parseRegistryMetadata(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || key == "" {
			return nil, &InvalidRegistryMetadataError{Value: value}
		}
		out[key] = val
	}
	return out, nil
}

// formatRegistryMetadata renders metadata as sorted key=value pairs.
func formatRegistryMetadata(metadata map[string]string) string {
	pairs := make([]string, 0, len(metadata))
	for key, value := range metadata {
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// parseLeaseID parses a positive lease ID argument.
func parseLeaseID(arg string) (int64, error) {
	leaseID, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || leaseID <= 0 {
		return 0, &InvalidLeaseIDError{Value: arg}
	}
	return leaseID, nil
}
