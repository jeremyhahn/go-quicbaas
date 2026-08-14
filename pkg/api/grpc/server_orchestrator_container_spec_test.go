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

package grpc

import (
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestProtoContainerSpecToService_NeverMapsPrivilegeFields is a deliberate
// regression guard, not a coverage test.
//
// A gRPC caller supplies container_config verbatim (SpawnSession →
// spawnSandboxedSession), so any ContainerSpec field this converter maps is
// caller-controlled. Four of them decide how much of the host the container
// can reach:
//
//	Mounts      — an arbitrary host bind mount (/, /var/run/docker.sock, ...)
//	Privileged  — the full capability set, unmasked /proc and /sys, no seccomp
//	CapAdd      — CAP_SYS_ADMIN and friends
//	User        — running the container as uid 0
//
// The proto ContainerSpec has no field for any of them today, and this
// converter must never grow one: mapping a single one of these would turn a
// tenant-supplied spawn request into a container-escape primitive, and it
// would be a one-line change. The assertions below are written against the
// PRODUCED domain spec, so they fail whether the field arrives via a new proto
// field or via a value invented inside the converter.
func TestProtoContainerSpecToService_NeverMapsPrivilegeFields(t *testing.T) {
	t.Parallel()

	// Every field a caller can populate today, all set, so nothing is
	// vacuously zero.
	got := protoContainerSpecToService(&pb.ContainerSpec{
		Name:    "caller-named",
		Image:   "evil:latest",
		Env:     map[string]string{"K": "v"},
		Volumes: []string{"/:/host", "/var/run/docker.sock:/var/run/docker.sock"},
		WorkDir: "/workspace",
		Network: "host",
		Command: []string{"sh", "-c", "id"},
	})
	if got == nil {
		t.Fatal("protoContainerSpecToService returned nil for a populated spec")
	}

	if len(got.Mounts) != 0 {
		t.Errorf("Mounts must never be mapped from a caller-supplied spec, got %+v", got.Mounts)
	}
	if got.Privileged {
		t.Error("Privileged must never be mapped from a caller-supplied spec")
	}
	if len(got.CapAdd) != 0 {
		t.Errorf("CapAdd must never be mapped from a caller-supplied spec, got %v", got.CapAdd)
	}
	if got.User != "" {
		t.Errorf("User must never be mapped from a caller-supplied spec, got %q", got.User)
	}

	// The fields that ARE mapped, asserted so this test also pins the exact
	// caller-controlled surface: a widening of that surface has to be a
	// deliberate edit here.
	want := orchestration.ContainerSpec{
		Name:    "caller-named",
		Image:   "evil:latest",
		Env:     map[string]string{"K": "v"},
		WorkDir: "/workspace",
		Network: "host",
		Command: []string{"sh", "-c", "id"},
	}
	if got.Name != want.Name || got.Image != want.Image || got.WorkDir != want.WorkDir ||
		got.Network != want.Network || len(got.Command) != len(want.Command) {
		t.Errorf("mapped spec = %+v, want the fields in %+v", *got, want)
	}
	// Volumes is a proto field with no domain destination: a caller cannot
	// turn it into a mount.
	if len(got.Mounts) != 0 {
		t.Error("proto Volumes must not become domain Mounts")
	}
}

// TestProtoContainerSpecToService_NilIsNil covers the nil guard: a request with
// no container_config must not synthesize a spec.
func TestProtoContainerSpecToService_NilIsNil(t *testing.T) {
	t.Parallel()

	if got := protoContainerSpecToService(nil); got != nil {
		t.Errorf("protoContainerSpecToService(nil) = %+v, want nil", got)
	}
}
