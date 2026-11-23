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
// --transport and --address, and the address that transport dials. It is the
// CLI's one transport-selection rule.
func selectTransport() (string, string) {
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

// Address schemes that exist only in --address: they name the transport and
// are dropped before the client dials.
const (
	quicAddressScheme = "quic://"
	mcpAddressScheme  = "mcp://"
)

// detectTransport chooses a transport and the address it dials, following the
// transport model's "Selecting a transport" table
// (docs/multiprotocol-api/architecture.md). An explicit choice wins, except
// that gRPC to a ".sock" address is gRPC over that Unix socket. Otherwise the
// address decides:
//
//   - http:// or https:// selects REST;
//   - quic://host:port selects QUIC and mcp://host:port selects MCP, each
//     dialing host:port;
//   - a path (leading "/" or a ".sock" file) and an empty address keep the
//     default, unix;
//   - anything else is host:port and selects gRPC over TLS.
//
// The quic:// and mcp:// schemes are dropped whichever way the transport was
// chosen: they belong to the CLI's address syntax, not to the SDK's.
func detectTransport(requested string, explicit bool, addr string) (string, string) {
	dial := addr
	scheme := ""
	switch {
	case strings.HasPrefix(addr, quicAddressScheme):
		dial, scheme = strings.TrimPrefix(addr, quicAddressScheme), "quic"
	case strings.HasPrefix(addr, mcpAddressScheme):
		dial, scheme = strings.TrimPrefix(addr, mcpAddressScheme), "mcp"
	}

	if explicit {
		if requested == "grpc" && strings.HasSuffix(addr, ".sock") {
			return "unix", dial
		}
		return requested, dial
	}
	switch {
	case scheme != "":
		return scheme, dial
	case strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://"):
		return "rest", dial
	case addr == "" || strings.HasPrefix(addr, "/") || strings.HasSuffix(addr, ".sock"):
		return defaultTransport, dial
	default:
		return "grpc", dial
	}
}
