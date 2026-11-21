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

//go:build !linux

package fleet

// statDisk reports filesystem space as unreadable off Linux, so the disk
// signals are unread rather than zero.
func statDisk(string) (diskUsage, error) {
	return diskUsage{}, ErrHostDiskUnsupported
}
