// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build linux && cgo

package containerruntime

import (
	"os"

	"github.com/opencontainers/runc/libcontainer"
	// nsenter's cgo constructor joins the container's namespaces before the
	// Go runtime starts, when this program is re-executed as container init.
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"go.podman.io/storage/pkg/reexec"
)

// libcontainer re-executes the running program as "<argv0> init", with its
// init pipe named in the environment. The program may have its own "init"
// command, so both must be present.
const (
	containerInitArg     = "init"
	containerInitPipeEnv = "_LIBCONTAINER_INITPIPE"
)

// RunContainerInit is the in-process runtime's re-exec entry point. A program
// that creates containers through this package must call it first thing in
// main and return when it reports true:
//
//	func main() {
//		if containerruntime.RunContainerInit() {
//			return
//		}
//		...
//	}
//
// It covers two re-execs of the running program:
//   - go.podman.io/storage's helpers, which unpack image layers; it runs
//     the helper and returns true.
//   - libcontainer's container init, which sets the container up and execs
//     its process; it never returns.
//
// In any other invocation it does nothing and returns false.
func RunContainerInit() bool {
	if reexec.Init() {
		return true
	}
	if isContainerInit(os.Args, os.Getenv(containerInitPipeEnv)) {
		libcontainer.Init()
	}
	return false
}

// isContainerInit reports whether args and the init pipe variable describe a
// libcontainer init re-exec.
func isContainerInit(args []string, initPipe string) bool {
	return len(args) > 1 && args[1] == containerInitArg && initPipe != ""
}
