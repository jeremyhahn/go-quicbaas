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

import "testing"

// =============================================================================
// The storage tier on the wire — the control-plane half
// =============================================================================
//
// A remote spawn is dispatched to a daemon that does NOT hold the project
// record, so the resolved tier can only reach it over the wire. Every field
// here is inert at its zero value, which is exactly why dropping one would not
// fail: the remote worker would simply run its checkout on the disk its
// operator configured the project off.

// TestRuntimeStorageToProto_CarriesEveryTermOfThePlan proves the encoder loses
// nothing an ephemeral tier depends on.
func TestRuntimeStorageToProto_CarriesEveryTermOfThePlan(t *testing.T) {
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  2048,
		ContainerMemoryMB: 4096,
	}.Plan()

	wire := runtimeStorageToProto(plan)
	if wire == nil {
		t.Fatal("a configured plan produced no wire message")
	}
	if wire.GetTier() != string(WorkspaceTierEphemeral) {
		t.Errorf("Tier = %q, want %q", wire.GetTier(), WorkspaceTierEphemeral)
	}
	if wire.GetTmpfsSizeBytes() != 2048*testMB {
		t.Errorf("TmpfsSizeBytes = %d, want %d", wire.GetTmpfsSizeBytes(), 2048*testMB)
	}
	if wire.GetMemoryBytes() != 4096*testMB {
		t.Errorf("MemoryBytes = %d, want %d", wire.GetMemoryBytes(), 4096*testMB)
	}
}

// TestRuntimeStorageToProto_AnInertPlanPutsNothingOnTheWire is the inertness
// half: a spawn for a project with no storage configuration must produce the
// same request it always produced, with no field for the daemon to interpret.
func TestRuntimeStorageToProto_AnInertPlanPutsNothingOnTheWire(t *testing.T) {
	if wire := runtimeStorageToProto(WorkspaceStoragePlan{}); wire != nil {
		t.Fatalf("an inert plan produced %+v, want no wire message at all", wire)
	}
}

// TestRuntimeMountsToProto_CarriesTheTmpfsSize proves the mount converter
// carries the size too. A tmpfs whose size did not travel would arrive
// unbounded — free to grow into the container's whole memory cgroup — which is
// the failure the budget check exists to prevent.
func TestRuntimeMountsToProto_CarriesTheTmpfsSize(t *testing.T) {
	wire := runtimeMountsToProto([]ContainerMount{
		{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: 2048 * testMB},
		{Source: "/srv/cache", Target: "/cache", ReadOnly: true, Type: MountTypeBind},
	})
	if len(wire) != 2 {
		t.Fatalf("mounts = %+v, want two", wire)
	}
	if wire[0].GetType() != MountTypeTmpfs || wire[0].GetTmpfsSizeBytes() != 2048*testMB {
		t.Errorf("mounts[0] = %+v, want a sized tmpfs", wire[0])
	}
	if wire[1].GetTmpfsSizeBytes() != 0 {
		t.Errorf("a bind mount carried a tmpfs size: %+v", wire[1])
	}
}

// TestRuntimeMountsToProto_NoMountsIsNil is the bad-input twin: an empty or nil
// slice must not become an empty repeated field that a decoder then turns into
// a non-nil empty slice, because the round trip is compared for equality.
func TestRuntimeMountsToProto_NoMountsIsNil(t *testing.T) {
	if wire := runtimeMountsToProto(nil); wire != nil {
		t.Errorf("runtimeMountsToProto(nil) = %+v, want nil", wire)
	}
	if wire := runtimeMountsToProto([]ContainerMount{}); wire != nil {
		t.Errorf("runtimeMountsToProto(empty) = %+v, want nil", wire)
	}
}
