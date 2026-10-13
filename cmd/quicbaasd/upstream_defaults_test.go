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
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

const (
	envRAGEmbeddingAPIURL     = "QUICBAAS_RAG_EMBEDDING_API_URL"
	envEmbeddedFleetOllamaURL = "QUICBAAS_SERVER_EMBEDDED_FLEET_NODE_OLLAMA_URL"
)

// resolveUpstreamConfig runs initConfig against a config file holding
// fileYAML, with the environment the caller set, and returns the resolved
// configuration. The package globals initConfig writes are restored after the
// test.
func resolveUpstreamConfig(t *testing.T, fileYAML string) *config.Config {
	t.Helper()
	origCfg, origLog, origCfgFile := cfg, log, cfgFile
	t.Cleanup(func() {
		cfg, log, cfgFile = origCfg, origLog, origCfgFile
		viper.Reset()
	})

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("logging:\n  level: warn\n"+fileYAML), 0o600))

	cfgFile = configPath
	viper.Reset()
	initConfig()
	require.NotNil(t, cfg)
	return cfg
}

// unsetEnv clears an environment variable for the duration of the test, so a
// value in the host environment cannot leak into a precedence assertion.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func TestInitConfig_RAGEmbeddingAPIURLPrecedence(t *testing.T) {
	const (
		fromFile = "http://file.example.test:11434"
		fromEnv  = "http://env.example.test:11434"
	)
	fileYAML := "rag:\n  embedding:\n    api_url: " + fromFile + "\n"

	t.Run("file", func(t *testing.T) {
		unsetEnv(t, envRAGEmbeddingAPIURL)
		got := resolveUpstreamConfig(t, fileYAML)
		assert.Equal(t, fromFile, got.RAG.Embedding.APIURL)
	})
	t.Run("env", func(t *testing.T) {
		t.Setenv(envRAGEmbeddingAPIURL, fromEnv)
		got := resolveUpstreamConfig(t, "")
		assert.Equal(t, fromEnv, got.RAG.Embedding.APIURL)
	})
	t.Run("env wins over file", func(t *testing.T) {
		t.Setenv(envRAGEmbeddingAPIURL, fromEnv)
		got := resolveUpstreamConfig(t, fileYAML)
		assert.Equal(t, fromEnv, got.RAG.Embedding.APIURL)
	})
}

func TestInitConfig_EmbeddedFleetNodeOllamaURLPrecedence(t *testing.T) {
	const (
		fromFile = "http://file.example.test:11434"
		fromEnv  = "http://env.example.test:11434"
		fromFlag = "http://flag.example.test:11434"
	)
	fileYAML := "server:\n  embedded_fleet_node:\n    ollama_url: " + fromFile + "\n"

	t.Run("file", func(t *testing.T) {
		unsetEnv(t, envEmbeddedFleetOllamaURL)
		got := resolveUpstreamConfig(t, fileYAML)
		assert.Equal(t, fromFile, got.Server.EmbeddedFleetNode.OllamaURL)
	})
	t.Run("env wins over file", func(t *testing.T) {
		t.Setenv(envEmbeddedFleetOllamaURL, fromEnv)
		got := resolveUpstreamConfig(t, fileYAML)
		assert.Equal(t, fromEnv, got.Server.EmbeddedFleetNode.OllamaURL)
	})
	t.Run("flag wins over env and file", func(t *testing.T) {
		t.Setenv(envEmbeddedFleetOllamaURL, fromEnv)
		got := resolveUpstreamConfig(t, fileYAML)
		cmd := newServerFlagCmd()
		require.NoError(t, cmd.Flags().Set("embedded-fleet-node-ollama-url", fromFlag))
		applyServerFlags(cmd)
		assert.Equal(t, fromFlag, got.Server.EmbeddedFleetNode.OllamaURL)
	})
	t.Run("an unset flag keeps the resolved value", func(t *testing.T) {
		t.Setenv(envEmbeddedFleetOllamaURL, fromEnv)
		got := resolveUpstreamConfig(t, fileYAML)
		applyServerFlags(newServerFlagCmd())
		assert.Equal(t, fromEnv, got.Server.EmbeddedFleetNode.OllamaURL)
	})
}

// TestInitConfig_UpstreamEndpointsNothingSet pins that, with no file, env or
// flag value, every inference endpoint resolves to the defaults-layer value.
func TestInitConfig_UpstreamEndpointsNothingSet(t *testing.T) {
	for _, key := range []string{envRAGEmbeddingAPIURL, envEmbeddedFleetOllamaURL, "QUICBAAS_OLLAMA_BASE_URL"} {
		unsetEnv(t, key)
	}
	defaults := config.DefaultConfig()

	got := resolveUpstreamConfig(t, "")
	applyServerFlags(newServerFlagCmd())

	assert.Equal(t, defaults.Ollama.BaseURL, got.Ollama.BaseURL)
	assert.Equal(t, defaults.RAG.Embedding.APIURL, got.RAG.Embedding.APIURL)
	assert.Equal(t, defaults.Server.EmbeddedFleetNode.OllamaURL, got.Server.EmbeddedFleetNode.OllamaURL)
	assert.Equal(t, config.DefaultEmbeddingAPIURL("ollama"), got.Server.EmbeddedFleetNode.OllamaURL)

	flag := newServerFlagCmd().Flags().Lookup("embedded-fleet-node-ollama-url")
	require.NotNil(t, flag)
	assert.Equal(t, defaults.Server.EmbeddedFleetNode.OllamaURL, flag.DefValue)
}
