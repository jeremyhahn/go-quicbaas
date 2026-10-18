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
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

var paramStoreTime = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// newParameterStoreClient drives the real SDK parameter store client over
// the embedded transport. A nil paramSvc leaves the service unconfigured.
func newParameterStoreClient(t *testing.T, paramSvc service.ParameterStoreService) parameterstore.Client {
	t.Helper()
	mock := &mockQuicBaaS{}
	if paramSvc != nil {
		mock.parameterStoreService = paramSvc
	}
	tr, err := New(mock)
	require.NoError(t, err)
	return parameterstore.NewClient(tr)
}

func serviceParameter(path string) *interfaces.ParameterValue {
	expires := paramStoreTime.Add(time.Hour)
	return &interfaces.ParameterValue{
		Path:         path,
		Value:        []byte("v"),
		Type:         "string",
		Description:  "d",
		Tags:         map[string]string{"env": "prod"},
		IsEncrypted:  true,
		KMSKeyID:     "key-1",
		ExpiresAt:    &expires,
		AuditEnabled: true,
		CreatedAt:    paramStoreTime,
		UpdatedAt:    paramStoreTime,
		CreatedBy:    "alice",
		UpdatedBy:    "bob",
	}
}

func TestEmbeddedParameterStore_GetParameter(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		GetParameterFunc: func(_ context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			assert.Equal(t, "/app/host", req.Path)
			assert.True(t, req.WithDecryption)
			return &interfaces.GetParameterResponse{Parameter: serviceParameter(req.Path), Timestamp: paramStoreTime}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).GetParameter(context.Background(),
		&parameterstore.GetParameterRequest{Path: "/app/host", WithDecryption: true})
	require.NoError(t, err)
	require.NotNil(t, resp.Parameter)
	assert.Equal(t, "/app/host", resp.Parameter.Path)
	assert.Equal(t, []byte("v"), resp.Parameter.Value)
	assert.Equal(t, "key-1", resp.Parameter.KMSKeyID)
	assert.Equal(t, "bob", resp.Parameter.UpdatedBy)
	require.NotNil(t, resp.Parameter.ExpiresAt)
	assert.Equal(t, paramStoreTime.Add(time.Hour), *resp.Parameter.ExpiresAt)
	assert.Equal(t, paramStoreTime, resp.Timestamp)
}

func TestEmbeddedParameterStore_GetParameter_Missing(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		GetParameterFunc: func(context.Context, *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			return nil, errors.NewNotFound("parameterstore.GetParameter", "parameter /app/none not found")
		},
	}
	_, err := newParameterStoreClient(t, paramSvc).GetParameter(context.Background(),
		&parameterstore.GetParameterRequest{Path: "/app/none"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestEmbeddedParameterStore_SetParameter(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		SetParameterFunc: func(_ context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
			assert.Equal(t, &interfaces.SetParameterRequest{
				Path: "/app/host", Value: []byte("h"), Type: "string", Description: "d",
				KMSKeyID: "key-1", Tags: map[string]string{"env": "prod"}, TTLSeconds: 60,
				AuditEnabled: true, Overwrite: true,
			}, req)
			return &interfaces.SetParameterResponse{Path: req.Path, Created: true, Timestamp: paramStoreTime}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).SetParameter(context.Background(), &parameterstore.SetParameterRequest{
		Path: "/app/host", Value: []byte("h"), Type: "string", Description: "d",
		KMSKeyID: "key-1", Tags: map[string]string{"env": "prod"}, TTLSeconds: 60,
		AuditEnabled: true, Overwrite: true,
	})
	require.NoError(t, err)
	assert.Equal(t, &parameterstore.SetParameterResponse{Path: "/app/host", Created: true, Timestamp: paramStoreTime}, resp)
}

func TestEmbeddedParameterStore_DeleteParameter(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		DeleteParameterFunc: func(_ context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
			return &interfaces.DeleteParameterResponse{Path: req.Path, Deleted: true, Timestamp: paramStoreTime}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).DeleteParameter(context.Background(),
		&parameterstore.DeleteParameterRequest{Path: "/app/host"})
	require.NoError(t, err)
	assert.Equal(t, &parameterstore.DeleteParameterResponse{Path: "/app/host", Deleted: true, Timestamp: paramStoreTime}, resp)
}

func TestEmbeddedParameterStore_GetParameters(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		GetParametersFunc: func(_ context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
			assert.Equal(t, []string{"/a", "/b"}, req.Paths)
			return &interfaces.GetParametersResponse{
				Parameters: []*interfaces.ParameterValue{serviceParameter("/a")},
				Errors:     map[string]string{"/b": "not found"},
				Timestamp:  paramStoreTime,
			}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).GetParameters(context.Background(),
		&parameterstore.GetParametersRequest{Paths: []string{"/a", "/b"}})
	require.NoError(t, err)
	require.Len(t, resp.Parameters, 1)
	assert.Equal(t, "/a", resp.Parameters[0].Path)
	assert.Equal(t, map[string]string{"/b": "not found"}, resp.Errors)
}

func TestEmbeddedParameterStore_SetParameters(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		SetParametersFunc: func(_ context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
			require.Len(t, req.Parameters, 2)
			assert.Equal(t, "/b", req.Parameters[1].Path)
			assert.Equal(t, []byte("2"), req.Parameters[1].Value)
			return &interfaces.SetParametersResponse{
				Results: []*interfaces.SetParameterResult{
					{Path: "/a", Created: true, Success: true},
					{Path: "/b", Error: "denied"},
				},
				Timestamp: paramStoreTime,
			}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).SetParameters(context.Background(), &parameterstore.SetParametersRequest{
		Parameters: []*parameterstore.SetParameterRequest{
			{Path: "/a", Value: []byte("1")},
			{Path: "/b", Value: []byte("2")},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []*parameterstore.SetParameterResult{
		{Path: "/a", Created: true, Success: true},
		{Path: "/b", Error: "denied"},
	}, resp.Results)
}

func TestEmbeddedParameterStore_DeleteParameters(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		DeleteParametersFunc: func(_ context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
			assert.Equal(t, []string{"/a", "/b"}, req.Paths)
			return &interfaces.DeleteParametersResponse{
				Results: []*interfaces.DeleteParameterResult{
					{Path: "/a", Deleted: true},
					{Path: "/b", Error: "not found"},
				},
				DeletedCount: 1,
				Timestamp:    paramStoreTime,
			}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).DeleteParameters(context.Background(),
		&parameterstore.DeleteParametersRequest{Paths: []string{"/a", "/b"}})
	require.NoError(t, err)
	assert.Equal(t, 1, resp.DeletedCount)
	assert.Equal(t, []*parameterstore.DeleteParameterResult{
		{Path: "/a", Deleted: true},
		{Path: "/b", Error: "not found"},
	}, resp.Results)
}

func TestEmbeddedParameterStore_ListParameters(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		ListParametersFunc: func(_ context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
			assert.Equal(t, &interfaces.ListParametersRequest{
				Prefix: "/app/", Recursive: true, WithDecryption: true, WithValues: true, Limit: 10, Cursor: "c1",
			}, req)
			return &interfaces.ListParametersResponse{
				Parameters: []*interfaces.ParameterValue{serviceParameter("/app/host")},
				NextCursor: "c2",
				HasMore:    true,
				TotalCount: 11,
				Timestamp:  paramStoreTime,
			}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).ListParameters(context.Background(), &parameterstore.ListParametersRequest{
		Prefix: "/app/", Recursive: true, WithDecryption: true, WithValues: true, Limit: 10, Cursor: "c1",
	})
	require.NoError(t, err)
	require.Len(t, resp.Parameters, 1)
	assert.Equal(t, "/app/host", resp.Parameters[0].Path)
	assert.Equal(t, "c2", resp.NextCursor)
	assert.True(t, resp.HasMore)
	assert.Equal(t, int64(11), resp.TotalCount)
}

func TestEmbeddedParameterStore_SearchByTags(t *testing.T) {
	paramSvc := &mocks.MockParameterStoreService{
		SearchByTagsFunc: func(_ context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
			assert.Equal(t, &interfaces.SearchByTagsRequest{
				Tags: map[string]string{"env": "prod"}, Prefix: "/app/", WithValues: true, Limit: 5, Cursor: "c1",
			}, req)
			return &interfaces.SearchByTagsResponse{
				Parameters: []*interfaces.ParameterValue{serviceParameter("/app/host")},
				NextCursor: "c2",
				HasMore:    true,
				Timestamp:  paramStoreTime,
			}, nil
		},
	}
	resp, err := newParameterStoreClient(t, paramSvc).SearchByTags(context.Background(), &parameterstore.SearchByTagsRequest{
		Tags: map[string]string{"env": "prod"}, Prefix: "/app/", WithValues: true, Limit: 5, Cursor: "c1",
	})
	require.NoError(t, err)
	require.Len(t, resp.Parameters, 1)
	assert.Equal(t, map[string]string{"env": "prod"}, resp.Parameters[0].Tags)
	assert.Equal(t, "c2", resp.NextCursor)
	assert.True(t, resp.HasMore)
}

// parameterStoreCalls invokes every client verb with a valid request.
func parameterStoreCalls(client parameterstore.Client) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"GetParameter": func(ctx context.Context) error {
			_, err := client.GetParameter(ctx, &parameterstore.GetParameterRequest{Path: "/a"})
			return err
		},
		"SetParameter": func(ctx context.Context) error {
			_, err := client.SetParameter(ctx, &parameterstore.SetParameterRequest{Path: "/a", Value: []byte("v")})
			return err
		},
		"DeleteParameter": func(ctx context.Context) error {
			_, err := client.DeleteParameter(ctx, &parameterstore.DeleteParameterRequest{Path: "/a"})
			return err
		},
		"GetParameters": func(ctx context.Context) error {
			_, err := client.GetParameters(ctx, &parameterstore.GetParametersRequest{Paths: []string{"/a"}})
			return err
		},
		"SetParameters": func(ctx context.Context) error {
			_, err := client.SetParameters(ctx, &parameterstore.SetParametersRequest{
				Parameters: []*parameterstore.SetParameterRequest{{Path: "/a", Value: []byte("v")}},
			})
			return err
		},
		"DeleteParameters": func(ctx context.Context) error {
			_, err := client.DeleteParameters(ctx, &parameterstore.DeleteParametersRequest{Paths: []string{"/a"}})
			return err
		},
		"ListParameters": func(ctx context.Context) error {
			_, err := client.ListParameters(ctx, &parameterstore.ListParametersRequest{Prefix: "/"})
			return err
		},
		"SearchByTags": func(ctx context.Context) error {
			_, err := client.SearchByTags(ctx, &parameterstore.SearchByTagsRequest{Tags: map[string]string{"k": "v"}})
			return err
		},
	}
}

func TestEmbeddedParameterStore_ServiceNotConfigured(t *testing.T) {
	calls := parameterStoreCalls(newParameterStoreClient(t, nil))
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var notConfigured *ServiceNotConfiguredError
			assert.ErrorAs(t, call(context.Background()), &notConfigured)
		})
	}
}

func TestEmbeddedParameterStore_InvalidRequestType(t *testing.T) {
	mock := &mockQuicBaaS{parameterStoreService: &mocks.MockParameterStoreService{}}
	for name := range parameterStoreCalls(nil) {
		t.Run(name, func(t *testing.T) {
			handler, ok := unaryHandlers[parameterStoreServiceName+"/"+name]
			require.True(t, ok)
			_, err := handler(context.Background(), mock, "not-a-request")
			var invalid *InvalidRequestTypeError
			assert.ErrorAs(t, err, &invalid)
		})
	}
}

// TestEmbeddedParameterStore_CoversClient fails when a parameterstore.Client
// verb has no embedded route, so a new verb cannot ship unrouted.
func TestEmbeddedParameterStore_CoversClient(t *testing.T) {
	clientType := reflect.TypeOf((*parameterstore.Client)(nil)).Elem()
	calls := parameterStoreCalls(nil)
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i).Name
		_, routed := unaryHandlers[parameterStoreServiceName+"/"+method]
		assert.True(t, routed, "parameterstore.Client.%s has no embedded handler", method)
		_, tested := calls[method]
		assert.True(t, tested, "parameterstore.Client.%s is missing from parameterStoreCalls", method)
	}
}
