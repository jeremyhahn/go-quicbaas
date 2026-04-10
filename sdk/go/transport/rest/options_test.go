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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithHTTPClient(t *testing.T) {
	t.Run("sets custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{}
		tr := &Transport{}

		opt := WithHTTPClient(customClient)
		opt(tr)

		assert.Same(t, customClient, tr.client)
	})

	t.Run("replaces existing HTTP client", func(t *testing.T) {
		original := &http.Client{}
		replacement := &http.Client{}
		tr := &Transport{client: original}

		opt := WithHTTPClient(replacement)
		opt(tr)

		assert.Same(t, replacement, tr.client)
		assert.NotSame(t, original, tr.client)
	})
}

func TestWithBaseURL(t *testing.T) {
	t.Run("sets custom base URL", func(t *testing.T) {
		tr := &Transport{}

		opt := WithBaseURL("https://api.quicbaas.example.com:8443")
		opt(tr)

		assert.Equal(t, "https://api.quicbaas.example.com:8443", tr.baseURL)
	})

	t.Run("overrides existing base URL", func(t *testing.T) {
		tr := &Transport{baseURL: "http://old-url:8080"}

		opt := WithBaseURL("https://new-url:9090")
		opt(tr)

		assert.Equal(t, "https://new-url:9090", tr.baseURL)
	})
}

func TestWithHeaders(t *testing.T) {
	t.Run("sets multiple headers", func(t *testing.T) {
		tr := &Transport{headers: make(map[string]string)}

		headers := map[string]string{
			"X-Custom-One": "value1",
			"X-Custom-Two": "value2",
		}
		opt := WithHeaders(headers)
		opt(tr)

		assert.Equal(t, "value1", tr.headers["X-Custom-One"])
		assert.Equal(t, "value2", tr.headers["X-Custom-Two"])
	})

	t.Run("initializes nil headers map", func(t *testing.T) {
		tr := &Transport{}
		assert.Nil(t, tr.headers)

		headers := map[string]string{"X-Test": "test-value"}
		opt := WithHeaders(headers)
		opt(tr)

		assert.NotNil(t, tr.headers)
		assert.Equal(t, "test-value", tr.headers["X-Test"])
	})

	t.Run("merges with existing headers", func(t *testing.T) {
		tr := &Transport{
			headers: map[string]string{"Existing": "keep-me"},
		}

		opt := WithHeaders(map[string]string{"New": "new-value"})
		opt(tr)

		assert.Equal(t, "keep-me", tr.headers["Existing"])
		assert.Equal(t, "new-value", tr.headers["New"])
	})
}

func TestWithHeader(t *testing.T) {
	t.Run("sets single header", func(t *testing.T) {
		tr := &Transport{headers: make(map[string]string)}

		opt := WithHeader("X-Request-ID", "abc-123")
		opt(tr)

		assert.Equal(t, "abc-123", tr.headers["X-Request-ID"])
	})

	t.Run("initializes nil headers map", func(t *testing.T) {
		tr := &Transport{}
		assert.Nil(t, tr.headers)

		opt := WithHeader("X-Test", "value")
		opt(tr)

		assert.NotNil(t, tr.headers)
		assert.Equal(t, "value", tr.headers["X-Test"])
	})

	t.Run("overwrites existing header with same key", func(t *testing.T) {
		tr := &Transport{
			headers: map[string]string{"X-Key": "old"},
		}

		opt := WithHeader("X-Key", "new")
		opt(tr)

		assert.Equal(t, "new", tr.headers["X-Key"])
	})
}

func TestWithAuthToken(t *testing.T) {
	t.Run("sets Authorization Bearer header", func(t *testing.T) {
		tr := &Transport{headers: make(map[string]string)}

		opt := WithAuthToken("my-secret-token")
		opt(tr)

		assert.Equal(t, "Bearer my-secret-token", tr.headers["Authorization"])
	})

	t.Run("initializes nil headers map", func(t *testing.T) {
		tr := &Transport{}
		assert.Nil(t, tr.headers)

		opt := WithAuthToken("token-123")
		opt(tr)

		assert.NotNil(t, tr.headers)
		assert.Equal(t, "Bearer token-123", tr.headers["Authorization"])
	})
}

func TestWithBasicAuth(t *testing.T) {
	t.Run("stores credentials in custom headers", func(t *testing.T) {
		tr := &Transport{headers: make(map[string]string)}

		opt := WithBasicAuth("admin", "secret-pass")
		opt(tr)

		assert.Equal(t, "admin", tr.headers["X-QuicBaaS-BasicAuth-User"])
		assert.Equal(t, "secret-pass", tr.headers["X-QuicBaaS-BasicAuth-Pass"])
	})

	t.Run("initializes nil headers map", func(t *testing.T) {
		tr := &Transport{}
		assert.Nil(t, tr.headers)

		opt := WithBasicAuth("user", "pass")
		opt(tr)

		assert.NotNil(t, tr.headers)
		assert.Equal(t, "user", tr.headers["X-QuicBaaS-BasicAuth-User"])
		assert.Equal(t, "pass", tr.headers["X-QuicBaaS-BasicAuth-Pass"])
	})
}

func TestWithUserAgent(t *testing.T) {
	t.Run("sets User-Agent header", func(t *testing.T) {
		tr := &Transport{headers: make(map[string]string)}

		opt := WithUserAgent("quicbaas-sdk-go/1.0.0")
		opt(tr)

		assert.Equal(t, "quicbaas-sdk-go/1.0.0", tr.headers["User-Agent"])
	})

	t.Run("initializes nil headers map", func(t *testing.T) {
		tr := &Transport{}
		assert.Nil(t, tr.headers)

		opt := WithUserAgent("custom-agent/2.0")
		opt(tr)

		assert.NotNil(t, tr.headers)
		assert.Equal(t, "custom-agent/2.0", tr.headers["User-Agent"])
	})
}

func TestOptionsComposition(t *testing.T) {
	t.Run("multiple options applied sequentially", func(t *testing.T) {
		customClient := &http.Client{}
		tr := &Transport{}

		opts := []Option{
			WithHTTPClient(customClient),
			WithBaseURL("https://api.example.com:8443"),
			WithAuthToken("my-token"),
			WithUserAgent("test-agent/1.0"),
			WithHeader("X-Custom", "custom-value"),
			WithBasicAuth("admin", "password"),
		}

		for _, opt := range opts {
			opt(tr)
		}

		assert.Same(t, customClient, tr.client)
		assert.Equal(t, "https://api.example.com:8443", tr.baseURL)
		assert.Equal(t, "Bearer my-token", tr.headers["Authorization"])
		assert.Equal(t, "test-agent/1.0", tr.headers["User-Agent"])
		assert.Equal(t, "custom-value", tr.headers["X-Custom"])
		assert.Equal(t, "admin", tr.headers["X-QuicBaaS-BasicAuth-User"])
		assert.Equal(t, "password", tr.headers["X-QuicBaaS-BasicAuth-Pass"])
	})

	t.Run("later options override earlier ones", func(t *testing.T) {
		tr := &Transport{}

		opts := []Option{
			WithBaseURL("https://first.example.com"),
			WithBaseURL("https://second.example.com"),
			WithAuthToken("first-token"),
			WithAuthToken("second-token"),
		}

		for _, opt := range opts {
			opt(tr)
		}

		assert.Equal(t, "https://second.example.com", tr.baseURL)
		assert.Equal(t, "Bearer second-token", tr.headers["Authorization"])
	})
}
