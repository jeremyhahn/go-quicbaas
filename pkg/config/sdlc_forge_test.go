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

package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// An EXTERNAL test package, like the surfacing drift test: it imports pkg/sdlc
// to pin the two defaults layers, and an in-package test doing so would be a
// cycle.

// TestSDLCForge_ShippedDefaultsMatchTheEngine pins the shipped forge policy to
// the engine's own defaults. Two statements of one value drift; this is what
// stops them.
func TestSDLCForge_ShippedDefaultsMatchTheEngine(t *testing.T) {
	shipped := config.Default().SDLC.Forge
	got := sdlc.ForgeTransport{
		ReadTimeout:       shipped.ReadTimeout,
		WriteTimeout:      shipped.WriteTimeout,
		HeavyWriteTimeout: shipped.HeavyWriteTimeout,
		ReadAttempts:      shipped.ReadAttempts,
		RetryBackoff:      shipped.RetryBackoff,
	}
	if got != sdlc.DefaultForgeTransport {
		t.Errorf("shipped %+v, engine %+v", got, sdlc.DefaultForgeTransport)
	}
}

// TestSDLCForge_TheEnvironmentReachesTheKnob walks the real Loader: the knob an
// operator turns up when a forge's host is loaded must be reachable without
// editing a file.
func TestSDLCForge_TheEnvironmentReachesTheKnob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "sdlc:\n  forge:\n    heavy_write_timeout: 7m\n    read_attempts: 5\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("QUICBAAS_SDLC_FORGE_READ_TIMEOUT", "45s")

	cfg, err := config.NewLoader().Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	forge := cfg.SDLC.Forge
	if forge.HeavyWriteTimeout != 7*time.Minute || forge.ReadAttempts != 5 {
		t.Errorf("from file: %+v, want 7m and 5 attempts", forge)
	}
	if forge.ReadTimeout != 45*time.Second {
		t.Errorf("read_timeout = %s, want the environment's 45s", forge.ReadTimeout)
	}
	if forge.WriteTimeout != time.Minute {
		t.Errorf("write_timeout = %s; an unset field must keep the shipped default", forge.WriteTimeout)
	}
}
