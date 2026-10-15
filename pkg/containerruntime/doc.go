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
// pkg/compute/podman. The agent's job wiring arrives in a later change; today
// only the agent's container init entry point (below) imports it.
//
// Where it runs:
//
//   - Only in the fleet node agent (the pkg/fleet daemon, `quicbaas fleet
//     serve`), never in quicbaasd. quicbaasd stays CGO_ENABLED=0;
//     TestDaemonNeverLinksContainerRuntime fails if the daemon links this
//     package.
//   - Everything runs in-process, with no external binaries and no sockets.
//     TestPackageNeverImportsOSExec fails if a non-test file imports os/exec.
//   - The agent runs on the same host as the server whenever jobs run there,
//     a single-node deployment included.
//   - The tenant is the SPIFFE trust domain.
//   - A node binds its advertised host; 0.0.0.0 is never a default bind.
//
// The runtime is three backends behind the Runtime type (engine.go):
//
//   - Images: go.podman.io/image/v5 copies an image from its registry into
//     local storage. The pull honours the signature policy
//     (RuntimeConfig.SignaturePolicyPath, else the system default
//     /etc/containers/policy.json). A digest-pinned reference must resolve to
//     the pinned digest, at pull and again at create; the check reuses
//     pkg/orchestration's image digest rules and fails with
//     orchestration.ErrImageDigestMismatch.
//   - Root filesystems: go.podman.io/storage unpacks the image into a
//     read-write container layer under the runtime's storage root and mounts
//     it.
//   - Containers: runc libcontainer creates, starts, signals and destroys the
//     container. Its init process is the agent binary itself, re-executed
//     through libcontainer's nsenter.
//
// The libcontainer engine needs linux and cgo (its files are
// `//go:build linux && cgo`); the storage-backed image and rootfs stores also
// need the tags containers_image_openpgp and exclude_graphdriver_btrfs, which
// the Makefile passes to the agent builds as CONTAINER_LIB_TAGS. On any other
// build NewRuntime fails with ErrRuntimeRequiresLinuxCgo.
//
// # Container init entry point
//
// libcontainer starts a container by re-executing the current binary as
// `<binary> init` with the _LIBCONTAINER_INITPIPE variable set. The binary
// must hand that process to libcontainer before doing anything else, so the
// agent's main calls RunContainerInit first and returns at once when it
// reports true:
//
//	func main() {
//		if runContainerInit() { // containerruntime.RunContainerInit, linux && cgo only
//			return
//		}
//		...
//	}
//
// RunContainerInit also serves go.podman.io/storage's reexec helpers. The
// quicbaas CLI has an `init` subcommand of its own; the init pipe variable
// tells the two apart, so `quicbaas init` run by a user still reaches the CLI.
//
// Exit codes are the init process's own; a process killed by a signal exits
// with 128 plus the signal number. Stop sends SIGTERM, waits out the grace
// period on the exit channel, then sends SIGKILL. Container stdout and stderr
// are kept in the container's bundle directory and served by Logs.
//
// TPM and attestation types from go-xkms and go-qrdb are reached only
// through the go-quicbaas sdk/go re-exports, never by importing those
// modules' internal packages.
package containerruntime
