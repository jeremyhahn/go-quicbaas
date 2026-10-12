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
	"testing"
)

const testBuild = "abc123"

func newTestSeeder(t *testing.T, store *Store) *Seeder {
	t.Helper()
	seeder, err := NewSeeder(store, testBuild)
	if err != nil {
		t.Fatalf("NewSeeder: %v", err)
	}
	return seeder
}

func noteRef(tenantID string) Ref {
	return Ref{TenantID: tenantID, Set: testSet, Name: testNote}
}

func TestNewSeeder_RefusesMissingInputs(t *testing.T) {
	if _, err := NewSeeder(nil, testBuild); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil store: %v", err)
	}
	if _, err := NewSeeder(newStoreFixture(t).store, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank build: %v", err)
	}
}

func TestSeeder_SeedTenantCopiesTheSystemSetAsFollowing(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	if _, err := f.store.Save(ctx, charterRef(systemTenant(), ""), edit("system STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	if err := newTestSeeder(t, f.store).SeedTenant(ctx, testTenant); err != nil {
		t.Fatalf("SeedTenant: %v", err)
	}

	want := map[Ref]string{
		charterRef(testTenant, ""): "system STOP:",
		noteRef(testTenant):        storeTestSpec().Prompts[1].Default,
	}
	for ref, body := range want {
		record, err := f.store.Get(ctx, ref)
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if record.Content != body || !record.FollowParent || record.Version != 1 || record.Revisions[0].Author != SeedAuthor {
			t.Fatalf("%s seeded as %+v", ref, record)
		}
	}
	// A following copy reads through to the system set.
	set, err := NewResolver(f.store).Resolve(ctx, testTenant, "", testSet)
	if err != nil {
		t.Fatal(err)
	}
	if ref := refFor(t, set, testCharter); ref.Scope != ScopeSystem {
		t.Fatalf("seeded tenant resolved from %+v", ref)
	}
}

func TestSeeder_SeedTenantIsIdempotentAndLeavesEditsAlone(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	seeder := newTestSeeder(t, f.store)
	if err := seeder.SeedTenant(ctx, testTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Save(ctx, charterRef(testTenant, ""), edit("mine STOP:", 1)); err != nil {
		t.Fatal(err)
	}

	report, err := seeder.Reconcile(ctx, testTenant)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if report.Changed() {
		t.Fatalf("a seeded tenant was written again: %+v", report)
	}
	record, err := f.store.Get(ctx, charterRef(testTenant, ""))
	if err != nil {
		t.Fatal(err)
	}
	if record.Content != "mine STOP:" || record.FollowParent || record.Version != 2 {
		t.Fatalf("the tenant's edit was touched: %+v", record)
	}
}

func TestSeeder_SeedsOnlyWhatATenantLacks(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	if _, err := f.store.Save(ctx, charterRef(testTenant, ""), edit("mine STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	report, err := newTestSeeder(t, f.store).Reconcile(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Created) != 1 || report.Created[0] != noteRef(testTenant) || len(report.Updated) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestSeeder_AFailedSeedDeletesItsOwnWrites(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	// Another prompt holds the note's slot, so the seed fails on its second
	// prompt after writing the first.
	f.plant(t, noteRef(testTenant), &Record{TenantID: testTenant, Set: testSet, Name: "other", Version: 1})

	err := newTestSeeder(t, f.store).SeedTenant(ctx, testTenant)
	var seedErr *SeedError
	if !errors.As(err, &seedErr) || !errors.Is(err, ErrCollision) || seedErr.Prompt != testNote {
		t.Fatalf("SeedTenant: %v", err)
	}
	if _, err := f.store.Get(ctx, charterRef(testTenant, "")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the charter the failed seed wrote survived: %v", err)
	}
}

func TestSeeder_SeedTenantSkipsTheSystemTenant(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	seeder := newTestSeeder(t, f.store)
	for _, tenantID := range []string{systemTenant(), ""} {
		if err := seeder.SeedTenant(ctx, tenantID); err != nil {
			t.Fatalf("SeedTenant(%q): %v", tenantID, err)
		}
	}
	if _, err := f.store.Get(ctx, charterRef(systemTenant(), "")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant seeding wrote the system set: %v", err)
	}
}

func TestSeeder_ReconcileSystemFollowsTheBuild(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	report, err := newTestSeeder(t, f.store).Reconcile(ctx, systemTenant())
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if len(report.Created) != 2 || len(report.Updated) != 0 {
		t.Fatalf("first boot report = %+v", report)
	}
	record, err := f.store.Get(ctx, charterRef(systemTenant(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != 1 || !record.FollowParent || record.Revisions[0].Author != "build "+testBuild {
		t.Fatalf("first boot wrote %+v", record)
	}

	// The same build again writes nothing.
	report, err = newTestSeeder(t, f.store).Reconcile(ctx, systemTenant())
	if err != nil || report.Changed() {
		t.Fatalf("unchanged build: report=%+v err=%v", report, err)
	}

	// A build whose charter default changed rewrites only the charter.
	changed := storeTestSpec()
	changed.Prompts[0].Default = "Work it. STOP: when done."
	next := NewRegistry()
	if _, err := next.Register(changed); err != nil {
		t.Fatal(err)
	}
	nextStore, err := NewStore(f.resolver, WithRegistry(next))
	if err != nil {
		t.Fatal(err)
	}
	report, err = newTestSeeder(t, nextStore).Reconcile(ctx, systemTenant())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Updated) != 1 || report.Updated[0] != charterRef(systemTenant(), "") || len(report.Created) != 0 {
		t.Fatalf("new build report = %+v", report)
	}
	record, err = nextStore.Get(ctx, charterRef(systemTenant(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != 2 || record.Content != changed.Prompts[0].Default || !record.FollowParent {
		t.Fatalf("new build wrote %+v", record)
	}
}

func TestSeeder_ReconcileSystemLeavesAnEditAlone(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	if _, err := f.store.Save(ctx, charterRef(systemTenant(), ""), edit("operator STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	report, err := newTestSeeder(t, f.store).Reconcile(ctx, systemTenant())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Created) != 1 || report.Created[0] != noteRef(systemTenant()) {
		t.Fatalf("report = %+v", report)
	}
	record, err := f.store.Get(ctx, charterRef(systemTenant(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if record.Content != "operator STOP:" || record.Version != 1 {
		t.Fatalf("the reconcile overwrote an edit: %+v", record)
	}
}

func TestSeeder_StoreFailuresAreSeedErrors(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	seeder := newTestSeeder(t, f.store)
	f.resolver.fail(errors.New("barrier sealed"))
	for _, tenantID := range []string{systemTenant(), testTenant} {
		if _, err := seeder.Reconcile(ctx, tenantID); !errors.Is(err, ErrSeed) || !errors.Is(err, ErrBackend) {
			t.Fatalf("Reconcile(%s) on a sealed store: %v", tenantID, err)
		}
	}
}
