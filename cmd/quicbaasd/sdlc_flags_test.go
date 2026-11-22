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
	"slices"
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

func sdlcFlagCases() []addressFlagCase {
	return []addressFlagCase{
		{
			flag:      "sdlc-enabled",
			fileYAML:  "sdlc:\n  enabled: true\n",
			fileValue: "true",
			envValue:  "false",
			flagValue: "true",
			get:       func(c *config.Config) string { return strconv.FormatBool(c.SDLC.Enabled) },
		},
		{
			flag:      "sdlc-issue-page-limit",
			fileYAML:  "sdlc:\n  issue_page_limit: 11\n",
			fileValue: "11",
			envValue:  "22",
			flagValue: "33",
			get:       func(c *config.Config) string { return strconv.Itoa(c.SDLC.IssuePageLimit) },
		},
		{
			flag:      "sdlc-checkout-root",
			fileYAML:  "sdlc:\n  checkout_root: /file-ws\n",
			fileValue: "/file-ws",
			envValue:  "/env-ws",
			flagValue: "/flag-ws",
			get:       func(c *config.Config) string { return c.SDLC.CheckoutRoot },
		},
		{
			flag:      "sdlc-merge-attempts",
			fileYAML:  "sdlc:\n  merge_attempts: 2\n",
			fileValue: "2",
			envValue:  "3",
			flagValue: "4",
			get:       func(c *config.Config) string { return strconv.Itoa(c.SDLC.MergeAttempts) },
		},
		{
			flag:      "sdlc-webhook-max-body",
			fileYAML:  "sdlc:\n  webhook:\n    max_body: 2048\n",
			fileValue: "2048",
			envValue:  "4096",
			flagValue: "8192",
			get:       func(c *config.Config) string { return strconv.FormatInt(c.SDLC.Webhook.MaxBody, 10) },
		},
		{
			flag:      "sdlc-webhook-public-url",
			fileYAML:  "sdlc:\n  webhook:\n    public_url: https://file.example.test\n",
			fileValue: "https://file.example.test",
			envValue:  "https://env.example.test",
			flagValue: "https://flag.example.test",
			get:       func(c *config.Config) string { return c.SDLC.Webhook.PublicURL },
		},
	}
}

// sdlcEnvName is the environment name the flag's config key binds to.
func sdlcEnvName(flag string) string {
	return "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(sdlcFlagKeys[flag], ".", "_"))
}

// resolveSDLCConfig parses args as quicbaasd sdlc flags, then runs initConfig
// over fileYAML. The shared flag values are restored afterwards.
func resolveSDLCConfig(t *testing.T, fileYAML string, args ...string) *config.Config {
	t.Helper()
	t.Cleanup(func() {
		sdlcFlags.VisitAll(func(f *pflag.Flag) {
			require.NoError(t, f.Value.Set(f.DefValue))
			f.Changed = false
		})
	})
	cmd := &cobra.Command{Use: "x"}
	addSDLCFlags(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	return resolveUpstreamConfig(t, fileYAML)
}

func TestInitConfig_SDLCKeyPrecedence(t *testing.T) {
	defaults := config.Default()
	for _, tc := range sdlcFlagCases() {
		t.Run(tc.flag, func(t *testing.T) {
			env := sdlcEnvName(tc.flag)
			flagArg := "--" + tc.flag + "=" + tc.flagValue

			t.Run("defaults", func(t *testing.T) {
				unsetEnv(t, env)
				got := resolveSDLCConfig(t, "")
				assert.Equal(t, tc.get(defaults), tc.get(got))
			})
			t.Run("file", func(t *testing.T) {
				unsetEnv(t, env)
				got := resolveSDLCConfig(t, tc.fileYAML)
				assert.Equal(t, tc.fileValue, tc.get(got))
			})
			t.Run("env over file", func(t *testing.T) {
				t.Setenv(env, tc.envValue)
				got := resolveSDLCConfig(t, tc.fileYAML)
				assert.Equal(t, tc.envValue, tc.get(got))
			})
			t.Run("flag over defaults", func(t *testing.T) {
				unsetEnv(t, env)
				got := resolveSDLCConfig(t, "", flagArg)
				assert.Equal(t, tc.flagValue, tc.get(got))
			})
			t.Run("flag over env and file", func(t *testing.T) {
				t.Setenv(env, tc.envValue)
				got := resolveSDLCConfig(t, tc.fileYAML, flagArg)
				assert.Equal(t, tc.flagValue, tc.get(got))
			})
		})
	}
}

func TestSDLCFlags_RegisteredOnDaemonCommands(t *testing.T) {
	for _, cmd := range []*cobra.Command{startCmd, nodeCmd, clusterCmd} {
		for name := range sdlcFlagKeys {
			assert.NotNil(t, cmd.Flags().Lookup(name), "%s lacks --%s", cmd.Name(), name)
		}
	}
}

// A flag may only set a key the environment can set too.
func TestSDLCFlags_KeysAreSDLCKeys(t *testing.T) {
	for name, key := range sdlcFlagKeys {
		assert.True(t, slices.Contains(config.SDLCKeys, key), "--%s sets %s, which is not in config.SDLCKeys", name, key)
	}
}

func TestBindSDLCKeys_UnknownFlag(t *testing.T) {
	sdlcFlagKeys["no-such-flag"] = "sdlc.no_such_key"
	t.Cleanup(func() { delete(sdlcFlagKeys, "no-such-flag") })

	err := bindSDLCKeys(viper.New())
	require.Error(t, err)
	var bindErr *api.FlagBindError
	assert.True(t, errors.As(err, &bindErr), "want FlagBindError, got %T: %v", err, err)
}
