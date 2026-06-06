//go:build iam

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

package iam

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newAuditEvent builds a minimal valid audit event for the given tenant.
func newAuditEvent(id, tenantID string, ts time.Time) *AuditEvent {
	return &AuditEvent{
		ID:        id,
		TenantID:  tenantID,
		Timestamp: ts,
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "auth.login",
		Outcome:   AuditOutcomeSuccess,
		Actor:     &AuditActor{UserID: "user-1"},
	}
}

// TestBackendAuditStorage_PersistAndRetrieve verifies the happy path: an event
// saved through the KV-backed store is retrievable by ID and surfaces through a
// tenant-scoped query.
func TestBackendAuditStorage_PersistAndRetrieve(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendAuditStorage(kv)
	ctx := context.Background()

	ev := newAuditEvent("evt-1", "acme", time.Now().UTC())
	if err := store.SaveEvent(ctx, ev); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}

	got, err := store.GetEvent(ctx, "acme", "evt-1")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.ID != "evt-1" || got.TenantID != "acme" {
		t.Fatalf("GetEvent returned %+v, want id=evt-1 tenant=acme", got)
	}
	if got.Action != "auth.login" || got.Outcome != AuditOutcomeSuccess {
		t.Fatalf("GetEvent lost schema fields: %+v", got)
	}

	events, total, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "acme", Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if total != 1 || len(events) != 1 || events[0].ID != "evt-1" {
		t.Fatalf("QueryEvents = %d events (total %d), want the one saved event", len(events), total)
	}
}

// TestBackendAuditStorage_TenantIsolation verifies an event written under
// tenant A is NOT visible from tenant B's store — neither by direct GetEvent
// nor by a tenant-scoped query — because each tenant routes to its own barrier.
func TestBackendAuditStorage_TenantIsolation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("tenant-a")
	kv.barrierTenant("tenant-b")
	store := NewBackendAuditStorage(kv)
	ctx := context.Background()

	if err := store.SaveEvent(ctx, newAuditEvent("evt-a", "tenant-a", time.Now().UTC())); err != nil {
		t.Fatalf("SaveEvent tenant-a: %v", err)
	}

	// Tenant B cannot read tenant A's event by id.
	if _, err := store.GetEvent(ctx, "tenant-b", "evt-a"); !errors.Is(err, ErrAuditEventNotFound) {
		t.Fatalf("GetEvent across tenants = %v, want ErrAuditEventNotFound", err)
	}

	// Tenant B's query returns nothing.
	eventsB, totalB, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "tenant-b", Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents tenant-b: %v", err)
	}
	if totalB != 0 || len(eventsB) != 0 {
		t.Fatalf("tenant-b saw %d events (total %d), want 0 — tenant isolation breached", len(eventsB), totalB)
	}

	// Tenant A still sees its own event.
	eventsA, totalA, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "tenant-a", Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents tenant-a: %v", err)
	}
	if totalA != 1 || len(eventsA) != 1 || eventsA[0].ID != "evt-a" {
		t.Fatalf("tenant-a query = %d events (total %d), want its single event", len(eventsA), totalA)
	}
}

// TestBackendAuditStorage_QueryFilterAndPagination verifies the filter
// predicates, newest-first ordering, and pagination match the documented
// semantics.
func TestBackendAuditStorage_QueryFilterAndPagination(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendAuditStorage(kv)
	ctx := context.Background()

	base := time.Now().UTC()
	// Three success events at increasing times and one failure event.
	for i, ts := range []time.Time{base, base.Add(time.Minute), base.Add(2 * time.Minute)} {
		ev := newAuditEvent("ok-"+string(rune('a'+i)), "acme", ts)
		if err := store.SaveEvent(ctx, ev); err != nil {
			t.Fatalf("SaveEvent ok-%d: %v", i, err)
		}
	}
	fail := newAuditEvent("fail-1", "acme", base.Add(3*time.Minute))
	fail.Outcome = AuditOutcomeFailure
	if err := store.SaveEvent(ctx, fail); err != nil {
		t.Fatalf("SaveEvent fail-1: %v", err)
	}

	// Filter by outcome: only the failure event matches.
	failures, total, err := store.QueryEvents(ctx, &AuditFilter{
		TenantID: "acme",
		Outcomes: []AuditOutcome{AuditOutcomeFailure},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("QueryEvents outcome filter: %v", err)
	}
	if total != 1 || len(failures) != 1 || failures[0].ID != "fail-1" {
		t.Fatalf("outcome filter = %+v (total %d), want only fail-1", failures, total)
	}

	// Newest-first ordering across all four events.
	all, total, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "acme", Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents all: %v", err)
	}
	if total != 4 || len(all) != 4 {
		t.Fatalf("QueryEvents all = %d (total %d), want 4", len(all), total)
	}
	if all[0].ID != "fail-1" {
		t.Fatalf("newest-first ordering broken: head = %s, want fail-1", all[0].ID)
	}

	// Pagination: limit 2, offset 2 returns the two oldest of the four.
	page, total, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "acme", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("QueryEvents page: %v", err)
	}
	if total != 4 || len(page) != 2 {
		t.Fatalf("pagination = %d (total %d), want 2 of 4", len(page), total)
	}
}

// TestBackendAuditStorage_DeleteEventsBefore verifies retention pruning removes
// only events older than the cutoff and is scoped to the named tenant.
func TestBackendAuditStorage_DeleteEventsBefore(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendAuditStorage(kv)
	ctx := context.Background()

	now := time.Now().UTC()
	old := newAuditEvent("old", "acme", now.Add(-48*time.Hour))
	fresh := newAuditEvent("fresh", "acme", now)
	if err := store.SaveEvent(ctx, old); err != nil {
		t.Fatalf("SaveEvent old: %v", err)
	}
	if err := store.SaveEvent(ctx, fresh); err != nil {
		t.Fatalf("SaveEvent fresh: %v", err)
	}

	deleted, err := store.DeleteEventsBefore(ctx, "acme", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteEventsBefore: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteEventsBefore deleted %d, want 1", deleted)
	}
	if _, err := store.GetEvent(ctx, "acme", "old"); !errors.Is(err, ErrAuditEventNotFound) {
		t.Fatalf("old event still present after prune: %v", err)
	}
	if _, err := store.GetEvent(ctx, "acme", "fresh"); err != nil {
		t.Fatalf("fresh event wrongly pruned: %v", err)
	}
}

// TestBackendAuditStorage_SaveEventValidation verifies invalid events are
// rejected with the validation error and never written.
func TestBackendAuditStorage_SaveEventValidation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendAuditStorage(kv)
	ctx := context.Background()

	// Missing TenantID fails Validate.
	bad := newAuditEvent("bad", "", time.Now().UTC())
	if err := store.SaveEvent(ctx, bad); err == nil {
		t.Fatal("SaveEvent with empty tenant ID must fail validation")
	}

	// Sealed tenant fails closed: SaveEvent propagates the resolve error and
	// never falls back to a shared store.
	kv.sealTenant("acme")
	if err := store.SaveEvent(ctx, newAuditEvent("evt", "acme", time.Now().UTC())); err == nil {
		t.Fatal("SaveEvent into a sealed tenant must fail closed")
	}
}

// TestBackendAuditStorage_GetEventNotFound verifies a missing id returns the
// typed not-found error.
func TestBackendAuditStorage_GetEventNotFound(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendAuditStorage(kv)

	if _, err := store.GetEvent(context.Background(), "acme", "nope"); !errors.Is(err, ErrAuditEventNotFound) {
		t.Fatalf("GetEvent missing = %v, want ErrAuditEventNotFound", err)
	}
}
