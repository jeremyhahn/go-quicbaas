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
	"fmt"

	"github.com/spf13/cobra"
)

// PendingCredentialBackendError is emitted by "credential" subcommand
// handlers before the credential service surface ships.
type PendingCredentialBackendError struct {
	Op string
}

// Error returns a human-readable description.
func (e *PendingCredentialBackendError) Error() string {
	return e.Op + " is not yet wired to the SDK CredentialService"
}

// newCanonicalInitCmd returns the canonical "init" command group,
// defined in cmd/quicbaas/init.go. Per Phase 12 of the
// seal→barrier rename plan, the single-call bootstrap form has been
// retired; the verb set now matches xkmsctl init verb-for-verb
// (status, claim-cert, claim-share, sign-csr, enroll-external-officer).
//
// The single-call bootstrap (formerly invoked as `quicbaas init
// --so-pin … --user-pin …`) is now driven by `quicbaasd init` on the
// daemon side; the operator CLI never bootstrap-initialises a remote
// daemon directly.
func newCanonicalInitCmd() *cobra.Command {
	return newInitCommand()
}

// newCanonicalBarrierCmd returns the canonical "barrier" command
// group, defined in cmd/quicbaas/barrier.go. The renamed "seal" tree
// is registered separately via newSealCommand() (deprecated alias).
func newCanonicalBarrierCmd() *cobra.Command {
	return newBarrierCommand()
}

// newCanonicalSealRedirectCmd returns the deprecated 'seal' alias
// command tree. Each subcommand prints a deprecation warning to
// stderr and forwards to the equivalent 'quicbaas barrier' command.
func newCanonicalSealRedirectCmd() *cobra.Command {
	return newSealCommand()
}

// newCanonicalCACmd builds the canonical "ca" command tree.
func newCanonicalCACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ca",
		Short: "CA operations (info, bundle, list, issue, revoke, export-bundle)",
	}
	for _, verb := range []string{"info", "bundle", "list", "issue", "revoke", "export-bundle"} {
		v := verb
		cmd.AddCommand(&cobra.Command{
			Use:   v,
			Short: "CA " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("quicbaas ca %s: pending SDK Security().Certificate() surface", v)
			},
		})
	}
	return cmd
}

// newCanonicalCeremonyCmd builds the canonical "ceremony" command.
func newCanonicalCeremonyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ceremony",
		Short: "Ceremony state-machine ops (status, claim-cert, claim-share, sign-csr)",
	}
	cmd.AddCommand(&cobra.Command{Use: "status", Short: "Show ceremony state"})
	claimCert := &cobra.Command{Use: "claim-cert", Short: "Certificate claim operations"}
	claimCert.AddCommand(&cobra.Command{Use: "begin", Short: "Begin certificate claim"})
	claimCert.AddCommand(&cobra.Command{Use: "complete", Short: "Complete certificate claim"})
	cmd.AddCommand(claimCert)
	cmd.AddCommand(&cobra.Command{Use: "claim-share", Short: "Claim a Shamir share"})
	cmd.AddCommand(&cobra.Command{Use: "sign-csr", Short: "Sign a CSR with SO authorization"})
	return cmd
}

// newCanonicalBootstrapCmd returns the canonical "bootstrap" command
// group, defined in cmd/quicbaas/bootstrap.go.
func newCanonicalBootstrapCmd() *cobra.Command {
	return newBootstrapCommand()
}

// newCanonicalCredentialCmd builds the canonical "credential" command.
func newCanonicalCredentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential",
		Short: "Post-init credential operations (submit, list, rotate)",
	}
	for _, verb := range []string{"submit", "list", "rotate"} {
		v := verb
		cmd.AddCommand(&cobra.Command{
			Use:   v,
			Short: "Credential " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return &PendingCredentialBackendError{Op: "credential " + v}
			},
		})
	}
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
	// Security().Auth().Factors().Login() wired version. The legacy
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
	rootCmd.AddCommand(newCanonicalSealRedirectCmd())
}
