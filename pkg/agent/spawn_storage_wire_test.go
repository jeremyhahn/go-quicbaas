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

package agent

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The storage tier on the wire — the daemon half
// =============================================================================
//
// This daemon does not hold the project record, so the tier it applies is the
// one the control plane sent. A decoder that dropped a term would leave the
// worker's checkout on this host's disk, or its tmpfs unbounded, with nothing
// failing to say so.

const wireMB = 1024 * 1024

// TestProtoStorageToRuntime_CarriesEveryTermOfThePlan proves the decoder loses
// nothing the ephemeral tier depends on.
func TestProtoStorageToRuntime_CarriesEveryTermOfThePlan(t *testing.T) {
	plan := protoStorageToRuntime(&proto.WorkspaceStoragePlan{
		Tier:           string(orchestration.WorkspaceTierEphemeral),
		TmpfsSizeBytes: 2048 * wireMB,
		MemoryBytes:    4096 * wireMB,
	})

	if !plan.Ephemeral() {
		t.Errorf("Tier = %q, want the ephemeral tier", plan.Tier)
	}
	if plan.TmpfsSizeBytes != 2048*wireMB {
		t.Errorf("TmpfsSizeBytes = %d, want %d", plan.TmpfsSizeBytes, 2048*wireMB)
	}
	if plan.MemoryBytes != 4096*wireMB {
		t.Errorf("MemoryBytes = %d, want %d", plan.MemoryBytes, 4096*wireMB)
	}
}

// TestProtoStorageToRuntime_AnAbsentMessageIsTheInertPlan is the bad-input
// twin, and it is the common case rather than an edge: a control plane with no
// storage configuration for the project sends no message, and the daemon must
// then launch exactly the container it always launched.
func TestProtoStorageToRuntime_AnAbsentMessageIsTheInertPlan(t *testing.T) {
	if plan := protoStorageToRuntime(nil); !plan.Inert() {
		t.Fatalf("protoStorageToRuntime(nil) = %+v, want the inert plan", plan)
	}
}

// TestProtoMountsToRuntime_CarriesTheTmpfsSize proves the mount decoder carries
// the size. A size lost here arrives as an unbounded tmpfs, free to grow into
// the container's whole memory cgroup.
func TestProtoMountsToRuntime_CarriesTheTmpfsSize(t *testing.T) {
	mounts := protoMountsToRuntime([]*proto.ContainerMount{
		{Target: "/workspace", Type: orchestration.MountTypeTmpfs, TmpfsSizeBytes: 2048 * wireMB},
		{Source: "/srv/cache", Target: "/cache", ReadOnly: true, Type: orchestration.MountTypeBind},
	})
	if len(mounts) != 2 {
		t.Fatalf("mounts = %+v, want two", mounts)
	}
	if mounts[0].Type != orchestration.MountTypeTmpfs || mounts[0].TmpfsSizeBytes != 2048*wireMB {
		t.Errorf("mounts[0] = %+v, want a sized tmpfs", mounts[0])
	}
	if mounts[1].TmpfsSizeBytes != 0 {
		t.Errorf("a bind mount arrived with a tmpfs size: %+v", mounts[1])
	}
}

// TestProtoMountsToRuntime_NoMountsIsNil is the bad-input twin.
func TestProtoMountsToRuntime_NoMountsIsNil(t *testing.T) {
	if mounts := protoMountsToRuntime(nil); mounts != nil {
		t.Errorf("protoMountsToRuntime(nil) = %+v, want nil", mounts)
	}
	if mounts := protoMountsToRuntime([]*proto.ContainerMount{}); mounts != nil {
		t.Errorf("protoMountsToRuntime(empty) = %+v, want nil", mounts)
	}
}
