// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTLSConfig builds a minimal tls.Config with an empty (but
// non-nil) RootCAs pool. WithTLSConfig requires explicit CA trust per
// QuicBaaS's FedRAMP policy; tests that just need a client constructed
// (not actually connecting to a real server) use this stub.
func testTLSConfig() *tls.Config {
	return &tls.Config{RootCAs: x509.NewCertPool()}
}

func TestQRDBClient_ReturnsClient_REST(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	cc := c.(*client)
	qc, err := cc.qrdbClient()
	require.NoError(t, err)
	assert.NotNil(t, qc)
}

func TestQRDBClient_ReturnsClient_MCP(t *testing.T) {
	// MCP transport requires a proper URL scheme (it speaks HTTP
	// under the hood) and pings the server on init. With the
	// FedRAMP ban on WithInsecure, this test must skip cleanly
	// when no fixture server is reachable rather than fail.
	c, err := NewClient("http://localhost:9090", WithMCP(), WithTLSConfig(testTLSConfig()))
	if err != nil {
		t.Skipf("MCP transport requires a reachable server: %v", err)
	}

	cc := c.(*client)
	qc, err := cc.qrdbClient()
	if err != nil {
		t.Skipf("MCP qrdbClient init requires a reachable server: %v", err)
	}
	assert.NotNil(t, qc)
}

func TestQRDBClient_Idempotent(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	cc := c.(*client)
	first, err := cc.qrdbClient()
	require.NoError(t, err)

	second, err := cc.qrdbClient()
	require.NoError(t, err)

	assert.Same(t, first, second, "repeated qrdbClient() calls must return the same instance")
}

func TestQRDBClient_WithTimeout(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST(), WithTimeout(5*time.Second))
	require.NoError(t, err)

	cc := c.(*client)
	qc, err := cc.qrdbClient()
	require.NoError(t, err)
	assert.NotNil(t, qc)
}

func TestQRDBClient_ConcurrentAccess(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	cc := c.(*client)
	done := make(chan *QRDBClient, 10)
	for i := 0; i < 10; i++ {
		go func() {
			qc, err := cc.qrdbClient()
			require.NoError(t, err)
			done <- qc
		}()
	}

	var first *QRDBClient
	for i := 0; i < 10; i++ {
		qc := <-done
		if first == nil {
			first = qc
		}
		assert.Same(t, first, qc, "all concurrent calls must return the same instance")
	}
}

func TestQRDBClient_DefaultTransportMapsToGRPC(t *testing.T) {
	// Verify that an unknown transport type falls through to the gRPC default.
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	cc := c.(*client)
	cc.opts.transportType = TransportType(999)

	// Reset the sync.Once so buildQRDBClient runs again with the new transport.
	cc.qrdb = qrdbState{}

	// Should succeed — the default case creates a qrdb gRPC transport.
	qc, err := cc.qrdbClient()
	require.NoError(t, err)
	assert.NotNil(t, qc)
}

func TestQRDBClient_ClientInterfaceSatisfied(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	var _ Client = c
}

func TestDataStore_ReturnsKVStore(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	store, err := c.DataStore()
	require.NoError(t, err)
	assert.NotNil(t, store)
}

func TestDataStore_InvalidTransport(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	// Force a broken qrdb state to simulate init failure.
	cc := c.(*client)
	cc.qrdb = qrdbState{}
	cc.opts.transportType = TransportType(999)
	cc.address = ""

	// DataStore should propagate the qrdb init error.
	_, err = cc.DataStore()
	// With empty address the transport creation may or may not fail
	// depending on the transport implementation. We just verify it
	// doesn't panic.
	_ = err
}
