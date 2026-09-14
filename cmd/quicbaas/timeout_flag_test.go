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

// timeout_flag_test.go pins the `--timeout` binding for the WHOLE command
// tree (#246).
//
// The defect was not that `barrier` forgot a flag. There were two package
// variables spelled almost the same — `globalTimeout`, which the root
// `--timeout` writes, and `timeout`, which seven command groups re-registered
// as their own persistent `--timeout` — and every verb outside those seven
// groups read `timeout` while the operator's value went into `globalTimeout`.
// Nothing bridged them, so on a `barrier` verb the effective deadline was
// whichever default cobra wrote into `timeout` last during root assembly: an
// emergent property of `rootCmd.AddCommand` ordering, not a decision.
//
// Adding the flag to `barrier` by hand would have fixed one verb and left the
// next one to be written with the same hole. There is now ONE variable bound
// ONCE, on the root, as a persistent flag: every command in the tree inherits
// it by construction and a new verb cannot miss it. TestTimeoutFlagIsBoundOnce
// is the ratchet that keeps that true — it walks the tree and fails on any
// command whose `--timeout` resolves to a different variable than the root's.
//
// Three commands deliberately shadow the root flag with a per-command budget
// of their own, each with its OWN variable. They are enumerated rather than
// pattern-matched, so adding a fourth is a decision someone makes here rather
// than a side effect of a flag registration nobody reviewed.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// perCommandTimeoutOverrides are the commands whose `--timeout` is a
// deliberate per-command budget rather than the root command timeout. Each
// binds its own variable; none of them may bind the root's.
var perCommandTimeoutOverrides = map[string]string{
	"quicbaas vector admin compact": "synchronous compaction runs far longer than a request; own default 5m",
	"quicbaas scheduler job create": "the timeout of the scheduled JOB, not of the CLI call",
	"quicbaas secrets watch":        "how long to keep watching; 0 means until Ctrl+C",
}

// TestTimeoutFlagIsBoundOnce walks the command tree and asserts that every
// command's effective `--timeout` resolves to the single root binding, except
// for the enumerated per-command overrides.
//
// Identity, not equality, is the assertion, and assert.Same rather than
// assert.Equal is load-bearing. pflag stores the ADDRESS of the variable a
// flag was bound to inside its Value, and testify's Equal follows
// reflect.DeepEqual straight through that pointer: two distinct
// *pflag.durationValue both holding 30s compare equal, which is precisely the
// pair the defect consisted of. Same compares the pointers themselves, so a
// second variable is caught even when its default is identical.
func TestTimeoutFlagIsBoundOnce(t *testing.T) {
	rootFlag := rootCmd.PersistentFlags().Lookup("timeout")
	require.NotNil(t, rootFlag, "the root command must own a persistent --timeout")

	seenOverrides := map[string]bool{}

	walkCommands(rootCmd, func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		f := cmd.Flag("timeout")
		require.NotNil(t, f, "%s: --timeout must be reachable; the root flag is persistent so every command inherits it", path)

		if _, ok := perCommandTimeoutOverrides[path]; ok {
			seenOverrides[path] = true
			assert.NotSame(t, rootFlag.Value, f.Value,
				"%s is listed as a per-command --timeout override but binds the root's variable; drop it from perCommandTimeoutOverrides", path)
			return
		}

		assert.Same(t, rootFlag.Value, f.Value,
			"%s binds --timeout to a variable other than the root's. Every verb reads globalTimeout, "+
				"so a second binding means the operator's --timeout is silently discarded on this command (#246). "+
				"Delete the registration and let the root persistent flag serve, or add the command to "+
				"perCommandTimeoutOverrides with its own variable if the override is deliberate.", path)
	})

	for path := range perCommandTimeoutOverrides {
		assert.True(t, seenOverrides[path],
			"%s is listed as a per-command --timeout override but is not in the command tree; the list has rotted", path)
	}
}

// TestRootTimeoutFlagReachesEveryVerb asserts the operator-visible half of the
// same property: the value typed on the command line is the value the verb
// reads. It parses through the real tree rather than setting globalTimeout
// directly, because the binding is what broke, not the read.
func TestRootTimeoutFlagReachesEveryVerb(t *testing.T) {
	const budget = 1234 * time.Millisecond

	// Verbs from groups that never bound --timeout, which is the population
	// the defect covered.
	for _, args := range [][]string{
		{"barrier", "status"},
		{"kms", "list-keys"},
		{"iam", "user", "list"},
		{"crypto", "sign"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			defer restoreGlobalTimeout(t)()

			globalTimeout = 0
			cmd, flags, err := rootCmd.Find(append(args, "--timeout", budget.String()))
			require.NoError(t, err)
			require.NoError(t, cmd.ParseFlags(flags))

			assert.Equal(t, budget, globalTimeout,
				"%s: --timeout did not reach the variable every verb reads", cmd.CommandPath())
		})
	}
}

// TestBarrierUnsealHonoursTheTimeoutFlag is the issue's own reproduction,
// bounded: a stalled writer on the passphrase pipe must abandon the read at
// the operator's --timeout rather than at whatever default a sibling group's
// flag registration wrote last.
//
// The assertion is the elapsed time, not merely that an error came back. With
// the defect restored the read is bounded by 30s, so the goroutine below is
// still blocked when the 5s guard fires and the test fails HERE, on the
// deadline this test owns.
func TestBarrierUnsealHonoursTheTimeoutFlag(t *testing.T) {
	const budget = 250 * time.Millisecond

	defer restoreGlobalTimeout(t)()
	defer resetBarrierFlags()

	cmd, flags, err := rootCmd.Find([]string{
		"barrier", "unseal", "--passphrase-stdin", "--timeout", budget.String(),
	})
	require.NoError(t, err)
	require.NoError(t, cmd.ParseFlags(flags))
	defer func() {
		require.NoError(t, cmd.Flags().Set(passphraseStdinFlag, "false"))
		cmd.SetIn(nil)
	}()

	// A writer that delivers half a passphrase and then stalls: the case
	// where a read in flight is holding plaintext and only the deadline ends
	// it.
	cmd.SetIn(&blockingReader{partial: []byte("half-a-pass")})

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- runBarrierUnsealCommand(cmd, nil) }()

	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("barrier unseal is still blocked on the passphrase read 5s after --timeout=%s; "+
			"the flag is not reaching the read (#246)", budget)
	}

	elapsed := time.Since(start)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrTimeout) || strings.Contains(err.Error(), "timed out"),
		"expected a timeout error, got %v", err)
	assert.GreaterOrEqual(t, elapsed, budget,
		"returned before the operator's --timeout elapsed, so the budget in force is not the one typed")
	assert.Less(t, elapsed, 5*time.Second,
		"the read outlived --timeout=%s by more than an order of magnitude", budget)
}

// restoreGlobalTimeout snapshots globalTimeout and returns the restorer. The
// tests here parse flags through the shared rootCmd, which writes the package
// variable every other test reads.
func restoreGlobalTimeout(t *testing.T) func() {
	t.Helper()
	orig := globalTimeout
	return func() { globalTimeout = orig }
}

// walkCommands visits cmd and every command beneath it.
func walkCommands(cmd *cobra.Command, fn func(*cobra.Command)) {
	fn(cmd)
	for _, child := range cmd.Commands() {
		walkCommands(child, fn)
	}
}
