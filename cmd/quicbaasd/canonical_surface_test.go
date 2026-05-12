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
	"errors"
	"testing"
)

// TestCanonicalSurface_RootHasInit verifies the root canonical "init"
// command is registered on quicbaasd.
func TestCanonicalSurface_RootHasInit(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"init"}); err != nil {
		t.Fatalf("expected canonical 'init' command at quicbaasd root: %v", err)
	}
}

// canonicalRootSubcommands lists the locked canonical command names
// per docs/08-bootstrap-auth-architecture.md §6.
var canonicalRootSubcommands = []string{
	"init",
	"auth",
	"barrier",
	"ceremony",
	"ca",
	"bootstrap",
	"credential",
}

// canonicalInitFlagNames lists the canonical init flag names.
var canonicalInitFlagNames = []string{
	"so-pin",
	"user-pin",
	"barrier-strategy",
	"shares",
	"threshold",
	"hostname",
	"sans",
	"admin-bundle",
}

// TestCanonicalSurface_RootHasAllSubcommands verifies every canonical
// subcommand appears at the root.
func TestCanonicalSurface_RootHasAllSubcommands(t *testing.T) {
	for _, name := range canonicalRootSubcommands {
		if _, _, err := rootCmd.Find([]string{name}); err != nil {
			t.Errorf("canonical command %q missing from quicbaasd root: %v", name, err)
		}
	}
}

// TestCanonicalSurface_InitFlagsExposed verifies the canonical init
// flag set is fully exposed.
func TestCanonicalSurface_InitFlagsExposed(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"init"})
	if err != nil {
		t.Fatalf("init not found: %v", err)
	}
	for _, name := range canonicalInitFlagNames {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("canonical init flag --%s missing", name)
		}
	}
}

// TestCanonicalSurface_LegacySealRedirects verifies the legacy
// "quicbaasd seal" path emits CanonicalRedirectError.
func TestCanonicalSurface_LegacySealRedirects(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"seal"})
	if err != nil {
		t.Fatalf("seal redirect not registered: %v", err)
	}
	if cmd.RunE == nil {
		t.Fatal("seal redirect has no RunE")
	}
	err = cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected CanonicalRedirectError from legacy quicbaasd seal")
	}
	var redirect *CanonicalRedirectError
	if !errors.As(err, &redirect) {
		t.Fatalf("expected *CanonicalRedirectError, got %T: %v", err, err)
	}
}

// TestCanonicalSurface_LegacySealInitRedirects verifies that
// "quicbaasd seal init" redirects to the canonical root init.
func TestCanonicalSurface_LegacySealInitRedirects(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"seal"})
	if err != nil {
		t.Fatalf("seal redirect not registered: %v", err)
	}
	err = cmd.RunE(cmd, []string{"init"})
	if err == nil {
		t.Fatal("expected CanonicalRedirectError")
	}
	var redirect *CanonicalRedirectError
	if !errors.As(err, &redirect) {
		t.Fatalf("expected *CanonicalRedirectError, got %T: %v", err, err)
	}
	if redirect.Old != "quicbaasd seal init" {
		t.Errorf("expected Old='quicbaasd seal init', got %q", redirect.Old)
	}
}

// TestCanonicalSurface_BootstrapHasDirect verifies the bootstrap group
// includes the new "direct" mode.
func TestCanonicalSurface_BootstrapHasDirect(t *testing.T) {
	for _, mode := range []string{"auto", "dane", "noise", "spki", "direct"} {
		if _, _, err := rootCmd.Find([]string{"bootstrap", mode}); err != nil {
			t.Errorf("bootstrap %s missing: %v", mode, err)
		}
	}
}

// TestCanonicalSurface_AuthHasFactor verifies the auth N-factor
// surface.
func TestCanonicalSurface_AuthHasFactor(t *testing.T) {
	for _, verb := range []string{"login", "factor"} {
		if _, _, err := rootCmd.Find([]string{"auth", verb}); err != nil {
			t.Errorf("auth %s missing: %v", verb, err)
		}
	}
	for _, verb := range []string{"list", "add", "remove", "rotate", "relabel"} {
		if _, _, err := rootCmd.Find([]string{"auth", "factor", verb}); err != nil {
			t.Errorf("auth factor %s missing: %v", verb, err)
		}
	}
}

// TestCanonicalSurface_BarrierHasOps verifies the canonical "barrier"
// group has seal/unseal/status/rekey + recovery.
func TestCanonicalSurface_BarrierHasOps(t *testing.T) {
	for _, verb := range []string{"seal", "unseal", "status", "rekey"} {
		if _, _, err := rootCmd.Find([]string{"barrier", verb}); err != nil {
			t.Errorf("barrier %s missing: %v", verb, err)
		}
	}
	for _, verb := range []string{"generate", "restore"} {
		if _, _, err := rootCmd.Find([]string{"barrier", "recovery", verb}); err != nil {
			t.Errorf("barrier recovery %s missing: %v", verb, err)
		}
	}
}
