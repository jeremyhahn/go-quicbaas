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

// Package main provides key conversion utilities for CLI commands.
package main

import (
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
)

// ParseKeyToID converts a string key to a uint64 ID.
// If the key is a valid uint64 string, it's parsed directly.
// Otherwise, FNV1a hash is used to generate a deterministic ID.
func ParseKeyToID(key string) uint64 {
	id, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		// Non-numeric key: hash it to get a deterministic ID
		return hash.FNV1a(key)
	}
	return id
}

// FormatIDToKey converts a uint64 ID back to a string key.
func FormatIDToKey(id uint64) string {
	return strconv.FormatUint(id, 10)
}
