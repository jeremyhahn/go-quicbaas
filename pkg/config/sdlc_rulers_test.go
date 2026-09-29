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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadSDLCYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return NewLoader().Load(path)
}

func TestLoad_SDLCRulersDecodePerTenant(t *testing.T) {
	cfg, err := loadSDLCYAML(t, "sdlc:\n  enabled: true\n  repo_hosts: [forge.test]\n  webhook:\n    public_url: https://node.test\n"+
		"  rulers:\n    default: [agent-company, ops]\n    tenants:\n      automatethethings: [lead-agent]\n      quiet: []\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"agent-company", "ops"}, cfg.SDLC.Rulers.Default)
	assert.Equal(t, []string{"lead-agent"}, cfg.SDLC.Rulers.Tenants["automatethethings"])
	quiet, ok := cfg.SDLC.Rulers.Tenants["quiet"]
	assert.True(t, ok, "a tenant given no rulers must keep its entry")
	assert.Empty(t, quiet)
}

func TestLoad_SDLCRulersUnsetLeaveTheDefaultNil(t *testing.T) {
	cfg, err := loadSDLCYAML(t, "sdlc:\n  enabled: true\n  repo_hosts: [forge.test]\n  webhook:\n    public_url: https://node.test\n")
	require.NoError(t, err)
	assert.Nil(t, cfg.SDLC.Rulers.Default, "an unset default must stay nil so the module takes its built-in rulers")
	assert.Empty(t, cfg.SDLC.Rulers.Tenants)
}

func TestLoad_SDLCRulersAnEmptyDefaultNamesNobody(t *testing.T) {
	cfg, err := loadSDLCYAML(t, "sdlc:\n  enabled: true\n  repo_hosts: [forge.test]\n  webhook:\n    public_url: https://node.test\n  rulers:\n    default: []\n")
	require.NoError(t, err)
	assert.NotNil(t, cfg.SDLC.Rulers.Default, "an explicit empty default must survive decoding as empty, not unset")
	assert.Empty(t, cfg.SDLC.Rulers.Default)
}
