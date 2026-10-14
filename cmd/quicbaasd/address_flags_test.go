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
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// addressFlagCase is one address key as quicbaasd resolves it: the flag that
// sets it, the YAML that sets it in the file, a value for each layer, and how
// to read it back.
type addressFlagCase struct {
	flag      string
	fileYAML  string
	fileValue string
	envValue  string
	flagValue string
	get       func(*config.Config) string
}

func addressFlagCases() []addressFlagCase {
	return []addressFlagCase{
		{
			flag:      "host",
			fileYAML:  "server:\n  host: 192.0.2.10\n",
			fileValue: "192.0.2.10",
			envValue:  "192.0.2.20",
			flagValue: "192.0.2.30",
			get:       func(c *config.Config) string { return c.Server.Host },
		},
		{
			flag:      "raft-enrollment-acme-challenge-port",
			fileYAML:  "raft:\n  raft_enrollment:\n    acme:\n      challenge_port: \"18080\"\n",
			fileValue: "18080",
			envValue:  "28080",
			flagValue: "38080",
			get:       func(c *config.Config) string { return c.Raft.RaftEnrollment.ACME.ChallengePort },
		},
		{
			flag:      "raft-enrollment-advertise-noise-port",
			fileYAML:  "raft:\n  raft_enrollment:\n    advertise_noise_port: 17006\n",
			fileValue: "17006",
			envValue:  "27006",
			flagValue: "37006",
			get:       func(c *config.Config) string { return strconv.Itoa(c.Raft.RaftEnrollment.AdvertiseNoisePort) },
		},
		{
			flag:      "rag-embedding-api-url",
			fileYAML:  "rag:\n  embedding:\n    api_url: http://file.example.test:11434\n",
			fileValue: "http://file.example.test:11434",
			envValue:  "http://env.example.test:11434",
			flagValue: "http://flag.example.test:11434",
			get:       func(c *config.Config) string { return c.RAG.Embedding.APIURL },
		},
		{
			flag:      "cache-l2-redis-addr",
			fileYAML:  "cache:\n  l2_redis_addr: file.example.test:6379\n",
			fileValue: "file.example.test:6379",
			envValue:  "env.example.test:6379",
			flagValue: "flag.example.test:6379",
			get:       func(c *config.Config) string { return c.Cache.L2RedisAddr },
		},
		{
			flag:      "discovery-multicast-group",
			fileYAML:  "discovery:\n  multicast:\n    group: 239.1.1.1\n",
			fileValue: "239.1.1.1",
			envValue:  "239.2.2.2",
			flagValue: "239.3.3.3",
			get:       func(c *config.Config) string { return c.Discovery.Multicast.Group },
		},
	}
}

// addressEnvName is the environment name the flag's config key binds to.
func addressEnvName(flag string) string {
	return "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(addressFlagKeys[flag], ".", "_"))
}

// resolveAddressConfig parses args as quicbaasd address flags, then runs
// initConfig over fileYAML. The shared flag values are restored afterwards.
func resolveAddressConfig(t *testing.T, fileYAML string, args ...string) *config.Config {
	t.Helper()
	t.Cleanup(func() {
		addressFlags.VisitAll(func(f *pflag.Flag) {
			require.NoError(t, f.Value.Set(f.DefValue))
			f.Changed = false
		})
	})
	cmd := &cobra.Command{Use: "x"}
	addAddressFlags(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	return resolveUpstreamConfig(t, fileYAML)
}

func TestInitConfig_AddressKeyPrecedence(t *testing.T) {
	defaults := config.Default()
	for _, tc := range addressFlagCases() {
		t.Run(tc.flag, func(t *testing.T) {
			env := addressEnvName(tc.flag)
			flagArg := "--" + tc.flag + "=" + tc.flagValue

			t.Run("defaults", func(t *testing.T) {
				unsetEnv(t, env)
				got := resolveAddressConfig(t, "")
				assert.Equal(t, tc.get(defaults), tc.get(got))
			})
			t.Run("file", func(t *testing.T) {
				unsetEnv(t, env)
				got := resolveAddressConfig(t, tc.fileYAML)
				assert.Equal(t, tc.fileValue, tc.get(got))
			})
			t.Run("env over file", func(t *testing.T) {
				t.Setenv(env, tc.envValue)
				got := resolveAddressConfig(t, tc.fileYAML)
				assert.Equal(t, tc.envValue, tc.get(got))
			})
			t.Run("flag over defaults", func(t *testing.T) {
				unsetEnv(t, env)
				got := resolveAddressConfig(t, "", flagArg)
				assert.Equal(t, tc.flagValue, tc.get(got))
			})
			t.Run("flag over env and file", func(t *testing.T) {
				t.Setenv(env, tc.envValue)
				got := resolveAddressConfig(t, tc.fileYAML, flagArg)
				assert.Equal(t, tc.flagValue, tc.get(got))
			})
		})
	}
}

func TestAddressFlags_RegisteredOnDaemonCommands(t *testing.T) {
	for _, cmd := range []*cobra.Command{startCmd, nodeCmd, clusterCmd} {
		for name := range addressFlagKeys {
			assert.NotNil(t, cmd.Flags().Lookup(name), "%s lacks --%s", cmd.Name(), name)
		}
	}
}

func TestAddressFlags_UnparsablePort(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().AddFlagSet(newAddressFlags())

	err := cmd.ParseFlags([]string{"--raft-enrollment-advertise-noise-port=not-a-port"})
	assert.Error(t, err)
}

func TestBindAddressKeys_UnknownFlag(t *testing.T) {
	addressFlagKeys["no-such-flag"] = "server.no_such_key"
	t.Cleanup(func() { delete(addressFlagKeys, "no-such-flag") })

	err := bindAddressKeys(viper.New())
	require.Error(t, err)
	var bindErr *api.FlagBindError
	assert.True(t, errors.As(err, &bindErr), "want FlagBindError, got %T: %v", err, err)
}
