// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Two daemons enrolled on one host: the records the adapter must group.
func twoDaemonsOneHost() []*FleetNode {
	return []*FleetNode{
		{ID: "n2", TenantID: "tenant-a", Host: "h1", Capabilities: []string{"gpu", "docker"}},
		{ID: "n1", TenantID: "tenant-a", Host: "h1", Capabilities: []string{"docker"}},
		{ID: "n3", TenantID: "tenant-a", Host: "h0"},
	}
}

func TestFleetInventory_GroupsRecordsByHostWithTheUnionOfCapabilities(t *testing.T) {
	t.Parallel()

	registry := &staticFleetRegistry{byTenant: map[string][]*FleetNode{"tenant-a": twoDaemonsOneHost()}}
	inventory, err := NewFleetInventory(registry)
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	hosts, err := inventory.PlacementHosts(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("PlacementHosts: %v", err)
	}
	want := []PlacementHost{
		{Host: "h0", NodeIDs: []string{"n3"}},
		{Host: "h1", Capabilities: []string{"docker", "gpu"}, NodeIDs: []string{"n1", "n2"}},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %+v, want %+v", hosts, want)
	}
	if _, err := IndexPlacementHosts(hosts); err != nil {
		t.Fatalf("the adapter's inventory must index: %v", err)
	}

	empty, err := inventory.PlacementHosts(context.Background(), "tenant-b")
	if err != nil || len(empty) != 0 {
		t.Fatalf("other tenant = %+v, %v; want none", empty, err)
	}
}

// The control: the same records taken one-for-one, without the adapter's
// grouping, describe h1 twice, and the index refuses them.
func TestIndexPlacementHosts_RefusesOneHostDescribedTwice(t *testing.T) {
	t.Parallel()

	var oneForOne []PlacementHost
	for _, node := range twoDaemonsOneHost() {
		oneForOne = append(oneForOne, PlacementHost{Host: node.Host, Capabilities: node.Capabilities, NodeIDs: []string{node.ID}})
	}
	_, err := IndexPlacementHosts(oneForOne)
	var ambiguous *PlacementInventoryAmbiguousError
	if !errors.As(err, &ambiguous) || ambiguous.Host != "h1" || !errors.Is(err, ErrPlacementInventoryAmbiguous) {
		t.Fatalf("IndexPlacementHosts = %v, want h1 refused as ambiguous", err)
	}

	if _, err := IndexPlacementHosts([]PlacementHost{{NodeIDs: []string{"n9"}}}); !errors.Is(err, ErrPlacementInventoryInvalid) {
		t.Fatalf("hostless entry = %v, want ErrPlacementInventoryInvalid", err)
	}
}

func TestFleetInventory_RefusesARecordThatNamesNoHost(t *testing.T) {
	t.Parallel()

	nodes := append(twoDaemonsOneHost(), &FleetNode{ID: "n4", TenantID: "tenant-a", Host: " "})
	inventory, err := NewFleetInventory(&staticFleetRegistry{byTenant: map[string][]*FleetNode{"tenant-a": nodes}})
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	_, err = inventory.PlacementHosts(context.Background(), "tenant-a")
	var invalid *PlacementInventoryInvalidError
	if !errors.As(err, &invalid) || invalid.NodeID != "n4" || invalid.TenantID != "tenant-a" {
		t.Fatalf("PlacementHosts = %v, want n4 refused as hostless", err)
	}
}

func TestFleetInventory_RegistryErrorsAndNilRegistry(t *testing.T) {
	t.Parallel()

	if _, err := NewFleetInventory(nil); !errors.Is(err, ErrFleetInventoryNoRegistry) {
		t.Fatalf("NewFleetInventory(nil) = %v, want ErrFleetInventoryNoRegistry", err)
	}
	boom := errors.New("registry down")
	inventory, err := NewFleetInventory(&erroringFleetRegistry{err: boom})
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	if _, err := inventory.PlacementHosts(context.Background(), "tenant-a"); !errors.Is(err, boom) {
		t.Fatalf("PlacementHosts = %v, want the registry's error", err)
	}
}
