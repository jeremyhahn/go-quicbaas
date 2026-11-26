//go:build mcp

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

package mcp

import "strconv"

// parseDecimalArg parses a uint64 argument sent as a decimal string. An
// optional argument that is absent or empty is 0.
func parseDecimalArg(args map[string]interface{}, key string, required bool) (uint64, error) {
	raw, present := args[key]
	if !present || raw == "" {
		if required {
			return 0, &UserToolInvalidArgumentError{Arg: key}
		}
		return 0, nil
	}
	text, ok := raw.(string)
	if !ok {
		return 0, &UserToolInvalidArgumentError{Arg: key}
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, &UserToolInvalidArgumentError{Arg: key, Err: err}
	}
	return value, nil
}
