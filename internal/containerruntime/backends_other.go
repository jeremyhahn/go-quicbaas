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

//go:build !(linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs)

package containerruntime

// newDefaultBackends reports that this build has no in-process container
// backends. They need linux, cgo and the containers_image_openpgp and
// exclude_graphdriver_btrfs build tags, which only the fleet agent's build
// sets; quicbaasd is built without cgo and never runs containers.
func newDefaultBackends(*RuntimeConfig) (*backends, error) {
	return nil, ErrRuntimeRequiresLinuxCgo
}
