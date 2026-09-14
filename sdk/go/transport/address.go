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

package transport

import (
	"net/url"
	"strings"
)

// AddressType represents the type of address.
type AddressType int

const (
	// AddressTypeTCP is a standard TCP address.
	AddressTypeTCP AddressType = iota

	// AddressTypeUnix is a Unix domain socket path.
	AddressTypeUnix

	// AddressTypeUnixAbstract is a Linux abstract Unix socket.
	AddressTypeUnixAbstract

	// AddressTypeHTTP is an HTTP URL address (for REST/MCP transports).
	AddressTypeHTTP

	// AddressTypeHTTPS is an HTTPS URL address (for REST/MCP transports).
	AddressTypeHTTPS
)

// String returns the string representation of the address type.
func (t AddressType) String() string {
	switch t {
	case AddressTypeTCP:
		return "tcp"
	case AddressTypeUnix:
		return "unix"
	case AddressTypeUnixAbstract:
		return "unix-abstract"
	case AddressTypeHTTP:
		return "http"
	case AddressTypeHTTPS:
		return "https"
	default:
		return "unknown"
	}
}

// ParsedAddress contains the parsed address components.
type ParsedAddress struct {
	// Type is the address type.
	Type AddressType

	// Address is the normalized address for connection.
	// For HTTP/HTTPS URLs, this is the host:port portion.
	Address string

	// Original is the original input address.
	Original string

	// DialAddress is the address to use with net.Dial or gRPC.
	// For HTTP/HTTPS URLs, this is the host:port extracted from the URL.
	DialAddress string

	// Path is the URL path component (only set for HTTP/HTTPS URLs).
	Path string

	// Scheme is the URL scheme (http, https, unix, etc.).
	Scheme string
}

// ParseAddress parses an address string and returns its components.
//
// Supported formats:
//   - "localhost:50051" - TCP address
//   - "/var/run/quicbaas.sock" - Unix socket (auto-detected by leading /)
//   - "unix:///var/run/quicbaas.sock" - Explicit Unix socket
//   - "unix-abstract://quicbaas" - Linux abstract Unix socket
//   - "http://localhost:8080/api" - HTTP URL (extracts host:port for gRPC)
//   - "https://localhost:8443/api" - HTTPS URL (extracts host:port for gRPC)
//
// For gRPC usage, the DialAddress field contains the properly formatted
// address string (e.g., "unix:///path" for Unix sockets, "host:port" for TCP/HTTP).
func ParseAddress(address string) *ParsedAddress {
	parsed := &ParsedAddress{
		Original: address,
	}

	// Check for explicit prefixes
	switch {
	case strings.HasPrefix(address, "unix-abstract://"):
		parsed.Type = AddressTypeUnixAbstract
		parsed.Scheme = "unix-abstract"
		parsed.Address = strings.TrimPrefix(address, "unix-abstract://")
		// Abstract sockets use @ prefix in the address
		parsed.DialAddress = "unix-abstract:" + parsed.Address

	case strings.HasPrefix(address, "unix://"):
		parsed.Type = AddressTypeUnix
		parsed.Scheme = "unix"
		parsed.Address = strings.TrimPrefix(address, "unix://")
		parsed.DialAddress = "unix://" + parsed.Address

	case strings.HasPrefix(address, "http://"):
		parseHTTPURL(address, "http", AddressTypeHTTP, parsed)

	case strings.HasPrefix(address, "https://"):
		parseHTTPURL(address, "https", AddressTypeHTTPS, parsed)

	case strings.HasPrefix(address, "/"):
		// Auto-detect Unix socket from path
		parsed.Type = AddressTypeUnix
		parsed.Scheme = "unix"
		parsed.Address = address
		parsed.DialAddress = "unix://" + address

	default:
		// Default to TCP
		parsed.Type = AddressTypeTCP
		parsed.Scheme = "tcp"
		parsed.Address = address
		parsed.DialAddress = address
	}

	return parsed
}

// parseHTTPURL extracts host:port from an HTTP/HTTPS URL.
func parseHTTPURL(address, scheme string, addrType AddressType, parsed *ParsedAddress) {
	parsed.Type = addrType
	parsed.Scheme = scheme

	// Parse the URL to extract components
	u, err := url.Parse(address)
	if err != nil {
		// Fallback: strip scheme and use remainder
		remainder := strings.TrimPrefix(address, scheme+"://")
		// Remove path if present
		if idx := strings.Index(remainder, "/"); idx >= 0 {
			parsed.Path = remainder[idx:]
			remainder = remainder[:idx]
		}
		parsed.Address = remainder
		parsed.DialAddress = remainder
		return
	}

	// Extract host:port
	parsed.Address = u.Host
	parsed.DialAddress = u.Host
	parsed.Path = u.Path

	// If no port specified, use default ports
	if !strings.Contains(u.Host, ":") {
		switch scheme {
		case "http":
			parsed.Address = u.Host + ":80"
			parsed.DialAddress = u.Host + ":80"
		case "https":
			parsed.Address = u.Host + ":443"
			parsed.DialAddress = u.Host + ":443"
		}
	}
}

// IsUnix returns true if this is any type of Unix socket address.
func (p *ParsedAddress) IsUnix() bool {
	return p.Type == AddressTypeUnix || p.Type == AddressTypeUnixAbstract
}

// IsTCP returns true if this is a TCP address.
func (p *ParsedAddress) IsTCP() bool {
	return p.Type == AddressTypeTCP
}

// IsHTTP returns true if this is an HTTP or HTTPS URL.
func (p *ParsedAddress) IsHTTP() bool {
	return p.Type == AddressTypeHTTP || p.Type == AddressTypeHTTPS
}

// Network returns the network type for use with net.Dial.
func (p *ParsedAddress) Network() string {
	switch p.Type {
	case AddressTypeUnix, AddressTypeUnixAbstract:
		return "unix"
	default:
		return "tcp"
	}
}

// NormalizeForGRPC returns the address in a format suitable for gRPC dialing.
func (p *ParsedAddress) NormalizeForGRPC() string {
	switch p.Type {
	case AddressTypeUnix:
		// gRPC expects "unix:path" format
		return "unix:" + p.Address
	case AddressTypeUnixAbstract:
		// Abstract sockets use @ prefix
		return "unix:@" + p.Address
	case AddressTypeHTTP, AddressTypeHTTPS:
		// For HTTP URLs, return just the host:port for gRPC
		return p.Address
	default:
		return p.Address
	}
}

// NormalizeForREST returns the address in a format suitable for HTTP clients.
// For Unix sockets, this returns an empty string as HTTP clients need special
// handling via custom dialers.
func (p *ParsedAddress) NormalizeForREST() string {
	if p.IsUnix() {
		// Unix sockets need custom transport configuration for HTTP
		return ""
	}
	return p.Address
}

// FullURL returns the complete URL for HTTP/HTTPS addresses.
// For non-HTTP addresses, returns the original address.
func (p *ParsedAddress) FullURL() string {
	if p.IsHTTP() {
		return p.Scheme + "://" + p.Address + p.Path
	}
	return p.Original
}

// =============================================================================
// HTTP endpoint construction
//
// REST and MCP (Streamable HTTP) clients need a scheme-qualified URL, not the
// bare "host:port" every other transport dials. Building that URL used to be
// duplicated per transport, and the go-qrdb sub-client (sdk/go qrdb.go) did
// not build it at all: it handed "127.0.0.1:18443" straight to go-qrdb's REST
// transport, which concatenates the path onto the address verbatim, so the
// first request died in url.Parse with
//
//	parse "127.0.0.1:18443/v1/barrier/status": first path segment in URL cannot contain colon
//
// (go-quicbaas#405). These helpers are the ONE implementation every HTTP-ish
// transport normalizes through.
// =============================================================================

// HTTPBaseURL returns the scheme-qualified origin ("scheme://host:port") an
// HTTP client dials for address:
//
//   - a Unix socket yields a placeholder host, since the dialer is overridden;
//     the scheme follows tlsConfigured;
//   - an explicit http:// or https:// URL keeps its own scheme and host;
//   - a bare "host:port" yields https. TLS is always required (see the repo's
//     TLS posture): an address with no scheme is never downgraded to http.
func HTTPBaseURL(address string, tlsConfigured bool) string {
	parsed := ParseAddress(address)

	if parsed.IsUnix() {
		if tlsConfigured {
			return "https://localhost"
		}
		return "http://localhost"
	}
	if parsed.IsHTTP() {
		return parsed.Scheme + "://" + parsed.Address
	}
	return "https://" + parsed.Address
}

// HTTPPath returns the path component of an explicit http(s):// address, with
// any trailing slash trimmed. Bare "host:port" and Unix socket addresses carry
// no path and yield "".
func HTTPPath(address string) string {
	parsed := ParseAddress(address)
	if !parsed.IsHTTP() {
		return ""
	}
	return strings.TrimSuffix(parsed.Path, "/")
}

// HTTPEndpoint returns the full URL for an HTTP endpoint mounted at suffix
// (e.g. "/mcp") on address: HTTPBaseURL joined with the address's own path and
// suffix. A path that already ends in suffix is used as-is, so an address a
// caller already spelled in full ("https://host:port/mcp") is not doubled.
func HTTPEndpoint(address string, tlsConfigured bool, suffix string) string {
	base := HTTPBaseURL(address, tlsConfigured) + HTTPPath(address)
	if suffix == "" || strings.HasSuffix(base, suffix) {
		return base
	}
	return base + suffix
}
