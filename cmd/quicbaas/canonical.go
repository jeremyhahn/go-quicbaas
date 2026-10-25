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

// canonical.go wires the locked canonical CLI surface for the quicbaas
// CLI client per docs/08-bootstrap-auth-architecture.md §6. The
// canonical surface is identical across qrdb / xkmsctl / quicbaas /
// platformctl: a root "init" command, plus barrier / ceremony / ca /
// bootstrap / credential / auth subcommand groups.
package main

import (
	"github.com/spf13/cobra"
)

// newCanonicalInitCmd returns the canonical "init" command group,
// defined in cmd/quicbaas/init.go. Per Phase 12 of the
// seal→barrier rename plan, the single-call bootstrap form has been
// retired; the verb set now matches xkmsctl init verb-for-verb
// (status, claim-share, sign-csr).
//
// The single-call bootstrap (formerly invoked as `quicbaas init
// --so-pin … --user-pin …`) is now driven by `quicbaasd init` on the
// daemon side; the operator CLI never bootstrap-initialises a remote
// daemon directly.
func newCanonicalInitCmd() *cobra.Command {
	return newInitCommand()
}

// newCanonicalBarrierCmd returns the canonical "barrier" command
// group, defined in cmd/quicbaas/barrier.go. It is the single
// canonical entry point for StorageBarrier lifecycle operations; the
// former "seal" alias tree has been removed.
func newCanonicalBarrierCmd() *cobra.Command {
	return newBarrierCommand()
}

// newCanonicalCACmd builds the canonical "ca" command tree. The
// implementation lives in cmd/quicbaas/ca.go where each verb wires
// directly to Security().Certificate() over the active SDK transport.
func newCanonicalCACmd() *cobra.Command {
	return newCACmd()
}

// newCanonicalCeremonyCmd builds the canonical "ceremony" command.
//
// Per docs/08-bootstrap-auth-architecture.md §6 the "ceremony"
// subcommand group exposes the same verb set as the top-level "init"
// command. Both reach the same SDK Security().Ceremony() backend; the
// two surfaces give operators a choice of whether to think of the
// flow as "initialise this daemon" (init) or "drive the ceremony
// state machine" (ceremony). We reuse the same cobra builders so
// the two trees stay byte-for-byte identical instead of drifting.
func newCanonicalCeremonyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ceremony",
		Short: "Ceremony state-machine ops (status, claim-share, sign-csr)",
		Long: `Ceremony state-machine commands.

Drives the initialisation ceremony state machine — releases each
officer's sealed share envelope and signs operator CSRs. The verb set matches 'quicbaas init' (each
verb routes to the same Security().Ceremony() SDK methods).

Verbs:
  status                   Show the current ceremony state
  claim-share              Claim the officer's share envelope
  sign-csr                 SO-authorised CSR signing`,
	}
	cmd.PersistentFlags().StringVar(&initOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newInitStatusCommand())
	cmd.AddCommand(newInitClaimShareCommand())
	cmd.AddCommand(newInitSignCSRCommand())
	return cmd
}

// newCanonicalBootstrapCmd returns the canonical "bootstrap" command
// group, defined in cmd/quicbaas/bootstrap.go.
func newCanonicalBootstrapCmd() *cobra.Command {
	return newBootstrapCommand()
}

// newCanonicalCredentialCmd builds the canonical "credential" command
// tree, delegating to Security().Credentials() over the active SDK
// transport (rest / grpc / quic / mcp / unix / embedded — all wired).
func newCanonicalCredentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential",
		Short: "Post-init credential operations (submit, list, rotate)",
	}
	cmd.AddCommand(newCredentialSubmitCmd())
	cmd.AddCommand(newCredentialListCmd())
	cmd.AddCommand(newCredentialRotateCmd())
	cmd.AddCommand(newCredentialRetrieveCmd())
	cmd.AddCommand(newCredentialVersionsCmd())
	cmd.AddCommand(newCredentialDeleteCmd())
	cmd.AddCommand(newCredentialGCCmd())
	return cmd
}

// augmentCanonicalAuthCmd attaches the canonical "factor" subgroup
// and replaces the legacy "auth login" command with the canonical
// per-method dispatcher per docs/08-bootstrap-auth-architecture.md §6
// (auth login / auth factor list|add|remove|rotate|relabel). The
// legacy informational handler is renamed to "auth login-info" so
// existing operator runbooks can still reach the help text if needed.
func augmentCanonicalAuthCmd(parent *cobra.Command) {
	existing := map[string]*cobra.Command{}
	for _, c := range parent.Commands() {
		existing[c.Use] = c
	}
	if _, ok := existing["factor"]; !ok {
		factor := &cobra.Command{Use: "factor", Short: "N-factor management"}
		factor.AddCommand(newCanonicalAuthFactorListCmd())
		factor.AddCommand(newCanonicalAuthFactorAddCmd())
		factor.AddCommand(newCanonicalAuthFactorRemoveCmd())
		factor.AddCommand(newCanonicalAuthFactorRotateCmd())
		factor.AddCommand(newCanonicalAuthFactorRelabelCmd())
		parent.AddCommand(factor)
	}
	// Replace the legacy "auth login" stub with the canonical
	// Security().Auth().BeginLogin/FinishLogin wired version. The legacy
	// command is rebound under "login-info" so its help text remains
	// reachable.
	if legacy, ok := existing["login"]; ok {
		parent.RemoveCommand(legacy)
		legacy.Use = "login-info"
		legacy.Short = "Display legacy login instructions (informational only)"
		parent.AddCommand(legacy)
	}
	parent.AddCommand(newCanonicalAuthLoginCmd())
}

// configureCanonicalSurface registers the canonical commands onto the
// quicbaas rootCmd.
func configureCanonicalSurface() {
	rootCmd.AddCommand(newCanonicalInitCmd())
	// Augment the existing legacy "auth" command with the canonical
	// "factor" subgroup rather than registering a parallel tree.
	for _, c := range rootCmd.Commands() {
		if c.Use == "auth" {
			augmentCanonicalAuthCmd(c)
			break
		}
	}
	rootCmd.AddCommand(newCanonicalBarrierCmd())
	rootCmd.AddCommand(newCanonicalCACmd())
	rootCmd.AddCommand(newCanonicalCeremonyCmd())
	rootCmd.AddCommand(newCanonicalCredentialCmd())
	rootCmd.AddCommand(newCanonicalBootstrapCmd())
}
