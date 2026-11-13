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

// canonical_verb_surface_test.go pins the init / barrier verb surface.
//
// The three CLIs in the stack — qrdb, xkmsctl, quicbaas — expose the SAME
// underlying go-qrdb capabilities. They had drifted into three different
// subsets of it: quicbaas was missing `barrier root-token` and
// `enable-encryption`, xkmsctl was missing `ceremony officer` and
// `barrier officer`, and only quicbaas had `ceremony initialize` and
// `enroll-external-officer`. Nobody was missing the engine, only the surface,
// and an operator moving between daemons had to learn three dialects.
//
// A capability that is reachable in one CLI and absent from another is a bug
// in the absent one. This test is the ratchet that keeps that true: adding a
// verb here forces it to exist, and a future refactor that drops one fails
// loudly instead of silently narrowing the operator's surface.

package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalInitVerbs is the locked `init` (ceremony) surface. It is the whole
// of go-qrdb's Security Officer distribution model plus the ceremony
// lifecycle, in the order an operator performs them.
var canonicalInitVerbs = []string{
	"initialize",              // run the ceremony (split, seal one share per officer)
	"status",                  // ceremony state
	"officer",                 // register / list / status
	"claim-cert",              // officer claims their issued certificate
	"claim-share",             // officer claims their share (release-once)
	"sign-csr",                // SO-authorised CSR signing
	"enroll-external-officer", // officer whose key material is external
}

// canonicalInitOfficerVerbs is the locked `init officer` sub-surface.
var canonicalInitOfficerVerbs = []string{"register", "list", "status"}

// canonicalBarrierVerbs is the locked `barrier` surface: the full lifecycle
// plus the two administrative verbs quicbaas previously lacked.
var canonicalBarrierVerbs = []string{
	"status",
	"init",
	"init-shamir",
	"unseal",
	"unseal-shares",
	"seal",
	"rekey",
	"recovery",
	"root-token",
	"enable-encryption",
}

// subcommandNames returns the direct subcommand names of cmd.
func subcommandNames(cmd *cobra.Command) map[string]bool {
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	return names
}

// findSubcommand returns the named direct subcommand, or nil.
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

// TestCanonicalSurface_Init pins the init/ceremony verb set.
func TestCanonicalSurface_Init(t *testing.T) {
	got := subcommandNames(newInitCommand())
	for _, verb := range canonicalInitVerbs {
		assert.True(t, got[verb],
			"`quicbaas init %s` is missing; it is part of the canonical ceremony surface", verb)
	}
}

// TestCanonicalSurface_InitOfficer pins the officer enrolment sub-surface —
// step 1 of the Shamir flow, without which the ceremony cannot run at all.
func TestCanonicalSurface_InitOfficer(t *testing.T) {
	officer := findSubcommand(newInitCommand(), "officer")
	require.NotNil(t, officer, "`quicbaas init officer` is missing")

	got := subcommandNames(officer)
	for _, verb := range canonicalInitOfficerVerbs {
		assert.True(t, got[verb], "`quicbaas init officer %s` is missing", verb)
	}
}

// TestCanonicalSurface_Barrier pins the barrier verb set.
func TestCanonicalSurface_Barrier(t *testing.T) {
	got := subcommandNames(newBarrierCommand())
	for _, verb := range canonicalBarrierVerbs {
		assert.True(t, got[verb],
			"`quicbaas barrier %s` is missing; it is part of the canonical barrier surface", verb)
	}
}

// TestCanonicalSurface_EveryVerbIsRunnable guards against a verb that exists
// only as a help entry. A command with neither a runner nor subcommands looks
// present in `--help` and does nothing when invoked, which is worse than being
// absent: the operator believes the capability is there.
func TestCanonicalSurface_EveryVerbIsRunnable(t *testing.T) {
	for _, group := range []*cobra.Command{newInitCommand(), newBarrierCommand()} {
		for _, sub := range group.Commands() {
			runnable := sub.RunE != nil || sub.Run != nil || sub.HasSubCommands()
			assert.True(t, runnable,
				"`%s %s` is declared but does nothing", group.Name(), sub.Name())
		}
	}
}

// TestCanonicalSurface_LoginFactors pins the login surface.
//
// Enrollment and login must speak ONE vocabulary: an administrator who enrols
// an "mtls" / "fido2" / "piv" factor at init has to be able to present that
// same factor here. A method dropped from this flag would strand every
// operator holding that credential, with no way back in short of re-running
// the ceremony.
func TestCanonicalSurface_LoginFactors(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"auth", "login"})
	require.NoError(t, err, "`quicbaas auth login` is not registered")

	method := cmd.Flags().Lookup("method")
	require.NotNil(t, method, "`auth login --method` is missing")
	for _, factor := range []string{"mtls", "fido2", "piv"} {
		assert.Contains(t, method.Usage, factor,
			"--method does not offer the %q factor, which init can enrol", factor)
	}

	// The per-factor inputs each method needs to actually run.
	for _, flag := range []string{
		"username",      // the subject being authenticated
		"client-cert",   // mtls
		"device",        // fido2 authenticator selection
		"piv-backend",   // piv: file | pkcs11 | tpm2
		"slot",          // piv slot
		"pkcs11-module", // piv via a hardware token
	} {
		assert.NotNil(t, cmd.Flags().Lookup(flag),
			"`auth login --%s` is missing; a factor cannot be presented without it", flag)
	}
}

// shareQuorumVerbs are the barrier verbs that collect a QUORUM of share-class
// secrets. Every one of them must offer both input modes.
//
// The list is a path from the `barrier` group, because two of the three are
// nested. `tenant barrier unseal-share` is deliberately absent: it submits
// exactly ONE share toward a quorum accumulated server-side, so a
// newline-delimited file is the wrong shape for it — it takes the share
// on stdin with --share-stdin instead (TestTenantBarrierUnsealShare_HasAStdinFlag).
var shareQuorumVerbs = [][]string{
	{"unseal-shares"},
	{"root-token"},
	{"recovery", "recover"},
}

// TestCanonicalSurface_BarrierVerbsCarryTheirFlags pins the flags an operator
// needs. A verb whose required input has no flag cannot be driven, so its
// presence in the tree would be misleading.
//
// The share-quorum verbs are checked as a CLASS rather than one at a time
// (#227). `barrier recovery recover` had --share and no --shares-file, so a
// recovery quorum could be supplied only on argv — where every process on the
// host reads it out of /proc/<pid>/cmdline — while its two siblings had
// offered a file since #222. Nothing stated a reason for the difference; it
// was an omission, and a per-verb assertion is exactly what let it sit. A
// fourth quorum verb added to the list without both flags fails here.
func TestCanonicalSurface_BarrierVerbsCarryTheirFlags(t *testing.T) {
	barrier := newBarrierCommand()

	for _, path := range shareQuorumVerbs {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			cmd := barrier
			for _, name := range path {
				cmd = findSubcommand(cmd, name)
				require.NotNil(t, cmd, "`barrier %s` is missing from the tree", strings.Join(path, " "))
			}
			for _, flag := range []string{"share", "shares-file"} {
				assert.NotNil(t, cmd.Flags().Lookup(flag),
					"`barrier %s --%s` is missing; a quorum cannot be supplied without it, "+
						"and without --shares-file it can be supplied only on argv",
					strings.Join(path, " "), flag)
			}
		})
	}

	enableEnc := findSubcommand(barrier, "enable-encryption")
	require.NotNil(t, enableEnc)
	for _, flag := range []string{"tenant", "strategy", "passphrase", "threshold", "total"} {
		assert.NotNil(t, enableEnc.Flags().Lookup(flag),
			"`barrier enable-encryption --%s` is missing", flag)
	}
}
