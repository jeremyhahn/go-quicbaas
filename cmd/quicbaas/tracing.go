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
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// initTracing initializes a basic stdout tracer when QUICBAAS_TRACING_ENABLED=1.
func initTracing() (func(context.Context) error, error) {
	if os.Getenv("QUICBAAS_TRACING_ENABLED") != "1" {
		return func(ctx context.Context) error { return nil }, nil
	}
	var (
		exp sdktrace.SpanExporter
		err error
	)
	switch os.Getenv("QUICBAAS_TRACING_EXPORTER") {
	case "otlpgrpc":
		endpoint := os.Getenv("QUICBAAS_OTLP_ENDPOINT")
		if endpoint == "" {
			endpoint = "localhost:4317"
		}
		exp, err = otlptracegrpc.New(context.Background(),
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
	default:
		exp, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	if err != nil {
		return nil, err
	}
	svc := os.Getenv("QUICBAAS_TRACING_SERVICE_NAME")
	if svc == "" {
		svc = "quicbaas"
	}
	res, err := sdkresource.New(context.Background(),
		sdkresource.WithAttributes(
			semconv.ServiceNameKey.String(svc),
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
