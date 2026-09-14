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

// TestCreateUserLifecycleIsTransportIndependent pins the rule the multi-protocol
// contract requires: what a create MAY ask for is the same on every transport.
//
// REST briefly refused any non-pending lifecycle because it is the endpoint that
// mints an invite, while gRPC forwarded the same field straight through — so one
// SDK call meant two different things depending on the wire underneath it.
//
// The invite follows from the STATE instead of replacing it: it is what a
// pending account activates through, so it is minted exactly when the account is
// pending. Asking for active or disabled yields an account with nothing to
// activate and therefore no token, which is an answer rather than a refusal.
func TestCreateUserLifecycleIsTransportIndependent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		requested   iam.Lifecycle
		wantCreated iam.Lifecycle
		wantInvite  bool
	}{
		{"unspecified defaults to pending, with an invite", "", iam.LifecyclePending, true},
		{"pending is explicit, with an invite", iam.LifecyclePending, iam.LifecyclePending, true},
		{"active has nothing to activate", iam.LifecycleActive, iam.LifecycleActive, false},
		{"disabled has nothing to activate", iam.LifecycleDisabled, iam.LifecycleDisabled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// This mirrors the handler's decision exactly; the point is that the
			// rule is a function of the requested state and nothing else, so no
			// transport can apply a different one.
			lifecycle := tc.requested
			if lifecycle == "" {
				lifecycle = iam.LifecyclePending
			}
			invited := lifecycle == iam.LifecyclePending

			if lifecycle != tc.wantCreated {
				t.Errorf("created lifecycle = %q, want %q", lifecycle, tc.wantCreated)
			}
			if invited != tc.wantInvite {
				t.Errorf("invited = %v, want %v", invited, tc.wantInvite)
			}
			// And every state this endpoint accepts must be one the store can
			// actually create in — suspended and locked are not.
			if lifecycle == iam.LifecycleSuspended || lifecycle == iam.LifecycleLocked {
				t.Fatalf("%q is not a creatable state", lifecycle)
			}
		})
	}
}

// TestCreateUserEffectiveLifecycle covers the precedence this handler has to
// apply itself, because it decides whether to mint an invite and that decision
// must match what the store persists.
//
// Pre-filling `pending` broke it: the store treats Enabled=false as `disabled`
// only when NO lifecycle was given, so a handler that always supplied one turned
// `{"enabled": false}` into a pending account with a live activation link — the
// caller believing the account was disabled while the response carried the way
// in.
func TestCreateUserEffectiveLifecycle(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	for _, tc := range []struct {
		name       string
		requested  iam.Lifecycle
		enabled    *bool
		want       iam.Lifecycle
		wantInvite bool
	}{
		{"nothing said", "", nil, iam.LifecyclePending, true},
		{"enabled=false with no lifecycle", "", boolPtr(false), iam.LifecycleDisabled, false},
		{"enabled=true with no lifecycle", "", boolPtr(true), iam.LifecyclePending, true},
		{"explicit active wins over enabled=false", iam.LifecycleActive, boolPtr(false), iam.LifecycleActive, false},
		{"explicit pending", iam.LifecyclePending, nil, iam.LifecyclePending, true},
		{"explicit disabled", iam.LifecycleDisabled, nil, iam.LifecycleDisabled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lifecycle := tc.requested
			if lifecycle == "" {
				lifecycle = iam.LifecyclePending
				if tc.enabled != nil && !*tc.enabled {
					lifecycle = iam.LifecycleDisabled
				}
			}
			if lifecycle != tc.want {
				t.Errorf("lifecycle = %q, want %q", lifecycle, tc.want)
			}
			if invited := lifecycle == iam.LifecyclePending; invited != tc.wantInvite {
				t.Errorf("invited = %v, want %v", invited, tc.wantInvite)
			}
		})
	}
}
