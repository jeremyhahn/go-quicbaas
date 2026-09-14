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

// TestLoad_RefusesRemovedSDLCKeysByName pins the config split (go-quicbaas#569):
// every sdlc: key the module no longer reads is refused at load, naming the
// key and where its setting went, from the file and from the environment.
func TestLoad_RefusesRemovedSDLCKeysByName(t *testing.T) {
	for key, replacement := range RemovedSDLCKeys {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quicbaas.yaml")
			section := strings.TrimPrefix(key, "sdlc.")
			require.NoError(t, os.WriteFile(path, []byte("sdlc:\n  enabled: true\n  "+section+": 1\n"), 0o600))
			_, err := NewLoader().Load(path)
			require.Error(t, err)
			var removed *RemovedSDLCKeyError
			require.True(t, errors.As(err, &removed), "%v", err)
			assert.Equal(t, key, removed.Key)
			assert.Equal(t, replacement, removed.Replacement)
			assert.True(t, errors.Is(err, ErrRemovedSDLCKey))
			assert.Contains(t, err.Error(), key)
		})
	}

	// The environment is refused too, and a configuration without the old
	// keys loads.
	t.Setenv("QUICBAAS_SDLC_SWEEP_INTERVAL", "30s")
	_, err := NewLoader().Load(filepath.Join(t.TempDir(), "absent.yaml"))
	var removed *RemovedSDLCKeyError
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
			var removed *RemovedSDLCKeyError
			require.True(t, errors.As(err, &removed), "%v", err)
			assert.Equal(t, key, removed.Key)
			assert.Equal(t, workerClonesItsOwnWorkspace, removed.Replacement)
			assert.True(t, errors.Is(err, ErrRemovedSDLCKey))
			assert.Contains(t, err.Error(), "context_repos")
		})
	}
}
