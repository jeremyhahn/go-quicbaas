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

//go:build integration && linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs

// Package containerruntime_test runs pkg/containerruntime end to end: images
// pulled from a real registry, root filesystems unpacked by go.podman.io/storage
// and containers run by libcontainer. It needs root, so it runs in the
// privileged container that `make integration-test-containerruntime` starts.
package containerruntime_test

import (
	"os"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/containerruntime"
)

// TestMain hands the process to libcontainer when it was re-executed as a
// container init: libcontainer starts every container by re-executing the
// current binary, which here is this test binary.
func TestMain(m *testing.M) {
	if containerruntime.RunContainerInit() {
		return
	}
	os.Exit(m.Run())
}
