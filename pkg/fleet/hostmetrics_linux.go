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

//go:build linux

package fleet

import "syscall"

// statDisk reads the filesystem holding path. Available is Bavail, the space
// left to an unprivileged writer, not Bfree, which counts root's reserve.
func statDisk(path string) (diskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return diskUsage{}, err
	}
	blockSize := float64(st.Bsize)
	return diskUsage{
		total:     float64(st.Blocks) * blockSize,
		available: float64(st.Bavail) * blockSize,
	}, nil
}
