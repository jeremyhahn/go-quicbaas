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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const nodeSpecBody = `host: agent-01.internal
ssh_user: agent
ssh_key_name: fleet-deploy
server: https://quicbaas.example.com:8443
tenant: spec-tenant
node_bind: 0.0.0.0:9191
backends: [acp, ollama]
binary: bin/from-spec
data_dir: state
projects_root: /srv/projects
wait_online: 30s
node:
  max_concurrent_jobs: 4
`

func writeNodeSpec(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-01.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// underRoot attaches cmd to a root carrying the global --tenant flag, as the
// real CLI does, parses args, and restores every global the parse touched.
func underRoot(t *testing.T, cmd *cobra.Command, args ...string) *cobra.Command {
	t.Helper()
	resetDeployFlags(t)
	origTenant := globalTenantID
	t.Cleanup(func() { globalTenantID = origTenant })

	root := &cobra.Command{Use: "quicbaas"}
	root.PersistentFlags().StringVar(&globalTenantID, "tenant", "system", "")
	root.AddCommand(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	return cmd
}

// TestResolveDeploySpec_SpecOverridesFlagDefaults: every value the spec
// declares beats the flag's default.
func TestResolveDeploySpec_SpecOverridesFlagDefaults(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, nodeSpecBody))

	spec, err := resolveDeploySpec(cmd.Flags())
	require.NoError(t, err)
	assert.Equal(t, "agent-01.internal", spec.Host)
	assert.Equal(t, "agent", spec.SSHUser)
	assert.Equal(t, "fleet-deploy", spec.SSHKeyName)
	assert.Equal(t, "spec-tenant", spec.Tenant)
	assert.Equal(t, "0.0.0.0:9191", spec.NodeBind)
	assert.Equal(t, []string{"acp", "ollama"}, spec.Backends)
	assert.Equal(t, "bin/from-spec", spec.Binary)
	assert.Equal(t, 30*time.Second, spec.WaitOnline)
	assert.Equal(t, 4, spec.Node.MaxConcurrentJobs)
	require.NoError(t, spec.ValidateDaemonConfig())
}

// TestResolveDeploySpec_ExplicitFlagsOverrideSpec is the precedence rule:
// a flag the operator set beats the spec; one left at its default does not.
func TestResolveDeploySpec_ExplicitFlagsOverrideSpec(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand(),
		"--spec", writeNodeSpec(t, nodeSpecBody),
		"--ssh", "root@agent-02.internal:2222",
		"--tenant", "flag-tenant",
		"--max-concurrent-jobs", "2",
		"--backend", "acp",
		"--wait-online", "0",
		"--systemd", "no",
		"--projects-root", "/srv/other",
		"--ssh-insecure-host-key",
	)

	spec, err := resolveDeploySpec(cmd.Flags())
	require.NoError(t, err)
	assert.Equal(t, "agent-02.internal:2222", spec.Host)
	assert.Equal(t, "root", spec.SSHUser)
	assert.Equal(t, "flag-tenant", spec.Tenant)
	assert.Equal(t, 2, spec.Node.MaxConcurrentJobs)
	assert.Equal(t, []string{"acp"}, spec.Backends)
	assert.Equal(t, time.Duration(0), spec.WaitOnline)
	require.NotNil(t, spec.Systemd)
	assert.False(t, *spec.Systemd)
	assert.Equal(t, "/srv/other", spec.ProjectsRoot)
	assert.True(t, spec.InsecureHostKey)
	// Not set on the command line: the spec stands.
	assert.Equal(t, "0.0.0.0:9191", spec.NodeBind)
	assert.Equal(t, "bin/from-spec", spec.Binary)
	assert.Equal(t, "https://quicbaas.example.com:8443", spec.Server)
}

func TestResolveDeploySpec_Errors(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, "hots: typo\n"))
	var fileErr *deploy.SpecFileError
	_, err := resolveDeploySpec(cmd.Flags())
	require.True(t, errors.As(err, &fileErr), "got %v", err)

	cmd = underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, nodeSpecBody), "--ssh", "no-at-sign")
	_, err = resolveDeploySpec(cmd.Flags())
	var invalid *deploy.InvalidSpecError
	require.True(t, errors.As(err, &invalid), "got %v", err)

	cmd = underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, nodeSpecBody), "--systemd", "banana")
	_, err = resolveDeploySpec(cmd.Flags())
	require.Error(t, err)

	// No flag set at all: the spec alone.
	deploySpecFile = writeNodeSpec(t, nodeSpecBody)
	spec, err := resolveDeploySpec(nil)
	require.NoError(t, err)
	assert.Equal(t, "agent-01.internal", spec.Host)
}

// TestBuildDeploySpec_CarriesCapacityAndDefaults: a flag-only deploy can
// declare capacity, and starts from the required sandbox policy.
func TestBuildDeploySpec_CarriesCapacityAndDefaults(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "alice@node01"
	deployMaxJobs = 3
	deployProjectsRoot = "/srv/p"

	spec, err := buildDeploySpec()
	require.NoError(t, err)
	assert.Equal(t, 3, spec.Node.MaxConcurrentJobs)
	assert.Equal(t, "/srv/p", spec.ProjectsRoot)
	assert.Equal(t, deploy.DefaultSpec().Node.SandboxPolicy, spec.Node.SandboxPolicy)
}

// TestDeployFlagAppliers_CoverEveryTargetFlag: a flag without an applier
// would be silently ignored whenever --spec is given.
func TestDeployFlagAppliers_CoverEveryTargetFlag(t *testing.T) {
	// --local is not a spec key: resolveDeploySpec reads it under --spec too.
	notSpecFields := map[string]bool{"spec": true, "preflight": true, "skip-preflight": true, "purge-workspaces": true, "local": true}
	for _, build := range []func() *cobra.Command{newFleetDeployCommand, newFleetUndeployCommand, newFleetStopCommand, newFleetStartCommand} {
		resetDeployFlags(t)
		cmd := build()
		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if notSpecFields[flag.Name] {
				return
			}
			if _, ok := deployFlagAppliers[flag.Name]; !ok {
				t.Errorf("`agent %s --%s` has no deployFlagAppliers entry; it would be ignored under --spec", cmd.Name(), flag.Name)
			}
		})
	}
}

// TestSpecRelaxesRequiredFlags: --ssh and --server are required only when no
// spec names the node.
func TestSpecRelaxesRequiredFlags(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand())
	require.NoError(t, cmd.PreRunE(cmd, nil))
	require.Error(t, cmd.ValidateRequiredFlags())

	cmd = underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, nodeSpecBody))
	require.NoError(t, cmd.PreRunE(cmd, nil))
	require.NoError(t, cmd.ValidateRequiredFlags())

	// A lifecycle verb needs no --ssh: --spec or --local names the node.
	stop := underRoot(t, newFleetStopCommand(), "--local")
	require.NoError(t, stop.ValidateRequiredFlags())
}

func installFullStub(t *testing.T, stub *stubDeployService) {
	t.Helper()
	orig := fleetDeployServiceFactory
	fleetDeployServiceFactory = func() (service.FleetDeployService, error) { return stub, nil }
	t.Cleanup(func() { fleetDeployServiceFactory = orig })
}

// TestRunAgentDeploy_PreflightOnly: --preflight reports and never deploys;
// a failed preflight is the command's error.
func TestRunAgentDeploy_PreflightOnly(t *testing.T) {
	failing := &deploy.PreflightReport{Host: "agent-01", Checks: []deploy.PreflightCheck{
		deploy.PassedCheck(deploy.CheckSSH, "ok"),
		deploy.FailedCheck(deploy.CheckLinger, &deploy.LingerNotEnabledError{User: "agent", Command: "sudo loginctl enable-linger agent"}),
	}}
	stub := &stubDeployService{
		fn: func(context.Context, *deploy.Spec) (*deploy.Result, error) {
			t.Fatal("--preflight deployed")
			return nil, nil
		},
		preflight: func(context.Context, *deploy.Spec) (*deploy.PreflightReport, error) {
			return failing, failing.Err()
		},
	}
	installFullStub(t, stub)
	cmd := underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, nodeSpecBody), "--preflight")
	deployPreflightOnly = true

	err := runFleetDeploy(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linger")

	stub.preflight = func(context.Context, *deploy.Spec) (*deploy.PreflightReport, error) {
		return &deploy.PreflightReport{Host: "agent-01"}, nil
	}
	require.NoError(t, runFleetDeploy(cmd, nil))
}

func TestRunAgentDeploy_SkipPreflightReachesService(t *testing.T) {
	var seen *deploy.Spec
	installFullStub(t, &stubDeployService{fn: func(_ context.Context, spec *deploy.Spec) (*deploy.Result, error) {
		seen = spec
		return &deploy.Result{Host: spec.Host}, nil
	}})
	cmd := underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, nodeSpecBody), "--skip-preflight")
	deploySkipPreflight = true
	require.NoError(t, runFleetDeploy(cmd, nil))
	require.NotNil(t, seen)
	assert.True(t, seen.SkipPreflight)
}

// TestLifecycleCommands_CallTheirVerb: each command runs its own service
// verb against the node the spec names; undeploy passes --purge-workspaces.
func TestLifecycleCommands_CallTheirVerb(t *testing.T) {
	for _, tc := range []struct {
		build func() *cobra.Command
		args  []string
		verb  string
		purge bool
	}{
		{newFleetStopCommand, nil, "stop", false},
		{newFleetStartCommand, nil, "start", false},
		{newFleetUndeployCommand, nil, "undeploy", false},
		{newFleetUndeployCommand, []string{"--purge-workspaces"}, "undeploy", true},
	} {
		var gotVerb string
		var gotOpts service.UndeployOptions
		var gotSpec *deploy.Spec
		installFullStub(t, &stubDeployService{lifecycle: func(verb string, spec *deploy.Spec, opts service.UndeployOptions) (*deploy.LifecycleResult, error) {
			gotVerb, gotSpec, gotOpts = verb, spec, opts
			return &deploy.LifecycleResult{Host: spec.Host, Action: verb, Deregistered: verb != "start"}, nil
		}})
		cmd := underRoot(t, tc.build(), append([]string{"--spec", writeNodeSpec(t, nodeSpecBody)}, tc.args...)...)
		require.NoError(t, cmd.RunE(cmd, nil))
		assert.Equal(t, tc.verb, gotVerb)
		assert.Equal(t, tc.purge, gotOpts.PurgeWorkspaces)
		assert.Equal(t, "agent-01.internal", gotSpec.Host)
		assert.Equal(t, "spec-tenant", gotSpec.Tenant)
	}
}

func TestLifecycleCommands_PropagateErrors(t *testing.T) {
	want := &service.FleetRegistryUnavailableError{}
	installFullStub(t, &stubDeployService{lifecycle: func(verb string, spec *deploy.Spec, _ service.UndeployOptions) (*deploy.LifecycleResult, error) {
		return &deploy.LifecycleResult{Host: spec.Host, Action: verb}, want
	}})
	cmd := underRoot(t, newFleetStopCommand(), "--spec", writeNodeSpec(t, nodeSpecBody))
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deregistered")

	cmd = underRoot(t, newFleetStopCommand(), "--ssh", "not-a-target")
	require.Error(t, cmd.RunE(cmd, nil))

	orig := fleetDeployServiceFactory
	fleetDeployServiceFactory = func() (service.FleetDeployService, error) { return nil, errors.New("no issuer") }
	t.Cleanup(func() { fleetDeployServiceFactory = orig })
	cmd = underRoot(t, newFleetStopCommand(), "--spec", writeNodeSpec(t, nodeSpecBody))
	require.Error(t, cmd.RunE(cmd, nil))
}

func TestFormatPreflightReport(t *testing.T) {
	report := &deploy.PreflightReport{Host: "agent-01", Checks: []deploy.PreflightCheck{
		deploy.PassedCheck(deploy.CheckSSH, "agent@agent-01"),
		deploy.FailedCheck(deploy.CheckLinger, &deploy.LingerNotEnabledError{User: "agent", Command: "sudo loginctl enable-linger agent"}),
	}}

	var table bytes.Buffer
	require.NoError(t, formatPreflightReport(report, "table", &table))
	for _, want := range []string{"ok", "FAIL", "linger", "fix:", "sudo loginctl enable-linger agent", "NOT ready to deploy"} {
		assert.Contains(t, table.String(), want)
	}

	var jsonOut bytes.Buffer
	require.NoError(t, formatPreflightReport(report, "json", &jsonOut))
	var decoded deploy.PreflightReport
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &decoded))
	assert.Equal(t, "sudo loginctl enable-linger agent", decoded.Checks[1].Fix)

	var yamlOut bytes.Buffer
	require.NoError(t, formatPreflightReport(report, "yaml", &yamlOut))
	assert.Contains(t, yamlOut.String(), "fix: sudo loginctl enable-linger agent")

	var ready bytes.Buffer
	require.NoError(t, formatPreflightReport(&deploy.PreflightReport{Host: "h"}, "table", &ready))
	assert.Contains(t, ready.String(), "ready to deploy")
	assert.NotContains(t, ready.String(), "NOT")

	require.Error(t, formatPreflightReport(report, "xml", &bytes.Buffer{}))
}

func TestFormatLifecycleResult(t *testing.T) {
	result := &deploy.LifecycleResult{
		Host: "agent-01", Action: "undeploy", Supervisor: deploy.SupervisorSystemd,
		NodeID: "agent-acme-agent-agent-01", Deregistered: true,
		Removed: []string{"/home/agent/.quicbaas/bin/quicbaas", "/home/agent/.quicbaas/bin/agent.yaml"},
		Note:    "kept workspaces",
	}
	var table bytes.Buffer
	require.NoError(t, formatLifecycleResult(result, "table", &table))
	for _, want := range []string{"undeploy", "systemd", "agent-acme-agent-agent-01", "Deregistered:", "true", "agent.yaml", "kept workspaces"} {
		assert.Contains(t, table.String(), want)
	}

	var start bytes.Buffer
	require.NoError(t, formatLifecycleResult(&deploy.LifecycleResult{Host: "h", Action: "start", OnlineAt: "2026-09-23T00:00:00Z"}, "table", &start))
	assert.NotContains(t, start.String(), "Deregistered")
	assert.Contains(t, start.String(), "Online At")

	var jsonOut bytes.Buffer
	require.NoError(t, formatLifecycleResult(result, "json", &jsonOut))
	assert.True(t, strings.Contains(jsonOut.String(), `"deregistered": true`))
	var yamlOut bytes.Buffer
	require.NoError(t, formatLifecycleResult(result, "yaml", &yamlOut))
	assert.Contains(t, yamlOut.String(), "action: undeploy")
	require.Error(t, formatLifecycleResult(result, "xml", &bytes.Buffer{}))
}

// TestExampleNodeSpecResolves: the committed template is a spec the CLI
// accepts as-is.
func TestExampleNodeSpecResolves(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand(), "--spec", filepath.Join("..", "..", "deployments", "fleet", "example.yaml"))
	spec, err := resolveDeploySpec(cmd.Flags())
	require.NoError(t, err)
	require.NoError(t, spec.ValidateDaemonConfig())
}
