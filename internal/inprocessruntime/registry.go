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

// Package inprocessruntime holds the fleet node agent's in-process container
// runtime registration. It is internal to this module: the agent's main
// registers the runtime from internal/containerruntime, and pkg/fleet
// resolves it, without pkg/fleet importing the runtime itself.
package inprocessruntime

import (
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Builder builds the in-process container runtime with all of its state
// under root, a directory no other runtime uses.
type Builder func(root string, logger interfaces.Logger) (orchestration.ContainerRuntime, error)

// registered is the registered Builder, nil until the agent's main
// registers one.
var registered atomic.Pointer[Builder]

// Register registers the agent's in-process runtime; nil unregisters it.
// Only the fleet node agent, built for linux with cgo, registers one.
func Register(build Builder) {
	if build == nil {
		registered.Store(nil)
		return
	}
	registered.Store(&build)
}

// Registered returns the registered Builder, or nil when none is.
func Registered() Builder {
	build := registered.Load()
	if build == nil {
		return nil
	}
	return *build
}
