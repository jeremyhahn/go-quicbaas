// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package quicbaas

import (
	"sync"
	"testing"
)

// TestClient_ClientAccessors_Concurrent tests concurrent access to ensure
// the double-check locking pattern works correctly.
func TestClient_ClientAccessors_Concurrent(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	tests := []struct {
		name   string
		getter func() interface{}
	}{
		{"KV", func() interface{} { return c.KV() }},
		{"Batch", func() interface{} { return c.Batch() }},
		{"Transaction", func() interface{} { return c.Transaction() }},
		{"Query", func() interface{} { return c.Query() }},
		{"Cluster", func() interface{} { return c.Cluster() }},
		{"Health", func() interface{} { return c.Health() }},
		{"Auth", func() interface{} { return c.auth() }},
		{"KMS", func() interface{} { return c.kms() }},
		{"Secrets", func() interface{} { return c.secrets() }},
		{"Crypto", func() interface{} { return c.crypto() }},
		{"Vector", func() interface{} { return c.Vector() }},
		{"TimeSeries", func() interface{} { return c.TimeSeries() }},
		{"Backup", func() interface{} { return c.Backup() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset client for each test
			c = &client{
				transport: mockT,
				opts:      defaultOptions(),
			}

			var wg sync.WaitGroup
			results := make([]interface{}, 10)

			// Launch 10 concurrent goroutines
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					results[idx] = tt.getter()
				}(i)
			}
			wg.Wait()

			// All results should be the same instance
			first := results[0]
			for i := 1; i < 10; i++ {
				if results[i] != first {
					t.Errorf("expected all instances to be the same")
					break
				}
			}
		})
	}
}

// TestClient_KV_DoubleCheckLocking explicitly tests the write lock path
func TestClient_KV_DoubleCheckLocking(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	// First call - goes through write lock path (kvClient is nil)
	kv1 := c.KV()
	if kv1 == nil {
		t.Fatal("expected non-nil KV client")
	}

	// Pre-populate the client to simulate race condition resolution
	c2 := &client{
		transport: mockT,
		opts:      defaultOptions(),
		kvClient:  kv1, // Pre-set to simulate already created
	}

	// This call should return the pre-set client
	kv2 := c2.KV()
	if kv2 != kv1 {
		t.Error("expected same KV client instance when pre-set")
	}
}

// TestClient_AllAccessors_Fresh ensures all accessors work on fresh client
func TestClient_AllAccessors_Fresh(t *testing.T) {
	mockT := &mockTransport{}

	tests := []struct {
		name   string
		getter func(*client) interface{}
	}{
		{"KV", func(c *client) interface{} { return c.KV() }},
		{"Batch", func(c *client) interface{} { return c.Batch() }},
		{"Transaction", func(c *client) interface{} { return c.Transaction() }},
		{"Query", func(c *client) interface{} { return c.Query() }},
		{"Cluster", func(c *client) interface{} { return c.Cluster() }},
		{"Health", func(c *client) interface{} { return c.Health() }},
		{"Auth", func(c *client) interface{} { return c.auth() }},
		{"KMS", func(c *client) interface{} { return c.kms() }},
		{"Secrets", func(c *client) interface{} { return c.secrets() }},
		{"Crypto", func(c *client) interface{} { return c.crypto() }},
		{"Vector", func(c *client) interface{} { return c.Vector() }},
		{"TimeSeries", func(c *client) interface{} { return c.TimeSeries() }},
		{"Backup", func(c *client) interface{} { return c.Backup() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh client for each test
			c := &client{
				transport: mockT,
				opts:      defaultOptions(),
			}

			// First call - goes through init path
			result1 := tt.getter(c)

			// Second call - should return cached
			result2 := tt.getter(c)

			if result1 != result2 {
				t.Error("expected same instance on repeated calls")
			}
		})
	}
}
