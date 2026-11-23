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

//go:build linux && cgo

package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/internal/containerruntime"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// registerInProcessContainerRuntime makes internal/containerruntime the runtime
// the agent registers (no --container-runtime value selects it yet, and "auto" never takes it). It lives here,
// in the agent's main package, because quicbaasd links pkg/fleet too and
// must never link the runtime.
func registerInProcessContainerRuntime() {
	fleet.SetInProcessContainerRuntime(newInProcessContainerRuntime)
}

// newInProcessContainerRuntime starts the in-process runtime with all of its
// state under root and serves it as the orchestration contract.
func newInProcessContainerRuntime(root string, _ interfaces.Logger) (orchestration.ContainerRuntime, error) {
	rt, err := containerruntime.NewRuntime(context.Background(), containerruntime.RuntimeConfigUnder(root))
	if err != nil {
		return nil, err
	}
	return containerruntime.NewJobRuntime(rt), nil
}

// runContainerInit runs the container init when the agent was re-executed
// as one by the in-process container runtime, and reports whether it did.
// It must run before anything else in main: the init takes over the process.
func runContainerInit() bool {
	return containerruntime.RunContainerInit()
}
