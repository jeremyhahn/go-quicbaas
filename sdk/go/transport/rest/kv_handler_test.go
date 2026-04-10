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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestNewKVHandler(t *testing.T) {
	t.Run("creates handler with transport reference", func(t *testing.T) {
		tr := &Transport{}
		handler := newKVHandler(tr)

		assert.NotNil(t, handler)
		assert.Same(t, tr, handler.transport)
	})
}

func TestKVHandler_HandleRequest_UnknownMethod(t *testing.T) {
	t.Run("returns Unimplemented error for unknown method", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "NonExistentMethod", nil)

		assert.Error(t, err)
		assert.True(t, errors.IsUnimplemented(err))
	})

	t.Run("error message includes method name", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "FancyOp", nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "FancyOp")
	})
}

func TestKVHandler_HandlePut(t *testing.T) {
	t.Run("successful put request", func(t *testing.T) {
		ts := time.Now().UTC().Truncate(time.Second)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/api/v1/kv/users/42", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)

			var reqBody map[string]interface{}
			err = json.Unmarshal(body, &reqBody)
			assert.NoError(t, err)
			assert.Contains(t, reqBody, "value")

			resp := map[string]interface{}{
				"entity_type": "users",
				"key":         "42",
				"success":     true,
				"message":     "stored",
				"timestamp":   ts.Format(time.RFC3339Nano),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			err = json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		result, err := handler.handleRequest(context.Background(), "Put", &service.PutRequest{
			EntityType: "users",
			ID:         42,
			Data:       []byte(`{"name":"test"}`),
		})

		assert.NoError(t, err)
		assert.NotNil(t, result)

		putResp, ok := result.(*service.PutResponse)
		assert.True(t, ok)
		assert.Equal(t, uint64(42), putResp.ID)
		assert.Equal(t, ts.UnixNano(), putResp.Timestamp)
	})

	t.Run("includes custom headers in request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

			resp := map[string]interface{}{
				"entity_type": "items",
				"success":     true,
				"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{"Authorization": "Bearer test-token"},
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Put", &service.PutRequest{
			EntityType: "items",
			ID:         1,
			Data:       []byte(`{}`),
		})
		assert.NoError(t, err)
	})

	t.Run("server returns error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			resp := map[string]string{"error": "storage failure"}
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Put", &service.PutRequest{
			EntityType: "users",
			ID:         1,
			Data:       []byte(`{}`),
		})
		assert.Error(t, err)
	})
}

func TestKVHandler_HandlePut_InvalidRequestType(t *testing.T) {
	t.Run("returns InvalidInput for wrong request type", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Put", "not-a-put-request")

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
		assert.Contains(t, err.Error(), "PutRequest")
	})

	t.Run("returns InvalidInput for nil request", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Put", nil)

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
	})
}

func TestKVHandler_HandleGet(t *testing.T) {
	t.Run("successful get request", func(t *testing.T) {
		ts := time.Now().UTC().Truncate(time.Second)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/kv/users/99", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Accept"))

			resp := map[string]interface{}{
				"entity_type": "users",
				"key":         "99",
				"value":       `{"name":"alice"}`,
				"found":       true,
				"timestamp":   ts.Format(time.RFC3339Nano),
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		result, err := handler.handleRequest(context.Background(), "Get", &service.GetRequest{
			EntityType: "users",
			ID:         99,
		})

		assert.NoError(t, err)
		assert.NotNil(t, result)

		getResp, ok := result.(*service.GetResponse)
		assert.True(t, ok)
		assert.True(t, getResp.Found)
		assert.NotEmpty(t, getResp.Data)
		assert.Equal(t, ts.UnixNano(), getResp.Timestamp)
	})

	t.Run("not found returns found=false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		result, err := handler.handleRequest(context.Background(), "Get", &service.GetRequest{
			EntityType: "users",
			ID:         999,
		})

		assert.NoError(t, err)
		assert.NotNil(t, result)

		getResp, ok := result.(*service.GetResponse)
		assert.True(t, ok)
		assert.False(t, getResp.Found)
	})

	t.Run("includes consistency query parameter", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "strong", r.URL.Query().Get("consistency"))

			resp := map[string]interface{}{
				"found":     true,
				"value":     `{"data":"test"}`,
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Get", &service.GetRequest{
			EntityType:  "data",
			ID:          1,
			Consistency: service.ConsistencyLevelStrong,
		})
		assert.NoError(t, err)
	})
}

func TestKVHandler_HandleGet_InvalidRequestType(t *testing.T) {
	t.Run("returns InvalidInput for wrong request type", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Get", 12345)

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
		assert.Contains(t, err.Error(), "GetRequest")
	})

	t.Run("returns InvalidInput for wrong struct type", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Get", &service.PutRequest{})

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
	})
}

func TestKVHandler_HandleDelete(t *testing.T) {
	t.Run("successful delete request", func(t *testing.T) {
		ts := time.Now().UTC().Truncate(time.Second)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v1/kv/users/55", r.URL.Path)

			resp := map[string]interface{}{
				"entity_type": "users",
				"key":         "55",
				"deleted":     true,
				"timestamp":   ts.Format(time.RFC3339Nano),
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		result, err := handler.handleRequest(context.Background(), "Delete", &service.DeleteRequest{
			EntityType: "users",
			ID:         55,
		})

		assert.NoError(t, err)
		assert.NotNil(t, result)

		deleteResp, ok := result.(*service.DeleteResponse)
		assert.True(t, ok)
		assert.True(t, deleteResp.Deleted)
		assert.Equal(t, ts.UnixNano(), deleteResp.Timestamp)
	})

	t.Run("server returns error status on delete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			resp := map[string]string{"error": "access denied"}
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Delete", &service.DeleteRequest{
			EntityType: "users",
			ID:         1,
		})
		assert.Error(t, err)
		assert.True(t, errors.IsForbidden(err))
	})
}

func TestKVHandler_HandleDelete_InvalidRequestType(t *testing.T) {
	t.Run("returns InvalidInput for wrong request type", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Delete", map[string]string{"id": "1"})

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
		assert.Contains(t, err.Error(), "DeleteRequest")
	})
}

func TestKVHandler_HandleList(t *testing.T) {
	t.Run("successful list request", func(t *testing.T) {
		ts := time.Now().UTC().Truncate(time.Second)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/kv/users", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Accept"))

			resp := map[string]interface{}{
				"entity_type": "users",
				"items": []map[string]interface{}{
					{
						"key":       "1",
						"value":     `{"name":"alice"}`,
						"timestamp": ts.Format(time.RFC3339Nano),
					},
					{
						"key":       "2",
						"value":     `{"name":"bob"}`,
						"timestamp": ts.Format(time.RFC3339Nano),
					},
				},
				"total":       2,
				"next_cursor": "",
				"has_more":    false,
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		result, err := handler.handleRequest(context.Background(), "List", &service.ListRequest{
			EntityType: "users",
		})

		assert.NoError(t, err)
		assert.NotNil(t, result)

		listResp, ok := result.(*service.ListResponse)
		assert.True(t, ok)
		assert.Len(t, listResp.Entities, 2)
		assert.Equal(t, uint64(2), listResp.TotalCount)
		assert.Empty(t, listResp.NextPageToken)
	})

	t.Run("includes pagination query parameters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "10", r.URL.Query().Get("limit"))
			assert.Equal(t, "cursor-abc", r.URL.Query().Get("cursor"))

			resp := map[string]interface{}{
				"entity_type": "orders",
				"items":       []map[string]interface{}{},
				"total":       0,
				"next_cursor": "",
				"has_more":    false,
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "List", &service.ListRequest{
			EntityType: "orders",
			PageSize:   10,
			PageToken:  "cursor-abc",
		})
		assert.NoError(t, err)
	})

	t.Run("returns next page token when more results available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]interface{}{
				"entity_type": "events",
				"items": []map[string]interface{}{
					{
						"key":       "1",
						"value":     `{}`,
						"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
				"total":       100,
				"next_cursor": "next-page-token",
				"has_more":    true,
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		result, err := handler.handleRequest(context.Background(), "List", &service.ListRequest{
			EntityType: "events",
			PageSize:   1,
		})

		assert.NoError(t, err)
		listResp, ok := result.(*service.ListResponse)
		assert.True(t, ok)
		assert.Equal(t, "next-page-token", listResp.NextPageToken)
		assert.Equal(t, uint64(100), listResp.TotalCount)
	})
}

func TestKVHandler_HandleList_InvalidRequestType(t *testing.T) {
	t.Run("returns InvalidInput for wrong request type", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "List", &service.GetRequest{})

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
		assert.Contains(t, err.Error(), "ListRequest")
	})

	t.Run("returns InvalidInput for nil request", func(t *testing.T) {
		tr := &Transport{
			headers: make(map[string]string),
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "List", nil)

		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
	})
}

func TestKVHandler_HandleGet_BasePath(t *testing.T) {
	t.Run("includes basePath in URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/custom/prefix/api/v1/kv/items/7", r.URL.Path)

			resp := map[string]interface{}{
				"found":     true,
				"value":     `{"data":"test"}`,
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(resp)
			assert.NoError(t, err)
		}))
		defer server.Close()

		tr := &Transport{
			baseURL:  server.URL,
			basePath: "/custom/prefix",
			client:   server.Client(),
			headers:  map[string]string{},
		}
		handler := newKVHandler(tr)

		_, err := handler.handleRequest(context.Background(), "Get", &service.GetRequest{
			EntityType: "items",
			ID:         7,
		})
		assert.NoError(t, err)
	})
}
