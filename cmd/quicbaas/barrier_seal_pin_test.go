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

// barrier_seal_pin_test.go keeps `barrier seal`'s HELP and its BEHAVIOUR in
// agreement about the Security Officer PIN (#491, #946).
//
// The two had drifted apart and nothing noticed. The help said "Operators with
// a PINBackend configured MUST supply --so-pin" and carried an example passing
// one, while runBarrierSealCommand discarded the value. An operator who
// followed the help put a Security Officer PIN into /proc/<pid>/cmdline —
// readable by every local process, captured by shell history and process
// accounting — and got nothing for it.
//
// #946 finished the plumbing: the verb now binds the PIN to the call context
// with the SDK's WithSOPIN, and the server verifies it before sealing. So the
// assertion is the RELATIONSHIP, and it is a biconditional:
//
//	the help solicits an SO PIN  <=>  the seal call receives one
//
// "Receives" is observed, not inferred: the probe runs the verb against a
// fake client and reads the PIN off the context BarrierSeal is called with.
// The PIN rides the context rather than a request argument, so a signature
// probe would answer the wrong question.
//
// A separate invariant holds whatever the plumbing does: no worked example
// puts the PIN on argv. The help solicits --so-pin-stdin, never --so-pin.

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// sealForwardsSOPin reports whether running cmd with the PIN on stdin
// delivers that PIN to the context BarrierSeal is called with.
//
// newCmd is a parameter so the probe can be exercised against a command
// that drops the PIN (see the test below). A probe that has only ever
// returned true is not evidence of anything.
func sealForwardsSOPin(t *testing.T, newCmd func() *cobra.Command) bool {
	t.Helper()
	t.Setenv(barrierSOPINEnv, "")
	defer resetBarrierFlags()

	const probePIN = "probe-pin"
	var got string
	fc := &fakeBarrierClient{sealFunc: func(ctx context.Context) error {
		pin, ok, err := qrdbtransport.SOPINFromContext(ctx)
		if err != nil {
			return err
		}
		if ok {
			got = string(pin)
		}
		return nil
	}}
	defer withFakeBarrier(fc)()

	cmd := newCmd()
	cmd.SetArgs([]string{"--so-pin-stdin", "--assume-yes"})
	cmd.SetIn(strings.NewReader(probePIN + "\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())
	return got == probePIN
}

// sealHelpSolicitsSOPin reports whether the command's help asks the operator
// to supply an SO PIN — either by calling it mandatory, or by demonstrating it
// in a worked example. Both are solicitations: an example is an instruction
// with the imperative left off.
//
// Only lines that mention the flag are considered, so unrelated prose about
// some other flag being required cannot trip it.
func sealHelpSolicitsSOPin(cmd *cobra.Command) bool {
	lines := strings.Split(cmd.Long, "\n")
	if f := cmd.Flags().Lookup("so-pin"); f != nil {
		lines = append(lines, f.Usage)
	}
	for _, line := range lines {
		l := strings.ToLower(line)
		if !strings.Contains(l, "so-pin") {
			continue
		}
		if strings.Contains(l, "must") || strings.Contains(l, "required") {
			return true
		}
		// A copy-pasteable invocation passing the PIN.
		if strings.Contains(l, "quicbaas barrier seal") && strings.Contains(l, "--so-pin") {
			return true
		}
	}
	return false
}

// sealHelpPutsSOPinOnArgv reports whether a worked example in the help
// passes the PIN as an argv value (`--so-pin X` or `--so-pin=X`), which
// would put it in the process list (#491).
func sealHelpPutsSOPinOnArgv(cmd *cobra.Command) bool {
	for _, line := range strings.Split(cmd.Long, "\n") {
		if !strings.Contains(line, "quicbaas barrier seal") {
			continue
		}
		if strings.Contains(line, "--so-pin ") || strings.Contains(line, "--so-pin=") {
			return true
		}
	}
	return false
}

// sealPINMismatch returns the operator-facing explanation when the help and
// the behaviour disagree, or "" when they agree.
//
// It is a pure function of the two booleans so the decision itself can be
// proved on all four combinations, including the two the current code
// cannot reach.
func sealPINMismatch(forwards, solicits bool) string {
	switch {
	case solicits && !forwards:
		return "`barrier seal` help asks the operator for an SO PIN, but the verb does not forward " +
			"it to the seal call. Following the help exposes a Security Officer PIN for no " +
			"benefit (#491). Either soften the help or restore the plumbing — do not leave them disagreeing."
	case forwards && !solicits:
		return "`barrier seal` forwards the SO PIN, so the server's PIN backend checks it — " +
			"but the help does not ask for it, which will cost operators a refused seal. Restore " +
			"the instruction to the long help (#946)."
	default:
		return ""
	}
}

// TestBarrierSeal_HelpAndBehaviourAgreeOnTheSOPin is the #491 invariant.
func TestBarrierSeal_HelpAndBehaviourAgreeOnTheSOPin(t *testing.T) {
	forwards := sealForwardsSOPin(t, newBarrierSealCommand)
	solicits := sealHelpSolicitsSOPin(newBarrierSealCommand())

	assert.True(t, forwards, "barrier seal must forward the SO PIN (#946)")
	if msg := sealPINMismatch(forwards, solicits); msg != "" {
		t.Error(msg)
	}
}

// TestBarrierSeal_HelpNeverPutsTheSOPinOnArgv keeps the worked examples on
// the stdin and environment paths.
func TestBarrierSeal_HelpNeverPutsTheSOPinOnArgv(t *testing.T) {
	assert.False(t, sealHelpPutsSOPinOnArgv(newBarrierSealCommand()),
		"a `barrier seal` example passes the SO PIN on argv; use --so-pin-stdin (#491)")
}

// TestSealHelpPutsSOPinOnArgv_DetectsBothForms proves the argv predicate
// flips, so the invariant above cannot pass vacuously.
func TestSealHelpPutsSOPinOnArgv_DetectsBothForms(t *testing.T) {
	cases := []struct {
		name string
		long string
		want bool
	}{
		{name: "space-separated value", long: "  quicbaas barrier seal --so-pin SO123 -y", want: true},
		{name: "equals value", long: "  quicbaas barrier seal --so-pin=SO123 -y", want: true},
		{name: "stdin flag", long: "  printf '%s\\n' \"$PIN\" | quicbaas barrier seal --so-pin-stdin -y", want: false},
		{name: "prose about the flag", long: "--so-pin is visible in the process list.", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "seal", Long: tc.long}
			assert.Equal(t, tc.want, sealHelpPutsSOPinOnArgv(cmd))
		})
	}
}

// TestSealPINMismatch_BothDirections proves the decision on all four
// combinations.
func TestSealPINMismatch_BothDirections(t *testing.T) {
	assert.Empty(t, sealPINMismatch(false, false),
		"PIN dropped, help stays quiet: consistent")
	assert.Empty(t, sealPINMismatch(true, true),
		"PIN forwarded, help asks for it: this is #946, and it agrees")

	assert.Contains(t, sealPINMismatch(false, true), "for no benefit",
		"help solicits a PIN the verb drops -- the #491 defect")
	assert.Contains(t, sealPINMismatch(true, false), "Restore the instruction",
		"PIN forwarded but the help does not ask for it")
}

// TestSealForwardsSOPin_DetectsADroppedPIN proves the probe can return
// false. Without this, the biconditional above is half-tested.
func TestSealForwardsSOPin_DetectsADroppedPIN(t *testing.T) {
	dropsPIN := func() *cobra.Command {
		cmd := newBarrierSealCommand()
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			client, err := barrierClientFactory()
			if err != nil {
				return err
			}
			return client.BarrierSeal(context.Background())
		}
		return cmd
	}
	assert.False(t, sealForwardsSOPin(t, dropsPIN),
		"probe reported a PIN the command never bound")
}

// TestSealHelpSolicitsSOPin_DetectsBothSolicitationShapes proves the other
// predicate flips too, on the exact wording #491 was filed about.
func TestSealHelpSolicitsSOPin_DetectsBothSolicitationShapes(t *testing.T) {
	cases := []struct {
		name string
		long string
		want bool
	}{
		{
			name: "the wording this issue was filed about",
			long: "Operators with a PINBackend configured MUST supply --so-pin.",
			want: true,
		},
		{
			name: "a worked example is an instruction too",
			long: "Examples:\n  printf '%s\\n' \"$PIN\" | quicbaas barrier seal --so-pin-stdin --assume-yes",
			want: true,
		},
		{
			name: "saying it is not transmitted is not a solicitation",
			long: "--so-pin is accepted for parity but this verb does not transmit it; leave it off.",
			want: false,
		},
		{
			name: "another flag being required does not count",
			long: "--assume-yes is required in scripted runbooks.",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "seal", Long: tc.long}
			assert.Equal(t, tc.want, sealHelpSolicitsSOPin(cmd))
		})
	}
}
