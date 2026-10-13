//go:build mcp

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

package mcp

// ConvertToArguments exposes the transport's request encoding to the
// external mcp_test package. That package exists because pkg/api/mcp imports
// this package, so an internal test cannot import pkg/api/mcp back.
func ConvertToArguments(req interface{}) map[string]interface{} {
	return (&Transport{}).convertToArguments(req)
}

// DecodeMCPFleetNodeHeartbeat exposes decodeMCPFleetNodeHeartbeat to the
// external mcp_test package.
var DecodeMCPFleetNodeHeartbeat = decodeMCPFleetNodeHeartbeat
