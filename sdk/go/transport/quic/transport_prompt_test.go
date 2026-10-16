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
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QUIC is the REST surface on HTTP/3: the Prompts service needs no QUIC code
// because this transport embeds *rest.Transport. These tests prove that on
// the wire rather than inferring it from the embedding.

var _ prompt.PromptsCapableTransport = (*Transport)(nil)

// TestRequest_PromptPutOverHTTP3 proves a prompt write reaches the REST route
// with its scope query and body, and decodes into the SDK record.
func TestRequest_PromptPutOverHTTP3(t *testing.T) {
	type seen struct {
		method, path, tenant, scope string
		body                        map[string]interface{}
	}
	got := make(chan seen, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &body))
		got <- seen{r.Method, r.URL.Path, r.URL.Query().Get("tenant_id"), r.URL.Query().Get("scope"), body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"t1","scope":"tenant","set":"s","name":"n","version":2,"content":"body"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	require.True(t, tr.SupportsPrompts(), "QUIC must declare the Prompts service it inherits from REST")

	record, err := prompt.NewClient(tr).Put(context.Background(), &prompt.PutRequest{
		Address: prompt.Address{TenantID: "t1", Scope: "tenant", Set: "s", Name: "n"},
		Content: "body", Note: "why", ExpectedVersion: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), record.Version)
	assert.Equal(t, "body", record.Content)

	request := <-got
	assert.Equal(t, http.MethodPut, request.method)
	assert.Equal(t, "/api/v1/prompts/s/n", request.path)
	assert.Equal(t, "t1", request.tenant)
	assert.Equal(t, "tenant", request.scope)
	assert.Equal(t, map[string]interface{}{"content": "body", "note": "why", "expected_version": float64(1)}, request.body)
}

// TestRequest_PromptGetOverHTTP3SurfacesNotFound proves a refusal keeps its
// class across HTTP/3 rather than decoding into an empty record.
func TestRequest_PromptGetOverHTTP3SurfacesNotFound(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"nothing overridden"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = prompt.NewClient(tr).Get(context.Background(), &prompt.GetRequest{Address: prompt.Address{Set: "s", Name: "n"}})
	require.Error(t, err)
	assert.True(t, sdkerrors.IsNotFound(err), "a 404 must stay NotFound: %v", err)
}
