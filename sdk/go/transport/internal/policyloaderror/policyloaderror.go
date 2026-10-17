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

// Package policyloaderror carries a flagged policy entry's load_error from
// the server's JSON policy shape to the SDK's. The REST and MCP transports
// decode the same server shape, so they share this one mapping.
package policyloaderror

// Key is the JSON key of a flagged entry's load error: the reason the
// stored record was not applied and its unknown key paths, never values.
const Key = "load_error"

// Copy sets sdkPolicy's load_error to serverPolicy's, when it has one. The
// value is already in the SDK's PolicyLoadError JSON shape.
func Copy(serverPolicy, sdkPolicy map[string]interface{}) {
	if loadError, ok := serverPolicy[Key]; ok {
		sdkPolicy[Key] = loadError
	}
}
