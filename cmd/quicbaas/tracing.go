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

//go:build otel

package main

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// tracingExporterOTLPGRPC selects the OTLP gRPC exporter; any other exporter
// value selects stdout.
const tracingExporterOTLPGRPC = "otlpgrpc"

// initTracing installs a tracer provider when settings.Enabled is set.
func initTracing(settings tracingSettings) (func(context.Context) error, error) {
	if !settings.Enabled {
		return func(ctx context.Context) error { return nil }, nil
	}
	var (
		exp sdktrace.SpanExporter
		err error
	)
	switch settings.Exporter {
	case tracingExporterOTLPGRPC:
		exp, err = otlptracegrpc.New(context.Background(),
			otlptracegrpc.WithEndpoint(settings.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
	default:
		exp, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	if err != nil {
		return nil, err
	}
	res, err := sdkresource.New(context.Background(),
		sdkresource.WithAttributes(
			semconv.ServiceNameKey.String(settings.ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithMaxExportBatchSize(512), sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	// Add a default attribute indicating tracing is enabled
	otel.SetTextMapPropagator(otel.GetTextMapPropagator())
	_ = attribute.Key("tracing.enabled").Bool(true)
	return tp.Shutdown, nil
}
