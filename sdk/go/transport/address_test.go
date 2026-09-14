// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package transport

import "testing"

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType AddressType
		expectedAddr string
		expectedPath string
	}{
		{
			name:         "TCP address",
			input:        "localhost:50051",
			expectedType: AddressTypeTCP,
			expectedAddr: "localhost:50051",
		},
		{
			name:         "TCP with host",
			input:        "192.168.1.1:8080",
			expectedType: AddressTypeTCP,
			expectedAddr: "192.168.1.1:8080",
		},
		{
			name:         "Unix socket auto-detected",
			input:        "/var/run/quicbaas.sock",
			expectedType: AddressTypeUnix,
			expectedAddr: "/var/run/quicbaas.sock",
		},
		{
			name:         "Unix socket explicit",
			input:        "unix:///var/run/quicbaas.sock",
			expectedType: AddressTypeUnix,
			expectedAddr: "/var/run/quicbaas.sock",
		},
		{
			name:         "Unix abstract socket",
			input:        "unix-abstract://quicbaas",
			expectedType: AddressTypeUnixAbstract,
			expectedAddr: "quicbaas",
		},
		{
			name:         "HTTP URL with port and path",
			input:        "http://test-node1:8888/mcp",
			expectedType: AddressTypeHTTP,
			expectedAddr: "test-node1:8888",
			expectedPath: "/mcp",
		},
		{
			name:         "HTTP URL without path",
			input:        "http://localhost:8080",
			expectedType: AddressTypeHTTP,
			expectedAddr: "localhost:8080",
		},
		{
			name:         "HTTP URL without port",
			input:        "http://example.com/api",
			expectedType: AddressTypeHTTP,
			expectedAddr: "example.com:80",
			expectedPath: "/api",
		},
		{
			name:         "HTTPS URL with port and path",
			input:        "https://secure.example.com:8443/api/v1",
			expectedType: AddressTypeHTTPS,
			expectedAddr: "secure.example.com:8443",
			expectedPath: "/api/v1",
		},
		{
			name:         "HTTPS URL without port",
			input:        "https://secure.example.com/api",
			expectedType: AddressTypeHTTPS,
			expectedAddr: "secure.example.com:443",
			expectedPath: "/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseAddress(tt.input)

			if parsed.Type != tt.expectedType {
				t.Errorf("expected type %v, got %v", tt.expectedType, parsed.Type)
			}
			if parsed.Address != tt.expectedAddr {
				t.Errorf("expected address %q, got %q", tt.expectedAddr, parsed.Address)
			}
			if parsed.Original != tt.input {
				t.Errorf("expected original %q, got %q", tt.input, parsed.Original)
			}
			if tt.expectedPath != "" && parsed.Path != tt.expectedPath {
				t.Errorf("expected path %q, got %q", tt.expectedPath, parsed.Path)
			}
		})
	}
}

func TestParsedAddress_IsUnix(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"localhost:50051", false},
		{"/var/run/quicbaas.sock", true},
		{"unix:///tmp/test.sock", true},
		{"unix-abstract://test", true},
		{"http://localhost:8080", false},
		{"https://localhost:8443", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.IsUnix(); got != tt.expected {
				t.Errorf("IsUnix() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestParsedAddress_IsTCP(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"localhost:50051", true},
		{"/var/run/quicbaas.sock", false},
		{"unix:///tmp/test.sock", false},
		{"http://localhost:8080", false},
		{"https://localhost:8443", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.IsTCP(); got != tt.expected {
				t.Errorf("IsTCP() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestParsedAddress_IsHTTP(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"localhost:50051", false},
		{"/var/run/quicbaas.sock", false},
		{"unix:///tmp/test.sock", false},
		{"http://localhost:8080", true},
		{"https://localhost:8443", true},
		{"http://test-node1:8888/mcp", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.IsHTTP(); got != tt.expected {
				t.Errorf("IsHTTP() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestParsedAddress_Network(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"localhost:50051", "tcp"},
		{"/var/run/quicbaas.sock", "unix"},
		{"unix-abstract://test", "unix"},
		{"http://localhost:8080", "tcp"},
		{"https://localhost:8443", "tcp"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.Network(); got != tt.expected {
				t.Errorf("Network() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParsedAddress_NormalizeForGRPC(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"localhost:50051", "localhost:50051"},
		{"/var/run/quicbaas.sock", "unix:/var/run/quicbaas.sock"},
		{"unix-abstract://test", "unix:@test"},
		{"http://test-node1:8888/mcp", "test-node1:8888"},
		{"https://secure.example.com:8443/api", "secure.example.com:8443"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.NormalizeForGRPC(); got != tt.expected {
				t.Errorf("NormalizeForGRPC() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParsedAddress_NormalizeForREST(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"localhost:8080", "localhost:8080"},
		{"/var/run/quicbaas.sock", ""},
		{"unix-abstract://test", ""},
		{"http://test-node1:8888/mcp", "test-node1:8888"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.NormalizeForREST(); got != tt.expected {
				t.Errorf("NormalizeForREST() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParsedAddress_FullURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"localhost:8080", "localhost:8080"},
		{"/var/run/quicbaas.sock", "/var/run/quicbaas.sock"},
		{"http://test-node1:8888/mcp", "http://test-node1:8888/mcp"},
		{"https://secure.example.com:8443/api/v1", "https://secure.example.com:8443/api/v1"},
		{"http://example.com/api", "http://example.com:80/api"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if got := parsed.FullURL(); got != tt.expected {
				t.Errorf("FullURL() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestAddressType_String(t *testing.T) {
	tests := []struct {
		addrType AddressType
		expected string
	}{
		{AddressTypeTCP, "tcp"},
		{AddressTypeUnix, "unix"},
		{AddressTypeUnixAbstract, "unix-abstract"},
		{AddressTypeHTTP, "http"},
		{AddressTypeHTTPS, "https"},
		{AddressType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.addrType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseAddress_Scheme(t *testing.T) {
	tests := []struct {
		input          string
		expectedScheme string
	}{
		{"localhost:50051", "tcp"},
		{"/var/run/quicbaas.sock", "unix"},
		{"unix:///tmp/test.sock", "unix"},
		{"unix-abstract://test", "unix-abstract"},
		{"http://localhost:8080", "http"},
		{"https://localhost:8443", "https"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseAddress(tt.input)
			if parsed.Scheme != tt.expectedScheme {
				t.Errorf("expected scheme %q, got %q", tt.expectedScheme, parsed.Scheme)
			}
		})
	}
}

// TestParseAddress_MCPAddress tests the specific MCP address format that caused the bug.
func TestParseAddress_MCPAddress(t *testing.T) {
	// This is the exact address format that was causing "too many colons in address" error
	address := "http://test-node1:8888/mcp"
	parsed := ParseAddress(address)

	// Verify the address is correctly parsed for gRPC dialing
	if parsed.Type != AddressTypeHTTP {
		t.Errorf("expected type AddressTypeHTTP, got %v", parsed.Type)
	}

	if parsed.Address != "test-node1:8888" {
		t.Errorf("expected address 'test-node1:8888', got %q", parsed.Address)
	}

	if parsed.DialAddress != "test-node1:8888" {
		t.Errorf("expected dialAddress 'test-node1:8888', got %q", parsed.DialAddress)
	}

	if parsed.Path != "/mcp" {
		t.Errorf("expected path '/mcp', got %q", parsed.Path)
	}

	grpcAddr := parsed.NormalizeForGRPC()
	if grpcAddr != "test-node1:8888" {
		t.Errorf("NormalizeForGRPC() should return 'test-node1:8888', got %q", grpcAddr)
	}

	// Verify the full URL can be reconstructed
	fullURL := parsed.FullURL()
	if fullURL != "http://test-node1:8888/mcp" {
		t.Errorf("FullURL() should return original URL, got %q", fullURL)
	}
}

// TestHTTPBaseURL covers every address shape an operator can supply: a bare
// host:port (promoted to https — TLS is always required), an explicit
// http/https URL (kept verbatim), and a Unix socket (placeholder host, scheme
// following the TLS posture, since the dialer is overridden anyway).
func TestHTTPBaseURL(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		tlsConfigured bool
		want          string
	}{
		{"bare host:port promotes to https", "127.0.0.1:18443", true, "https://127.0.0.1:18443"},
		{"bare host:port promotes to https without a tls config", "127.0.0.1:18443", false, "https://127.0.0.1:18443"},
		{"explicit https kept", "https://node.example.com:8443", true, "https://node.example.com:8443"},
		{"explicit http kept", "http://node.example.com:8080", false, "http://node.example.com:8080"},
		{"explicit https with a path keeps only the origin", "https://node.example.com:8443/base", true, "https://node.example.com:8443"},
		{"https default port", "https://node.example.com", true, "https://node.example.com:443"},
		{"unix socket with tls", "/var/run/quicbaas/quicbaasd.sock", true, "https://localhost"},
		{"unix socket without tls", "/var/run/quicbaas/quicbaasd.sock", false, "http://localhost"},
		{"unix scheme with tls", "unix:///var/run/quicbaas/quicbaasd.sock", true, "https://localhost"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTTPBaseURL(tc.address, tc.tlsConfigured); got != tc.want {
				t.Errorf("HTTPBaseURL(%q, %v): got %q, want %q", tc.address, tc.tlsConfigured, got, tc.want)
			}
		})
	}
}

// TestHTTPBaseURL_DegenerateAddresses proves the helper answers a usable
// string for inputs that are not addresses at all rather than panicking or
// producing something url.Parse will later reject in a caller's hot path. An
// empty address yields the https origin of an empty host, which fails loudly
// at dial time — the honest place for it.
func TestHTTPBaseURL_DegenerateAddresses(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"empty", "", "https://"},
		{"no port", "node.example.com", "https://node.example.com"},
		// ParseAddress's own documented fallback for an unparseable URL:
		// strip the scheme, treat the remainder as the host. The origin is
		// reassembled from it, so the failure surfaces at dial time with the
		// operator's own string in the message rather than being silently
		// rewritten here.
		{"unparseable url falls back to the literal remainder", "https://%zz", "https://%zz"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTTPBaseURL(tc.address, true); got != tc.want {
				t.Errorf("HTTPBaseURL(%q): got %q, want %q", tc.address, got, tc.want)
			}
		})
	}
}

// TestHTTPPath proves only an explicit http(s):// address carries a path, and
// that a trailing slash is trimmed so callers can concatenate a suffix
// without producing a doubled separator.
func TestHTTPPath(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"bare host:port has no path", "127.0.0.1:18443", ""},
		{"unix socket has no path", "/var/run/quicbaas/quicbaasd.sock", ""},
		{"url without a path", "https://node.example.com:8443", ""},
		{"url with a path", "https://node.example.com:8443/base", "/base"},
		{"url with a trailing slash", "https://node.example.com:8443/base/", "/base"},
		{"url with only a slash", "https://node.example.com:8443/", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTTPPath(tc.address); got != tc.want {
				t.Errorf("HTTPPath(%q): got %q, want %q", tc.address, got, tc.want)
			}
		})
	}
}

// TestHTTPEndpoint proves the suffix is appended exactly once, whatever the
// caller already spelled out, and that an empty suffix is the origin+path.
func TestHTTPEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		address string
		suffix  string
		want    string
	}{
		{"bare host:port", "127.0.0.1:18443", "/mcp", "https://127.0.0.1:18443/mcp"},
		{"suffix already present", "https://node.example.com:8443/mcp", "/mcp", "https://node.example.com:8443/mcp"},
		{"suffix under a mount path", "https://node.example.com:8443/base", "/mcp", "https://node.example.com:8443/base/mcp"},
		{"trailing slash is not doubled", "https://node.example.com:8443/base/", "/mcp", "https://node.example.com:8443/base/mcp"},
		{"empty suffix", "127.0.0.1:18443", "", "https://127.0.0.1:18443"},
		{"unix socket", "/var/run/quicbaas/quicbaasd.sock", "/mcp", "https://localhost/mcp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTTPEndpoint(tc.address, true, tc.suffix); got != tc.want {
				t.Errorf("HTTPEndpoint(%q, true, %q): got %q, want %q", tc.address, tc.suffix, got, tc.want)
			}
		})
	}
}
