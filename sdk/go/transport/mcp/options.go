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

import (
	"context"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	secure "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// Option is a function that configures the MCP transport.
type Option func(*Transport)

// WithHTTPClient sets a custom HTTP client for the transport.
// This allows full control over HTTP client configuration including
// custom transports, cookie jars, and redirect policies.
func WithHTTPClient(client *http.Client) Option {
	return func(t *Transport) {
		t.client = client
	}
}

// WithBaseURL sets a custom base URL for the transport.
// This overrides the automatic URL construction from the address.
// Example: "https://api.quicbaas.example.com:8443"
func WithBaseURL(baseURL string) Option {
	return func(t *Transport) {
		t.baseURL = baseURL
		t.endpoint = baseURL + "/mcp"
	}
}

// WithEndpoint sets a custom MCP endpoint URL.
// This overrides both the base URL and default /mcp path.
// Example: "https://api.quicbaas.example.com:8443/v1/mcp"
func WithEndpoint(endpoint string) Option {
	return func(t *Transport) {
		t.endpoint = endpoint
	}
}

// WithHeaders sets custom headers to include in all requests.
// These headers are merged with default headers (Content-Type, Accept).
// Example: map[string]string{"X-Custom-Header": "value"}
func WithHeaders(headers map[string]string) Option {
	return func(t *Transport) {
		if t.headers == nil {
			t.headers = make(map[string]string)
		}
		for k, v := range headers {
			t.headers[k] = v
		}
	}
}

// WithHeader sets a single custom header to include in all requests.
func WithHeader(key, value string) Option {
	return func(t *Transport) {
		if t.headers == nil {
			t.headers = make(map[string]string)
		}
		t.headers[key] = value
	}
}

// WithAuthToken sets a Bearer token for authorization.
// The token is sent in the Authorization header as "Bearer <token>".
func WithAuthToken(token string) Option {
	return func(t *Transport) {
		if t.headers == nil {
			t.headers = make(map[string]string)
		}
		t.headers["Authorization"] = "Bearer " + token
		t.accessToken = token
		t.authenticated.Store(true)
	}
}

// WithCredentials sets the username and password for auto-authentication.
// The transport will automatically authenticate using the quicbaas_login tool
// before making requests that require authentication.
func WithCredentials(username, password string) Option {
	return func(t *Transport) {
		t.username = username
		if t.password != nil {
			t.password.Destroy()
			t.password = nil
		}
		t.optionErr = nil
		if password == "" {
			return
		}
		protected, err := secure.NewSecret(context.Background(), []byte(password))
		if err != nil {
			t.optionErr = errors.Wrap(err, errors.CodeInternal, "mcp.WithCredentials",
				"failed to protect password")
			return
		}
		t.password = protected
	}
}

// WithProtocolVersion sets the MCP protocol version to use.
// Default is "2024-11-05".
func WithProtocolVersion(version string) Option {
	return func(t *Transport) {
		t.protocolVersion = version
	}
}

// WithUserAgent sets a custom User-Agent header.
func WithUserAgent(userAgent string) Option {
	return func(t *Transport) {
		if t.headers == nil {
			t.headers = make(map[string]string)
		}
		t.headers["User-Agent"] = userAgent
	}
}
