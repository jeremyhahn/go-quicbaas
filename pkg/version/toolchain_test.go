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

package version

import (
	"errors"
	"testing"
)

// TestToolchain_EveryLineIsAnAssignment keeps toolchain.mk in the one shape
// both readers agree on. A line make understands and Go does not would be a
// pin the daemon silently lacks.
func TestToolchain_EveryLineIsAnAssignment(t *testing.T) {
	pins, err := Toolchain()
	if err != nil {
		t.Fatalf("Toolchain: %v", err)
	}
	for _, name := range []string{"GO_VERSION", "GOLANGCI_VERSION", "GOSEC_VERSION", "GOVULNCHECK_VERSION", "SERENA_VERSION", "GOPLS_VERSION", "GO_TOOL_DEPENDENCY_FLOORS",
		"OPENCODE_VERSION", "OPENCODE_SHA256_X86_64", "OPENCODE_SHA256_AARCH64"} {
		if pins[name] == "" {
			t.Errorf("toolchain.mk does not pin %s", name)
		}
	}
}

func TestParseToolchain_RefusesWhatItCannotRead(t *testing.T) {
	cases := map[string]string{
		"recursive assignment": "GO_VERSION = 1.27.0",
		"a reference":          "GO_VERSION := $(OTHER)",
		"empty value":          "GO_VERSION :=",
		"conditional":          "ifeq ($(X),y)",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			var unreadable *ToolchainUnreadableError
			if _, err := parseToolchain(text); !errors.As(err, &unreadable) || len(unreadable.Lines) != 1 {
				t.Fatalf("err = %v", err)
			}
		})
	}
	pins, err := parseToolchain("# comment\n\nA := 1\n  B :=   two  \n")
	if err != nil || pins["A"] != "1" || pins["B"] != "two" {
		t.Fatalf("pins = %v, %v", pins, err)
	}
}
