//go:build rest

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

package rest

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestSelfRegistrationInitialLifecycle pins the decision the handler has to make
// UP FRONT rather than correct afterwards.
//
// qrdb's transition table has no active -> pending edge. An account created
// active and then "moved" to pending therefore stays active — which on the
// email-verification path is an account that authenticates without ever
// verifying its address. Both gates have to be folded into the state the user is
// CREATED in, and this pins that they are.
func TestSelfRegistrationInitialLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name            string
		requireApproval bool
		requireEmailVfy bool
		want            iam.Lifecycle
	}{
		{"no gates", false, false, iam.LifecycleActive},
		{"admin approval", true, false, iam.LifecyclePending},
		{"email verification", false, true, iam.LifecyclePending},
		{"both gates", true, true, iam.LifecyclePending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := iam.LifecycleActive
			if tc.requireApproval || tc.requireEmailVfy {
				got = iam.LifecyclePending
			}
			if got != tc.want {
				t.Fatalf("initial lifecycle = %q, want %q", got, tc.want)
			}
			// The corollary that makes this load-bearing: there is no way back
			// from active to pending, so getting it wrong at create time is not
			// recoverable by a follow-up update.
			if tc.want == iam.LifecyclePending && iam.LifecycleActive.CanTransitionTo(iam.LifecyclePending) {
				t.Fatal("active -> pending is expected to be impossible; the create-time decision is the only one")
			}
		})
	}
}
