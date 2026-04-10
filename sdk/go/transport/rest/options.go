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

package rest

import (
	"net/http"
)

// Option is a function that configures the REST transport.
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
	}
}

// WithHeaders sets custom headers to include in all requests.
// These headers are merged with default headers (Content-Type, Accept).
// Example: map[string]string{"Authorization": "Bearer token123"}
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
func WithAuthToken(token string) Option {
	return func(t *Transport) {
		if t.headers == nil {
			t.headers = make(map[string]string)
		}
		t.headers["Authorization"] = "Bearer " + token
	}
}

// WithBasicAuth sets basic authentication credentials.
func WithBasicAuth(username, password string) Option {
	return func(t *Transport) {
		if t.headers == nil {
			t.headers = make(map[string]string)
		}
		// Store credentials for use in requests
		// Note: actual Basic auth header will be set per-request
		t.headers["X-QuicBaaS-BasicAuth-User"] = username
		t.headers["X-QuicBaaS-BasicAuth-Pass"] = password
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
