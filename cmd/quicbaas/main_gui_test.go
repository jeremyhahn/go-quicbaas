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
