// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package transport

import "testing"

// TestParseHTTPURL_ParseErrorFallback exercises the fallback path taken when
// url.Parse fails on the input (e.g. an invalid percent escape). In that case
// parseHTTPURL strips the scheme and uses the remainder, splitting off any path.
func TestParseHTTPURL_ParseErrorFallback(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType AddressType
		expectedAddr string
		expectedPath string
	}{
		{
			name: "http invalid escape with path",
			// "%zz" is an invalid URL escape, so url.Parse returns an error and
			// the fallback branch is taken. The remainder before the first "/"
			// becomes the address; the rest becomes the path.
			input:        "http://host%zz:8080/path",
			expectedType: AddressTypeHTTP,
			expectedAddr: "host%zz:8080",
			expectedPath: "/path",
		},
		{
			name:         "http invalid escape without path",
			input:        "http://host%zz:8080",
			expectedType: AddressTypeHTTP,
			expectedAddr: "host%zz:8080",
			expectedPath: "",
		},
		{
			name:         "https invalid escape with path",
			input:        "https://secure%zz:9443/api/v1",
			expectedType: AddressTypeHTTPS,
			expectedAddr: "secure%zz:9443",
			expectedPath: "/api/v1",
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
			if parsed.DialAddress != tt.expectedAddr {
				t.Errorf("expected dial address %q, got %q", tt.expectedAddr, parsed.DialAddress)
			}
			if parsed.Path != tt.expectedPath {
				t.Errorf("expected path %q, got %q", tt.expectedPath, parsed.Path)
			}
			// The original input must always be preserved.
			if parsed.Original != tt.input {
				t.Errorf("expected original %q, got %q", tt.input, parsed.Original)
			}
		})
	}
}
