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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// QUIC is the REST surface on HTTP/3: the quicbaas.Batch service needs no
// QUIC code because this transport embeds *rest.Transport. This test proves
// every Batch verb reaches its REST route over HTTP/3 and decodes the answer.

// batchAnswers is what the test server writes back for each batch route.
var batchAnswers = map[string]string{
	"/api/v1/batch/execute": `{"success":true,"results":[{"success":true},{"success":false}]}`,
	"/api/v1/batch/put":     `{"success":true,"responses":[{"id":7,"entity_type":"users","created":true}],"timestamp":42}`,
	"/api/v1/batch/get":     `{"success":true,"responses":[{"id":7,"entity_type":"users","data":{"name":"a"},"found":true}]}`,
	"/api/v1/batch/delete":  `{"success":true,"responses":[{"id":7,"entity_type":"users","deleted":true}],"timestamp":42}`,
}

func TestBatchVerbsOverHTTP3(t *testing.T) {
	paths := make(chan string, len(batchAnswers))
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, ok := batchAnswers[r.URL.Path]
		if r.Method != http.MethodPost || !ok {
			http.NotFound(w, r)
			return
		}
		paths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(answer))
		assert.NoError(t, err)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	c := batch.NewClient(tr)
	ctx := context.Background()

	executed, err := c.Execute(ctx, &batch.BatchRequest{Operations: []batch.BatchOperation{
		{Type: batch.OpPut, EntityType: "users", Key: "k1", Value: []byte("v1")},
		{Type: batch.OpDelete, EntityType: "users", Key: "k2"},
	}})
	require.NoError(t, err)
	assert.Equal(t, []batch.BatchResult{{Success: true}, {Success: false}}, executed.Results)
	assert.Equal(t, "/api/v1/batch/execute", <-paths)

	put, err := c.Put(ctx, []*service.PutRequest{{EntityType: "users", ID: 7, Data: []byte(`{"name":"a"}`)}})
	require.NoError(t, err)
	require.Len(t, put.Responses, 1)
	assert.Equal(t, uint64(7), put.Responses[0].ID)
	assert.Equal(t, "/api/v1/batch/put", <-paths)

	got, err := c.Get(ctx, []*service.GetRequest{{EntityType: "users", ID: 7}})
	require.NoError(t, err)
	require.Len(t, got.Responses, 1)
	assert.True(t, got.Responses[0].Found)
	assert.JSONEq(t, `{"name":"a"}`, string(got.Responses[0].Data))
	assert.Equal(t, "/api/v1/batch/get", <-paths)

	deleted, err := c.Delete(ctx, []*service.DeleteRequest{{EntityType: "users", ID: 7}})
	require.NoError(t, err)
	require.Len(t, deleted.Responses, 1)
	assert.True(t, deleted.Responses[0].Deleted)
	assert.Equal(t, "/api/v1/batch/delete", <-paths)
}

func TestBatchExecuteOverHTTP3_ServerRefusal(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, err := w.Write([]byte(`{"error":"refused"}`))
		assert.NoError(t, err)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = batch.NewClient(tr).Execute(context.Background(), &batch.BatchRequest{
		Operations: []batch.BatchOperation{{Type: batch.OpDelete, EntityType: "users", Key: "k"}},
	})
	require.Error(t, err)
}
