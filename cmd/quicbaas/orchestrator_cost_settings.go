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
	"math"
	"os"
	"sort"
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
// quicbaas orchestrator cost-settings — electricity costing (go-quicbaas#1058)
// =============================================================================

const (
	electricityPriceFlag = "electricity-price"
	engineWattsFlag      = "engine-watts"
	clearCostFlag        = "clear"
)

// CostSettingsFlagError is a cost-settings flag the command cannot read.
type CostSettingsFlagError struct {
	Flag    string
	Value   string
	Message string
}

// Error implements the error interface.
func (e *CostSettingsFlagError) Error() string {
	return "invalid --" + e.Flag + " " + strconv.Quote(e.Value) + ": " + e.Message
}

// CostSettingsFlagsRequiredError is a set command that names nothing.
type CostSettingsFlagsRequiredError struct{}

// Error implements the error interface.
func (e *CostSettingsFlagsRequiredError) Error() string {
	return "cost-settings set: give --" + electricityPriceFlag + ", --" + engineWattsFlag + " (repeatable) or --" + clearCostFlag
}

// newOrchCostSettingsCommand creates the cost-settings command group.
func newOrchCostSettingsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost-settings",
		Short: "Show or set how the tenant's local inference is costed",
		Long: `Show or set the tenant's runtime cost settings: the electricity rate and each
local engine's device draw. A local turn costs

  power_watts × turn seconds / 3.6e6 kWh × electricity price per kWh

A setting here beats the tenant's configured override, which beats the
deployment's; the rate defaults to $0.12/kWh. A model priced per token in the
price table keeps its per-token price. The set REPLACES the settings: give
every value to keep, or --clear to return to the configuration.

Examples:
  quicbaas orchestrator cost-settings get
  quicbaas orchestrator cost-settings set --electricity-price 0.15 --engine-watts openvino=12 --engine-watts ollama=250
  quicbaas orchestrator cost-settings set --clear`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Show the tenant's cost settings and the rates in force",
		Args:  cobra.NoArgs,
		RunE:  runOrchCostSettingsGet,
	})
	set := &cobra.Command{
		Use:   "set",
		Short: "Replace the tenant's cost settings",
		Args:  cobra.NoArgs,
		RunE:  runOrchCostSettingsSet,
	}
	set.Flags().String(electricityPriceFlag, "", "Electricity price in US dollars per kWh")
	set.Flags().StringArray(engineWattsFlag, nil, "An engine's device draw while serving, ENGINE=WATTS (repeatable); 0 switches electricity costing off for the engine")
	set.Flags().Bool(clearCostFlag, false, "Remove every setting and return to the configuration")
	cmd.AddCommand(set)
	return cmd
}

// runOrchCostSettingsGet executes cost-settings get.
func runOrchCostSettingsGet(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()
	ctx, cancel := orchContext()
	defer cancel()

	settings, err := client.GetCostSettings(ctx, &orchestratorSDK.GetCostSettingsRequest{TenantID: globalTenantID})
	if err != nil {
		return formatError(err)
	}
	return formatCostSettings(settings, orchOutputFmt, os.Stdout)
}

// runOrchCostSettingsSet executes cost-settings set.
func runOrchCostSettingsSet(cmd *cobra.Command, args []string) error {
	req, err := costSettingsFromFlags(cmd)
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

	settings, err := client.SetCostSettings(ctx, req)
	if err != nil {
		return formatError(err)
	}
	return formatCostSettings(settings, orchOutputFmt, os.Stdout)
}

// costSettingsFromFlags reads the set command's flags into a request. --clear
// is the empty request and combines with nothing.
func costSettingsFromFlags(cmd *cobra.Command) (*orchestratorSDK.SetCostSettingsRequest, error) {
	price, err := cmd.Flags().GetString(electricityPriceFlag)
	if err != nil {
		return nil, err
	}
	engines, err := cmd.Flags().GetStringArray(engineWattsFlag)
	if err != nil {
		return nil, err
	}
	clearAll, err := cmd.Flags().GetBool(clearCostFlag)
	if err != nil {
		return nil, err
	}
	req := &orchestratorSDK.SetCostSettingsRequest{TenantID: globalTenantID}
	if clearAll {
		if price != "" || len(engines) > 0 {
			return nil, &CostSettingsFlagError{Flag: clearCostFlag, Value: "true", Message: "cannot be combined with other settings"}
		}
		return req, nil
	}
	if price == "" && len(engines) == 0 {
		return nil, &CostSettingsFlagsRequiredError{}
	}
	if price != "" {
		rate, ok := parseNonNegative(price)
		if !ok {
			return nil, &CostSettingsFlagError{Flag: electricityPriceFlag, Value: price, Message: "must be a finite, non-negative dollar amount"}
		}
		req.ElectricityPricePerKWh = &rate
	}
	for _, value := range engines {
		engine, rawWatts, found := strings.Cut(value, "=")
		watts, ok := parseNonNegative(rawWatts)
		if !found || strings.TrimSpace(engine) == "" || !ok {
			return nil, &CostSettingsFlagError{Flag: engineWattsFlag, Value: value, Message: "must be ENGINE=WATTS with a finite, non-negative wattage"}
		}
		if req.EnginePowerWatts == nil {
			req.EnginePowerWatts = make(map[string]float64, len(engines))
		}
		req.EnginePowerWatts[strings.TrimSpace(engine)] = watts
	}
	return req, nil
}

// parseNonNegative parses a finite, non-negative number.
func parseNonNegative(value string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, false
	}
	return f, true
}

// formatCostSettings renders a tenant's settings and the rates in force.
func formatCostSettings(settings *orchestratorSDK.CostSettings, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(settings)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(settings)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Tenant:\t%s\n", settings.Settings.TenantID)
		fmt.Fprintf(tw, "Electricity:\t$%s/kWh (%s)\n",
			strconv.FormatFloat(settings.Electricity.PricePerKWh, 'f', -1, 64), settings.Electricity.Source)
		if settings.Settings.UpdatedAt != nil {
			fmt.Fprintf(tw, "Updated:\t%s\n", settings.Settings.UpdatedAt.Format(time.RFC3339))
		}
		if len(settings.EngineDraws) > 0 {
			fmt.Fprintf(tw, "\nENGINE\tWATTS\tSET HERE\n")
			engines := make([]string, 0, len(settings.EngineDraws))
			for engine := range settings.EngineDraws {
				engines = append(engines, engine)
			}
			sort.Strings(engines)
			for _, engine := range engines {
				_, setHere := settings.Settings.EnginePowerWatts[engine]
				fmt.Fprintf(tw, "%s\t%s\t%t\n", engine,
					strconv.FormatFloat(settings.EngineDraws[engine], 'f', -1, 64), setHere)
			}
		}
		return tw.Flush()
	}
}
