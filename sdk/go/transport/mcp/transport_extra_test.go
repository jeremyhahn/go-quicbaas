// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// transport_extra_test.go covers transport-level helpers that the primary
// transport tests miss: buildBaseURL address variants, convertHTTPError's
// error-classification branches, and the embedding handler's toMap fallback.

package mcp

import (
	"context"
	"errors"
	"testing"

	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestBuildBaseURLVariants(t *testing.T) {
	t.Run("unix socket", func(t *testing.T) {
		tr, err := New(&sdktransport.Config{Address: "unix:///tmp/quicbaas.sock"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer tr.Close()
		if tr.baseURL != "http://localhost" || tr.endpoint != "http://localhost/mcp" {
			t.Errorf("unix base=%q endpoint=%q", tr.baseURL, tr.endpoint)
		}
	})

	t.Run("http with path", func(t *testing.T) {
		tr, err := New(&sdktransport.Config{Address: "http://example.com:8080/api"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer tr.Close()
		if tr.endpoint != "http://example.com:8080/api/mcp" {
			t.Errorf("endpoint = %q", tr.endpoint)
		}
	})

	t.Run("http with mcp path", func(t *testing.T) {
		tr, err := New(&sdktransport.Config{Address: "http://example.com:8080/v1/mcp"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer tr.Close()
		if tr.endpoint != "http://example.com:8080/v1/mcp" {
			t.Errorf("endpoint = %q", tr.endpoint)
		}
	})

	t.Run("plain tcp requires https", func(t *testing.T) {
		tr, err := New(&sdktransport.Config{Address: "example.com:8443"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer tr.Close()
		if tr.baseURL != "https://example.com:8443" {
			t.Errorf("base = %q", tr.baseURL)
		}
	})
}

func TestConvertHTTPError(t *testing.T) {
	tr, err := New(&sdktransport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	if tr.convertHTTPError(nil, "op") != nil {
		t.Error("nil error should yield nil")
	}
	if e := tr.convertHTTPError(context.Canceled, "op"); e == nil {
		t.Error("canceled should yield error")
	}
	if e := tr.convertHTTPError(context.DeadlineExceeded, "op"); e == nil {
		t.Error("deadline should yield error")
	}
	if e := tr.convertHTTPError(errors.New("boom"), "op"); e == nil {
		t.Error("generic error should yield error")
	}

	// A net.Error timeout maps to the timeout code.
	if e := tr.convertHTTPError(timeoutNetErr{}, "op"); e == nil {
		t.Error("net timeout should yield error")
	}
	if e := tr.convertHTTPError(nonTimeoutNetErr{}, "op"); e == nil {
		t.Error("net non-timeout should yield error")
	}
}

// timeoutNetErr is a net.Error reporting a timeout.
type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

// nonTimeoutNetErr is a net.Error that is not a timeout.
type nonTimeoutNetErr struct{}

func (nonTimeoutNetErr) Error() string   { return "connection refused" }
func (nonTimeoutNetErr) Timeout() bool   { return false }
func (nonTimeoutNetErr) Temporary() bool { return false }

func TestEmbeddingToMap(t *testing.T) {
	h := newEmbeddingHandler()

	// Already a map: returned unchanged.
	m, err := h.toMap(map[string]interface{}{"k": "v"})
	if err != nil || m["k"] != "v" {
		t.Errorf("map passthrough = %v, %v", m, err)
	}

	// A struct is JSON round-tripped into a map.
	type sample struct {
		Name string `json:"name"`
	}
	m2, err := h.toMap(sample{Name: "x"})
	if err != nil || m2["name"] != "x" {
		t.Errorf("struct round-trip = %v, %v", m2, err)
	}

	// A value that cannot be marshaled into a JSON object errors.
	if _, err := h.toMap(make(chan int)); err == nil {
		t.Error("unmarshalable value must error")
	}
}
