//go:build vector

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

package embedding

import (
	"errors"
	"net"
	"syscall"
)

// isConnectionRefused checks if the error is a connection refused error.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}

	// Check for network errors
	var netErr *net.OpError
	if ok := errors.As(err, &netErr); ok {
		// Check for connection refused
		if errors.Is(netErr.Err, syscall.ECONNREFUSED) {
			return true
		}
	}

	return false
}
