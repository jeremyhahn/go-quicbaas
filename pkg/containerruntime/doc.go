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

// Package containerruntime is the stack's one container runtime: the
// PodmanManager lifecycle model, attestor hooks, exec, attach and pty, and
// per-container metrics.
//
// It was moved here, with its history and tests, from go-trusted-platform
// pkg/compute/podman. It is not wired yet: nothing in this module imports it.
//
// Where it runs:
//
//   - Only in the fleet node agent (the pkg/fleet daemon, `quicbaas fleet
//     serve`), never in quicbaasd. quicbaasd stays CGO_ENABLED=0;
//     TestDaemonNeverLinksContainerRuntime fails if the daemon links this
//     package.
//   - runc libcontainer runs in-process in the agent, with no external
//     runtime binaries and no sockets. The agent is built with cgo for
//     libcontainer's nsenter init. The exec core here (runtime.go,
//     manager_ops.go, which call crun, podman and skopeo) is the moved code
//     and is replaced by that libcontainer core in a later change.
//   - The agent runs on the same host as the server whenever jobs run there,
//     a single-node deployment included.
//   - The tenant is the SPIFFE trust domain.
//   - A node binds its advertised host; 0.0.0.0 is never a default bind.
//
// TPM and attestation types from go-xkms and go-qrdb are reached only
// through the go-quicbaas sdk/go re-exports, never by importing those
// modules' internal packages.
package containerruntime
