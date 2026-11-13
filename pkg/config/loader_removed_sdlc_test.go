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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad_RefusesRemovedKeysByName pins the config split (go-quicbaas#569):
// every key nothing reads any more is refused at load, naming the key and
// where its setting went, from the file and from the environment.
func TestLoad_RefusesRemovedKeysByName(t *testing.T) {
	for key, replacement := range RemovedKeys {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quicbaas.yaml")
			require.NoError(t, os.WriteFile(path, []byte(nestedYAML(key, "1")), 0o600))
			_, err := NewLoader().Load(path)
			require.Error(t, err)
			var removed *RemovedKeyError
			require.True(t, errors.As(err, &removed), "%v", err)
			assert.Equal(t, key, removed.Key)
			assert.Equal(t, replacement, removed.Replacement)
			assert.True(t, errors.Is(err, ErrRemovedKey))
			assert.Contains(t, err.Error(), key)
		})
	}

	// The environment is refused too, and a configuration without the old
	// keys loads.
	t.Setenv("QUICBAAS_SDLC_SWEEP_INTERVAL", "30s")
	_, err := NewLoader().Load(filepath.Join(t.TempDir(), "absent.yaml"))
	var removed *RemovedKeyError
	require.True(t, errors.As(err, &removed), "%v", err)
	assert.Equal(t, "sdlc.sweep_interval", removed.Key)
	t.Setenv("QUICBAAS_SDLC_SWEEP_INTERVAL", "")
	os.Unsetenv("QUICBAAS_SDLC_SWEEP_INTERVAL")

	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(path, []byte("sdlc:\n  enabled: true\n  repo_hosts: [forge.test]\n  webhook:\n    max_body: 2048\n"), 0o600))
	cfg, err := NewLoader().Load(path)
	require.NoError(t, err)
	assert.Equal(t, int64(2048), cfg.SDLC.Webhook.MaxBody)
	assert.Equal(t, []string{"forge.test"}, cfg.SDLC.RepoHosts)
}

// TestLoad_RefusesTheDaemonSideWorkspaceKeys pins the owner's ruling that the
// daemon does no clone, copy or worktree for the SDLC path: the worker's
// container clones the project and its context repositories itself, so both
// workspace keys are refused by name rather than quietly ignored.
func TestLoad_RefusesTheDaemonSideWorkspaceKeys(t *testing.T) {
	for key, body := range map[string]string{
		"sdlc.companions":    "sdlc:\n  enabled: true\n  companions:\n    - name: handbook\n      source: /srv/handbook\n",
		"sdlc.worktree_root": "sdlc:\n  enabled: true\n  worktree_root: /var/lib/quicbaas/trees\n",
	} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quicbaas.yaml")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			_, err := NewLoader().Load(path)
			require.Error(t, err)
			var removed *RemovedKeyError
			require.True(t, errors.As(err, &removed), "%v", err)
			assert.Equal(t, key, removed.Key)
			assert.Equal(t, workerClonesItsOwnWorkspace, removed.Replacement)
			assert.True(t, errors.Is(err, ErrRemovedKey))
			assert.Contains(t, err.Error(), "context_repos")
		})
	}
}

// TestLoad_RefusesSDLCVerificationCommands pins go-quicbaas#628: the key was
// loaded, defaulted and validated and then read by nothing, so an operator who
// set it got no error and no effect. It is refused by name now, and the
// refusal names both places the setting actually lives.
func TestLoad_RefusesSDLCVerificationCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "sdlc:\n  enabled: true\n  verification_commands: [\"make build\", \"make test\"]\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, err := NewLoader().Load(path)
	require.Error(t, err)
	var removed *RemovedKeyError
	require.True(t, errors.As(err, &removed), "%v", err)
	assert.Equal(t, "sdlc.verification_commands", removed.Key)
	assert.True(t, errors.Is(err, ErrRemovedKey))
	assert.Contains(t, err.Error(), "orchestration.verification_commands")
	assert.Contains(t, err.Error(), "binding's verification list")
}

// nestedYAML renders a dotted key as the nested YAML that sets it.
func nestedYAML(key, value string) string {
	var b strings.Builder
	parts := strings.Split(key, ".")
	for i, part := range parts {
		b.WriteString(strings.Repeat("  ", i) + part + ":")
		if i == len(parts)-1 {
			b.WriteString(" " + value)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestLoad_AgentReferencesDecode: the lead agent and the default triage agent
// are references to a tenant Agent or pool.
func TestLoad_AgentReferencesDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "orchestration:\n  lead_agent:\n    agent_id: claude-lead\nsdlc:\n  triage_agent:\n    pool_id: triagers\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cfg, err := NewLoader().Load(path)
	require.NoError(t, err)
	assert.Equal(t, "claude-lead", cfg.Orchestration.LeadAgent.AgentID)
	assert.Equal(t, "triagers", cfg.SDLC.TriageAgent.PoolID)
	assert.Empty(t, cfg.SDLC.TriageAgent.AgentID)
}
