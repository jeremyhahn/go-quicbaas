// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"encoding/json"
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

func TestFleetInventory_RefusesMalformedRegistryRecords(t *testing.T) {
	t.Parallel()

	foreign, err := json.Marshal(&FleetNode{
		ID: "foreign", TenantID: "tenant-b", Name: "foreign",
		Host: "host-2", NodeType: "claude-code", Backend: "acp",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	cases := []struct {
		name   string
		key    string
		value  []byte
		wantID string
	}{
		{name: "only good records"},
		{name: "undecodable", key: "orch/agent/tenant-a/corrupt", value: []byte("}{"), wantID: "corrupt"},
		{name: "foreign tenant", key: "orch/agent/tenant-a/foreign", value: foreign, wantID: "foreign"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			kv := NewMemoryTaskKV()
			registry := NewFleetRegistry(kv)
			seedAgent(t, registry, "tenant-a", "good-1")
			if tc.key != "" {
				if err := kv.Put(ctx, tc.key, tc.value); err != nil {
					t.Fatalf("Put: %v", err)
				}
			}
			inventory, err := NewFleetInventory(registry)
			if err != nil {
				t.Fatalf("NewFleetInventory: %v", err)
			}
			hosts, err := inventory.PlacementHosts(ctx, "tenant-a")
			if tc.wantID == "" {
				if err != nil || len(hosts) != 1 || hosts[0].Host != "host-1" {
					t.Fatalf("PlacementHosts = %v, %v, want one host host-1", hosts, err)
				}
				return
			}
			if !errors.Is(err, ErrPlacementInventoryInvalid) {
				t.Fatalf("PlacementHosts = %v, want ErrPlacementInventoryInvalid", err)
			}
			var invalid *PlacementInventoryInvalidError
			if !errors.As(err, &invalid) || invalid.TenantID != "tenant-a" || invalid.NodeID != tc.wantID || invalid.Reason == "" {
				t.Fatalf("PlacementHosts = %#v, want %q refused with a reason", err, tc.wantID)
			}

			listed, err := registry.ListFleetNodes(ctx, "tenant-a", nil)
			if err != nil || len(listed) != 1 || listed[0].ID != "good-1" {
				t.Fatalf("ListFleetNodes = %v, %v, want only good-1", listed, err)
			}
		})
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

func TestPlacementHostIdentity(t *testing.T) {
	t.Parallel()
	for address, want := range map[string]string{
		"worker:9091":    "worker",
		" worker:9091 ":  "worker",
		"worker":         "worker",
		"10.0.0.7:9091":  "10.0.0.7",
		"[fd00::1]:9091": "fd00::1",
		"[fd00::1]":      "fd00::1",
		"fd00::1":        "fd00::1",
		":9091":          "",
		"":               "",
	} {
		if got := PlacementHostIdentity(address); got != want {
			t.Errorf("PlacementHostIdentity(%q) = %q, want %q", address, got, want)
		}
	}
}

// A record's Host is the daemon's dialable host:port. On 8faa2dbae1 the
// inventory grouped on it as-is, so worker:9091 and worker:9092 were two
// hosts; they are one.
func TestFleetInventory_GroupsDaemonEndpointsByHost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	nodes := []*FleetNode{
		{ID: "n1", TenantID: "tenant-a", Host: "worker:9091", Capabilities: []string{"docker"}},
		{ID: "n2", TenantID: "tenant-a", Host: "worker:9092", Capabilities: []string{"gpu"}},
		{ID: "n3", TenantID: "tenant-a", Host: "10.0.0.7:9091"},
		{ID: "n4", TenantID: "tenant-a", Host: "[fd00::1]:9091"},
	}
	inventory, err := NewFleetInventory(&staticFleetRegistry{byTenant: map[string][]*FleetNode{"tenant-a": nodes}})
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	got, err := inventory.PlacementHosts(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("PlacementHosts: %v", err)
	}
	want := []PlacementHost{
		{Host: "10.0.0.7", NodeIDs: []string{"n3"}},
		{Host: "fd00::1", NodeIDs: []string{"n4"}},
		{Host: "worker", Capabilities: []string{"docker", "gpu"}, NodeIDs: []string{"n1", "n2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlacementHosts = %+v, want %+v", got, want)
	}
	if _, err := IndexPlacementHosts(got); err != nil {
		t.Fatalf("IndexPlacementHosts: %v", err)
	}

	withPortOnly := append(append([]*FleetNode{}, nodes...), &FleetNode{ID: "n5", TenantID: "tenant-a", Host: ":9091"})
	inventory, err = NewFleetInventory(&staticFleetRegistry{byTenant: map[string][]*FleetNode{"tenant-a": withPortOnly}})
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	_, err = inventory.PlacementHosts(ctx, "tenant-a")
	var invalid *PlacementInventoryInvalidError
	if !errors.As(err, &invalid) || invalid.NodeID != "n5" {
		t.Fatalf("PlacementHosts = %v, want n5 refused as hostless", err)
	}
}
