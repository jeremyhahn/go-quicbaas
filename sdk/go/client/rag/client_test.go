// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package rag

import (
	"context"
	"testing"

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

func (m *mockTransport) Conn() interface{} { return nil }
func (m *mockTransport) Close() error      { return nil }
func (m *mockTransport) Healthy() bool     { return true }

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_Query_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &QueryResponse{
				Context:     "test context",
				Tokens:      10,
				ResultCount: 1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *QueryRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty query",
			req:     &QueryRequest{Namespace: "docs"},
			wantErr: true,
		},
		{
			name:    "empty namespace",
			req:     &QueryRequest{Query: "what is quicbaas"},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &QueryRequest{
				Query:     "what is quicbaas",
				Namespace: "docs",
			},
			wantErr: false,
		},
		{
			name: "valid request with optional fields",
			req: &QueryRequest{
				Query:            "what is quicbaas",
				Namespace:        "docs",
				TopK:             5,
				MaxTokens:        1024,
				MinScore:         0.7,
				Template:         "markdown",
				IncludeCitations: true,
				Model:            "all-MiniLM-L6-v2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Query(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Query() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Query_NilContext(t *testing.T) {
	mockT := &mockTransport{}
	client := NewClient(mockT)

	_, err := client.Query(nil, &QueryRequest{
		Query:     "what is quicbaas",
		Namespace: "docs",
	})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Query_TransportError(t *testing.T) {
	transportErr := errors.NewUnavailable("transport", "connection refused")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	}
	client := NewClient(mockT)

	_, err := client.Query(context.Background(), &QueryRequest{
		Query:     "what is quicbaas",
		Namespace: "docs",
	})
	if err == nil {
		t.Fatal("expected error from transport")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_Query_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)

	_, err := client.Query(context.Background(), &QueryRequest{
		Query:     "what is quicbaas",
		Namespace: "docs",
	})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", errors.Code(err))
	}
}

func TestClient_Query_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "unexpected string response", nil
		},
	}
	client := NewClient(mockT)

	_, err := client.Query(context.Background(), &QueryRequest{
		Query:     "what is quicbaas",
		Namespace: "docs",
	})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", errors.Code(err))
	}
}

func TestClient_Query_Success(t *testing.T) {
	expectedResp := &QueryResponse{
		Context:     "QuicBaaS is a distributed key-value database.",
		Tokens:      42,
		Truncated:   true,
		ResultCount: 3,
		Citations: []Citation{
			{
				Index:     0,
				FilePath:  "/docs/overview.md",
				StartLine: 10,
				EndLine:   25,
				Score:     0.95,
			},
			{
				Index:     1,
				FilePath:  "/docs/architecture.md",
				StartLine: 5,
				EndLine:   15,
				Score:     0.87,
			},
		},
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, service)
			}
			if method != "Query" {
				t.Errorf("expected method %q, got %q", "Query", method)
			}
			queryReq, ok := req.(*QueryRequest)
			if !ok {
				t.Fatalf("expected *QueryRequest, got %T", req)
			}
			if queryReq.Query != "what is quicbaas" {
				t.Errorf("expected query %q, got %q", "what is quicbaas", queryReq.Query)
			}
			if queryReq.Namespace != "docs" {
				t.Errorf("expected namespace %q, got %q", "docs", queryReq.Namespace)
			}
			return expectedResp, nil
		},
	}
	client := NewClient(mockT)

	resp, err := client.Query(context.Background(), &QueryRequest{
		Query:     "what is quicbaas",
		Namespace: "docs",
		TopK:      5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Context != expectedResp.Context {
		t.Errorf("expected Context=%q, got %q", expectedResp.Context, resp.Context)
	}
	if resp.Tokens != expectedResp.Tokens {
		t.Errorf("expected Tokens=%d, got %d", expectedResp.Tokens, resp.Tokens)
	}
	if resp.Truncated != expectedResp.Truncated {
		t.Errorf("expected Truncated=%v, got %v", expectedResp.Truncated, resp.Truncated)
	}
	if resp.ResultCount != expectedResp.ResultCount {
		t.Errorf("expected ResultCount=%d, got %d", expectedResp.ResultCount, resp.ResultCount)
	}
	if len(resp.Citations) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(resp.Citations))
	}
	if resp.Citations[0].FilePath != "/docs/overview.md" {
		t.Errorf("expected citation[0] FilePath=%q, got %q", "/docs/overview.md", resp.Citations[0].FilePath)
	}
	if resp.Citations[0].Score != 0.95 {
		t.Errorf("expected citation[0] Score=0.95, got %f", resp.Citations[0].Score)
	}
	if resp.Citations[1].Index != 1 {
		t.Errorf("expected citation[1] Index=1, got %d", resp.Citations[1].Index)
	}
}

func TestClient_BuildContext_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BuildContextResponse{
				Context:     "built context",
				Tokens:      5,
				ResultCount: 1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BuildContextRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty results",
			req:     &BuildContextRequest{Results: []ResultInput{}},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &BuildContextRequest{
				Results: []ResultInput{
					{Content: "some content", Score: 0.9},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with optional fields",
			req: &BuildContextRequest{
				Results: []ResultInput{
					{Content: "result 1", Score: 0.95, Metadata: map[string]string{"source": "wiki"}},
					{Content: "result 2", Score: 0.80},
				},
				MaxTokens:          2048,
				Template:           "json",
				IncludeCitations:   true,
				TruncationStrategy: "smart",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.BuildContext(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildContext() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_BuildContext_NilContext(t *testing.T) {
	mockT := &mockTransport{}
	client := NewClient(mockT)

	_, err := client.BuildContext(nil, &BuildContextRequest{
		Results: []ResultInput{
			{Content: "some content", Score: 0.9},
		},
	})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_BuildContext_TransportError(t *testing.T) {
	transportErr := errors.NewUnavailable("transport", "connection refused")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	}
	client := NewClient(mockT)

	_, err := client.BuildContext(context.Background(), &BuildContextRequest{
		Results: []ResultInput{
			{Content: "some content", Score: 0.9},
		},
	})
	if err == nil {
		t.Fatal("expected error from transport")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_BuildContext_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)

	_, err := client.BuildContext(context.Background(), &BuildContextRequest{
		Results: []ResultInput{
			{Content: "some content", Score: 0.9},
		},
	})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", errors.Code(err))
	}
}

func TestClient_BuildContext_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return 42, nil
		},
	}
	client := NewClient(mockT)

	_, err := client.BuildContext(context.Background(), &BuildContextRequest{
		Results: []ResultInput{
			{Content: "some content", Score: 0.9},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", errors.Code(err))
	}
}

func TestClient_BuildContext_Success(t *testing.T) {
	expectedResp := &BuildContextResponse{
		Context:     "Context from 2 results:\n1. First result\n2. Second result",
		Tokens:      28,
		Truncated:   false,
		ResultCount: 2,
		Citations: []Citation{
			{
				Index:     0,
				FilePath:  "/data/first.txt",
				StartLine: 1,
				EndLine:   10,
				Score:     0.95,
			},
		},
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, service)
			}
			if method != "BuildContext" {
				t.Errorf("expected method %q, got %q", "BuildContext", method)
			}
			buildReq, ok := req.(*BuildContextRequest)
			if !ok {
				t.Fatalf("expected *BuildContextRequest, got %T", req)
			}
			if len(buildReq.Results) != 2 {
				t.Errorf("expected 2 results, got %d", len(buildReq.Results))
			}
			return expectedResp, nil
		},
	}
	client := NewClient(mockT)

	resp, err := client.BuildContext(context.Background(), &BuildContextRequest{
		Results: []ResultInput{
			{Content: "First result", Score: 0.95, Metadata: map[string]string{"source": "file1"}},
			{Content: "Second result", Score: 0.80},
		},
		MaxTokens:        512,
		Template:         "markdown",
		IncludeCitations: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Context != expectedResp.Context {
		t.Errorf("expected Context=%q, got %q", expectedResp.Context, resp.Context)
	}
	if resp.Tokens != expectedResp.Tokens {
		t.Errorf("expected Tokens=%d, got %d", expectedResp.Tokens, resp.Tokens)
	}
	if resp.Truncated != expectedResp.Truncated {
		t.Errorf("expected Truncated=%v, got %v", expectedResp.Truncated, resp.Truncated)
	}
	if resp.ResultCount != expectedResp.ResultCount {
		t.Errorf("expected ResultCount=%d, got %d", expectedResp.ResultCount, resp.ResultCount)
	}
	if len(resp.Citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(resp.Citations))
	}
	if resp.Citations[0].FilePath != "/data/first.txt" {
		t.Errorf("expected citation FilePath=%q, got %q", "/data/first.txt", resp.Citations[0].FilePath)
	}
	if resp.Citations[0].Score != 0.95 {
		t.Errorf("expected citation Score=0.95, got %f", resp.Citations[0].Score)
	}
}
