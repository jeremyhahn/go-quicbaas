// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package parameterstore

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		c := NewClient(&mockTransport{})
		if c == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		c := NewClient(nil)
		if c != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_GetParameter_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("unexpected service: %s", service)
			}
			if method != "GetParameter" {
				t.Errorf("unexpected method: %s", method)
			}
			getReq, ok := req.(*GetParameterRequest)
			if !ok {
				t.Errorf("unexpected request type: %T", req)
			}
			if getReq.Path != "/app/config/test" {
				t.Errorf("unexpected path: %s", getReq.Path)
			}
			return &GetParameterResponse{
				Parameter: &ParameterValue{
					Path:      "/app/config/test",
					Value:     []byte("test-value"),
					Type:      "string",
					CreatedAt: now,
					UpdatedAt: now,
				},
				Timestamp: now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.GetParameter(ctx, &GetParameterRequest{
		Path:           "/app/config/test",
		WithDecryption: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Parameter == nil {
		t.Fatal("expected non-nil parameter")
	}
	if resp.Parameter.Path != "/app/config/test" {
		t.Errorf("unexpected path: %s", resp.Parameter.Path)
	}
	if string(resp.Parameter.Value) != "test-value" {
		t.Errorf("unexpected value: %s", string(resp.Parameter.Value))
	}
}

func TestClient_GetParameter_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetParameterResponse{
				Parameter: &ParameterValue{Path: "/app/config/test"},
				Timestamp: time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetParameterRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty path",
			req:     &GetParameterRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetParameterRequest{Path: "/app/config/test"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.GetParameter(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetParameter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetParameter_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "server unavailable")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.GetParameter(ctx, &GetParameterRequest{Path: "/app/config/test"})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_GetParameter_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.GetParameter(ctx, &GetParameterRequest{Path: "/app/config/test"})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_GetParameter_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.GetParameter(ctx, &GetParameterRequest{Path: "/app/config/test"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_SetParameter_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "SetParameter" {
				t.Errorf("unexpected method: %s", method)
			}
			setReq, ok := req.(*SetParameterRequest)
			if !ok {
				t.Errorf("unexpected request type: %T", req)
			}
			if setReq.Path != "/app/config/new" {
				t.Errorf("unexpected path: %s", setReq.Path)
			}
			return &SetParameterResponse{
				Path:      "/app/config/new",
				Created:   true,
				Timestamp: now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.SetParameter(ctx, &SetParameterRequest{
		Path:        "/app/config/new",
		Value:       []byte("new-value"),
		Description: "Test parameter",
		Tags:        map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Path != "/app/config/new" {
		t.Errorf("unexpected path: %s", resp.Path)
	}
	if !resp.Created {
		t.Error("expected Created to be true")
	}
}

func TestClient_SetParameter_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SetParameterResponse{Path: "/app/config/test", Created: true, Timestamp: time.Now()}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SetParameterRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty path",
			req:     &SetParameterRequest{Value: []byte("value")},
			wantErr: true,
		},
		{
			name:    "empty value",
			req:     &SetParameterRequest{Path: "/app/config/test"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &SetParameterRequest{Path: "/app/config/test", Value: []byte("value")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.SetParameter(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetParameter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_SetParameter_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewConflict("test", "parameter exists")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameter(ctx, &SetParameterRequest{Path: "/app/config/test", Value: []byte("value")})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsConflict(err) {
		t.Errorf("expected Conflict error, got %v", err)
	}
}

func TestClient_SetParameter_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameter(ctx, &SetParameterRequest{Path: "/app/config/test", Value: []byte("value")})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_SetParameter_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameter(ctx, &SetParameterRequest{Path: "/app/config/test", Value: []byte("value")})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_DeleteParameter_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "DeleteParameter" {
				t.Errorf("unexpected method: %s", method)
			}
			return &DeleteParameterResponse{
				Path:      "/app/config/delete",
				Deleted:   true,
				Timestamp: now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.DeleteParameter(ctx, &DeleteParameterRequest{Path: "/app/config/delete"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Deleted {
		t.Error("expected Deleted to be true")
	}
}

func TestClient_DeleteParameter_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteParameterResponse{Deleted: true, Timestamp: time.Now()}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DeleteParameterRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty path",
			req:     &DeleteParameterRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteParameterRequest{Path: "/app/config/test"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.DeleteParameter(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteParameter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_DeleteParameter_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("test", "parameter not found")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.DeleteParameter(ctx, &DeleteParameterRequest{Path: "/app/config/missing"})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_DeleteParameter_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.DeleteParameter(ctx, &DeleteParameterRequest{Path: "/app/config/test"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_DeleteParameter_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.DeleteParameter(ctx, &DeleteParameterRequest{Path: "/app/config/test"})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_GetParameters_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "GetParameters" {
				t.Errorf("unexpected method: %s", method)
			}
			return &GetParametersResponse{
				Parameters: []*ParameterValue{
					{Path: "/app/config/a", Value: []byte("value-a")},
					{Path: "/app/config/b", Value: []byte("value-b")},
				},
				Errors:    map[string]string{"/app/config/c": "not found"},
				Timestamp: now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.GetParameters(ctx, &GetParametersRequest{
		Paths:          []string{"/app/config/a", "/app/config/b", "/app/config/c"},
		WithDecryption: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(resp.Parameters))
	}
	if len(resp.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(resp.Errors))
	}
}

func TestClient_GetParameters_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetParametersResponse{
				Parameters: []*ParameterValue{},
				Timestamp:  time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetParametersRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty paths",
			req:     &GetParametersRequest{Paths: []string{}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetParametersRequest{Paths: []string{"/app/config/a"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.GetParameters(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetParameters() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetParameters_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.GetParameters(ctx, &GetParametersRequest{Paths: []string{"/app/config/a"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Parameters == nil {
		t.Error("expected non-nil parameters slice")
	}
}

func TestClient_GetParameters_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewTimeout("test", "request timed out")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.GetParameters(ctx, &GetParametersRequest{Paths: []string{"/app/config/a"}})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsTimeout(err) {
		t.Errorf("expected Timeout error, got %v", err)
	}
}

func TestClient_GetParameters_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.GetParameters(ctx, &GetParametersRequest{Paths: []string{"/app/config/a"}})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_SetParameters_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "SetParameters" {
				t.Errorf("unexpected method: %s", method)
			}
			return &SetParametersResponse{
				Results: []*SetParameterResult{
					{Path: "/app/config/a", Created: true, Success: true},
					{Path: "/app/config/b", Created: false, Success: true},
				},
				Timestamp: now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.SetParameters(ctx, &SetParametersRequest{
		Parameters: []*SetParameterRequest{
			{Path: "/app/config/a", Value: []byte("value-a")},
			{Path: "/app/config/b", Value: []byte("value-b")},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestClient_SetParameters_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SetParametersResponse{Results: []*SetParameterResult{}, Timestamp: time.Now()}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SetParametersRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty parameters",
			req:     &SetParametersRequest{Parameters: []*SetParameterRequest{}},
			wantErr: true,
		},
		{
			name: "nil parameter in slice",
			req: &SetParametersRequest{Parameters: []*SetParameterRequest{
				nil,
			}},
			wantErr: true,
		},
		{
			name: "parameter with empty path",
			req: &SetParametersRequest{Parameters: []*SetParameterRequest{
				{Value: []byte("value")},
			}},
			wantErr: true,
		},
		{
			name: "parameter with empty value",
			req: &SetParametersRequest{Parameters: []*SetParameterRequest{
				{Path: "/app/config/a"},
			}},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &SetParametersRequest{Parameters: []*SetParameterRequest{
				{Path: "/app/config/a", Value: []byte("value")},
			}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.SetParameters(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetParameters() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_SetParameters_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "server unavailable")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameters(ctx, &SetParametersRequest{
		Parameters: []*SetParameterRequest{
			{Path: "/app/config/a", Value: []byte("value")},
		},
	})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_SetParameters_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameters(ctx, &SetParametersRequest{
		Parameters: []*SetParameterRequest{
			{Path: "/app/config/a", Value: []byte("value")},
		},
	})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_SetParameters_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameters(ctx, &SetParametersRequest{
		Parameters: []*SetParameterRequest{
			{Path: "/app/config/a", Value: []byte("value")},
		},
	})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_DeleteParameters_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "DeleteParameters" {
				t.Errorf("unexpected method: %s", method)
			}
			return &DeleteParametersResponse{
				Results: []*DeleteParameterResult{
					{Path: "/app/config/a", Deleted: true},
					{Path: "/app/config/b", Deleted: true},
				},
				DeletedCount: 2,
				Timestamp:    now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.DeleteParameters(ctx, &DeleteParametersRequest{
		Paths: []string{"/app/config/a", "/app/config/b"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.DeletedCount != 2 {
		t.Errorf("expected DeletedCount 2, got %d", resp.DeletedCount)
	}
}

func TestClient_DeleteParameters_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteParametersResponse{
				Results:      []*DeleteParameterResult{},
				DeletedCount: 0,
				Timestamp:    time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DeleteParametersRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty paths",
			req:     &DeleteParametersRequest{Paths: []string{}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteParametersRequest{Paths: []string{"/app/config/a"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.DeleteParameters(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteParameters() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_DeleteParameters_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewTimeout("test", "request timed out")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.DeleteParameters(ctx, &DeleteParametersRequest{Paths: []string{"/app/config/a"}})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsTimeout(err) {
		t.Errorf("expected Timeout error, got %v", err)
	}
}

func TestClient_DeleteParameters_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.DeleteParameters(ctx, &DeleteParametersRequest{Paths: []string{"/app/config/a"}})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_DeleteParameters_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.DeleteParameters(ctx, &DeleteParametersRequest{Paths: []string{"/app/config/a"}})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_ListParameters_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "ListParameters" {
				t.Errorf("unexpected method: %s", method)
			}
			return &ListParametersResponse{
				Parameters: []*ParameterValue{
					{Path: "/app/config/a"},
					{Path: "/app/config/b"},
				},
				NextCursor: "cursor123",
				HasMore:    true,
				TotalCount: 10,
				Timestamp:  now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.ListParameters(ctx, &ListParametersRequest{
		Prefix:    "/app/config/",
		Recursive: true,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(resp.Parameters))
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true")
	}
	if resp.NextCursor != "cursor123" {
		t.Errorf("unexpected NextCursor: %s", resp.NextCursor)
	}
}

func TestClient_ListParameters_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListParametersResponse{
				Parameters: []*ParameterValue{},
				Timestamp:  time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ListParametersRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty prefix is valid (lists all)",
			req:     &ListParametersRequest{},
			wantErr: false,
		},
		{
			name:    "valid request with prefix",
			req:     &ListParametersRequest{Prefix: "/app/"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.ListParameters(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListParameters() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_ListParameters_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.ListParameters(ctx, &ListParametersRequest{Prefix: "/app/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Parameters == nil {
		t.Error("expected non-nil parameters slice")
	}
}

func TestClient_ListParameters_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "server unavailable")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.ListParameters(ctx, &ListParametersRequest{Prefix: "/app/"})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_ListParameters_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.ListParameters(ctx, &ListParametersRequest{Prefix: "/app/"})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestClient_SearchByTags_Success(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "SearchByTags" {
				t.Errorf("unexpected method: %s", method)
			}
			searchReq, ok := req.(*SearchByTagsRequest)
			if !ok {
				t.Errorf("unexpected request type: %T", req)
			}
			if searchReq.Tags["env"] != "production" {
				t.Errorf("unexpected tag value: %s", searchReq.Tags["env"])
			}
			return &SearchByTagsResponse{
				Parameters: []*ParameterValue{
					{Path: "/app/config/a", Tags: map[string]string{"env": "production"}},
					{Path: "/app/config/b", Tags: map[string]string{"env": "production"}},
				},
				HasMore:   false,
				Timestamp: now,
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.SearchByTags(ctx, &SearchByTagsRequest{
		Tags:       map[string]string{"env": "production"},
		WithValues: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(resp.Parameters))
	}
}

func TestClient_SearchByTags_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SearchByTagsResponse{
				Parameters: []*ParameterValue{},
				Timestamp:  time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SearchByTagsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty tags",
			req:     &SearchByTagsRequest{Tags: map[string]string{}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &SearchByTagsRequest{Tags: map[string]string{"env": "prod"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.SearchByTags(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchByTags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_SearchByTags_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.SearchByTags(ctx, &SearchByTagsRequest{Tags: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Parameters == nil {
		t.Error("expected non-nil parameters slice")
	}
}

func TestClient_SearchByTags_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewTimeout("test", "request timed out")
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SearchByTags(ctx, &SearchByTagsRequest{Tags: map[string]string{"env": "prod"}})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsTimeout(err) {
		t.Errorf("expected Timeout error, got %v", err)
	}
}

func TestClient_SearchByTags_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SearchByTags(ctx, &SearchByTagsRequest{Tags: map[string]string{"env": "prod"}})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

func TestValidateSetParametersRequest_ContextInError(t *testing.T) {
	req := &SetParametersRequest{
		Parameters: []*SetParameterRequest{
			{Path: "/valid/path", Value: []byte("value")},
			{Path: "", Value: []byte("value")}, // Invalid: empty path
		},
	}

	err := validateSetParametersRequest(req)
	if err == nil {
		t.Fatal("expected error")
	}

	qbErr, ok := err.(*errors.QBError)
	if !ok {
		t.Fatalf("expected QBError, got %T", err)
	}

	// Verify context contains index
	if qbErr.Context == nil {
		t.Fatal("expected non-nil context")
	}
	if qbErr.Context["index"] != 1 {
		t.Errorf("expected index 1 in context, got %v", qbErr.Context["index"])
	}
}

func TestValidateSetParametersRequest_ValueRequired(t *testing.T) {
	req := &SetParametersRequest{
		Parameters: []*SetParameterRequest{
			{Path: "/valid/path", Value: nil}, // Invalid: empty value
		},
	}

	err := validateSetParametersRequest(req)
	if err == nil {
		t.Fatal("expected error")
	}

	qbErr, ok := err.(*errors.QBError)
	if !ok {
		t.Fatalf("expected QBError, got %T", err)
	}

	// Verify context contains path
	if qbErr.Context == nil {
		t.Fatal("expected non-nil context")
	}
	if qbErr.Context["path"] != "/valid/path" {
		t.Errorf("expected path in context, got %v", qbErr.Context["path"])
	}
}

func TestClient_GetParameter_WithDecryption(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			getReq := req.(*GetParameterRequest)
			if !getReq.WithDecryption {
				t.Error("expected WithDecryption to be true")
			}
			return &GetParameterResponse{
				Parameter: &ParameterValue{
					Path:        "/app/secrets/key",
					Value:       []byte("decrypted-value"),
					IsEncrypted: true,
					KMSKeyID:    "alias/my-key",
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.GetParameter(ctx, &GetParameterRequest{
		Path:           "/app/secrets/key",
		WithDecryption: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.Parameter.Value) != "decrypted-value" {
		t.Errorf("unexpected value: %s", string(resp.Parameter.Value))
	}
}

func TestClient_SetParameter_WithTTL(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			setReq := req.(*SetParameterRequest)
			if setReq.TTLSeconds != 3600 {
				t.Errorf("expected TTLSeconds 3600, got %d", setReq.TTLSeconds)
			}
			return &SetParameterResponse{
				Path:      setReq.Path,
				Created:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameter(ctx, &SetParameterRequest{
		Path:       "/app/tokens/session",
		Value:      []byte("temp-token"),
		TTLSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_SetParameter_WithEncryption(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			setReq := req.(*SetParameterRequest)
			if setReq.KMSKeyID != "alias/my-key" {
				t.Errorf("expected KMSKeyID alias/my-key, got %s", setReq.KMSKeyID)
			}
			return &SetParameterResponse{
				Path:      setReq.Path,
				Created:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	_, err := c.SetParameter(ctx, &SetParameterRequest{
		Path:     "/app/secrets/api-key",
		Value:    []byte("secret-value"),
		KMSKeyID: "alias/my-key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_ListParameters_WithPagination(t *testing.T) {
	callCount := 0
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			listReq := req.(*ListParametersRequest)
			callCount++
			if callCount == 1 {
				if listReq.Cursor != "" {
					t.Errorf("expected empty cursor on first call, got %s", listReq.Cursor)
				}
				return &ListParametersResponse{
					Parameters: []*ParameterValue{
						{Path: "/app/config/a"},
						{Path: "/app/config/b"},
					},
					NextCursor: "cursor-page-2",
					HasMore:    true,
					Timestamp:  time.Now(),
				}, nil
			}
			if listReq.Cursor != "cursor-page-2" {
				t.Errorf("expected cursor-page-2, got %s", listReq.Cursor)
			}
			return &ListParametersResponse{
				Parameters: []*ParameterValue{
					{Path: "/app/config/c"},
				},
				HasMore:   false,
				Timestamp: time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	// First page
	resp1, err := c.ListParameters(ctx, &ListParametersRequest{Prefix: "/app/config/", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp1.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(resp1.Parameters))
	}
	if !resp1.HasMore {
		t.Error("expected HasMore to be true")
	}

	// Second page
	resp2, err := c.ListParameters(ctx, &ListParametersRequest{Prefix: "/app/config/", Limit: 2, Cursor: resp1.NextCursor})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp2.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(resp2.Parameters))
	}
	if resp2.HasMore {
		t.Error("expected HasMore to be false")
	}
}

func TestClient_SearchByTags_WithPrefixFilter(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			searchReq := req.(*SearchByTagsRequest)
			if searchReq.Prefix != "/app/config/" {
				t.Errorf("expected prefix /app/config/, got %s", searchReq.Prefix)
			}
			if searchReq.Tags["env"] != "prod" {
				t.Errorf("expected env=prod tag, got %s", searchReq.Tags["env"])
			}
			return &SearchByTagsResponse{
				Parameters: []*ParameterValue{
					{Path: "/app/config/a", Tags: map[string]string{"env": "prod"}},
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	c := NewClient(mockT)
	ctx := context.Background()

	resp, err := c.SearchByTags(ctx, &SearchByTagsRequest{
		Tags:   map[string]string{"env": "prod"},
		Prefix: "/app/config/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(resp.Parameters))
	}
}
