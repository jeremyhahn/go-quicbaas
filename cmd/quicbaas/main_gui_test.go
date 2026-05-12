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
	"testing"
)

func TestIsGUIOnly_NoArgs(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"quicbaas"}
	if !isGUIOnly() {
		t.Error("expected isGUIOnly() to return true with no args")
	}
}

func TestIsGUIOnly_WithSubcommand(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"quicbaas", "data", "get", "mykey"}
	if isGUIOnly() {
		t.Error("expected isGUIOnly() to return false with subcommand")
	}
}

func TestIsGUIOnly_OnlyFlags(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"quicbaas", "--log-level", "--address"}
	// "--log-level" starts with "-" but "--address" does not
	// Wait - actually "data" doesn't start with "-" but "--log-level" does.
	// Let me fix: all args start with "-"
	os.Args = []string{"quicbaas", "--log-level", "--address"}
	// Both start with "--" so isGUIOnly returns true
	if !isGUIOnly() {
		t.Error("expected isGUIOnly() to return true with only flags")
	}
}

func TestIsGUIOnly_MixedFlagsAndSubcommand(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"quicbaas", "--log-level", "info", "data"}
	// "info" does not start with "-" so isGUIOnly returns false
	if isGUIOnly() {
		t.Error("expected isGUIOnly() to return false with mixed flags and subcommand")
	}
}

// TestIsGUIOnly_GlobalFlagsWithValues — user report regression. The user
// ran `./bin/quicbaas --log-level debug --log-file /tmp/x.log` expecting
// the GUI, and got the cobra "no command specified" fallback because the
// old implementation treated `debug` (value of `--log-level`) as a
// subcommand. isGUIOnly must skip flag values so `--log-level X` counts
// as GUI-only intent.
func TestIsGUIOnly_GlobalFlagsWithValues(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	guiForms := [][]string{
		{"quicbaas", "--log-level", "debug"},
		{"quicbaas", "--log-level", "debug", "--log-file", "/tmp/x.log"},
		{"quicbaas", "--log-level=debug"},
		{"quicbaas", "--log-file=/tmp/x.log"},
		{"quicbaas", "--address", "localhost:9090", "--tenant", "default"},
		{"quicbaas", "--config", "/etc/quicbaas.yaml", "--log-level", "debug"},
		{"quicbaas", "--fips"},                    // bool flag
		{"quicbaas", "--tls", "--tls-skip-verify"}, // two bool flags
		{"quicbaas", "--log-level", "debug", "--fips"}, // mixed
	}
	for _, args := range guiForms {
		os.Args = args
		if !isGUIOnly() {
			t.Errorf("expected isGUIOnly()=true for %v (GUI launch with flags)", args)
		}
	}

	cliForms := [][]string{
		{"quicbaas", "data", "put", "key", "value"},
		{"quicbaas", "--log-level", "debug", "data", "get", "mykey"}, // flag then subcommand
		{"quicbaas", "--address", "localhost:9090", "iam", "user", "list"},
	}
	for _, args := range cliForms {
		os.Args = args
		if isGUIOnly() {
			t.Errorf("expected isGUIOnly()=false for %v (subcommand after flags)", args)
		}
	}
}

// TestIsGUIOnly_HelpAndVersionFallThroughToCLI locks in the rule that
// `quicbaas --help`, `-h`, `--version`, `-v`, `help`, `version` are CLI
// intents, not GUI launches — even though they look like "no subcommand"
// to a naive check. Regression for a bug where `./quicbaas --help` on a
// machine with DISPLAY set tried to open a Wails window.
func TestIsGUIOnly_HelpAndVersionFallThroughToCLI(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	cliIntentForms := [][]string{
		{"quicbaas", "--help"},
		{"quicbaas", "-h"},
		{"quicbaas", "help"},
		{"quicbaas", "--version"},
		{"quicbaas", "-v"},
		{"quicbaas", "version"},
	}

	for _, args := range cliIntentForms {
		os.Args = args
		if isGUIOnly() {
			t.Errorf("expected isGUIOnly() to return false for CLI-intent args %v", args)
		}
	}
}
