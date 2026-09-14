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

package sdlc

import (
	"errors"
	"strings"
	"testing"
)

// TestDiscoveryMode_AdmitsTheRightPaths walks the whole matrix. It is the
// contract both triggers read, so every cell is stated here rather than
// inferred at either call site.
func TestDiscoveryMode_AdmitsTheRightPaths(t *testing.T) {
	for name, tc := range map[string]struct {
		mode            DiscoveryMode
		delivery, sweep bool
		effective       DiscoveryMode
	}{
		"the zero value is both":               {"", true, true, DiscoveryBoth},
		"whitespace is the default":            {"   ", true, true, DiscoveryBoth},
		"both admits both":                     {DiscoveryBoth, true, true, DiscoveryBoth},
		"poll admits only the sweep":           {DiscoveryPoll, false, true, DiscoveryPoll},
		"webhook admits only the delivery":     {DiscoveryWebhook, true, false, DiscoveryWebhook},
		"an operator's capitals still resolve": {"Poll", false, true, DiscoveryPoll},
		"a padded value still resolves":        {" webhook ", true, false, DiscoveryWebhook},
		// An unknown mode admits NOTHING. It is refused at bind time, and if
		// one ever reached a trigger it must not silently behave like the
		// default: a project would then be discovered by a path its operator
		// did not name.
		"an unknown mode admits nothing": {"gossip", false, false, "gossip"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.mode.Effective(); got != tc.effective {
				t.Fatalf("Effective() = %q, want %q", got, tc.effective)
			}
			if got := tc.mode.Admits(DiscoveryPathDelivery); got != tc.delivery {
				t.Fatalf("Admits(delivery) = %v, want %v", got, tc.delivery)
			}
			if got := tc.mode.Admits(DiscoveryPathSweep); got != tc.sweep {
				t.Fatalf("Admits(sweep) = %v, want %v", got, tc.sweep)
			}
			if got := tc.mode.String(); got != string(tc.effective) {
				t.Fatalf("String() = %q, want %q", got, tc.effective)
			}
		})
	}

	// A path nothing declares is admitted by no mode, so a future caller that
	// invents one is refused rather than silently allowed.
	for _, mode := range []DiscoveryMode{DiscoveryBoth, DiscoveryPoll, DiscoveryWebhook} {
		if mode.Admits(DiscoveryPath("carrier-pigeon")) {
			t.Fatalf("%s admitted an undeclared path", mode)
		}
	}
}

// TestDiscoveryMode_Validate proves the three modes are accepted, the empty
// one is the default rather than an error, and anything else is refused with
// a message naming what is allowed.
func TestDiscoveryMode_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		mode DiscoveryMode
		ok   bool
	}{
		"empty takes the default":    {"", true},
		"both":                       {DiscoveryBoth, true},
		"poll":                       {DiscoveryPoll, true},
		"webhook":                    {DiscoveryWebhook, true},
		"capitals are normalised":    {"BOTH", true},
		"a typo is refused":          {"webook", false},
		"a mode from another system": {"push", false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.mode.Validate()
			if (err == nil) != tc.ok {
				t.Fatalf("Validate() = %v, want ok=%v", err, tc.ok)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("refusal is not an invalid-input error: %v", err)
			}
			for _, want := range []string{string(DiscoveryPoll), string(DiscoveryWebhook), string(DiscoveryBoth)} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("%q does not name %q", err.Error(), want)
				}
			}
		})
	}
}

// TestProjectBinding_ValidatesItsDiscoveryMode proves the refusal happens
// where an operator is watching — at bind time — rather than as a project
// that looks configured and discovers nothing.
func TestProjectBinding_ValidatesItsDiscoveryMode(t *testing.T) {
	binding := testBinding()
	if err := binding.Validate(); err != nil {
		t.Fatalf("the default binding names no mode and must be valid: %v", err)
	}
	for _, mode := range []DiscoveryMode{DiscoveryBoth, DiscoveryPoll, DiscoveryWebhook} {
		binding.Discovery = mode
		if err := binding.Validate(); err != nil {
			t.Fatalf("%s was refused: %v", mode, err)
		}
	}
	binding.Discovery = "whenever"
	err := binding.Validate()
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unknown mode was bound: %v", err)
	}
}
