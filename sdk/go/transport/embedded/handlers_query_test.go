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

package embedded

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newQueryClient returns a query client over the embedded transport whose
// QuicBaaS.Query is served by queryFunc.
func newQueryClient(t *testing.T, queryFunc func(context.Context, *service.QueryRequest, chan<- *service.QueryResponse) error) query.Client {
	t.Helper()
	tr, err := New(&mockQuicBaaS{queryFunc: queryFunc})
	require.NoError(t, err)
	return query.NewClient(tr)
}

func TestEmbeddedQuery_Execute(t *testing.T) {
	var got *service.QueryRequest
	c := newQueryClient(t, func(_ context.Context, req *service.QueryRequest, out chan<- *service.QueryResponse) error {
		got = req
		out <- &service.QueryResponse{Entities: []*service.Entity{{ID: 7, Data: []byte("alice")}}}
		out <- &service.QueryResponse{Entities: []*service.Entity{{ID: 9, Data: []byte("bob")}}}
		close(out)
		return nil
	})

	resp, err := c.Execute(context.Background(), &query.QueryRequest{
		EntityType: "user",
		Index:      "email",
		Value:      "a@example.com",
		Operator:   query.OpEqual,
	})

	require.NoError(t, err)
	assert.Equal(t, &service.QueryRequest{EntityType: "user", IndexName: "email", Value: []byte("a@example.com")}, got)
	assert.Equal(t, []query.QueryResult{{Key: "7", Value: []byte("alice")}, {Key: "9", Value: []byte("bob")}}, resp.Results)
	assert.False(t, resp.HasMore)
}

func TestEmbeddedQuery_Execute_Limit(t *testing.T) {
	c := newQueryClient(t, func(_ context.Context, _ *service.QueryRequest, out chan<- *service.QueryResponse) error {
		out <- &service.QueryResponse{Entities: []*service.Entity{{ID: 1}, {ID: 2}, {ID: 3}}}
		close(out)
		return nil
	})

	resp, err := c.Execute(context.Background(), &query.QueryRequest{
		EntityType: "user", Index: "email", Value: []byte("x"), Operator: query.OpEqual, Limit: 2,
	})

	require.NoError(t, err)
	assert.Equal(t, []query.QueryResult{{Key: "1"}, {Key: "2"}}, resp.Results)
	assert.True(t, resp.HasMore)
}

func TestEmbeddedQuery_Execute_ServiceError(t *testing.T) {
	c := newQueryClient(t, func(_ context.Context, _ *service.QueryRequest, out chan<- *service.QueryResponse) error {
		close(out)
		return errors.NewNotFound("query.Query", "index email not found")
	})

	_, err := c.Execute(context.Background(), &query.QueryRequest{
		EntityType: "user", Index: "email", Value: "x", Operator: query.OpEqual,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestEmbeddedQuery_Execute_UnsupportedFields(t *testing.T) {
	tests := []struct {
		name  string
		req   *query.QueryRequest
		field string
	}{
		{"operator", &query.QueryRequest{EntityType: "user", Index: "age", Value: "30", Operator: query.OpGreaterThan}, "Operator"},
		{"cursor", &query.QueryRequest{EntityType: "user", Index: "age", Value: "30", Cursor: "page-2"}, "Cursor"},
		{"value type", &query.QueryRequest{EntityType: "user", Index: "age", Value: 30}, "Value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			c := newQueryClient(t, func(_ context.Context, _ *service.QueryRequest, out chan<- *service.QueryResponse) error {
				called = true
				close(out)
				return nil
			})

			_, err := c.Execute(context.Background(), tt.req)

			var fieldErr *UnsupportedQueryFieldError
			require.ErrorAs(t, err, &fieldErr)
			assert.Equal(t, tt.field, fieldErr.Field)
			assert.False(t, called, "the service must not be called")
		})
	}
}

func TestEmbeddedQuery_Execute_InvalidRequestType(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)

	for _, req := range []interface{}{(*query.QueryRequest)(nil), &service.QueryRequest{}} {
		_, err := tr.Request(context.Background(), queryServiceName, "Execute", req)
		var typeErr *InvalidRequestTypeError
		assert.ErrorAs(t, err, &typeErr, "request %T", req)
	}
}

func TestEmbeddedQuery_ExecuteRegistered(t *testing.T) {
	_, routed := unaryHandlers[queryServiceName+"/Execute"]
	assert.True(t, routed, "query.Client.Execute has no embedded handler")
}
