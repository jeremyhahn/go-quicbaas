// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadBotConfig_Happy parses a valid YAML file and returns a
// fully-defaulted config.
func TestLoadBotConfig_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
botIdentity: "+15551234567"
goSignalURL: "https://signal.example.com"
quicbaasAddr: "localhost:9090"
authMode: implicit
serviceAccount: bot-svc
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadBotConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.BotIdentity != "+15551234567" {
		t.Fatalf("identity: %q", cfg.BotIdentity)
	}
	if cfg.TenantID != "default" {
		t.Fatalf("default tenant: %q", cfg.TenantID)
	}
	if cfg.DeviceName != "orchestrator-bot" {
		t.Fatalf("default device: %q", cfg.DeviceName)
	}
}

// TestLoadBotConfig_MissingFile returns a storage-kind error.
func TestLoadBotConfig_MissingFile(t *testing.T) {
	if _, err := LoadBotConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected error")
	}
}

// TestLoadBotConfig_EmptyPath rejects.
func TestLoadBotConfig_EmptyPath(t *testing.T) {
	if _, err := LoadBotConfig(""); err == nil {
		t.Fatalf("expected error")
	}
}

// TestLoadBotConfig_MalformedYAML returns an invalid-input error.
func TestLoadBotConfig_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(path, []byte("::not yaml"), 0o600)
	if _, err := LoadBotConfig(path); err == nil {
		t.Fatalf("expected error")
	}
}

// TestBotConfig_ValidateErrors covers required-field checks.
func TestBotConfig_ValidateErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  BotConfig
	}{
		{"no bot identity", BotConfig{GoSignalURL: "x", QuicBaaSAddr: "y", AuthMode: AuthModeImplicit, ServiceAccount: "z"}},
		{"no go-signal url", BotConfig{BotIdentity: "x", QuicBaaSAddr: "y", AuthMode: AuthModeImplicit, ServiceAccount: "z"}},
		{"no quicbaas addr", BotConfig{BotIdentity: "x", GoSignalURL: "y", AuthMode: AuthModeImplicit, ServiceAccount: "z"}},
		{"bad auth mode", BotConfig{BotIdentity: "x", GoSignalURL: "y", QuicBaaSAddr: "z", AuthMode: "huh"}},
		{"implicit no service account", BotConfig{BotIdentity: "x", GoSignalURL: "y", QuicBaaSAddr: "z", AuthMode: AuthModeImplicit}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

// TestBotConfig_ApplyDefaults fills known defaults.
func TestBotConfig_ApplyDefaults(t *testing.T) {
	c := BotConfig{}
	c.applyDefaults()
	if c.DeviceName == "" {
		t.Fatal("device")
	}
	if c.TenantID == "" {
		t.Fatal("tenant")
	}
	if c.AuthMode == "" {
		t.Fatal("auth")
	}
	if c.PreKeyRotationThreshold == 0 {
		t.Fatal("threshold")
	}
	if c.PreKeyCheckInterval == 0 {
		t.Fatal("interval")
	}
	if c.StatePath == "" {
		t.Fatal("state")
	}
}

// TestDefaultVerbosityPrefs returns all-true.
func TestDefaultVerbosityPrefs(t *testing.T) {
	p := DefaultVerbosityPrefs()
	if !p.StreamTokens || !p.ToolDetails || !p.FileOps {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}
