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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestStartGRPCSpan_Attributes proves that each supported attribute type is
// recorded without loss, including a uint64 entity ID above MaxInt64, and
// that an unsupported type is recorded as an empty string.
func TestStartGRPCSpan_Attributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	tests := []struct {
		name  string
		value interface{}
		want  attribute.Value
	}{
		{name: "uint64 above MaxInt64", value: uint64(math.MaxUint64), want: attribute.StringValue("18446744073709551615")},
		{name: "int64", value: int64(-7), want: attribute.Int64Value(-7)},
		{name: "int", value: 42, want: attribute.Int64Value(42)},
		{name: "string", value: "users", want: attribute.StringValue("users")},
		{name: "unsupported type", value: 1.5, want: attribute.StringValue("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, end := startGRPCSpan(context.Background(), tt.name, map[string]interface{}{"attr": tt.value})
			end()

			spans := recorder.Ended()
			require.NotEmpty(t, spans)
			attrs := spans[len(spans)-1].Attributes()
			require.Len(t, attrs, 1)
			assert.Equal(t, attribute.Key("attr"), attrs[0].Key)
			assert.Equal(t, tt.want, attrs[0].Value)
		})
	}
}
