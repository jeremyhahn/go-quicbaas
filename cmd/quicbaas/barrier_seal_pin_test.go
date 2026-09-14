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
// agreement about --so-pin (#491).
//
// The two had drifted apart and nothing noticed. The help said "Operators with
// a PINBackend configured MUST supply --so-pin" and carried an example passing
// one, while runBarrierSealCommand discarded the value at `_ = barrierSOPin`,
// because the SDK's BarrierSeal takes no request and there is nowhere to put
// it. An operator who followed the help put a Security Officer PIN into
// /proc/<pid>/cmdline — readable by every local process, captured by shell
// history and process accounting — and got nothing for it.
//
// Neither half was wrong on its own. Keeping the FLAG for surface parity with
// the canonical CLI tree is a deliberate choice the constructor's doc comment
// makes and defends. Discarding the value is honest given the SDK signature.
// It was the COMBINATION that instructed an exposure buying nothing, and a
// per-half test could not see it: one would have asserted the flag exists (it
// does) and the other that seal seals (it does).
//
// So the assertion is the RELATIONSHIP, and it is a biconditional:
//
//	the help solicits an SO PIN  <=>  BarrierSeal can carry one
//
// Both directions matter. Left-to-right is today's defect. Right-to-left is
// what makes finishing the plumbing safe later: the moment BarrierSeal gains a
// request bearing SOPin, this test fails and tells whoever did it to restore
// the instruction to the help. That is the half a "don't say MUST" assertion
// would have got wrong, by freezing the softened wording forever.

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// sealCarriesSOPin reports whether the BarrierSeal method on iface has
// anywhere to put a Security Officer PIN: an argument, or a pointer to one,
// whose struct has an SOPin field.
//
// The signature is the mechanism, not a proxy for it. The value is discarded
// today because `BarrierSeal(ctx) error` offers no destination, so asking the
// type system is asking the actual question — and it updates itself, with no
// list for anyone to maintain, the moment the interface changes.
//
// iface is a parameter rather than hard-coded so the probe can be exercised
// against a request-bearing signature (see the test below). A probe that has
// only ever returned false is not evidence of anything.
func sealCarriesSOPin(iface reflect.Type) bool {
	m, ok := iface.MethodByName("BarrierSeal")
	if !ok {
		return false
	}
	// An interface method's Type carries no receiver, so every In is a real
	// argument.
	for i := 0; i < m.Type.NumIn(); i++ {
		in := m.Type.In(i)
		for in.Kind() == reflect.Pointer {
			in = in.Elem()
		}
		if in.Kind() != reflect.Struct {
			continue
		}
		if _, found := in.FieldByName("SOPin"); found {
			return true
		}
	}
	return false
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
		// A copy-pasteable invocation passing the flag.
		if strings.Contains(l, "quicbaas barrier seal") && strings.Contains(l, "--so-pin") {
			return true
		}
	}
	return false
}

// sealPINMismatch returns the operator-facing explanation when the help and
// the behaviour disagree, or "" when they agree.
//
// It is a pure function of the two booleans so the decision itself can be
// proved on all four combinations. That matters because only ONE of the four
// is reachable from the code as it stands: BarrierSeal takes no request, so
// `carries` is false today and no mutation of this repository can make it
// true — the signature lives on the SDK's Barrier() surface, and changing it
// is the cross-repo work of #491 option B. Left as an inline switch, the
// branch that protects option B would be unreachable and unproven, which is
// the same "it compiles, so presumably it works" that let the original
// disagreement survive.
func sealPINMismatch(carries, solicits bool) string {
	switch {
	case solicits && !carries:
		return "`barrier seal` help asks the operator for --so-pin, but BarrierSeal takes no " +
			"request bearing SOPin, so runBarrierSealCommand discards it. Following the help puts a " +
			"Security Officer PIN in /proc/<pid>/cmdline for no benefit (#491). Either soften the help " +
			"or finish the plumbing — do not leave them disagreeing."
	case carries && !solicits:
		return "BarrierSeal now carries an SOPin, so the value an operator supplies is finally used — " +
			"but `barrier seal` help still tells them to leave --so-pin off, which is now wrong and will " +
			"cost them a failed seal. Restore the instruction to the long help and the flag usage " +
			"(#491 option B). The softened wording was correct only while the value was discarded."
	default:
		return ""
	}
}

// TestBarrierSeal_HelpAndBehaviourAgreeOnTheSOPin is the #491 invariant.
func TestBarrierSeal_HelpAndBehaviourAgreeOnTheSOPin(t *testing.T) {
	carries := sealCarriesSOPin(reflect.TypeOf((*BarrierClient)(nil)).Elem())
	solicits := sealHelpSolicitsSOPin(newBarrierSealCommand())

	if msg := sealPINMismatch(carries, solicits); msg != "" {
		t.Error(msg)
	}
}

// TestSealPINMismatch_BothDirections proves the decision on all four
// combinations, including the two the current code cannot reach.
//
// The right-to-left case is the one worth spelling out: it is what makes
// option B safe to land. Whoever gives BarrierSeal a request bearing SOPin
// gets a failing test naming the help they now have to restore, instead of
// shipping a verb that silently needs a PIN its own help tells operators to
// leave off.
func TestSealPINMismatch_BothDirections(t *testing.T) {
	assert.Empty(t, sealPINMismatch(false, false),
		"discarded value, help stays quiet: this is today, and it agrees")
	assert.Empty(t, sealPINMismatch(true, true),
		"value used, help asks for it: this is option B done properly, and it agrees")

	assert.Contains(t, sealPINMismatch(false, true), "for no benefit",
		"help solicits a PIN nothing can carry -- the #491 defect")
	assert.Contains(t, sealPINMismatch(true, false), "Restore the instruction",
		"BarrierSeal can carry a PIN but the help says to leave it off -- option B, half-landed")
}

// TestSealCarriesSOPin_DetectsARequestBearingSignature proves the probe can
// return true. Without this, the biconditional above is half-tested: a probe
// that always returned false would make the "restore the instruction" branch
// unreachable and the softened help permanent by accident.
func TestSealCarriesSOPin_DetectsARequestBearingSignature(t *testing.T) {
	assert.True(t, sealCarriesSOPin(reflect.TypeOf((*sealWithPIN)(nil)).Elem()),
		"probe missed an SOPin field on the request argument")
	assert.False(t, sealCarriesSOPin(reflect.TypeOf((*sealWithoutPIN)(nil)).Elem()),
		"probe found an SO PIN in a request that has no such field")
	assert.False(t, sealCarriesSOPin(reflect.TypeOf((*sealNoRequest)(nil)).Elem()),
		"probe found an SO PIN in a signature with no request at all")
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
			long: "Examples:\n  quicbaas barrier seal --so-pin SO123 --assume-yes",
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

// The three stand-in interfaces below exist only to exercise sealCarriesSOPin.
// They mirror the shapes BarrierSeal could plausibly take.

// sealPINRequest is a request that carries an SO PIN, as a request-bearing
// BarrierSeal would.
type sealPINRequest struct{ SOPin string }

// sealPINlessRequest is a request that exists but has nowhere to put a PIN —
// the case a NumIn() > 1 check alone would get wrong.
type sealPINlessRequest struct{ Reason string }

type sealWithPIN interface {
	BarrierSeal(ctx context.Context, req *sealPINRequest) error
}

type sealWithoutPIN interface {
	BarrierSeal(ctx context.Context, req *sealPINlessRequest) error
}

type sealNoRequest interface {
	BarrierSeal(ctx context.Context) error
}
