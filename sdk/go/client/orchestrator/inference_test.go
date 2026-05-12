// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Inference (aggregated) tests
// =============================================================================

// TestInference_HappyPath verifies that Inference calls the transport with the
// correct service/method and unmarshals the response.
func TestInference_HappyPath(t *testing.T) {
	t.Parallel()

	want := &InferenceResponse{
		Content:    []InferenceContentBlock{{Type: "text", Text: "haiku text"}},
		Model:      "qwen3:1.7b",
		StopReason: "end_turn",
	}

	mt := &mockTransport{
		requestFn: func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, "Inference", method)
			wireReq, ok := req.(*InferenceRequest)
			require.True(t, ok, "unexpected request type %T", req)
			assert.Equal(t, "ollama", wireReq.Backend)
			assert.Equal(t, "write a haiku", wireReq.Prompt)
			return want, nil
		},
	}

	c := NewClient(mt)
	resp, err := c.Inference(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "write a haiku",
		Model:   "qwen3:1.7b",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "haiku text", resp.Content[0].Text)
	assert.Equal(t, "qwen3:1.7b", resp.Model)
	assert.Equal(t, "end_turn", resp.StopReason)
}

// TestInference_NilRequest verifies that a nil request returns an error.
func TestInference_NilRequest(t *testing.T) {
	t.Parallel()

	c := NewClient(&mockTransport{})
	_, err := c.Inference(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is required")
}

// TestInference_EmptyBackend verifies that an empty backend field returns an
// error before hitting the transport.
func TestInference_EmptyBackend(t *testing.T) {
	t.Parallel()

	c := NewClient(&mockTransport{})
	_, err := c.Inference(context.Background(), &InferenceRequest{
		Backend: "",
		Prompt:  "hello",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend is required")
}

// TestInference_EmptyPrompt verifies that an empty prompt field returns an
// error before hitting the transport.
func TestInference_EmptyPrompt(t *testing.T) {
	t.Parallel()

	c := NewClient(&mockTransport{})
	_, err := c.Inference(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

// TestInference_TransportError verifies that a transport error is propagated.
func TestInference_TransportError(t *testing.T) {
	t.Parallel()

	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("mock", "backend offline")
		},
	}

	c := NewClient(mt)
	_, err := c.Inference(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})
	require.Error(t, err)
}

// TestInference_NilResponse verifies that a nil response from the transport
// returns an internal error.
func TestInference_NilResponse(t *testing.T) {
	t.Parallel()

	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	c := NewClient(mt)
	_, err := c.Inference(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

// =============================================================================
// InferenceStream tests
// =============================================================================

// TestInferenceStream_HappyPath verifies that InferenceStream returns chunks
// through the channel in order.
func TestInferenceStream_HappyPath(t *testing.T) {
	t.Parallel()

	chunks := []*InferenceChunk{
		{Delta: []InferenceContentBlock{{Type: "text", Text: "hello "}}, Final: false},
		{Delta: []InferenceContentBlock{{Type: "text", Text: "world"}}, Model: "qwen3:1.7b", StopReason: "end_turn", Final: true},
	}
	idx := 0

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(chunks) {
				return nil, errors.NewInternal("mock", "stream ended")
			}
			c := chunks[idx]
			idx++
			return c, nil
		},
	}

	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, service, method string, req interface{}) (transport.Stream, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, "InferenceStream", method)
			// The client now passes *InferenceRequest directly to RequestStream.
			wireReq, ok := req.(*InferenceRequest)
			require.True(t, ok)
			assert.Equal(t, "ollama", wireReq.Backend)
			return ms, nil
		},
	}

	c := NewClient(mt)
	ch, err := c.InferenceStream(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, ch)

	var received []*InferenceChunk
	for chunk := range ch {
		received = append(received, chunk)
	}

	require.Len(t, received, 2)
	assert.Equal(t, "hello ", received[0].Delta[0].Text)
	assert.True(t, received[1].Final)
	assert.Equal(t, "qwen3:1.7b", received[1].Model)
	assert.Equal(t, "end_turn", received[1].StopReason)
}

// TestInferenceStream_NilRequest verifies that a nil request is rejected.
func TestInferenceStream_NilRequest(t *testing.T) {
	t.Parallel()

	c := NewClient(&mockTransport{})
	_, err := c.InferenceStream(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is required")
}

// TestInferenceStream_EmptyBackend verifies that an empty backend is rejected.
func TestInferenceStream_EmptyBackend(t *testing.T) {
	t.Parallel()

	c := NewClient(&mockTransport{})
	_, err := c.InferenceStream(context.Background(), &InferenceRequest{
		Backend: "",
		Prompt:  "hello",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend is required")
}

// TestInferenceStream_EmptyPrompt verifies that an empty prompt is rejected.
func TestInferenceStream_EmptyPrompt(t *testing.T) {
	t.Parallel()

	c := NewClient(&mockTransport{})
	_, err := c.InferenceStream(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

// TestInferenceStream_TransportError verifies that a stream dial error is
// propagated as a non-nil error return.
func TestInferenceStream_TransportError(t *testing.T) {
	t.Parallel()

	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return nil, errors.NewInternal("mock", "dial failed")
		},
	}

	c := NewClient(mt)
	_, err := c.InferenceStream(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})
	require.Error(t, err)
}

// TestInferenceStream_ContextCancel verifies that context cancellation closes
// the channel without hanging.
func TestInferenceStream_ContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	blockCh := make(chan struct{})
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			<-blockCh
			return nil, errors.NewInternal("mock", "cancelled")
		},
	}

	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}

	c := NewClient(mt)
	ch, err := c.InferenceStream(ctx, &InferenceRequest{
		Backend: "ollama",
		Prompt:  "slow query",
	})
	require.NoError(t, err)
	require.NotNil(t, ch)

	cancel()
	close(blockCh)

	// Channel must close within a reasonable deadline; no goroutine leak.
	timeout := time.After(2 * time.Second)
	select {
	case <-ch:
		// drained
	case <-timeout:
		t.Fatal("channel did not close after context cancel")
	}
	for range ch {
	}
}

// TestInferenceStream_EmptyStream verifies that a stream that immediately
// errors closes the channel with no chunks.
func TestInferenceStream_EmptyStream(t *testing.T) {
	t.Parallel()

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			return nil, errors.NewInternal("mock", "stream ended immediately")
		},
	}

	mt := &mockTransport{
		requestStreamFn: func(_ context.Context, _, _ string, _ interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}

	c := NewClient(mt)
	ch, err := c.InferenceStream(context.Background(), &InferenceRequest{
		Backend: "ollama",
		Prompt:  "empty",
	})
	require.NoError(t, err)
	require.NotNil(t, ch)

	var received []*InferenceChunk
	for chunk := range ch {
		received = append(received, chunk)
	}
	assert.Empty(t, received)
}
