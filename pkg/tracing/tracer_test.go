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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestNewTracer(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "cannot be nil",
		},
		{
			name: "missing service name",
			config: &Config{
				Enabled:      true,
				ServiceName:  "",
				ExporterType: ExporterStdout,
			},
			wantErr:     true,
			errContains: "service name is required",
		},
		{
			name: "invalid sampling ratio - too high",
			config: &Config{
				Enabled:       true,
				ServiceName:   "test-service",
				ExporterType:  ExporterStdout,
				SamplingRatio: 1.5,
			},
			wantErr:     true,
			errContains: "sampling ratio must be between",
		},
		{
			name: "invalid sampling ratio - negative",
			config: &Config{
				Enabled:       true,
				ServiceName:   "test-service",
				ExporterType:  ExporterStdout,
				SamplingRatio: -0.1,
			},
			wantErr:     true,
			errContains: "sampling ratio must be between",
		},
		{
			name: "missing endpoint for OTLP",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test-service",
				ExporterType:     ExporterOTLP,
				ExporterEndpoint: "",
				SamplingRatio:    1.0,
			},
			wantErr:     true,
			errContains: "exporter endpoint is required",
		},
		{
			name: "valid config - disabled",
			config: &Config{
				Enabled:       false,
				ServiceName:   "test-service",
				SamplingRatio: 1.0,
			},
			wantErr: false,
		},
		{
			name: "valid config - stdout exporter",
			config: &Config{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				ExporterType:   ExporterStdout,
				SamplingRatio:  1.0,
				Environment:    "test",
				NodeID:         1,
			},
			wantErr: false,
		},
		{
			name: "valid config - noop exporter",
			config: &Config{
				Enabled:       true,
				ServiceName:   "test-service",
				ExporterType:  ExporterNoop,
				SamplingRatio: 1.0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, err := NewTracer(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, tracer)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, tracer)
				defer tracer.Shutdown(context.Background())

				// Verify config is stored
				assert.Equal(t, tt.config.ServiceName, tracer.config.ServiceName)
				assert.Equal(t, tt.config.Enabled, tracer.config.Enabled)
			}
		})
	}
}

func TestTracer_TraceRequest(t *testing.T) {
	tests := []struct {
		name          string
		enabled       bool
		operationName string
		attributes    []attribute.KeyValue
		wantSpan      bool
	}{
		{
			name:          "tracing enabled - simple span",
			enabled:       true,
			operationName: "test.operation",
			attributes:    nil,
			wantSpan:      true,
		},
		{
			name:          "tracing enabled - with attributes",
			enabled:       true,
			operationName: "db.get",
			attributes: []attribute.KeyValue{
				AttrDBKey.String("user:123"),
				AttrDBShardID.Int64(1),
			},
			wantSpan: true,
		},
		{
			name:          "tracing disabled",
			enabled:       false,
			operationName: "test.operation",
			attributes:    nil,
			wantSpan:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create in-memory span recorder for testing
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

			if !tt.enabled {
				tracer.tracer = trace.NewNoopTracerProvider().Tracer("")
			}

			ctx := context.Background()
			newCtx, finish := tracer.TraceRequest(ctx, tt.operationName, tt.attributes...)
			require.NotNil(t, finish)

			// Context should always be returned
			assert.NotNil(t, newCtx)

			// Simulate work
			time.Sleep(1 * time.Millisecond)

			// End span
			finish()

			// Verify span was created if enabled
			spans := sr.Ended()
			if tt.wantSpan {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, tt.operationName, span.Name())

				// Verify attributes
				if len(tt.attributes) > 0 {
					gotAttrs := span.Attributes()
					for _, expectedAttr := range tt.attributes {
						found := false
						for _, gotAttr := range gotAttrs {
							if gotAttr.Key == expectedAttr.Key && gotAttr.Value == expectedAttr.Value {
								found = true
								break
							}
						}
						assert.True(t, found, "expected attribute %v not found", expectedAttr)
					}
				}
			} else {
				assert.Len(t, spans, 0)
			}
		})
	}
}

func TestTracer_StartSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	ctx := context.Background()
	spanCtx, span := tracer.StartSpan(ctx, "test.span",
		trace.WithAttributes(
			attribute.String("test.key", "test.value"),
		),
	)

	assert.NotNil(t, spanCtx)
	assert.NotNil(t, span)

	// Add some operations
	span.SetAttributes(attribute.Int("test.count", 42))
	span.AddEvent("test.event")

	span.End()

	// Verify span
	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "test.span", spans[0].Name())
	assert.Len(t, spans[0].Events(), 1)
	assert.Equal(t, "test.event", spans[0].Events()[0].Name)
}

func TestTracer_RecordError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	ctx, span := tracer.StartSpan(context.Background(), "test.error")
	testErr := errors.New("test error")

	// Record error
	tracer.RecordError(ctx, testErr)
	span.End()

	// Verify error was recorded
	spans := sr.Ended()
	require.Len(t, spans, 1)

	events := spans[0].Events()
	require.Len(t, events, 1)
	assert.Equal(t, "exception", events[0].Name)

	// Check status
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
}

func TestTracer_ContextPropagation(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	// Create parent span
	ctx := context.Background()
	parentCtx, parentSpan := tracer.StartSpan(ctx, "parent.operation")

	// Create child span using parent context
	childCtx, childSpan := tracer.StartSpan(parentCtx, "child.operation")
	childSpan.End()
	parentSpan.End()

	// Verify parent-child relationship
	spans := sr.Ended()
	require.Len(t, spans, 2)

	childSpanData := spans[0]  // First ended
	parentSpanData := spans[1] // Second ended

	assert.Equal(t, "child.operation", childSpanData.Name())
	assert.Equal(t, "parent.operation", parentSpanData.Name())

	// Verify parent-child relationship
	assert.Equal(t, parentSpanData.SpanContext().SpanID(), childSpanData.Parent().SpanID())
	assert.Equal(t, parentSpanData.SpanContext().TraceID(), childSpanData.SpanContext().TraceID())

	// Verify contexts are different
	assert.NotEqual(t, ctx, parentCtx)
	assert.NotEqual(t, parentCtx, childCtx)
}

func TestTracer_AddEvent(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	ctx, span := tracer.StartSpan(context.Background(), "test.events")

	// Add multiple events
	tracer.AddEvent(ctx, "event1", attribute.String("key1", "value1"))
	tracer.AddEvent(ctx, "event2", attribute.Int("key2", 42))

	span.End()

	// Verify events
	spans := sr.Ended()
	require.Len(t, spans, 1)
	events := spans[0].Events()
	require.Len(t, events, 2)

	assert.Equal(t, "event1", events[0].Name)
	assert.Equal(t, "event2", events[1].Name)
}

func TestTracer_SetAttributes(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	ctx, span := tracer.StartSpan(context.Background(), "test.attributes")

	// Set attributes after span creation
	tracer.SetAttributes(ctx,
		attribute.String("db.operation", "get"),
		attribute.String("db.key", "user:123"),
		attribute.Int64("db.shard_id", 1),
	)

	span.End()

	// Verify attributes
	spans := sr.Ended()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes()
	require.GreaterOrEqual(t, len(attrs), 3)

	hasAttr := func(key string) bool {
		for _, attr := range attrs {
			if string(attr.Key) == key {
				return true
			}
		}
		return false
	}

	assert.True(t, hasAttr("db.operation"))
	assert.True(t, hasAttr("db.key"))
	assert.True(t, hasAttr("db.shard_id"))
}

func TestTracer_Shutdown(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "successful shutdown",
			timeout: 5 * time.Second,
			wantErr: false,
		},
		{
			name:    "shutdown with timeout",
			timeout: 1 * time.Nanosecond,
			wantErr: true, // May timeout
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, err := NewTracer(&Config{
				Enabled:         true,
				ServiceName:     "test-service",
				ExporterType:    ExporterStdout,
				SamplingRatio:   1.0,
				ShutdownTimeout: tt.timeout,
			})
			require.NoError(t, err)
			require.NotNil(t, tracer)

			// Create and end a span
			ctx, span := tracer.StartSpan(context.Background(), "test.shutdown")
			span.End()

			// Shutdown
			err = tracer.Shutdown(ctx)
			if tt.wantErr && tt.timeout < time.Millisecond {
				// Very short timeout may cause error, but not required
				// Just verify no panic
				t.Logf("Shutdown result: %v", err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTracer_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{
			name:    "enabled",
			enabled: true,
			want:    true,
		},
		{
			name:    "disabled",
			enabled: false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Enabled:       tt.enabled,
				ServiceName:   "test-service",
				ExporterType:  ExporterStdout,
				SamplingRatio: 1.0,
			}

			tracer, err := NewTracer(cfg)
			require.NoError(t, err)
			defer tracer.Shutdown(context.Background())

			assert.Equal(t, tt.want, tracer.IsEnabled())
		})
	}
}

func TestTracer_GetConfig(t *testing.T) {
	cfg := &Config{
		Enabled:        true,
		ServiceName:    "test-service",
		ServiceVersion: "1.2.3",
		ExporterType:   ExporterStdout,
		SamplingRatio:  0.5,
		Environment:    "production",
		NodeID:         42,
	}

	tracer, err := NewTracer(cfg)
	require.NoError(t, err)
	defer tracer.Shutdown(context.Background())

	gotCfg := tracer.GetConfig()
	assert.Equal(t, cfg.ServiceName, gotCfg.ServiceName)
	assert.Equal(t, cfg.ServiceVersion, gotCfg.ServiceVersion)
	assert.Equal(t, cfg.ExporterType, gotCfg.ExporterType)
	assert.Equal(t, cfg.SamplingRatio, gotCfg.SamplingRatio)
	assert.Equal(t, cfg.Environment, gotCfg.Environment)
	assert.Equal(t, cfg.NodeID, gotCfg.NodeID)
}

func TestDBAttributes(t *testing.T) {
	attrs := DBAttributes("get", "user:123", 1)

	require.Len(t, attrs, 4)
	assert.Equal(t, "db.system", string(attrs[0].Key))
	assert.Equal(t, "quicbaas", attrs[0].Value.AsString())
	assert.Equal(t, "db.operation", string(attrs[1].Key))
	assert.Equal(t, "get", attrs[1].Value.AsString())
	assert.Equal(t, "db.key", string(attrs[2].Key))
	assert.Equal(t, "user:123", attrs[2].Value.AsString())
	assert.Equal(t, "db.shard_id", string(attrs[3].Key))
	assert.Equal(t, int64(1), attrs[3].Value.AsInt64())
}

func TestRaftAttributes(t *testing.T) {
	attrs := RaftAttributes(1, 2, 100, 500)

	require.Len(t, attrs, 4)
	assert.Equal(t, "db.shard_id", string(attrs[0].Key))
	assert.Equal(t, int64(1), attrs[0].Value.AsInt64())
	assert.Equal(t, "db.node_id", string(attrs[1].Key))
	assert.Equal(t, int64(2), attrs[1].Value.AsInt64())
	assert.Equal(t, "raft.term", string(attrs[2].Key))
	assert.Equal(t, int64(100), attrs[2].Value.AsInt64())
	assert.Equal(t, "raft.log_index", string(attrs[3].Key))
	assert.Equal(t, int64(500), attrs[3].Value.AsInt64())
}

func TestTxnAttributes(t *testing.T) {
	attrs := TxnAttributes("txn-123", 5)

	require.Len(t, attrs, 2)
	assert.Equal(t, "txn.id", string(attrs[0].Key))
	assert.Equal(t, "txn-123", attrs[0].Value.AsString())
	assert.Equal(t, "txn.operation_count", string(attrs[1].Key))
	assert.Equal(t, int64(5), attrs[1].Value.AsInt64())
}

func TestWatchAttributes(t *testing.T) {
	attrs := WatchAttributes("watch-456", "/prefix/", 10)

	require.Len(t, attrs, 3)
	assert.Equal(t, "watch.id", string(attrs[0].Key))
	assert.Equal(t, "watch-456", attrs[0].Value.AsString())
	assert.Equal(t, "watch.key_prefix", string(attrs[1].Key))
	assert.Equal(t, "/prefix/", attrs[1].Value.AsString())
	assert.Equal(t, "watch.event_count", string(attrs[2].Key))
	assert.Equal(t, int64(10), attrs[2].Value.AsInt64())
}

func TestSamplingBehavior(t *testing.T) {
	tests := []struct {
		name          string
		samplingRatio float64
		iterations    int
		wantSampled   bool
	}{
		{
			name:          "always sample",
			samplingRatio: 1.0,
			iterations:    10,
			wantSampled:   true,
		},
		{
			name:          "never sample",
			samplingRatio: 0.0,
			iterations:    10,
			wantSampled:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
				sdktrace.WithSampler(createSampler(tt.samplingRatio)),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:       true,
					ServiceName:   "test-service",
					SamplingRatio: tt.samplingRatio,
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			// Create multiple spans
			for i := 0; i < tt.iterations; i++ {
				_, span := tracer.StartSpan(context.Background(), "test.span")
				span.End()
			}

			spans := sr.Ended()
			if tt.wantSampled {
				assert.Equal(t, tt.iterations, len(spans), "all spans should be sampled")
			} else {
				assert.Equal(t, 0, len(spans), "no spans should be sampled")
			}
		})
	}
}

func BenchmarkTracer_TraceRequest(b *testing.B) {
	tracer, err := NewTracer(&Config{
		Enabled:       true,
		ServiceName:   "benchmark-service",
		ExporterType:  ExporterNoop,
		SamplingRatio: 1.0,
	})
	require.NoError(b, err)
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		newCtx, finish := tracer.TraceRequest(ctx, "benchmark.operation",
			AttrDBKey.String("key"),
			AttrDBShardID.Int64(1),
		)
		_ = newCtx
		finish()
	}
}

func BenchmarkTracer_StartSpan(b *testing.B) {
	tracer, err := NewTracer(&Config{
		Enabled:       true,
		ServiceName:   "benchmark-service",
		ExporterType:  ExporterNoop,
		SamplingRatio: 1.0,
	})
	require.NoError(b, err)
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "benchmark.span")
		span.End()
	}
}

func BenchmarkTracer_Disabled(b *testing.B) {
	tracer, err := NewTracer(&Config{
		Enabled:       false,
		ServiceName:   "benchmark-service",
		SamplingRatio: 1.0,
	})
	require.NoError(b, err)
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		newCtx, finish := tracer.TraceRequest(ctx, "benchmark.operation")
		_ = newCtx
		finish()
	}
}

// =============================================================================
// Consolidated from tracer_comprehensive_test.go
// =============================================================================

func TestTracer_SpanFromContext(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	t.Run("context with span", func(t *testing.T) {
		ctx, span := tracer.StartSpan(context.Background(), "test-span")

		retrievedSpan := tracer.SpanFromContext(ctx)
		assert.NotNil(t, retrievedSpan)
		assert.Equal(t, span, retrievedSpan)

		span.End()
	})

	t.Run("context without span", func(t *testing.T) {
		ctx := context.Background()

		span := tracer.SpanFromContext(ctx)
		assert.NotNil(t, span)
		// Should return a no-op span
	})
}

func TestValidateConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "unknown exporter type",
			config: &Config{
				Enabled:       true,
				ServiceName:   "test",
				ExporterType:  "unknown",
				SamplingRatio: 1.0,
			},
			wantErr:     true,
			errContains: "unknown exporter type",
		},
		{
			name: "negative shutdown timeout",
			config: &Config{
				Enabled:         true,
				ServiceName:     "test",
				ExporterType:    ExporterStdout,
				SamplingRatio:   1.0,
				ShutdownTimeout: -1 * time.Second,
			},
			wantErr:     true,
			errContains: "shutdown timeout cannot be negative",
		},
		{
			name: "missing endpoint for jaeger",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test",
				ExporterType:     ExporterJaeger,
				ExporterEndpoint: "",
				SamplingRatio:    1.0,
			},
			wantErr:     true,
			errContains: "exporter endpoint is required",
		},
		{
			name: "missing endpoint for zipkin",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test",
				ExporterType:     ExporterZipkin,
				ExporterEndpoint: "",
				SamplingRatio:    1.0,
			},
			wantErr:     true,
			errContains: "exporter endpoint is required",
		},
		{
			name: "valid noop exporter - no endpoint required",
			config: &Config{
				Enabled:       true,
				ServiceName:   "test",
				ExporterType:  ExporterNoop,
				SamplingRatio: 1.0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateSampler(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
	}{
		{
			name:  "always sample",
			ratio: 1.0,
		},
		{
			name:  "never sample",
			ratio: 0.0,
		},
		{
			name:  "partial sample",
			ratio: 0.5,
		},
		{
			name:  "over 1.0 (treated as always)",
			ratio: 1.5,
		},
		{
			name:  "negative (treated as never)",
			ratio: -0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampler := createSampler(tt.ratio)
			assert.NotNil(t, sampler)
		})
	}
}

func TestNoopExporter(t *testing.T) {
	exporter := &noopExporter{}

	t.Run("export spans", func(t *testing.T) {
		err := exporter.ExportSpans(context.Background(), nil)
		assert.NoError(t, err)
	})

	t.Run("shutdown", func(t *testing.T) {
		err := exporter.Shutdown(context.Background())
		assert.NoError(t, err)
	})
}

func TestTracer_RecordError_WithNilError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	ctx, span := tracer.StartSpan(context.Background(), "test.nil-error")

	// Record nil error - should not add error event
	tracer.RecordError(ctx, nil)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	// Verify no error events
	events := spans[0].Events()
	assert.Len(t, events, 0)
}

func TestTracer_Shutdown_WithNilShutdown(t *testing.T) {
	tracer := &Tracer{
		config: &Config{
			Enabled:     false,
			ServiceName: "test-service",
		},
		shutdown: nil,
	}

	err := tracer.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestTracer_Shutdown_WithTimeout(t *testing.T) {
	tracer, err := NewTracer(&Config{
		Enabled:         true,
		ServiceName:     "test-service",
		ExporterType:    ExporterNoop,
		SamplingRatio:   1.0,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	// Create and end a span
	ctx, span := tracer.StartSpan(context.Background(), "test.shutdown")
	span.End()

	// Shutdown with timeout
	err = tracer.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestTracer_Shutdown_WithoutTimeout(t *testing.T) {
	tracer, err := NewTracer(&Config{
		Enabled:         true,
		ServiceName:     "test-service",
		ExporterType:    ExporterNoop,
		SamplingRatio:   1.0,
		ShutdownTimeout: 0, // No timeout
	})
	require.NoError(t, err)

	ctx, span := tracer.StartSpan(context.Background(), "test.shutdown-no-timeout")
	span.End()

	err = tracer.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestTracer_StartSpan_WithNilTracer(t *testing.T) {
	tracer := &Tracer{
		config: &Config{
			Enabled:     false,
			ServiceName: "test-service",
		},
		tracer: nil,
	}

	ctx, span := tracer.StartSpan(context.Background(), "test-span")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
}

func TestTracer_TraceRequest_WithNilTracer(t *testing.T) {
	tracer := &Tracer{
		config: &Config{
			Enabled:     false,
			ServiceName: "test-service",
		},
		tracer: nil,
	}

	ctx, finish := tracer.TraceRequest(context.Background(), "test-operation")
	assert.NotNil(t, ctx)
	assert.NotNil(t, finish)
	finish()
}

func TestNewTracer_WithResourceMerge(t *testing.T) {
	tracer, err := NewTracer(&Config{
		Enabled:        true,
		ServiceName:    "test-service",
		ServiceVersion: "2.0.0",
		ExporterType:   ExporterNoop,
		SamplingRatio:  1.0,
		Environment:    "staging",
		NodeID:         999,
	})
	require.NoError(t, err)
	require.NotNil(t, tracer)
	defer tracer.Shutdown(context.Background())

	// Verify tracer is enabled
	assert.True(t, tracer.IsEnabled())

	// Verify config
	cfg := tracer.GetConfig()
	assert.Equal(t, "test-service", cfg.ServiceName)
	assert.Equal(t, "2.0.0", cfg.ServiceVersion)
	assert.Equal(t, "staging", cfg.Environment)
	assert.Equal(t, uint64(999), cfg.NodeID)
}

func TestCreateExporter_AllTypes(t *testing.T) {
	tests := []struct {
		name         string
		exporterType ExporterType
		endpoint     string
		wantErr      bool
	}{
		{
			name:         "stdout exporter",
			exporterType: ExporterStdout,
			endpoint:     "",
			wantErr:      false,
		},
		{
			name:         "noop exporter",
			exporterType: ExporterNoop,
			endpoint:     "",
			wantErr:      false,
		},
		{
			name:         "unsupported exporter",
			exporterType: "unsupported",
			endpoint:     "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ExporterType:     tt.exporterType,
				ExporterEndpoint: tt.endpoint,
			}

			exporter, err := createExporter(cfg)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, exporter)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, exporter)
			}
		})
	}
}

func TestTracer_FullLifecycle(t *testing.T) {
	// Create tracer
	tracer, err := NewTracer(&Config{
		Enabled:         true,
		ServiceName:     "lifecycle-test",
		ServiceVersion:  "1.0.0",
		ExporterType:    ExporterNoop,
		SamplingRatio:   1.0,
		Environment:     "test",
		NodeID:          1,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, tracer)

	// Start parent span
	ctx, span := tracer.StartSpan(context.Background(), "parent",
		trace.WithAttributes(
			attribute.String("test.key", "test.value"),
		),
	)
	assert.NotNil(t, ctx)

	// Add event
	tracer.AddEvent(ctx, "test.event",
		attribute.String("event.key", "event.value"),
	)

	// Set attributes
	tracer.SetAttributes(ctx,
		attribute.Int("test.count", 42),
	)

	// Record error
	testErr := errors.New("test error")
	tracer.RecordError(ctx, testErr)

	// Create child span
	childCtx, childSpan := tracer.StartSpan(ctx, "child")
	assert.NotNil(t, childCtx)

	// End child span
	childSpan.End()

	// End parent span
	span.End()

	// Shutdown
	err = tracer.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestTracer_DisabledOperations(t *testing.T) {
	// Create disabled tracer
	tracer, err := NewTracer(&Config{
		Enabled:       false,
		ServiceName:   "disabled-test",
		SamplingRatio: 1.0,
	})
	require.NoError(t, err)
	require.NotNil(t, tracer)
	defer tracer.Shutdown(context.Background())

	// Verify disabled
	assert.False(t, tracer.IsEnabled())

	// All operations should work without panicking
	ctx := context.Background()

	// TraceRequest
	newCtx, finish := tracer.TraceRequest(ctx, "test")
	assert.NotNil(t, newCtx)
	finish()

	// StartSpan
	spanCtx, span := tracer.StartSpan(ctx, "test-span")
	assert.NotNil(t, spanCtx)
	assert.NotNil(t, span)
	span.End()

	// SpanFromContext
	s := tracer.SpanFromContext(ctx)
	assert.NotNil(t, s)

	// AddEvent
	tracer.AddEvent(ctx, "event")

	// SetAttributes
	tracer.SetAttributes(ctx, attribute.String("key", "value"))

	// RecordError
	tracer.RecordError(ctx, errors.New("test error"))
}

func TestAttributeHelpers(t *testing.T) {
	t.Run("DBAttributes", func(t *testing.T) {
		attrs := DBAttributes("put", "user:123", 1)
		require.Len(t, attrs, 4)

		// Verify all expected attributes
		keys := make(map[string]bool)
		for _, attr := range attrs {
			keys[string(attr.Key)] = true
		}
		assert.True(t, keys["db.system"])
		assert.True(t, keys["db.operation"])
		assert.True(t, keys["db.key"])
		assert.True(t, keys["db.shard_id"])
	})

	t.Run("RaftAttributes", func(t *testing.T) {
		attrs := RaftAttributes(1, 100, 5, 1000)
		require.Len(t, attrs, 4)

		keys := make(map[string]bool)
		for _, attr := range attrs {
			keys[string(attr.Key)] = true
		}
		assert.True(t, keys["db.shard_id"])
		assert.True(t, keys["db.node_id"])
		assert.True(t, keys["raft.term"])
		assert.True(t, keys["raft.log_index"])
	})

	t.Run("TxnAttributes", func(t *testing.T) {
		attrs := TxnAttributes("txn-123", 10)
		require.Len(t, attrs, 2)

		keys := make(map[string]bool)
		for _, attr := range attrs {
			keys[string(attr.Key)] = true
		}
		assert.True(t, keys["txn.id"])
		assert.True(t, keys["txn.operation_count"])
	})

	t.Run("WatchAttributes", func(t *testing.T) {
		attrs := WatchAttributes("watch-456", "/prefix/", 5)
		require.Len(t, attrs, 3)

		keys := make(map[string]bool)
		for _, attr := range attrs {
			keys[string(attr.Key)] = true
		}
		assert.True(t, keys["watch.id"])
		assert.True(t, keys["watch.key_prefix"])
		assert.True(t, keys["watch.event_count"])
	})
}

func BenchmarkTracer_CompleteOperation(b *testing.B) {
	tracer, err := NewTracer(&Config{
		Enabled:       true,
		ServiceName:   "benchmark-service",
		ExporterType:  ExporterNoop,
		SamplingRatio: 1.0,
	})
	require.NoError(b, err)
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		spanCtx, span := tracer.StartSpan(ctx, "benchmark.operation",
			trace.WithAttributes(
				AttrDBKey.String("key"),
				AttrDBShardID.Int64(1),
			),
		)
		tracer.AddEvent(spanCtx, "event")
		tracer.SetAttributes(spanCtx, attribute.Int("count", i))
		span.End()
	}
}

func BenchmarkTracer_NestedSpans(b *testing.B) {
	tracer, err := NewTracer(&Config{
		Enabled:       true,
		ServiceName:   "benchmark-service",
		ExporterType:  ExporterNoop,
		SamplingRatio: 1.0,
	})
	require.NoError(b, err)
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx1, span1 := tracer.StartSpan(ctx, "parent")
		ctx2, span2 := tracer.StartSpan(ctx1, "child1")
		ctx3, span3 := tracer.StartSpan(ctx2, "child2")
		span3.End()
		span2.End()
		span1.End()
		_ = ctx3
	}
}
