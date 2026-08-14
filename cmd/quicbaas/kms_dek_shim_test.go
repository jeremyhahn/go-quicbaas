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

// kms_dek_shim_test.go guards the collapse of the two DEK command trees.
//
// `quicbaas kms dek` used to be an eight-verb fork backed by a gRPC-pinned
// client factory. It is now a deprecated alias for the SDK-backed
// `quicbaas dek` tree. These tests pin both halves of that contract: no legacy
// verb was dropped, and no legacy verb reaches a non-SDK code path.

package main

import (
	stderrors "errors"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// errStubDEKFactory is returned by the stub SDK factory so every verb fails at
// exactly the same, observable point.
var errStubDEKFactory = stderrors.New("stub: sdk dek factory")

// legacyKMSDEKVerbs is the verb set the retired `kms dek` subtree exposed,
// paired with the flags each one accepted. Every entry must still resolve on
// the canonical tree.
var legacyKMSDEKVerbs = map[string][]string{
	"generate":    {"algorithm", "description"},
	"get":         nil,
	"list":        {"prefix", "limit"},
	"rotate":      nil,
	"delete":      {"force"},
	"status":      nil,
	"set-default": nil,
	"set-safety":  {"max-operations", "max-bytes", "warning-percent"},
}

// TestKMSDEK_LegacyVerbsAllResolve is the verb-by-verb equivalence check for
// the collapse: every command the old subtree offered still resolves under
// `kms dek`, and every flag it accepted is still registered.
func TestKMSDEK_LegacyVerbsAllResolve(t *testing.T) {
	root := newKMSDEKCommand()

	for verb, flags := range legacyKMSDEKVerbs {
		t.Run(verb, func(t *testing.T) {
			cmd, _, err := root.Find([]string{verb})
			require.NoError(t, err, "legacy verb %q no longer resolves", verb)
			require.NotNil(t, cmd)
			require.NotEqual(t, root, cmd,
				"legacy verb %q fell through to the parent instead of resolving", verb)

			for _, flag := range flags {
				assert.NotNil(t, cmd.Flags().Lookup(flag),
					"legacy verb %q lost its --%s flag", verb, flag)
			}
		})
	}
}

// TestKMSDEK_IsASupersetOfTheLegacyTree proves the collapse only added
// capability: the alias carries the canonical eleven verbs, which strictly
// contain the legacy eight.
func TestKMSDEK_IsASupersetOfTheLegacyTree(t *testing.T) {
	shim := newKMSDEKCommand()
	canonical := newDEKCommand()

	assert.Equal(t, verbNames(canonical), verbNames(shim),
		"the kms dek alias must expose exactly the canonical dek verb set")
	assert.Greater(t, len(verbNames(shim)), len(legacyKMSDEKVerbs),
		"the alias must be a strict superset of the retired eight-verb tree")

	// "generate" survives only as an alias of "create"; both must land on the
	// same command so scripts written against either name keep working.
	generate, _, err := shim.Find([]string{"generate"})
	require.NoError(t, err)
	create, _, err := shim.Find([]string{"create"})
	require.NoError(t, err)
	assert.Same(t, create, generate,
		"`generate` must be an alias of `create`, not a second implementation")
}

// TestKMSDEK_IsMarkedDeprecated pins the migration signal so the alias cannot
// silently become a permanent second surface.
func TestKMSDEK_IsMarkedDeprecated(t *testing.T) {
	shim := newKMSDEKCommand()
	require.NotEmpty(t, shim.Deprecated, "the kms dek alias must be marked deprecated")
	assert.Contains(t, shim.Deprecated, "quicbaas dek",
		"the deprecation notice must name the replacement command")

	assert.Empty(t, newDEKCommand().Deprecated,
		"the canonical dek tree must NOT be marked deprecated")
}

// TestKMSDEK_MountedUnderKMS confirms the alias is still reachable as
// `quicbaas kms dek` and that collapsing it left the rest of `kms` intact.
func TestKMSDEK_MountedUnderKMS(t *testing.T) {
	kms := newKMSCommand()

	dek, _, err := kms.Find([]string{"dek"})
	require.NoError(t, err)
	require.Equal(t, "dek", dek.Name())
	assert.NotEmpty(t, dek.Deprecated)

	// The sibling KMS verbs must be untouched by the collapse.
	for _, verb := range []string{"key", "encrypt", "decrypt"} {
		cmd, _, findErr := kms.Find([]string{verb})
		require.NoError(t, findErr, "kms lost its %q verb", verb)
		assert.Equal(t, verb, cmd.Name())
	}
}

// TestKMSDEK_UsesTheSDKFactory proves the alias routes through
// Security().DEK() and never through the retired gRPC-pinned factory. Swapping
// the SDK factory for a stub and observing that every verb reaches it is the
// direct evidence; a surviving gRPC path would bypass the stub entirely.
func TestKMSDEK_UsesTheSDKFactory(t *testing.T) {
	orig := sdkDEKClientFactory
	t.Cleanup(func() { sdkDEKClientFactory = orig })

	var calls int
	sdkDEKClientFactory = func() (quicbaas.DEKClient, error) {
		calls++
		return nil, errStubDEKFactory
	}

	// A slice keeps the order deterministic: the dek flag vars are
	// package-level, so a map's random order would make failures irreproducible.
	// set-safety validates its flags before dialing, hence the explicit limit.
	cases := []struct {
		verb string
		args []string
	}{
		{"generate", []string{"/tenants/acme"}},
		{"get", []string{"/tenants/acme"}},
		{"list", nil},
		{"rotate", []string{"/tenants/acme"}},
		{"delete", []string{"/tenants/acme"}},
		{"status", []string{"/tenants/acme"}},
		{"set-default", []string{"/tenants/acme"}},
		{"set-safety", []string{"/tenants/acme", "--max-operations", "1000"}},
	}

	for _, tc := range cases {
		verb, args := tc.verb, tc.args
		t.Run(verb, func(t *testing.T) {
			before := calls
			shim := newKMSDEKCommand()
			shim.SetArgs(append([]string{verb}, args...))
			shim.SetOut(io.Discard)
			shim.SetErr(io.Discard)

			// The stub factory always fails, so the command must error — the
			// point is that it errored *from the SDK factory*.
			err := shim.Execute()
			require.Error(t, err, "%s unexpectedly succeeded against a failing factory", verb)
			assert.Greater(t, calls, before,
				"%s did not go through sdkDEKClientFactory; a non-SDK path survives", verb)
		})
	}
}

// verbNames lists the immediate subcommand names of cmd, sorted by cobra's own
// ordering.
func verbNames(cmd *cobra.Command) []string {
	out := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		out = append(out, c.Name())
	}
	return out
}
