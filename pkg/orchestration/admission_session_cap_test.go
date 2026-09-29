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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// The provider axis reads the session cap and the expiry buffer
// =============================================================================

// cappedHeadroomRig is headroomRig with the live-session count wired, as the
// server wires it.
func cappedHeadroomRig(t *testing.T, sessions AccountSessionLookup) *headroomRig {
	t.Helper()
	rig := newHeadroomRig(t)
	reader, err := NewAccountPoolHeadroom(rig.accounts, NewQuotaAuthority(rig.ledger), WithHeadroomSessions(sessions))
	if err != nil {
		t.Fatalf("NewAccountPoolHeadroom: %v", err)
	}
	rig.reader = reader
	return rig
}

// cappedAccount creates a pooled account with a session cap.
func (r *headroomRig) cappedAccount(t *testing.T, id string, maxSessions int) {
	t.Helper()
	account := newTestAccount(admissionTenant, id, AccountProviderClaude)
	account.Status = AccountStatusActive
	account.MaxConcurrentSessions = maxSessions
	if err := r.accounts.Create(context.Background(), account); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
}

func TestAccountPoolHeadroom_APoolAtItsSessionCapHasNoHeadroom(t *testing.T) {
	rig := cappedHeadroomRig(t, &fakeSessionLookup{live: map[string]int{"claude1": 2, "claude2": 1}})
	rig.cappedAccount(t, "claude1", 2)
	rig.cappedAccount(t, "claude2", 1)

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC())
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.HasHeadroom() {
		t.Fatalf("headroom = %+v, want none: every account is serving its maximum", headroom)
	}
	if !strings.Contains(headroom.Reason, "2 are serving their maximum concurrent sessions") {
		t.Fatalf("Reason = %q, want it to say the caps are what is holding the pool", headroom.Reason)
	}
}

func TestAccountPoolHeadroom_CappedAccountsBoundTheSessionsLeft(t *testing.T) {
	rig := cappedHeadroomRig(t, &fakeSessionLookup{live: map[string]int{"claude1": 1, "claude2": 1}})
	rig.cappedAccount(t, "claude1", 3)
	rig.cappedAccount(t, "claude2", 1) // full

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC())
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Ready != 1 || !headroom.SessionBounded || headroom.SessionSlots != 2 {
		t.Fatalf("headroom = %+v, want one ready account with two free sessions", headroom)
	}
}

func TestAccountPoolHeadroom_AnUnmeteredAccountLeavesThePoolUnbounded(t *testing.T) {
	rig := cappedHeadroomRig(t, &fakeSessionLookup{live: map[string]int{"claude1": 0}})
	rig.cappedAccount(t, "claude1", 1)
	rig.account(t, "claude2", "", false)

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC())
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Ready != 2 || headroom.SessionBounded || headroom.SessionSlots != 0 {
		t.Fatalf("headroom = %+v, want two ready and no session bound", headroom)
	}
}

func TestAccountPoolHeadroom_AnExpiringStaticTokenIsNotReady(t *testing.T) {
	rig := cappedHeadroomRig(t, &fakeSessionLookup{})
	account := newTestAccount(admissionTenant, "claude1", AccountProviderClaude)
	account.Status = AccountStatusActive
	account.RefreshPolicy = AccountCredentialModeStaticToken
	account.CredentialExpiresAt = time.Now().UTC().Add(time.Minute)
	if err := rig.accounts.Create(context.Background(), account); err != nil {
		t.Fatalf("create: %v", err)
	}

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC())
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.HasHeadroom() {
		t.Fatalf("headroom = %+v, want none: the only credential expires inside the buffer", headroom)
	}
}

func TestAccountPoolHeadroom_AnUnreadableSessionCountFailsClosed(t *testing.T) {
	boom := errors.New("session store down")
	rig := cappedHeadroomRig(t, &fakeSessionLookup{err: boom})
	rig.cappedAccount(t, "claude1", 2)

	_, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC())
	var undecidable *AdmissionUndecidableError
	if !errors.As(err, &undecidable) || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want an undecidable admission wrapping the failure", err)
	}
}

// =============================================================================
// Admission: a capped pool bounds, and a full one defers
// =============================================================================

func TestPlan_ACappedPoolBoundsTheProjectToItsFreeSessions(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.headroom = &fakePoolHeadroom{byProject: map[string]PoolHeadroom{
		"quota":   {Pooled: 3, Ready: 1, SessionBounded: true, SessionSlots: 2},
		"noquota": {Pooled: 3, Ready: 1, SessionBounded: true, SessionSlots: 1},
	}}
	rig.project(t, admissionTenant, "quota", 5)
	rig.project(t, admissionTenant, "noquota", 0)
	rig.queued(admissionTenant, "quota", 5)
	rig.queued(admissionTenant, "noquota", 5)

	plan, err := rig.controller(t, 20).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for project, want := range map[string]int{"quota": 2, "noquota": 1} {
		decision := plan.For(admissionTenant, project)
		if decision.Deferred {
			t.Fatalf("%s: a pool with free sessions is bounded, not deferred", project)
		}
		if got := decision.DispatchSlots(); got != want {
			t.Fatalf("%s: DispatchSlots = %d, want the pool's %d free sessions", project, got, want)
		}
		if decision.Axis != AdmissionAxisProviderHeadroom {
			t.Fatalf("%s: axis = %s, want provider_headroom", project, decision.Axis)
		}
	}
}

func TestPlan_AFullPoolDefersLikeASpentOne(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.headroom = &fakePoolHeadroom{byProject: map[string]PoolHeadroom{
		"full": {Pooled: 3, Ready: 0, Reason: "3 are serving their maximum concurrent sessions"},
	}}
	rig.project(t, admissionTenant, "full", 5)
	rig.queued(admissionTenant, "full", 5)

	plan, err := rig.controller(t, 10).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	decision := plan.For(admissionTenant, "full")
	if !decision.Deferred || decision.DispatchSlots() != 0 {
		t.Fatalf("decision = %+v, want deferred with no slots", decision)
	}
	if !strings.Contains(decision.Reason, "maximum concurrent sessions") {
		t.Fatalf("Reason = %q, want the pool's own sentence", decision.Reason)
	}
}
