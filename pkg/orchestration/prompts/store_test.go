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

package prompts

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestStore_SaveCreatesVersionOne(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")

	if _, err := f.store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before save: %v, want ErrNotFound", err)
	}
	body := "Do the work. STOP: when done."
	record, err := f.store.Save(ctx, ref, edit(body, 0))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if record.Version != 1 || record.FollowParent || record.ContentHash != Hash(body) || len(record.Revisions) != 1 {
		t.Fatalf("created record = %+v", record)
	}
	got, err := f.store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	revision := got.Revisions[0]
	if got.Content != body || revision.Author != "alice" || revision.Note != "tune" || revision.Version != 1 {
		t.Fatalf("stored record = %+v", got)
	}
	if got.CreatedAt.IsZero() || !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Fatalf("timestamps not stamped: %+v", got)
	}
}

func TestStore_StaleVersionIsAConflictAndWritesNothing(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")
	if _, err := f.store.Save(ctx, ref, edit("v1 STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Save(ctx, ref, edit("v2 STOP:", 1)); err != nil {
		t.Fatal(err)
	}

	for _, expected := range []int64{0, 1, 7} {
		_, err := f.store.Save(ctx, ref, edit("late STOP:", expected))
		var conflict *VersionConflictError
		if !errors.As(err, &conflict) || conflict.Expected != expected {
			t.Fatalf("expected %d: err=%v, want VersionConflictError", expected, err)
		}
	}
	// Saving against a version on a record nobody created is a conflict too.
	if _, err := f.store.Save(ctx, charterRef(testTenant, testProject), edit("x STOP:", 3)); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("save v3 of an absent record: %v", err)
	}
	got, err := f.store.Get(ctx, ref)
	if err != nil || got.Version != 2 || got.Content != "v2 STOP:" {
		t.Fatalf("after conflicts: %+v, %v", got, err)
	}
}

func TestStore_HistoryIsBoundedAndDropsTheOldest(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")
	writes := MaxRevisions + 5
	for version := int64(0); version < int64(writes); version++ {
		if _, err := f.store.Save(ctx, ref, edit("body "+strconv.FormatInt(version, 10)+" STOP:", version)); err != nil {
			t.Fatalf("save %d: %v", version, err)
		}
	}
	history, err := f.store.History(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != MaxRevisions {
		t.Fatalf("history length = %d, want %d", len(history), MaxRevisions)
	}
	if history[0].Version != int64(writes-MaxRevisions+1) || history[len(history)-1].Version != int64(writes) {
		t.Fatalf("history spans v%d..v%d", history[0].Version, history[len(history)-1].Version)
	}
	// A dropped revision can no longer be rolled back to.
	_, err = f.store.Rollback(ctx, ref, 1, Stamp{Author: "alice", ExpectedVersion: int64(writes)})
	var unknown *UnknownRevisionError
	if !errors.As(err, &unknown) || unknown.Version != 1 {
		t.Fatalf("rollback to a dropped revision: %v", err)
	}
}

func TestStore_RollbackWritesTheOldBodyAsANewHeadThatNoLongerFollows(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")
	seeded := Change{Stamp: Stamp{Author: "seed", ExpectedVersion: 0}, Content: "seeded STOP:", FollowParent: true}
	if _, err := f.store.Save(ctx, ref, seeded); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Save(ctx, ref, edit("edited STOP:", 1)); err != nil {
		t.Fatal(err)
	}

	record, err := f.store.Rollback(ctx, ref, 1, Stamp{Author: "bob", Note: "undo", ExpectedVersion: 2})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if record.Version != 3 || record.ContentHash != Hash("seeded STOP:") || record.FollowParent {
		t.Fatalf("rolled-back head = %+v", record)
	}
	if head := record.Revisions[len(record.Revisions)-1]; head.Author != "bob" || head.Note != "undo" || len(record.Revisions) != 3 {
		t.Fatalf("rollback revision = %+v", record.Revisions)
	}
}

func TestStore_RollbackRefusesWhatItCannotRestore(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")

	if _, err := f.store.Rollback(ctx, ref, 1, Stamp{Author: "a", ExpectedVersion: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rollback of an absent record: %v", err)
	}
	if _, err := f.store.Save(ctx, ref, edit("v1 STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Rollback(ctx, ref, 9, Stamp{Author: "a", ExpectedVersion: 1}); !errors.Is(err, ErrUnknownRevision) {
		t.Fatalf("rollback to an unknown version: %v", err)
	}
	if _, err := f.store.Rollback(ctx, ref, 1, Stamp{Author: "a", ExpectedVersion: 4}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("rollback on a stale version: %v", err)
	}
	// A revision written before the spec grew a marker is refused by name.
	f.plant(t, ref, &Record{
		TenantID: testTenant, Set: testSet, Name: testCharter, Content: "fine STOP:", Version: 2,
		Revisions: []Revision{{Version: 1, Content: "no marker"}, {Version: 2, Content: "fine STOP:"}},
	})
	_, err := f.store.Rollback(ctx, ref, 1, Stamp{Author: "a", ExpectedVersion: 2})
	var missing *MissingMarkerError
	if !errors.As(err, &missing) || missing.Marker != "STOP:" {
		t.Fatalf("rollback to a body the spec refuses: %v", err)
	}
}

func TestStore_ResetFlipsFollowParentAndKeepsTheBody(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, testProject)
	if _, err := f.store.Reset(ctx, ref, Stamp{Author: "a", ExpectedVersion: 0}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset of an absent record: %v", err)
	}
	if _, err := f.store.Save(ctx, ref, edit("override STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	record, err := f.store.Reset(ctx, ref, Stamp{Author: "carol", Note: "back to tenant", ExpectedVersion: 1})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !record.FollowParent || record.Version != 2 || record.Content != "override STOP:" {
		t.Fatalf("reset record = %+v", record)
	}
	if _, err := f.store.Reset(ctx, ref, Stamp{Author: "carol", ExpectedVersion: 1}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("reset on a stale version: %v", err)
	}
}

func TestStore_SaveValidatesTheBodyAgainstItsSpec(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	_, err := f.store.Save(ctx, charterRef(testTenant, ""), edit("dropped the clause", 0))
	var missing *MissingMarkerError
	if !errors.As(err, &missing) || missing.Marker != "STOP:" || !strings.Contains(err.Error(), "STOP:") {
		t.Fatalf("save without the marker: %v", err)
	}
	note := Ref{TenantID: testTenant, Set: testSet, Name: testNote}
	if _, err := f.store.Save(ctx, note, edit("Mind ${typo}.", 0)); !errors.Is(err, ErrUnknownPlaceholder) {
		t.Fatalf("save with an undeclared placeholder: %v", err)
	}
	if _, err := f.store.Get(ctx, charterRef(testTenant, "")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused save wrote something: %v", err)
	}
}

func TestStore_RefusesBadInput(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	cases := map[string]struct {
		ref    Ref
		change Change
		want   error
	}{
		"no tenant":       {Ref{Set: testSet, Name: testCharter}, edit("x STOP:", 0), ErrInvalidInput},
		"reserved scope":  {charterRef(testTenant, tenantScope), edit("x STOP:", 0), ErrInvalidInput},
		"slash project":   {charterRef(testTenant, "a/b"), edit("x STOP:", 0), ErrInvalidInput},
		"dot name":        {Ref{TenantID: testTenant, Set: testSet, Name: ".."}, edit("x STOP:", 0), ErrInvalidInput},
		"unclean set":     {Ref{TenantID: testTenant, Set: "a/../b", Name: "x"}, edit("x STOP:", 0), ErrInvalidInput},
		"no author":       {charterRef(testTenant, ""), Change{Content: "x STOP:"}, ErrInvalidInput},
		"negative":        {charterRef(testTenant, ""), edit("x STOP:", -1), ErrInvalidInput},
		"unknown set":     {Ref{TenantID: testTenant, Set: "nope", Name: testCharter}, edit("x STOP:", 0), ErrUnknownSet},
		"unknown prompt":  {Ref{TenantID: testTenant, Set: testSet, Name: "nope"}, edit("x STOP:", 0), ErrUnknownPrompt},
		"empty name":      {Ref{TenantID: testTenant, Set: testSet}, edit("x STOP:", 0), ErrInvalidInput},
		"blank set":       {Ref{TenantID: testTenant, Name: testCharter}, edit("x STOP:", 0), ErrInvalidInput},
		"nul in project":  {charterRef(testTenant, "a\x00b"), edit("x STOP:", 0), ErrInvalidInput},
		"spaces tenantID": {charterRef("  ", ""), edit("x STOP:", 0), ErrInvalidInput},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.store.Save(ctx, tc.ref, tc.change); !errors.Is(err, tc.want) {
				t.Fatalf("Save: %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := f.store.Get(ctx, Ref{TenantID: testTenant, Set: "nope", Name: "x"}); !errors.Is(err, ErrUnknownSet) {
		t.Fatalf("Get of an unknown set: %v", err)
	}
	if err := f.store.Delete(ctx, Ref{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Delete of an empty ref: %v", err)
	}
	if _, err := f.store.List(ctx, "", "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("List without a tenant: %v", err)
	}
	if _, err := NewStore(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewStore(nil): %v", err)
	}
}

func TestStore_ACollidingSlotIsRefusedNotOverwritten(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")
	// Stand for another identity whose digest landed on this ref's slot.
	f.plant(t, ref, &Record{TenantID: testTenant, ProjectID: "other", Set: testSet, Name: testNote, Content: "theirs", Version: 1})

	_, err := f.store.Save(ctx, ref, edit("mine STOP:", 0))
	var collision *CollisionError
	if !errors.As(err, &collision) || collision.Held.ProjectID != "other" || collision.Held.Name != testNote {
		t.Fatalf("save into a held slot: %v", err)
	}
	if _, err := f.store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get of a collided slot: %v", err)
	}
	if err := f.store.Delete(ctx, ref); err != nil {
		t.Fatalf("delete of a collided slot must leave it alone: %v", err)
	}
	records, err := f.store.List(ctx, testTenant, "other", "")
	if err != nil || len(records) != 1 || records[0].Content != "theirs" {
		t.Fatalf("the other identity's record = %+v, %v", records, err)
	}
}

func TestStore_SealedStoreIsABackendError(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, "")
	f.resolver.fail(errors.New("barrier sealed"))

	checks := map[string]error{}
	_, checks["Get"] = f.store.Get(ctx, ref)
	_, checks["Save"] = f.store.Save(ctx, ref, edit("x STOP:", 0))
	_, checks["Rollback"] = f.store.Rollback(ctx, ref, 1, Stamp{Author: "a", ExpectedVersion: 1})
	_, checks["Reset"] = f.store.Reset(ctx, ref, Stamp{Author: "a", ExpectedVersion: 1})
	_, checks["History"] = f.store.History(ctx, ref)
	_, checks["List"] = f.store.List(ctx, testTenant, "", testSet)
	checks["Delete"] = f.store.Delete(ctx, ref)
	for op, err := range checks {
		var backend *BackendError
		if !errors.As(err, &backend) || !errors.Is(err, ErrBackend) || !strings.Contains(err.Error(), "barrier sealed") {
			t.Fatalf("%s on a sealed store: %v", op, err)
		}
	}
}

func TestStore_ListAndDeleteAddressOneScope(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	tenantRef := charterRef(testTenant, "")
	projectRef := charterRef(testTenant, testProject)
	noteRef := Ref{TenantID: testTenant, Set: testSet, Name: testNote}
	for _, ref := range []Ref{tenantRef, projectRef, noteRef} {
		if _, err := f.store.Save(ctx, ref, edit("Mind it. STOP:", 0)); err != nil {
			t.Fatal(err)
		}
	}
	tenantWide, err := f.store.List(ctx, testTenant, "", testSet)
	if err != nil || len(tenantWide) != 2 {
		t.Fatalf("tenant-wide list = %d, %v", len(tenantWide), err)
	}
	project, err := f.store.List(ctx, testTenant, testProject, "")
	if err != nil || len(project) != 1 || project[0].ProjectID != testProject || project[0].ID != projectRef.entityID() {
		t.Fatalf("project list = %+v, %v", project, err)
	}
	if other, err := f.store.List(ctx, "tenant-b", "", ""); err != nil || len(other) != 0 {
		t.Fatalf("another tenant's list = %+v, %v", other, err)
	}

	if err := f.store.Delete(ctx, projectRef); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := f.store.Delete(ctx, projectRef); err != nil {
		t.Fatalf("Delete of an absent record must be a no-op: %v", err)
	}
	if _, err := f.store.Get(ctx, projectRef); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := f.store.Get(ctx, tenantRef); err != nil {
		t.Fatalf("deleting the project record touched the tenant's: %v", err)
	}
}

func TestStore_AuditsIdentifiersOnly(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	ref := charterRef(testTenant, testProject)
	secret := "a body nobody should find in the audit trail STOP:"
	if _, err := f.store.Save(ctx, ref, edit(secret, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Save(ctx, ref, edit(secret, 0)); !errors.Is(err, ErrVersionConflict) {
		t.Fatal(err)
	}
	if _, err := f.store.Rollback(ctx, ref, 1, Stamp{Author: "a", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Reset(ctx, ref, Stamp{Author: "a", ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	events := f.auditor.recorded()
	wantActions := []string{AuditActionCreate, AuditActionCreate, AuditActionRollback, AuditActionReset, AuditActionDelete}
	wantSuccess := []bool{true, false, true, true, true}
	if len(events) != len(wantActions) {
		t.Fatalf("events = %+v", events)
	}
	for i, event := range events {
		if event.Action != wantActions[i] || event.Success != wantSuccess[i] {
			t.Fatalf("event %d = %+v", i, event)
		}
		if event.Resource != AuditResource || event.TenantID != testTenant ||
			event.ResourceID != testProject+"/"+string(testSet)+"/"+testCharter {
			t.Fatalf("event %d identifiers = %+v", i, event)
		}
		if strings.Contains(event.ResourceID, secret) {
			t.Fatal("the audit trail carries prompt content")
		}
	}
}

func TestRef_ScopeAndString(t *testing.T) {
	cases := map[Ref]struct {
		scope Scope
		text  string
	}{
		charterRef(testTenant, ""):          {ScopeTenant, "tenant-a/_tenant/store_test/charter"},
		charterRef(testTenant, testProject): {ScopeProject, "tenant-a/proj-1/store_test/charter"},
		charterRef(systemTenant(), ""):      {ScopeSystem, systemTenant() + "/_tenant/store_test/charter"},
	}
	for ref, want := range cases {
		if ref.Scope() != want.scope || ref.String() != want.text {
			t.Fatalf("%+v: scope=%s string=%s", ref, ref.Scope(), ref.String())
		}
	}
	if charterRef(testTenant, "").entityID() == charterRef(testTenant, testProject).entityID() {
		t.Fatal("the tenant-wide and project records share a slot")
	}
}
