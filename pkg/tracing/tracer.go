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

// Package tracing provides OpenTelemetry distributed tracing for QuicBaaS.
// It supports multiple exporters (Jaeger, Zipkin, OTLP) and provides context
// propagation across gRPC, REST, Raft, and internal operations.
package tracing

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ExporterType defines the type of trace exporter.
type ExporterType string

const (
	// ExporterJaeger exports traces to Jaeger.
	ExporterJaeger ExporterType = "jaeger"
	// ExporterZipkin exports traces to Zipkin.
	ExporterZipkin ExporterType = "zipkin"
	// ExporterOTLP exports traces using OTLP protocol.
	ExporterOTLP ExporterType = "otlp"
	// ExporterStdout exports traces to stdout (for testing).
	ExporterStdout ExporterType = "stdout"
	// ExporterNoop is a no-op exporter (disables tracing).
	ExporterNoop ExporterType = "noop"
)

// TLSConfig holds TLS configuration for the OTLP exporter.
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled for the exporter connection
	Enabled bool

	// CertFile is the path to the client certificate file (for mTLS)
	CertFile string

	// KeyFile is the path to the client private key file (for mTLS)
	KeyFile string

	// CAFile is the path to the CA certificate file for server verification
	CAFile string
}

// Config holds configuration for the tracer.
type Config struct {
	// Enabled controls whether tracing is enabled
	Enabled bool

	// ServiceName identifies the service in traces
	ServiceName string

	// ServiceVersion identifies the service version
	ServiceVersion string

	// ExporterType specifies which exporter to use
	ExporterType ExporterType

	// ExporterEndpoint is the endpoint for the exporter (OTLP, Jaeger, Zipkin)
	ExporterEndpoint string

	// SamplingRatio controls trace sampling (0.0 to 1.0)
	// 1.0 = sample all traces, 0.0 = sample no traces
	SamplingRatio float64

	// Environment identifies the deployment environment (e.g., "production", "staging")
	Environment string

	// NodeID identifies the specific node in a cluster
	NodeID uint64

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	ShutdownTimeout time.Duration

	// TLS holds TLS configuration for secure exporter connections
	TLS *TLSConfig
}

// Tracer wraps OpenTelemetry tracing functionality.
type Tracer struct {
	config   *Config
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	shutdown func(context.Context) error
}

// NewTracer creates a new tracer with the given configuration.
func NewTracer(cfg *Config) (*Tracer, error) {
	if cfg == nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "tracing.NewTracer",
			Err:  ErrConfigNil,
		}
	}

	// Validate config
	if err := validateConfig(cfg); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "tracing.NewTracer",
			Err:  err,
		}
	}

	// If tracing is disabled, return a no-op tracer
	if !cfg.Enabled {
		return &Tracer{
			config:   cfg,
			provider: nil,
			tracer:   noop.NewTracerProvider().Tracer(""),
			shutdown: func(context.Context) error { return nil },
		}, nil
	}

	// Create resource with service information
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"", // Empty schema URL to avoid conflicts
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			attribute.String("environment", cfg.Environment),
			attribute.Int64("node.id", int64(cfg.NodeID)), //nolint:gosec // G115: uint64 to int64 conversion for tracing attribute
			attribute.String("db.system", "quicbaas"),
		),
	)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "tracing.NewTracer",
			Err:  &ResourceCreationError{Err: err},
		}
	}

	// Create exporter based on configuration
	exporter, err := createExporter(cfg)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "tracing.NewTracer",
			Err:  err,
		}
	}

	// Create sampler
	sampler := createSampler(cfg.SamplingRatio)

	// Create trace provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set as global provider
	otel.SetTracerProvider(provider)

	// Set global propagator for context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create tracer
	tracer := provider.Tracer(
		"github.com/jeremyhahn/go-quicbaas",
		trace.WithInstrumentationVersion(cfg.ServiceVersion),
	)

	return &Tracer{
		config:   cfg,
		provider: provider,
		tracer:   tracer,
		shutdown: func(ctx context.Context) error {
			return provider.Shutdown(ctx)
		},
	}, nil
}

// TraceRequest creates a new span for tracing an operation
// Returns a new context with the span and a function to end the span.
func (t *Tracer) TraceRequest(ctx context.Context, operationName string, attrs ...attribute.KeyValue) (context.Context, func()) {
	if t.tracer == nil {
		// No-op tracer
		return ctx, func() {}
	}

	ctx, span := t.tracer.Start(ctx, operationName, trace.WithAttributes(attrs...))

	return ctx, func() {
		span.End()
	}
}

// StartSpan starts a new span with the given name and options.
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t.tracer == nil {
		// No-op tracer
		return ctx, trace.SpanFromContext(ctx)
	}

	return t.tracer.Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from context.
func (t *Tracer) SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddEvent adds an event to the current span.
func (t *Tracer) AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes adds attributes to the current span.
func (t *Tracer) SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// RecordError records an error on the current span.
func (t *Tracer) RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, opts...)
}

// Shutdown gracefully shuts down the tracer, flushing any pending spans.
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t.shutdown == nil {
		return nil
	}

	shutdownCtx := ctx
	if t.config.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(ctx, t.config.ShutdownTimeout)
		defer cancel()
	}

	return t.shutdown(shutdownCtx)
}

// IsEnabled returns whether tracing is enabled.
func (t *Tracer) IsEnabled() bool {
	return t.config.Enabled && t.provider != nil
}

// GetConfig returns the tracer configuration (read-only).
func (t *Tracer) GetConfig() Config {
	return *t.config
}

// validateConfig validates the tracer configuration.
func validateConfig(cfg *Config) error {
	if cfg.ServiceName == "" {
		return ErrServiceNameRequired
	}

	if cfg.SamplingRatio < 0.0 || cfg.SamplingRatio > 1.0 {
		return &SamplingRatioError{Ratio: cfg.SamplingRatio}
	}

	if cfg.Enabled {
		switch cfg.ExporterType {
		case ExporterJaeger, ExporterZipkin, ExporterOTLP:
			if cfg.ExporterEndpoint == "" {
				return &ExporterEndpointError{ExporterType: cfg.ExporterType}
			}
		case ExporterStdout, ExporterNoop:
			// No endpoint required
		default:
			return &UnknownExporterError{ExporterType: cfg.ExporterType}
		}

		// Validate TLS configuration if enabled
		if cfg.TLS != nil && cfg.TLS.Enabled {
			if err := validateTLSConfig(cfg.TLS); err != nil {
				return err
			}
		}
	}

	if cfg.ShutdownTimeout < 0 {
		return ErrShutdownTimeoutNegative
	}

	return nil
}

// validateTLSConfig validates the TLS configuration.
func validateTLSConfig(tlsCfg *TLSConfig) error {
	// If mTLS is configured (client cert), both cert and key must be provided
	if tlsCfg.CertFile != "" || tlsCfg.KeyFile != "" {
		if tlsCfg.CertFile == "" {
			return ErrTLSCertRequired
		}
		if tlsCfg.KeyFile == "" {
			return ErrTLSKeyRequired
		}
	}

	return nil
}

// createExporter creates a trace exporter based on configuration.
func createExporter(cfg *Config) (sdktrace.SpanExporter, error) {
	switch cfg.ExporterType {
	case ExporterOTLP, ExporterJaeger, ExporterZipkin:
		// OTLP exporter works for Jaeger and Zipkin with OTLP support
		// For Zipkin without OTLP, use the dedicated Zipkin exporter
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.ExporterEndpoint),
		}

		// TLS is required for OTLP exporter connections
		if cfg.TLS != nil && cfg.TLS.Enabled {
			creds, err := loadOTLPTLSCredentials(cfg.TLS)
			if err != nil {
				return nil, err
			}
			opts = append(opts, otlptracegrpc.WithTLSCredentials(creds))
		} else {
			return nil, &ExporterCreationError{
				ExporterType: "OTLP",
				Err:          ErrTLSRequired,
			}
		}

		exporter, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, &ExporterCreationError{ExporterType: "OTLP", Err: err}
		}
		return exporter, nil

	case ExporterStdout:
		// Stdout exporter for testing
		exporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, &ExporterCreationError{ExporterType: "stdout", Err: err}
		}
		return exporter, nil

	case ExporterNoop:
		// No-op exporter
		return &noopExporter{}, nil

	default:
		return nil, &UnsupportedExporterError{ExporterType: cfg.ExporterType}
	}
}

// loadOTLPTLSCredentials loads TLS credentials for the OTLP gRPC exporter.
func loadOTLPTLSCredentials(tlsCfg *TLSConfig) (credentials.TransportCredentials, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	// Load CA certificate if provided for server verification
	if tlsCfg.CAFile != "" {
		caCert, err := os.ReadFile(tlsCfg.CAFile)
		if err != nil {
			return nil, &TLSCAReadError{Path: tlsCfg.CAFile, Err: err}
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, &TLSCAParseError{Path: tlsCfg.CAFile}
		}

		tlsConfig.RootCAs = certPool
	}

	// Load client certificate for mutual TLS if provided
	if tlsCfg.CertFile != "" && tlsCfg.KeyFile != "" {
		clientCert, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile)
		if err != nil {
			return nil, &TLSCertLoadError{CertPath: tlsCfg.CertFile, KeyPath: tlsCfg.KeyFile, Err: err}
		}

		tlsConfig.Certificates = []tls.Certificate{clientCert}
	}

	return credentials.NewTLS(tlsConfig), nil
}

// createSampler creates a sampler based on sampling ratio.
func createSampler(ratio float64) sdktrace.Sampler {
	if ratio >= 1.0 {
		return sdktrace.AlwaysSample()
	}
	if ratio <= 0.0 {
		return sdktrace.NeverSample()
	}
	return sdktrace.TraceIDRatioBased(ratio)
}

// noopExporter is a no-op span exporter.
type noopExporter struct{}

func (e *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *noopExporter) Shutdown(ctx context.Context) error {
	return nil
}

// Common attribute keys for QuicBaaS operations.
var (
	// Database operation attributes.
	AttrDBOperation = attribute.Key("db.operation")
	AttrDBKey       = attribute.Key("db.key")
	AttrDBKeyCount  = attribute.Key("db.key.count")
	AttrDBValueSize = attribute.Key("db.value.size")
	AttrDBShardID   = attribute.Key("db.shard_id")
	AttrDBNodeID    = attribute.Key("db.node_id")
	AttrDBClusterID = attribute.Key("db.cluster_id")
	AttrDBSystem    = attribute.Key("db.system")

	// Raft operation attributes.
	AttrRaftProposalID = attribute.Key("raft.proposal_id")
	AttrRaftLogIndex   = attribute.Key("raft.log_index")
	AttrRaftTerm       = attribute.Key("raft.term")
	AttrRaftLeaderID   = attribute.Key("raft.leader_id")

	// Transaction attributes.
	AttrTxnID        = attribute.Key("txn.id")
	AttrTxnOpCount   = attribute.Key("txn.operation_count")
	AttrTxnIsolation = attribute.Key("txn.isolation")

	// Watch attributes.
	AttrWatchID         = attribute.Key("watch.id")
	AttrWatchEventCount = attribute.Key("watch.event_count")
	AttrWatchKeyPrefix  = attribute.Key("watch.key_prefix")

	// Network attributes.
	AttrPeerAddress     = attribute.Key("peer.address")
	AttrNetworkProtocol = attribute.Key("network.protocol")
)

// Helper functions for creating common attribute sets

// DBAttributes creates standard database operation attributes.
func DBAttributes(operation, key string, shardID uint64) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrDBSystem.String("quicbaas"),
		AttrDBOperation.String(operation),
		AttrDBKey.String(key),
		AttrDBShardID.Int64(int64(shardID)), //nolint:gosec // G115: uint64 to int64 conversion for tracing attribute
	}
}

// RaftAttributes creates standard Raft operation attributes.
func RaftAttributes(shardID, nodeID uint64, term, index uint64) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrDBShardID.Int64(int64(shardID)),  //nolint:gosec // G115: uint64 to int64 conversion for tracing attribute
		AttrDBNodeID.Int64(int64(nodeID)),    //nolint:gosec // G115: uint64 to int64 conversion for tracing attribute
		AttrRaftTerm.Int64(int64(term)),      //nolint:gosec // G115: uint64 to int64 conversion for tracing attribute
		AttrRaftLogIndex.Int64(int64(index)), //nolint:gosec // G115: uint64 to int64 conversion for tracing attribute
	}
}

// TxnAttributes creates standard transaction attributes.
func TxnAttributes(txnID string, opCount int) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrTxnID.String(txnID),
		AttrTxnOpCount.Int(opCount),
	}
}

// WatchAttributes creates standard watch attributes.
func WatchAttributes(watchID, keyPrefix string, eventCount int) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrWatchID.String(watchID),
		AttrWatchKeyPrefix.String(keyPrefix),
		AttrWatchEventCount.Int(eventCount),
	}
}
