// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Tracing defaults-layer values. Every other layer (config file, environment,
// flag) is read through viper by tracingSettingsFrom.
const (
	defaultTracingExporter    = "stdout"
	defaultOTLPEndpoint       = "localhost:4317"
	defaultTracingServiceName = "quicbaas"
)

// Tracing viper keys.
const (
	tracingEnabledKey     = "tracing.enabled"
	tracingExporterKey    = "tracing.exporter"
	tracingEndpointKey    = "tracing.otlp_endpoint"
	tracingServiceNameKey = "tracing.service_name"
)

// flagBinding ties one CLI setting's viper key to its flag and its
// environment variable.
type flagBinding struct {
	key  string
	flag string
	env  string
}

// tracingBindings lists every tracing setting. The environment names are the
// ones the CLI has always documented, QUICBAAS_OTLP_ENDPOINT included.
var tracingBindings = []flagBinding{
	{key: tracingEnabledKey, flag: "tracing-enabled", env: "QUICBAAS_TRACING_ENABLED"},
	{key: tracingExporterKey, flag: "tracing-exporter", env: "QUICBAAS_TRACING_EXPORTER"},
	{key: tracingEndpointKey, flag: "otlp-endpoint", env: "QUICBAAS_OTLP_ENDPOINT"},
	{key: tracingServiceNameKey, flag: "tracing-service-name", env: "QUICBAAS_TRACING_SERVICE_NAME"},
}

// tracingSettings is the resolved tracing configuration.
type tracingSettings struct {
	Enabled      bool
	Exporter     string
	OTLPEndpoint string
	ServiceName  string
}

// bindTracingFlags registers the tracing flags on flags and binds each one,
// with its environment variable, to v. Precedence is viper's: flag, then
// environment, then config file, then the flag default (the defaults layer).
func bindTracingFlags(v *viper.Viper, flags *pflag.FlagSet) error {
	flags.Bool("tracing-enabled", false, "Enable OpenTelemetry tracing")
	flags.String("tracing-exporter", defaultTracingExporter, "Tracing exporter (stdout|otlpgrpc)")
	flags.String("otlp-endpoint", defaultOTLPEndpoint, "OTLP gRPC collector endpoint (host:port)")
	flags.String("tracing-service-name", defaultTracingServiceName, "Service name reported in traces")

	return bindFlagKeys(v, flags, tracingBindings...)
}

// bindFlagKeys binds each viper key to its flag and its environment
// variable, giving the precedence flag > env > config file > flag default.
func bindFlagKeys(v *viper.Viper, flags *pflag.FlagSet, bindings ...flagBinding) error {
	for _, b := range bindings {
		if err := v.BindPFlag(b.key, flags.Lookup(b.flag)); err != nil {
			return err
		}
		if err := v.BindEnv(b.key, b.env); err != nil {
			return err
		}
	}
	return nil
}

// tracingSettingsFrom resolves the tracing settings from v.
func tracingSettingsFrom(v *viper.Viper) tracingSettings {
	return tracingSettings{
		Enabled:      v.GetBool(tracingEnabledKey),
		Exporter:     v.GetString(tracingExporterKey),
		OTLPEndpoint: v.GetString(tracingEndpointKey),
		ServiceName:  v.GetString(tracingServiceNameKey),
	}
}
