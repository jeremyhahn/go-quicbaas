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
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTracingViper returns a fresh viper and flag set with the tracing
// settings bound, optionally loading configYAML as the config-file layer.
func newTracingViper(t *testing.T, configYAML string) (*viper.Viper, *pflag.FlagSet) {
	t.Helper()
	v := viper.New()
	flags := pflag.NewFlagSet("tracing", pflag.ContinueOnError)
	require.NoError(t, bindTracingFlags(v, flags))
	if configYAML != "" {
		v.SetConfigType("yaml")
		require.NoError(t, v.ReadConfig(strings.NewReader(configYAML)))
	}
	return v, flags
}

func clearTracingEnv(t *testing.T) {
	t.Helper()
	for _, b := range tracingBindings {
		t.Setenv(b.env, "")
	}
}

func TestTracingSettings_DefaultsLayer(t *testing.T) {
	clearTracingEnv(t)
	v, _ := newTracingViper(t, "")

	got := tracingSettingsFrom(v)
	assert.Equal(t, tracingSettings{
		Enabled:      false,
		Exporter:     "stdout",
		OTLPEndpoint: "localhost:4317",
		ServiceName:  "quicbaas",
	}, got)
}

func TestTracingSettings_OTLPEndpointPrecedence(t *testing.T) {
	const (
		fileEndpoint = "collector-file.tracing.test:4317"
		envEndpoint  = "collector-env.tracing.test:4317"
		flagEndpoint = "collector-flag.tracing.test:4317"
	)
	fileYAML := "tracing:\n  otlp_endpoint: " + fileEndpoint + "\n"

	t.Run("config file overrides default", func(t *testing.T) {
		clearTracingEnv(t)
		v, _ := newTracingViper(t, fileYAML)
		assert.Equal(t, fileEndpoint, tracingSettingsFrom(v).OTLPEndpoint)
	})

	t.Run("env without a file", func(t *testing.T) {
		clearTracingEnv(t)
		t.Setenv("QUICBAAS_OTLP_ENDPOINT", envEndpoint)
		v, _ := newTracingViper(t, "")
		assert.Equal(t, envEndpoint, tracingSettingsFrom(v).OTLPEndpoint)
	})

	t.Run("env beats file", func(t *testing.T) {
		clearTracingEnv(t)
		t.Setenv("QUICBAAS_OTLP_ENDPOINT", envEndpoint)
		v, _ := newTracingViper(t, fileYAML)
		assert.Equal(t, envEndpoint, tracingSettingsFrom(v).OTLPEndpoint)
	})

	t.Run("flag beats env and file", func(t *testing.T) {
		clearTracingEnv(t)
		t.Setenv("QUICBAAS_OTLP_ENDPOINT", envEndpoint)
		v, flags := newTracingViper(t, fileYAML)
		require.NoError(t, flags.Set("otlp-endpoint", flagEndpoint))
		assert.Equal(t, flagEndpoint, tracingSettingsFrom(v).OTLPEndpoint)
	})
}

func TestTracingSettings_SiblingSettings(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		clearTracingEnv(t)
		t.Setenv("QUICBAAS_TRACING_ENABLED", "1")
		t.Setenv("QUICBAAS_TRACING_EXPORTER", tracingExporterOTLPGRPCName)
		t.Setenv("QUICBAAS_TRACING_SERVICE_NAME", "svc-env")
		v, _ := newTracingViper(t, "")

		got := tracingSettingsFrom(v)
		assert.True(t, got.Enabled)
		assert.Equal(t, tracingExporterOTLPGRPCName, got.Exporter)
		assert.Equal(t, "svc-env", got.ServiceName)
	})

	t.Run("config file", func(t *testing.T) {
		clearTracingEnv(t)
		v, _ := newTracingViper(t, "tracing:\n  enabled: true\n  exporter: otlpgrpc\n  service_name: svc-file\n")

		got := tracingSettingsFrom(v)
		assert.True(t, got.Enabled)
		assert.Equal(t, tracingExporterOTLPGRPCName, got.Exporter)
		assert.Equal(t, "svc-file", got.ServiceName)
	})

	t.Run("flags", func(t *testing.T) {
		clearTracingEnv(t)
		v, flags := newTracingViper(t, "")
		require.NoError(t, flags.Set("tracing-enabled", "true"))
		require.NoError(t, flags.Set("tracing-exporter", tracingExporterOTLPGRPCName))
		require.NoError(t, flags.Set("tracing-service-name", "svc-flag"))

		got := tracingSettingsFrom(v)
		assert.True(t, got.Enabled)
		assert.Equal(t, tracingExporterOTLPGRPCName, got.Exporter)
		assert.Equal(t, "svc-flag", got.ServiceName)
	})
}

func TestRootCommand_RegistersTracingFlags(t *testing.T) {
	for _, b := range tracingBindings {
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup(b.flag), "root command must expose --%s", b.flag)
	}
	assert.Equal(t, "localhost:4317", rootCmd.PersistentFlags().Lookup("otlp-endpoint").DefValue)
}

// tracingExporterOTLPGRPCName mirrors the otel-tagged exporter selector so the
// untagged tests do not depend on the otel build tag.
const tracingExporterOTLPGRPCName = "otlpgrpc"
