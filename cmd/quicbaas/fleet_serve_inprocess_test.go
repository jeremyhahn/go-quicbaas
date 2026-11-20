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

package main

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// noneOnlyRuntime is an in-process runtime that can give a sandbox only the
// "none" network, as the libcontainer runtime can.
type noneOnlyRuntime struct {
	orchestration.ContainerRuntime
}

func (noneOnlyRuntime) IsAvailable() bool { return true }

func (noneOnlyRuntime) SupportsSandboxNetwork(n orchestration.SandboxNetwork) bool {
	return n == orchestration.SandboxNetworkNone
}

// registerNoneOnlyRuntime registers noneOnlyRuntime as the in-process
// runtime for the test. The registration is process-wide, so the tests
// using it are not parallel.
func registerNoneOnlyRuntime(t *testing.T) {
	t.Helper()
	fleet.SetInProcessContainerRuntime(func(string, interfaces.Logger) (orchestration.ContainerRuntime, error) {
		return noneOnlyRuntime{}, nil
	})
	t.Cleanup(func() { fleet.SetInProcessContainerRuntime(nil) })
}

// The agent resolves its runtime without validating its configuration first,
// so the resolver itself must refuse the in-process runtime's key, even with
// that runtime registered.
func TestBuildFleetRuntime_LibcontainerSelectorIsRefused(t *testing.T) {
	registerNoneOnlyRuntime(t)
	backends := map[string]orchestration.AgentBackend{"stub": &serveStubBackend{}}
	cfg := serveRuntimeConfig(fleet.ContainerRuntimeSelector("libcontainer"), orchestration.SandboxPolicyOptIn, t.TempDir())
	cfg.SandboxNetwork = "none"

	rt, err := buildFleetRuntime(cfg, backends, newCaptureLogger())
	if rt != nil {
		t.Fatal("no --container-runtime value may select the in-process runtime")
	}
	if !errors.Is(err, fleet.ErrConfigUnknownRuntime) {
		t.Fatalf("want ErrConfigUnknownRuntime, got %v", err)
	}
}
