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

import "github.com/jeremyhahn/go-quicbaas/pkg/containerruntime"

// runContainerInit runs the container init when the agent was re-executed
// as one by the in-process container runtime, and reports whether it did.
// It must run before anything else in main: the init takes over the process.
func runContainerInit() bool {
	return containerruntime.RunContainerInit()
}
