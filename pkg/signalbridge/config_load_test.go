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

// TestLoadBotConfig_ValidYAMLFailsValidation parses cleanly but is
// rejected by Validate (implicit mode without a service account), covering
// the Validate error branch of LoadBotConfig.
func TestLoadBotConfig_ValidYAMLFailsValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	content := `
botIdentity: "+15551234567"
goSignalURL: "https://signal.example.com"
quicbaasAddr: "localhost:9090"
authMode: implicit
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadBotConfig(path); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input from Validate, got %v", err)
	}
}

// TestLoadBotConfig_LinkedAndGroupModes loads valid configs in the two
// non-implicit modes (which do not require a service account).
func TestLoadBotConfig_LinkedAndGroupModes(t *testing.T) {
	for _, mode := range []string{"linked", "group-only"} {
		path := filepath.Join(t.TempDir(), mode+".yaml")
		content := "botIdentity: \"+1\"\ngoSignalURL: \"https://x\"\nquicbaasAddr: \"y\"\nauthMode: " + mode + "\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg, err := LoadBotConfig(path)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if string(cfg.AuthMode) != mode {
			t.Fatalf("mode = %q, want %q", cfg.AuthMode, mode)
		}
	}
}
