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
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// parsedServeCommand builds `fleet serve`, parses args into it exactly as
// cobra would, and restores every serve* global afterwards.
func parsedServeCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	withServeGlobals(t)
	origConfig, origJobs := serveConfigFile, serveMaxJobs
	t.Cleanup(func() { serveConfigFile, serveMaxJobs = origConfig, origJobs })

	cmd := newFleetServeCommand()
	require.NoError(t, cmd.ParseFlags(args))
	return cmd
}

func writeServeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const serveConfigBody = `server_addr: https://file.example.com:8443
tenant_id: file-tenant
bind_addr: 0.0.0.0:9191
work_dir: /srv/agent/work
node_cert_file: /srv/agent/agent.crt
node_key_file: /srv/agent/agent.key
server_ca_file: /srv/agent/server-ca.crt
max_concurrent_jobs: 4
allowed_mount_roots: [/srv/sockets]
ollama_base_url: http://gpu-host.internal:11434
`

// TestResolveServeConfig_FileOverridesDefaults: a key the file names beats the
// flag's default, and a key it omits keeps that default.
func TestResolveServeConfig_FileOverridesDefaults(t *testing.T) {
	path := writeServeConfig(t, serveConfigBody)
	cmd := parsedServeCommand(t, "--config", path)

	cfg, err := resolveServeConfig(cmd.Flags())
	require.NoError(t, err)
	assert.Equal(t, "https://file.example.com:8443", cfg.ServerAddr)
	assert.Equal(t, "0.0.0.0:9191", cfg.BindAddr)
	assert.Equal(t, 4, cfg.MaxConcurrentJobs)
	assert.Equal(t, []string{"/srv/sockets"}, cfg.AllowedMountRoots)
	assert.Equal(t, "http://gpu-host.internal:11434", cfg.OllamaBaseURL)
	// Omitted by the file: the flag defaults stand.
	assert.Equal(t, orchestration.SandboxPolicyRequired, cfg.SandboxPolicy)
	assert.Equal(t, 30*time.Second, cfg.HeartbeatInterval)
	assert.Equal(t, []fleet.BackendKind{fleet.BackendACP, fleet.BackendOllama}, cfg.Backends)
	require.NoError(t, cfg.Validate())
}

// TestResolveServeConfig_ExplicitFlagsOverrideFile is the precedence rule the
// deploy relies on for a one-off override: a flag set on the command line
// beats the file, a flag left at its default does not.
func TestResolveServeConfig_ExplicitFlagsOverrideFile(t *testing.T) {
	path := writeServeConfig(t, serveConfigBody)
	cmd := parsedServeCommand(t,
		"--config", path,
		"--max-concurrent-jobs", "2",
		"--tenant", "flag-tenant",
		"--allowed-mount-root", "/srv/other",
		"--sandbox-policy", "preferred",
	)

	cfg, err := resolveServeConfig(cmd.Flags())
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.MaxConcurrentJobs)
	assert.Equal(t, "flag-tenant", cfg.TenantID)
	assert.Equal(t, []string{"/srv/other"}, cfg.AllowedMountRoots)
	assert.Equal(t, orchestration.SandboxPolicyPreferred, cfg.SandboxPolicy)
	// Not set on the command line: the file still wins over the default.
	assert.Equal(t, "0.0.0.0:9191", cfg.BindAddr)
	assert.Equal(t, "https://file.example.com:8443", cfg.ServerAddr)
}

// TestResolveServeConfig_SandboxHardeningPrecedence: the #1077 sandbox
// hardening settings follow the same rule as every other field — the file sets
// them, and an explicit --sandbox-cap-add / --sandbox-pids-limit overrides it.
func TestResolveServeConfig_SandboxHardeningPrecedence(t *testing.T) {
	path := writeServeConfig(t, serveConfigBody+"sandbox_cap_add: [CHOWN]\nsandbox_pids_limit: 256\nsandbox_ulimits: [\"memlock=1024\"]\n")

	fromFile := parsedServeCommand(t, "--config", path)
	cfg, err := resolveServeConfig(fromFile.Flags())
	require.NoError(t, err)
	assert.Equal(t, []string{"CHOWN"}, cfg.SandboxCapAdd)
	assert.Equal(t, int64(256), cfg.SandboxPidsLimit)
	assert.Equal(t, []string{"memlock=1024"}, cfg.SandboxUlimits)

	overridden := parsedServeCommand(t, "--config", path,
		"--sandbox-cap-add", "SETUID,SETGID", "--sandbox-pids-limit", "512", "--sandbox-ulimit", "memlock=2048")
	cfg, err = resolveServeConfig(overridden.Flags())
	require.NoError(t, err)
	assert.Equal(t, []string{"SETUID", "SETGID"}, cfg.SandboxCapAdd)
	assert.Equal(t, int64(512), cfg.SandboxPidsLimit)
	assert.Equal(t, []string{"memlock=2048"}, cfg.SandboxUlimits)
}

// TestResolveServeConfig_NoConfigIsFlagsOnly keeps the flag-only path exactly
// what it was, now carrying --max-concurrent-jobs.
func TestResolveServeConfig_NoConfigIsFlagsOnly(t *testing.T) {
	cmd := parsedServeCommand(t, "--server", "https://x:8443", "--tenant", "t", "--max-concurrent-jobs", "3")

	cfg, err := resolveServeConfig(cmd.Flags())
	require.NoError(t, err)
	assert.Equal(t, "https://x:8443", cfg.ServerAddr)
	assert.Equal(t, 3, cfg.MaxConcurrentJobs)
}

func TestResolveServeConfig_BadFileIsTyped(t *testing.T) {
	path := writeServeConfig(t, "not_a_setting: true\n")
	cmd := parsedServeCommand(t, "--config", path)

	_, err := resolveServeConfig(cmd.Flags())
	var fileErr *fleet.ConfigFileError
	require.True(t, errors.As(err, &fileErr), "got %v", err)
}

func TestResolveServeConfig_BadFlagValue(t *testing.T) {
	cmd := parsedServeCommand(t, "--sandbox-policy", "bogus")
	_, err := resolveServeConfig(cmd.Flags())
	require.Error(t, err)
}

// TestResolveServeConfig_AdvertisePrecedence: the advertised address follows
// the configuration convention — flag beats environment beats file beats the
// empty default — with or without a config file.
func TestResolveServeConfig_AdvertisePrecedence(t *testing.T) {
	path := writeServeConfig(t, serveConfigBody+"advertise_addr: file-host:9191\n")

	cfg, err := resolveServeConfig(parsedServeCommand(t, "--config", path).Flags())
	require.NoError(t, err)
	assert.Equal(t, "file-host:9191", cfg.AdvertiseAddr)

	t.Setenv(serveAdvertiseEnv, "env-host:9191")
	cfg, err = resolveServeConfig(parsedServeCommand(t, "--config", path).Flags())
	require.NoError(t, err)
	assert.Equal(t, "env-host:9191", cfg.AdvertiseAddr)

	cfg, err = resolveServeConfig(parsedServeCommand(t, "--server", "https://x:8443").Flags())
	require.NoError(t, err)
	assert.Equal(t, "env-host:9191", cfg.AdvertiseAddr, "the environment applies without a file too")

	cfg, err = resolveServeConfig(parsedServeCommand(t, "--config", path, "--advertise", "flag-host:9191").Flags())
	require.NoError(t, err)
	assert.Equal(t, "flag-host:9191", cfg.AdvertiseAddr)
}

// TestServeEnvFields_NameStringFields: an environment entry naming a field
// fleet.Config lacks, or one that is not a string, could never be applied.
func TestServeEnvFields_NameStringFields(t *testing.T) {
	cfgType := reflect.TypeOf(fleet.Config{})
	for name, field := range serveEnvFields {
		f, ok := cfgType.FieldByName(field)
		if !ok || f.Type.Kind() != reflect.String {
			t.Errorf("%s maps to %q, which is not a string field of fleet.Config", name, field)
		}
	}
}

// TestServeRequiredFlags_RelaxedOnlyWithConfig: a flag-only invocation still
// needs --server and friends; with --config the file may supply them.
func TestServeRequiredFlags_RelaxedOnlyWithConfig(t *testing.T) {
	flagOnly := parsedServeCommand(t)
	require.NoError(t, flagOnly.PreRunE(flagOnly, nil))
	require.Error(t, flagOnly.ValidateRequiredFlags())

	withFile := parsedServeCommand(t, "--config", writeServeConfig(t, serveConfigBody))
	require.NoError(t, withFile.PreRunE(withFile, nil))
	require.NoError(t, withFile.ValidateRequiredFlags())
}

// TestServeFlagFields_CoverEveryFlag: a flag absent from serveFlagFields would
// be silently ignored whenever --config is given, and a table entry naming a
// field fleet.Config does not have would panic at reflect time.
func TestServeFlagFields_CoverEveryFlag(t *testing.T) {
	cmd := parsedServeCommand(t)
	cfgType := reflect.TypeOf(fleet.Config{})
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "config" {
			return
		}
		field, ok := serveFlagFields[flag.Name]
		if !ok {
			t.Errorf("--%s has no entry in serveFlagFields; it would be ignored under --config", flag.Name)
			return
		}
		if _, ok := cfgType.FieldByName(field); !ok {
			t.Errorf("--%s maps to %q, which fleet.Config does not have", flag.Name, field)
		}
	})
	for name := range serveFlagFields {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("serveFlagFields names --%s, which `fleet serve` does not accept", name)
		}
	}
}
