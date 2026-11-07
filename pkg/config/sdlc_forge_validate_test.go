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
	"strings"
	"testing"
	"time"
)

// TestSDLCForge_NegativeIsRefusedAndZeroIsNot proves the validator refuses
// each negative bound by its key, and reads zero as "take the default".
//
// IN-PACKAGE, against validateSDLC alone, like the label-reconcile tests: the
// whole-config Validate also checks the storage directories, which is the host
// and not this section.
func TestSDLCForge_NegativeIsRefusedAndZeroIsNot(t *testing.T) {
	cases := map[string]func(*SDLCForge){
		"sdlc.forge.read_timeout":        func(f *SDLCForge) { f.ReadTimeout = -time.Second },
		"sdlc.forge.write_timeout":       func(f *SDLCForge) { f.WriteTimeout = -time.Second },
		"sdlc.forge.heavy_write_timeout": func(f *SDLCForge) { f.HeavyWriteTimeout = -time.Second },
		"sdlc.forge.read_attempts":       func(f *SDLCForge) { f.ReadAttempts = -1 },
		"sdlc.forge.retry_backoff":       func(f *SDLCForge) { f.RetryBackoff = -time.Second },
	}
	for key, mutate := range cases {
		t.Run(key, func(t *testing.T) {
			cfg := enabledSDLCSection()
			mutate(&cfg.Forge)
			v := NewValidator()
			v.validateSDLC(cfg)
			err := v.Errors()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("validateSDLC = %v, want %s refused", err, key)
			}
		})
	}

	cfg := enabledSDLCSection()
	cfg.Forge = SDLCForge{}
	v := NewValidator()
	v.validateSDLC(cfg)
	if err := v.Errors(); err != nil {
		t.Errorf("an unset forge section was refused: %v", err)
	}
}

func TestSDLCForge_TokenCommandRefusalsNameTheirKey(t *testing.T) {
	const argv = "/usr/local/bin/forge-mint"
	cases := map[string]map[string]SDLCForgeTokenCommand{
		"sdlc.forge.token_commands.gitlab":                {"gitlab": {Argv: []string{argv}}},
		"sdlc.forge.token_commands.github.argv":           {"github": {}},
		"sdlc.forge.token_commands.gitea.argv":            {"gitea": {Argv: []string{"bin/forge-mint"}}},
		"sdlc.forge.token_commands.github.refresh_before": {"github": {Argv: []string{argv}, RefreshBefore: -time.Second}},
		"sdlc.forge.token_commands.github.timeout":        {"github": {Argv: []string{argv}, Timeout: -time.Second}},
	}
	for key, commands := range cases {
		t.Run(key, func(t *testing.T) {
			cfg := enabledSDLCSection()
			cfg.Forge.TokenCommands = commands
			v := NewValidator()
			v.validateSDLC(cfg)
			err := v.Errors()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("validateSDLC = %v, want %s refused", err, key)
			}
		})
	}

	cfg := enabledSDLCSection()
	cfg.Forge.TokenCommands = map[string]SDLCForgeTokenCommand{
		"github": {Argv: []string{argv, "--app", "pilot"}, RefreshBefore: 5 * time.Minute, Timeout: 30 * time.Second},
		"gitea":  {Argv: []string{argv}},
	}
	v := NewValidator()
	v.validateSDLC(cfg)
	if err := v.Errors(); err != nil {
		t.Errorf("valid token commands were refused: %v", err)
	}
}

// enabledSDLCSection is the default SDLC section with the module on and the
// one value enabling it requires.
func enabledSDLCSection() *SDLCConfig {
	cfg := Default().SDLC
	cfg.Enabled = true
	cfg.Webhook.PublicURL = "https://factory.test"
	return &cfg
}
