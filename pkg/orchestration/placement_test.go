// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

func validPlacementSpec() *PlacementSpec {
	return &PlacementSpec{Name: "reviews", Role: "review", HostCount: 5}
}

func TestPlacementSpec_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		edit  func(*PlacementSpec)
		field string
	}{
		{"host count", func(*PlacementSpec) {}, ""},
		{"named hosts", func(s *PlacementSpec) { s.HostCount = 0; s.Hosts = []string{"h1", "h2"} }, ""},
		{"capabilities only", func(s *PlacementSpec) { s.HostCount = 0; s.RequiredCapabilities = []string{"docker"} }, ""},
		{"profile", func(s *PlacementSpec) { s.Profile = &FleetJobProfile{CPUCores: 3.5, MemoryBytes: 4 << 30} }, ""},
		{"empty name", func(s *PlacementSpec) { s.Name = "" }, "name"},
		{"name with separator", func(s *PlacementSpec) { s.Name = "a/b" }, "name"},
		{"empty role", func(s *PlacementSpec) { s.Role = " " }, "role"},
		{"negative count", func(s *PlacementSpec) { s.HostCount = -1 }, "host_count"},
		{"count and hosts", func(s *PlacementSpec) { s.Hosts = []string{"h1"} }, "host_count"},
		{"no target", func(s *PlacementSpec) { s.HostCount = 0 }, "host_count"},
		{"empty host", func(s *PlacementSpec) { s.HostCount = 0; s.Hosts = []string{"h1", ""} }, "hosts[1]"},
		{"repeated host", func(s *PlacementSpec) { s.HostCount = 0; s.Hosts = []string{"h1", "h1"} }, "hosts[1]"},
		{"repeated capability", func(s *PlacementSpec) { s.RequiredCapabilities = []string{"gpu", "gpu"} }, "required_capabilities[1]"},
		{"zero-core profile", func(s *PlacementSpec) { s.Profile = &FleetJobProfile{MemoryBytes: 1} }, "profile"},
		{"zero-memory profile", func(s *PlacementSpec) { s.Profile = &FleetJobProfile{CPUCores: 1} }, "profile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := validPlacementSpec()
			tc.edit(spec)
			err := spec.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			var invalid *PlacementSpecInvalidError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrPlacementSpecInvalid) {
				t.Fatalf("Validate() = %v, want *PlacementSpecInvalidError", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("Field = %q, want %q (%v)", invalid.Field, tc.field, err)
			}
		})
	}

	var nilSpec *PlacementSpec
	if err := nilSpec.Validate(); !errors.Is(err, ErrPlacementSpecInvalid) {
		t.Fatalf("nil spec: Validate() = %v, want ErrPlacementSpecInvalid", err)
	}
}

func newTestPlacementStore(t *testing.T) PlacementStore {
	t.Helper()
	store, err := NewPlacementStore(NewMemoryTaskKV())
	if err != nil {
		t.Fatalf("NewPlacementStore: %v", err)
	}
	return store
}

func TestNewPlacementStore_RefusesAStoreThatCannotGuardAWrite(t *testing.T) {
	t.Parallel()

	for name, kv := range map[string]interfaces.KVStore{"nil": nil, "plain memory": kvstore.NewMemory()} {
		_, err := NewPlacementStore(kv)
		var unguarded *PlacementStoreUnguardedError
		if !errors.As(err, &unguarded) || !errors.Is(err, ErrPlacementStoreUnguarded) {
			t.Fatalf("%s: NewPlacementStore = %v, want *PlacementStoreUnguardedError", name, err)
		}
	}
}

func TestPlacementStore_DeclareGetListWithdraw(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)

	created, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Generation != 1 || created.TenantID != "tenant-a" {
		t.Fatalf("create stored generation %d tenant %q, want 1 and tenant-a", created.Generation, created.TenantID)
	}

	edit := created.clone()
	edit.HostCount = 6
	edited, err := store.DeclarePlacement(ctx, "tenant-a", edit)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if edited.Generation != 2 || edited.HostCount != 6 {
		t.Fatalf("edit stored generation %d count %d, want 2 and 6", edited.Generation, edited.HostCount)
	}

	got, err := store.GetPlacement(ctx, "tenant-a", "reviews")
	if err != nil || !reflect.DeepEqual(got, edited) {
		t.Fatalf("get = %+v, %v; want %+v", got, err, edited)
	}

	agents := &PlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1", "h2"}}
	if _, err := store.DeclarePlacement(ctx, "tenant-a", agents); err != nil {
		t.Fatalf("create agents: %v", err)
	}
	list, err := store.ListPlacements(ctx, "tenant-a")
	if err != nil || len(list) != 2 || list[0].Name != "agents" || list[1].Name != "reviews" {
		t.Fatalf("list = %+v, %v; want agents then reviews", list, err)
	}

	if err := store.WithdrawPlacement(ctx, "tenant-a", "reviews"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	var notFound *PlacementNotFoundError
	if _, err := store.GetPlacement(ctx, "tenant-a", "reviews"); !errors.As(err, &notFound) {
		t.Fatalf("get after withdraw = %v, want *PlacementNotFoundError", err)
	}
	if err := store.WithdrawPlacement(ctx, "tenant-a", "reviews"); !errors.Is(err, ErrPlacementNotFound) {
		t.Fatalf("second withdraw = %v, want ErrPlacementNotFound", err)
	}
}

func TestPlacementStore_RefusesAnInvalidOrMisnamedSpec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)

	invalid := validPlacementSpec()
	invalid.Hosts = []string{"h1"}
	if _, err := store.DeclarePlacement(ctx, "tenant-a", invalid); !errors.Is(err, ErrPlacementSpecInvalid) {
		t.Fatalf("invalid declare = %v, want ErrPlacementSpecInvalid", err)
	}

	foreign := validPlacementSpec()
	foreign.TenantID = "tenant-b"
	var refused *PlacementSpecInvalidError
	if _, err := store.DeclarePlacement(ctx, "tenant-a", foreign); !errors.As(err, &refused) || refused.Field != "tenant_id" {
		t.Fatalf("foreign-tenant declare = %v, want tenant_id refusal", err)
	}
}

func TestPlacementStore_TenantsAreIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)

	if _, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec()); err != nil {
		t.Fatalf("create in a: %v", err)
	}
	if _, err := store.GetPlacement(ctx, "tenant-b", "reviews"); !errors.Is(err, ErrPlacementNotFound) {
		t.Fatalf("get from b = %v, want ErrPlacementNotFound", err)
	}
	if list, err := store.ListPlacements(ctx, "tenant-b"); err != nil || len(list) != 0 {
		t.Fatalf("list in b = %+v, %v; want empty", list, err)
	}
	if err := store.WithdrawPlacement(ctx, "tenant-b", "reviews"); !errors.Is(err, ErrPlacementNotFound) {
		t.Fatalf("withdraw from b = %v, want ErrPlacementNotFound", err)
	}
	// The same name in another tenant is its own spec, created at generation 1.
	created, err := store.DeclarePlacement(ctx, "tenant-b", validPlacementSpec())
	if err != nil || created.Generation != 1 {
		t.Fatalf("create in b = %+v, %v; want generation 1", created, err)
	}
}

func TestPlacementStore_CreateOfAnExistingNameIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)

	if _, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec()); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec())
	var stale *PlacementSpecStaleError
	if !errors.As(err, &stale) || stale.Read != 0 || stale.Current != 1 {
		t.Fatalf("second create = %v, want stale (read 0, current 1)", err)
	}
}

// Racing edits of one generation: exactly one is stored, and every other is
// refused with the generation that won, never silently overwritten.
func TestPlacementStore_RacingEditsLoseNone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)

	created, err := store.DeclarePlacement(ctx, "tenant-a", validPlacementSpec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const writers = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []*PlacementSpec
		stale   int
		other   []error
	)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(count int) {
			defer wg.Done()
			edit := created.clone()
			edit.HostCount = count
			<-start
			stored, err := store.DeclarePlacement(ctx, "tenant-a", edit)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, stored)
			case errors.Is(err, ErrPlacementSpecStale):
				stale++
			default:
				other = append(other, err)
			}
		}(10 + i)
	}
	close(start)
	wg.Wait()

	if len(other) != 0 || len(winners) != 1 || stale != writers-1 {
		t.Fatalf("winners %d stale %d other %v; want 1, %d, none", len(winners), stale, other, writers-1)
	}
	got, err := store.GetPlacement(ctx, "tenant-a", "reviews")
	if err != nil || got.Generation != 2 || got.HostCount != winners[0].HostCount {
		t.Fatalf("stored = %+v, %v; want the winner %+v", got, err, winners[0])
	}

	// An edit still written against generation 1 is refused, naming 2.
	late := created.clone()
	late.HostCount = 99
	var refused *PlacementSpecStaleError
	if _, err := store.DeclarePlacement(ctx, "tenant-a", late); !errors.As(err, &refused) || refused.Read != 1 || refused.Current != 2 {
		t.Fatalf("late edit = %v, want stale (read 1, current 2)", err)
	}
}

func TestPlacementStore_ReturnedSpecsAreCopies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)

	spec := &PlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"},
		Profile: &FleetJobProfile{CPUCores: 0.02, MemoryBytes: 3 << 30}}
	created, err := store.DeclarePlacement(ctx, "tenant-a", spec)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	spec.Hosts[0] = "mutated"
	spec.Profile.CPUCores = 64
	if created.Hosts[0] != "h1" || created.Profile.CPUCores != 0.02 {
		t.Fatalf("returned spec shares the caller's memory: %+v", created)
	}
}
