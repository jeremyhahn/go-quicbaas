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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// addressKeyCase is one address key: the YAML that sets it in the file,
// the value the file and the environment carry, and how to read it back.
type addressKeyCase struct {
	key       string
	fileYAML  string
	fileValue string
	envValue  string
	get       func(*Config) string
}

func addressKeyCases() []addressKeyCase {
	return []addressKeyCase{
		{
			key:       KeyServerHost,
			fileYAML:  "server:\n  host: 192.0.2.10\n",
			fileValue: "192.0.2.10",
			envValue:  "192.0.2.20",
			get:       func(c *Config) string { return c.Server.Host },
		},
		{
			key:       KeyRaftEnrollmentACMEChallengePort,
			fileYAML:  "raft:\n  raft_enrollment:\n    acme:\n      challenge_port: \"18080\"\n",
			fileValue: "18080",
			envValue:  "28080",
			get:       func(c *Config) string { return c.Raft.RaftEnrollment.ACME.ChallengePort },
		},
		{
			key:       KeyRaftEnrollmentAdvertiseNoisePort,
			fileYAML:  "raft:\n  raft_enrollment:\n    advertise_noise_port: 17006\n",
			fileValue: "17006",
			envValue:  "27006",
			get:       func(c *Config) string { return strconv.Itoa(c.Raft.RaftEnrollment.AdvertiseNoisePort) },
		},
		{
			key:       KeyRAGEmbeddingAPIURL,
			fileYAML:  "rag:\n  embedding:\n    api_url: http://file.example.test:11434\n",
			fileValue: "http://file.example.test:11434",
			envValue:  "http://env.example.test:11434",
			get:       func(c *Config) string { return c.RAG.Embedding.APIURL },
		},
		{
			key:       KeyCacheL2RedisAddr,
			fileYAML:  "cache:\n  l2_redis_addr: file.example.test:6379\n",
			fileValue: "file.example.test:6379",
			envValue:  "env.example.test:6379",
			get:       func(c *Config) string { return c.Cache.L2RedisAddr },
		},
		{
			key:       KeyDiscoveryMulticastGroup,
			fileYAML:  "discovery:\n  multicast:\n    group: 239.1.1.1\n",
			fileValue: "239.1.1.1",
			envValue:  "239.2.2.2",
			get:       func(c *Config) string { return c.Discovery.Multicast.Group },
		},
	}
}

// addressEnvName is the environment name an address key binds to.
func addressEnvName(key string) string {
	return "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// loadAddressConfig loads fileYAML (empty for no file) through the Loader.
func loadAddressConfig(t *testing.T, fileYAML string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "missing.yaml")
	if fileYAML != "" {
		path = filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(fileYAML), 0o600))
	}
	return NewLoader().Load(path)
}

// unsetAddressEnv clears every address environment name for the test.
func unsetAddressEnv(t *testing.T) {
	t.Helper()
	for _, key := range addressKeys {
		name := addressEnvName(key)
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
}

func TestLoader_AddressKeyPrecedence(t *testing.T) {
	defaults := Default()
	for _, tc := range addressKeyCases() {
		t.Run(tc.key, func(t *testing.T) {
			env := addressEnvName(tc.key)

			t.Run("defaults", func(t *testing.T) {
				unsetAddressEnv(t)
				got, err := loadAddressConfig(t, "")
				require.NoError(t, err)
				assert.Equal(t, tc.get(defaults), tc.get(got))
			})
			t.Run("file", func(t *testing.T) {
				unsetAddressEnv(t)
				got, err := loadAddressConfig(t, tc.fileYAML)
				require.NoError(t, err)
				assert.Equal(t, tc.fileValue, tc.get(got))
			})
			t.Run("env over defaults", func(t *testing.T) {
				unsetAddressEnv(t)
				t.Setenv(env, tc.envValue)
				got, err := loadAddressConfig(t, "")
				require.NoError(t, err)
				assert.Equal(t, tc.envValue, tc.get(got))
			})
			t.Run("env over file", func(t *testing.T) {
				unsetAddressEnv(t)
				t.Setenv(env, tc.envValue)
				got, err := loadAddressConfig(t, tc.fileYAML)
				require.NoError(t, err)
				assert.Equal(t, tc.envValue, tc.get(got))
			})
		})
	}
}

func TestLoader_AddressKeyUnparsablePort(t *testing.T) {
	unsetAddressEnv(t)
	t.Setenv(addressEnvName(KeyRaftEnrollmentAdvertiseNoisePort), "not-a-port")

	_, err := loadAddressConfig(t, "")
	require.Error(t, err)
	var unmarshalErr *api.ConfigUnmarshalError
	assert.True(t, errors.As(err, &unmarshalErr), "want ConfigUnmarshalError, got %T: %v", err, err)
}

func TestValidator_AddressBindings(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantField string
	}{
		{"defaults pass", func(*Config) {}, ""},
		{"valid values pass", func(c *Config) {
			c.Raft.RaftEnrollment.ACME.ChallengePort = "18080"
			c.Raft.RaftEnrollment.AdvertiseNoisePort = 17006
			c.Discovery.Multicast.Group = "239.255.77.77"
		}, ""},
		{"challenge port not a number", func(c *Config) {
			c.Raft.RaftEnrollment.ACME.ChallengePort = "http"
		}, KeyRaftEnrollmentACMEChallengePort},
		{"challenge port out of range", func(c *Config) {
			c.Raft.RaftEnrollment.ACME.ChallengePort = "70000"
		}, KeyRaftEnrollmentACMEChallengePort},
		{"advertise noise port negative", func(c *Config) {
			c.Raft.RaftEnrollment.AdvertiseNoisePort = -1
		}, KeyRaftEnrollmentAdvertiseNoisePort},
		{"advertise noise port out of range", func(c *Config) {
			c.Raft.RaftEnrollment.AdvertiseNoisePort = 65536
		}, KeyRaftEnrollmentAdvertiseNoisePort},
		{"multicast group not an IP", func(c *Config) {
			c.Discovery.Multicast.Group = "not-an-ip"
		}, KeyDiscoveryMulticastGroup},
		{"multicast group unicast IP", func(c *Config) {
			c.Discovery.Multicast.Group = "192.0.2.1"
		}, KeyDiscoveryMulticastGroup},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(cfg)
			v := NewValidator()
			v.validateAddressBindings(cfg)
			if tt.wantField == "" {
				assert.Empty(t, v.errors)
				return
			}
			require.Len(t, v.errors, 1)
			assert.Equal(t, tt.wantField, v.errors[0].Field)
		})
	}
}

func TestValidate_RunsAddressBindings(t *testing.T) {
	cfg := Default()
	cfg.Discovery.Multicast.Group = "not-an-ip"

	err := Validate(cfg)
	require.Error(t, err)
	var verrs ValidationErrors
	require.True(t, errors.As(err, &verrs), "want ValidationErrors, got %T", err)
	fields := make([]string, 0, len(verrs))
	for _, e := range verrs {
		fields = append(fields, e.Field)
	}
	assert.Contains(t, fields, KeyDiscoveryMulticastGroup)
}
