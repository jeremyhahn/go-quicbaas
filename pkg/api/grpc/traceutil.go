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

package grpc

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func startGRPCSpan(ctx context.Context, name string, attrs map[string]interface{}) (context.Context, func()) {
	tracer := otel.Tracer("quicbaas/grpc")
	var kvs []attribute.KeyValue
	for k, v := range attrs {
		switch t := v.(type) {
		case string:
			kvs = append(kvs, attribute.String(k, t))
		case int64:
			kvs = append(kvs, attribute.Int64(k, t))
		case int:
			kvs = append(kvs, attribute.Int64(k, int64(t)))
		default:
			kvs = append(kvs, attribute.String(k, ""))
		}
	}
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(kvs...))
	return ctx, span.End
}
