package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// localNodeSpecBody is a spec for a --local deploy: no host, no ssh
// settings, and no node_bind, so the bind default applies.
const localNodeSpecBody = `server: https://quicbaas.example.com:8443
tenant: spec-tenant
node:
  max_concurrent_jobs: 4
`

// withRaftAddress points the CLI's node config at raftAddress for one test.
func withRaftAddress(t *testing.T, raftAddress string) {
	t.Helper()
	orig := cfg
	t.Cleanup(func() { cfg = orig })
	cfg = config.Default()
	cfg.Raft.RaftAddress = raftAddress
}

func TestResolveDeploySpec_SSHBindsTheTargetHost(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand(), "--ssh", "alice@node01", "--server", "https://c:8443")

	spec, err := resolveDeploySpec(cmd.Flags())

	require.NoError(t, err)
	assert.Equal(t, "node01:9091", spec.NodeBind)
	assert.False(t, spec.Local)
}

func TestResolveDeploySpec_SpecWithoutBindBindsItsHost(t *testing.T) {
	body := strings.Replace(nodeSpecBody, "node_bind: 0.0.0.0:9191\n", "", 1)
	cmd := underRoot(t, newFleetDeployCommand(), "--spec", writeNodeSpec(t, body))

	spec, err := resolveDeploySpec(cmd.Flags())

	require.NoError(t, err)
	assert.Equal(t, "agent-01.internal:9091", spec.NodeBind)
}

func TestResolveDeploySpec_LocalBindsTheAdvertisedHost(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	cmd := underRoot(t, newFleetDeployCommand(), "--local", "--server", "https://c:8443")

	spec, err := resolveDeploySpec(cmd.Flags())

	require.NoError(t, err)
	assert.True(t, spec.Local)
	assert.Equal(t, "node02.internal", spec.Host)
	assert.Empty(t, spec.SSHUser)
	assert.Equal(t, "node02.internal:9091", spec.NodeBind)
}

func TestResolveDeploySpec_LocalSpecFileBindsTheAdvertisedHost(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	cmd := underRoot(t, newFleetDeployCommand(), "--local", "--spec", writeNodeSpec(t, localNodeSpecBody))

	spec, err := resolveDeploySpec(cmd.Flags())

	require.NoError(t, err)
	assert.True(t, spec.Local)
	assert.Equal(t, "node02.internal:9091", spec.NodeBind)
}

func TestResolveDeploySpec_NoDefaultBindIsAWildcard(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	for _, args := range [][]string{
		{"--ssh", "alice@node01", "--server", "https://c:8443"},
		{"--local", "--server", "https://c:8443"},
	} {
		cmd := underRoot(t, newFleetDeployCommand(), args...)

		spec, err := resolveDeploySpec(cmd.Flags())

		require.NoError(t, err, args)
		assert.NotContains(t, spec.NodeBind, "0.0.0.0", args)
	}
}

func TestResolveDeploySpec_ExplicitBindWins(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	for _, args := range [][]string{
		{"--ssh", "alice@node01", "--server", "https://c:8443", "--bind", "0.0.0.0:9091"},
		{"--local", "--server", "https://c:8443", "--bind", "0.0.0.0:9091"},
	} {
		cmd := underRoot(t, newFleetDeployCommand(), args...)

		spec, err := resolveDeploySpec(cmd.Flags())

		require.NoError(t, err, args)
		assert.Equal(t, "0.0.0.0:9091", spec.NodeBind, args)
	}
}

func TestResolveDeploySpec_LocalWithSSHIsRefused(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	cmd := underRoot(t, newFleetDeployCommand(), "--local", "--ssh", "alice@node01", "--server", "https://c:8443")

	_, err := resolveDeploySpec(cmd.Flags())

	var conflict *deploy.LocalSSHConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "--ssh", conflict.Field)
}

func TestResolveDeploySpec_LocalWithSpecHostIsRefused(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	cmd := underRoot(t, newFleetDeployCommand(), "--local", "--spec", writeNodeSpec(t, nodeSpecBody))

	_, err := resolveDeploySpec(cmd.Flags())

	var conflict *deploy.LocalSSHConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "host", conflict.Field)
}

func TestResolveDeploySpec_LocalWithSSHKeyIsRefused(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	cmd := underRoot(t, newFleetDeployCommand(), "--local", "--ssh-key-name", "alice-laptop", "--server", "https://c:8443")

	_, err := resolveDeploySpec(cmd.Flags())

	var conflict *deploy.LocalSSHConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "ssh_key_name", conflict.Field)
}

func TestResolveDeploySpec_LocalNeedsAnAdvertisedHost(t *testing.T) {
	withRaftAddress(t, "0.0.0.0:63000")
	cmd := underRoot(t, newFleetDeployCommand(), "--local", "--server", "https://c:8443")

	_, err := resolveDeploySpec(cmd.Flags())

	var hostErr *config.RaftAdvertisedHostError
	require.ErrorAs(t, err, &hostErr)
}

func TestResolveDeploySpec_SSHTargetStillRequiredWithoutLocal(t *testing.T) {
	cmd := underRoot(t, newFleetDeployCommand(), "--server", "https://c:8443")

	_, err := resolveDeploySpec(cmd.Flags())

	var invalid *deploy.InvalidSpecError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "ssh", invalid.Field)
}

func TestFleetDeployHelp_ShowsLocalExampleLast(t *testing.T) {
	long := newFleetDeployCommand().Long

	assert.True(t, strings.HasSuffix(long, deployLocalExample))
	assert.Contains(t, long, "--local")
}

// lifecycleCommands are the verbs that manage an installed node.
var lifecycleCommands = map[string]func() *cobra.Command{
	"stop":     newFleetStopCommand,
	"start":    newFleetStartCommand,
	"undeploy": newFleetUndeployCommand,
}

// recordLifecycle installs a deploy service that records the spec each
// lifecycle verb reaches it with, and returns where that spec lands.
func recordLifecycle(t *testing.T) **deploy.Spec {
	t.Helper()
	var got *deploy.Spec
	installFullStub(t, &stubDeployService{lifecycle: func(verb string, spec *deploy.Spec, _ service.UndeployOptions) (*deploy.LifecycleResult, error) {
		got = spec
		return &deploy.LifecycleResult{Host: spec.Host, Action: verb}, nil
	}})
	return &got
}

func TestLifecycleCommands_LocalReachesTheLocalTarget(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	for verb, build := range lifecycleCommands {
		for _, args := range [][]string{
			{"--local"},
			{"--local", "--spec", writeNodeSpec(t, localNodeSpecBody)},
		} {
			got := recordLifecycle(t)
			cmd := underRoot(t, build(), args...)

			require.NoError(t, cmd.RunE(cmd, nil), verb, args)
			require.NotNil(t, *got, verb, args)
			assert.True(t, (*got).Local, verb, args)
			assert.Equal(t, "node02.internal", (*got).Host, verb, args)
			assert.Empty(t, (*got).SSHUser, verb, args)
		}
	}
}

func TestLifecycleCommands_LocalWithSSHTargetIsRefused(t *testing.T) {
	withRaftAddress(t, "node02.internal:63000")
	for verb, build := range lifecycleCommands {
		for _, tc := range []struct {
			args  []string
			field string
		}{
			{[]string{"--local", "--ssh", "alice@node01"}, "--ssh"},
			{[]string{"--local", "--spec", writeNodeSpec(t, nodeSpecBody)}, "host"},
		} {
			got := recordLifecycle(t)
			cmd := underRoot(t, build(), tc.args...)

			err := cmd.RunE(cmd, nil)

			var conflict *deploy.LocalSSHConflictError
			require.ErrorAs(t, err, &conflict, verb, tc.args)
			assert.Equal(t, tc.field, conflict.Field, verb, tc.args)
			assert.Nil(t, *got, "%s reached the service", verb)
		}
	}
}

func TestLifecycleCommands_HostlessSpecNeedsLocal(t *testing.T) {
	for verb, build := range lifecycleCommands {
		got := recordLifecycle(t)
		cmd := underRoot(t, build(), "--spec", writeNodeSpec(t, localNodeSpecBody))

		err := cmd.RunE(cmd, nil)

		var invalid *deploy.InvalidSpecError
		require.ErrorAs(t, err, &invalid, verb)
		assert.Equal(t, "host", invalid.Field, verb)
		assert.Nil(t, *got, "%s reached the service", verb)
	}
}

func TestLifecycleHelp_ShowsLocal(t *testing.T) {
	for verb, build := range lifecycleCommands {
		cmd := build()

		assert.NotNil(t, cmd.Flags().Lookup("local"), verb)
		assert.Contains(t, cmd.Long, "quicbaas fleet "+verb+" --local", verb)
	}
}
