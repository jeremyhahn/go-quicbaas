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

package quic

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// startKVConditionalServer serves the KV conditional-write routes over
// HTTP/3, answering every request with {"applied": applied} and recording
// what it received.
func startKVConditionalServer(t *testing.T, applied bool) (*Transport, <-chan inviteCall) {
	t.Helper()
	calls := make(chan inviteCall, 1)
	addr, shutdown := startTestHTTP3Server(t, inviteServer(t, calls, map[string]interface{}{"applied": applied}))
	t.Cleanup(shutdown)

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tr.Close()) })
	return tr, calls
}

// receiveCall returns the request the server recorded.
func receiveCall(t *testing.T, calls <-chan inviteCall) inviteCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("server received no request")
		return inviteCall{}
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestCompareAndSwapOverHTTP3(t *testing.T) {
	for _, applied := range []bool{true, false} {
		tr, calls := startKVConditionalServer(t, applied)

		swapped, err := kv.NewClient(tr).CompareAndSwap(context.Background(),
			&service.CompareAndSwapRequest{EntityType: "users", ID: 42, Expect: []byte("old"), Set: []byte("new")})
		require.NoError(t, err)
		assert.Equal(t, applied, swapped, "a conflict must come back as applied=false, not an error")

		call := receiveCall(t, calls)
		assert.Equal(t, http.MethodPost, call.method)
		assert.Equal(t, "/api/v1/entities/users/42/compare-and-swap", call.path)
		assert.Equal(t, b64("old"), call.body["expect"])
		assert.Equal(t, b64("new"), call.body["set"])
	}
}

func TestPutIfAbsentOverHTTP3(t *testing.T) {
	for _, applied := range []bool{true, false} {
		tr, calls := startKVConditionalServer(t, applied)

		stored, err := kv.NewClient(tr).PutIfAbsent(context.Background(),
			&service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner")})
		require.NoError(t, err)
		assert.Equal(t, applied, stored, "a present entity must come back as applied=false, not an error")

		call := receiveCall(t, calls)
		assert.Equal(t, http.MethodPost, call.method)
		assert.Equal(t, "/api/v1/entities/locks/7/put-if-absent", call.path)
		assert.Equal(t, b64("owner"), call.body["data"])
	}
}

func TestCompareAndSwapOverHTTP3_ServerError(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(`{"error": "entity type is invalid"}`)); err != nil {
			t.Errorf("write reply: %v", err)
		}
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = kv.NewClient(tr).CompareAndSwap(context.Background(),
		&service.CompareAndSwapRequest{EntityType: "users", ID: 1})
	assert.True(t, errors.IsInvalidInput(err), "expected a typed invalid-input error, got %v", err)
}
