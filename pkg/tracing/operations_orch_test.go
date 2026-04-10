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

package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceJobLifecycle(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		jobID     string
		state     string
		err       error
		wantSpans int
		wantAttrs []string
	}{
		{
			name:      "successful lifecycle transition",
			enabled:   true,
			jobID:     "job-123",
			state:     "executing",
			err:       nil,
			wantSpans: 1,
			wantAttrs: []string{"orch.job_id", "orch.state"},
		},
		{
			name:      "lifecycle transition with error",
			enabled:   true,
			jobID:     "job-456",
			state:     "failed",
			err:       errors.New("decomposition failed"),
			wantSpans: 1,
			wantAttrs: []string{"orch.job_id", "orch.state"},
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			jobID:     "job-789",
			state:     "completed",
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceJobLifecycle(context.Background(), tt.jobID, tt.state)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "orch.job.lifecycle", span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				for _, a := range tt.wantAttrs {
					assert.True(t, hasAttr(a), "missing attribute: %s", a)
				}
			}
		})
	}
}

func TestTraceAgentOperation(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		agentID   string
		operation string
		err       error
		wantSpans int
		wantAttrs []string
	}{
		{
			name:      "successful spawn",
			enabled:   true,
			agentID:   "agent-a",
			operation: "spawn",
			err:       nil,
			wantSpans: 1,
			wantAttrs: []string{"orch.agent_id", "orch.operation"},
		},
		{
			name:      "prompt with error",
			enabled:   true,
			agentID:   "agent-b",
			operation: "prompt",
			err:       errors.New("prompt failed"),
			wantSpans: 1,
			wantAttrs: []string{"orch.agent_id", "orch.operation"},
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			agentID:   "agent-c",
			operation: "shutdown",
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceAgentOperation(context.Background(), tt.agentID, tt.operation)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "orch.agent."+tt.operation, span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				for _, a := range tt.wantAttrs {
					assert.True(t, hasAttr(a), "missing attribute: %s", a)
				}
			}
		})
	}
}

func TestTracePeerMessage(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		from      string
		to        string
		action    string
		err       error
		wantSpans int
		wantAttrs []string
	}{
		{
			name:      "successful send",
			enabled:   true,
			from:      "agent-a",
			to:        "agent-b",
			action:    "send",
			err:       nil,
			wantSpans: 1,
			wantAttrs: []string{"orch.from_agent", "orch.to_agent", "orch.action"},
		},
		{
			name:      "deliver with error",
			enabled:   true,
			from:      "agent-c",
			to:        "agent-d",
			action:    "deliver",
			err:       errors.New("delivery failed"),
			wantSpans: 1,
			wantAttrs: []string{"orch.from_agent", "orch.to_agent", "orch.action"},
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			from:      "agent-a",
			to:        "agent-b",
			action:    "acknowledge",
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TracePeerMessage(context.Background(), tt.from, tt.to, tt.action)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "orch.peer."+tt.action, span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				for _, a := range tt.wantAttrs {
					assert.True(t, hasAttr(a), "missing attribute: %s", a)
				}
			}
		})
	}
}

func TestTraceFileLock(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		projectID string
		filePath  string
		operation string
		err       error
		wantSpans int
		wantAttrs []string
	}{
		{
			name:      "successful acquire",
			enabled:   true,
			projectID: "proj-1",
			filePath:  "/src/main.go",
			operation: "acquire",
			err:       nil,
			wantSpans: 1,
			wantAttrs: []string{"orch.project_id", "orch.file_path", "orch.operation"},
		},
		{
			name:      "release with error",
			enabled:   true,
			projectID: "proj-2",
			filePath:  "/src/handler.go",
			operation: "release",
			err:       errors.New("release failed"),
			wantSpans: 1,
			wantAttrs: []string{"orch.project_id", "orch.file_path", "orch.operation"},
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			projectID: "proj-3",
			filePath:  "/src/test.go",
			operation: "renew",
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceFileLock(context.Background(), tt.projectID, tt.filePath, tt.operation)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "orch.filelock."+tt.operation, span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				for _, a := range tt.wantAttrs {
					assert.True(t, hasAttr(a), "missing attribute: %s", a)
				}
			}
		})
	}
}

func TestTraceIndexing(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		projectID string
		filePath  string
		operation string
		err       error
		wantSpans int
		wantAttrs []string
	}{
		{
			name:      "successful index",
			enabled:   true,
			projectID: "proj-1",
			filePath:  "/src/main.go",
			operation: "index",
			err:       nil,
			wantSpans: 1,
			wantAttrs: []string{"orch.project_id", "orch.file_path", "orch.operation"},
		},
		{
			name:      "embed with error",
			enabled:   true,
			projectID: "proj-2",
			filePath:  "/src/handler.go",
			operation: "embed",
			err:       errors.New("embedding failed"),
			wantSpans: 1,
			wantAttrs: []string{"orch.project_id", "orch.file_path", "orch.operation"},
		},
		{
			name:      "tracing disabled",
			enabled:   false,
			projectID: "proj-3",
			filePath:  "/src/test.go",
			operation: "chunk",
			err:       nil,
			wantSpans: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			ctx, finish := tracer.TraceIndexing(context.Background(), tt.projectID, tt.filePath, tt.operation)
			assert.NotNil(t, ctx)
			assert.NotNil(t, finish)

			finish(tt.err)

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, "orch.index."+tt.operation, span.Name())

				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				for _, a := range tt.wantAttrs {
					assert.True(t, hasAttr(a), "missing attribute: %s", a)
				}
			}
		})
	}
}

// =============================================================================
// Attribute Helper Tests
// =============================================================================

func TestOrchJobAttributes(t *testing.T) {
	attrs := OrchJobAttributes("job-123", "executing")
	assert.Len(t, attrs, 2)

	attrMap := make(map[string]string)
	for _, a := range attrs {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	assert.Equal(t, "job-123", attrMap["orch.job_id"])
	assert.Equal(t, "executing", attrMap["orch.state"])
}

func TestOrchAgentAttributes(t *testing.T) {
	attrs := OrchAgentAttributes("agent-a", "spawn")
	assert.Len(t, attrs, 2)

	attrMap := make(map[string]string)
	for _, a := range attrs {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	assert.Equal(t, "agent-a", attrMap["orch.agent_id"])
	assert.Equal(t, "spawn", attrMap["orch.operation"])
}

func TestOrchPeerAttributes(t *testing.T) {
	attrs := OrchPeerAttributes("agent-a", "agent-b", "send")
	assert.Len(t, attrs, 3)

	attrMap := make(map[string]string)
	for _, a := range attrs {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	assert.Equal(t, "agent-a", attrMap["orch.from_agent"])
	assert.Equal(t, "agent-b", attrMap["orch.to_agent"])
	assert.Equal(t, "send", attrMap["orch.action"])
}

// =============================================================================
// Noop Tracer Tests (disabled tracing returns valid no-op functions)
// =============================================================================

func TestOrchTracingFunctions_NoopTracer(t *testing.T) {
	tracer := &Tracer{
		config: &Config{
			Enabled:     false,
			ServiceName: "test-service",
		},
	}

	ctx := context.Background()

	// Verify all orchestration tracing functions work with disabled tracer
	ctx1, finish1 := tracer.TraceJobLifecycle(ctx, "job-1", "executing")
	assert.Equal(t, ctx, ctx1)
	finish1(nil)

	ctx2, finish2 := tracer.TraceAgentOperation(ctx, "agent-a", "spawn")
	assert.Equal(t, ctx, ctx2)
	finish2(errors.New("test error"))

	ctx3, finish3 := tracer.TracePeerMessage(ctx, "agent-a", "agent-b", "send")
	assert.Equal(t, ctx, ctx3)
	finish3(nil)

	ctx4, finish4 := tracer.TraceFileLock(ctx, "proj-1", "/file.go", "acquire")
	assert.Equal(t, ctx, ctx4)
	finish4(nil)

	ctx5, finish5 := tracer.TraceIndexing(ctx, "proj-1", "/file.go", "index")
	assert.Equal(t, ctx, ctx5)
	finish5(nil)
}
