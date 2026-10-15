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

package main

import "strings"

// defaultTransport is the --transport default: gRPC over the Unix socket
// named by --address.
const defaultTransport = "unix"

// selectTransport returns the transport every command dials for the current
// --transport and --address. It is the CLI's one transport-selection rule.
func selectTransport() string {
	return detectTransport(transport, transportExplicit(), address)
}

// transportExplicit reports whether the transport was chosen rather than left
// at its default: either --transport was passed, or a command set it (the mcp
// commands select "mcp" themselves).
func transportExplicit() bool {
	if f := rootCmd.PersistentFlags().Lookup("transport"); f != nil && f.Changed {
		return true
	}
	return transport != "" && transport != defaultTransport
}

// detectTransport chooses a transport. An explicit choice wins, except that
// gRPC to a ".sock" address is gRPC over that Unix socket. Otherwise the
// address decides: an http:// or https:// URL selects REST, and anything else
// (a socket path included) keeps the default, unix.
func detectTransport(requested string, explicit bool, addr string) string {
	if explicit {
		if requested == "grpc" && strings.HasSuffix(addr, ".sock") {
			return "unix"
		}
		return requested
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return "rest"
	}
	return defaultTransport
}
