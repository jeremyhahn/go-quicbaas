//go:build mcp

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

package mcp

import (
	"context"
	stderrors "errors"
	"math"
	"strconv"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// requireInvalidIntArg asserts err is the typed invalid-input refusal of a
// whole-number argument, caused by a conv range error, and that it reaches an
// MCP client as an invalid-input tool error naming the argument.
func requireInvalidIntArg(t *testing.T, err error, key string) {
	t.Helper()
	var qbErr *qberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, qberrors.ErrInvalidInput, qbErr.Code)
	requireConvRangeError(t, err)

	result := ToolErrorResult(nil, "quicbaas_test_tool", err)
	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPInvalidInput}, result.StructuredContent)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Contains(t, text.Text, "invalid "+key)
}

// requireConvRangeError asserts err carries a conv range error, or the
// strconv error of a string that is not a uint64.
func requireConvRangeError(t *testing.T, err error) {
	t.Helper()
	var overflow *conv.ConversionOverflowError
	var negative *conv.ConversionNegativeError
	var numErr *strconv.NumError
	require.True(t,
		stderrors.As(err, &overflow) || stderrors.As(err, &negative) || stderrors.As(err, &numErr),
		"error %v carries no conversion cause", err)
}

func TestIntArgReader_Int(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    int
		wantErr bool
	}{
		{name: "absent", value: nil, want: 0},
		{name: "float", value: float64(25), want: 25},
		{name: "int", value: 7, want: 7},
		{name: "int64", value: int64(9), want: 9},
		{name: "uint64", value: uint64(11), want: 11},
		{name: "string", value: "42", want: 42},
		{name: "empty string", value: "", want: 0},
		{name: "unsupported type", value: true, want: 0},
		{name: "negative float", value: float64(-1), wantErr: true},
		{name: "negative int", value: -1, wantErr: true},
		{name: "negative int64", value: int64(-1), wantErr: true},
		{name: "float beyond uint64", value: float64(2e19), wantErr: true},
		{name: "NaN", value: math.NaN(), wantErr: true},
		{name: "uint64 beyond int", value: uint64(math.MaxUint64), wantErr: true},
		{name: "string beyond int", value: "18446744073709551615", wantErr: true},
		{name: "string beyond uint64", value: "18446744073709551616", wantErr: true},
		{name: "negative string", value: "-5", wantErr: true},
		{name: "non-numeric string", value: "ten", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{}
			if tt.value != nil {
				args["limit"] = tt.value
			}
			ints := newIntArgReader(args)
			got := ints.Int("limit")

			if tt.wantErr {
				requireInvalidIntArg(t, ints.Err(), "limit")
				require.Zero(t, got)
				return
			}
			require.NoError(t, ints.Err())
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIntArgReader_Int64AndUint64(t *testing.T) {
	ints := newIntArgReader(map[string]interface{}{
		"version": "18446744073709551615",
		"offset":  uint64(math.MaxUint64),
	})
	require.Equal(t, uint64(math.MaxUint64), ints.Uint64("version"))
	require.NoError(t, ints.Err())

	require.Zero(t, ints.Int64("offset"))
	requireInvalidIntArg(t, ints.Err(), "offset")
}

func TestIntArgReader_IntOr(t *testing.T) {
	ints := newIntArgReader(map[string]interface{}{"limit": float64(5), "offset": float64(0)})
	require.Equal(t, 5, ints.IntOr("limit", 100))
	require.Equal(t, 100, ints.IntOr("offset", 100))
	require.Equal(t, 100, ints.IntOr("absent", 100))
	require.NoError(t, ints.Err())

	refused := newIntArgReader(map[string]interface{}{"limit": float64(-3)})
	require.Equal(t, 100, refused.IntOr("limit", 100))
	requireInvalidIntArg(t, refused.Err(), "limit")
}

// TestIntArgReader_KeepsFirstError proves a handler that reads every argument
// before checking Err reports the first refused one, not a later one.
func TestIntArgReader_KeepsFirstError(t *testing.T) {
	ints := newIntArgReader(map[string]interface{}{
		"threshold":    float64(-1),
		"total_shares": "many",
		"offset":       float64(4),
	})
	ints.Int("threshold")
	ints.Int("total_shares")
	require.Equal(t, 4, ints.Int("offset"))

	requireInvalidIntArg(t, ints.Err(), "threshold")
	require.NotContains(t, ints.Err().Error(), "total_shares")
}

func TestCappedIntArg(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    int
		wantErr bool
	}{
		{name: "absent", value: nil, want: 0},
		{name: "below cap", value: float64(10), want: 10},
		{name: "at cap", value: float64(100), want: 100},
		{name: "above cap saturates", value: float64(20000), want: 100},
		{name: "beyond int saturates", value: "18446744073709551615", want: 100},
		{name: "negative", value: float64(-1), wantErr: true},
		{name: "non-numeric string", value: "all", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{}
			if tt.value != nil {
				args["limit"] = tt.value
			}
			got, err := cappedIntArg(args, "limit", 100)

			if tt.wantErr {
				requireInvalidIntArg(t, err, "limit")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIntArgs_HandlersRefuseOutOfRange drives handlers whose arguments are
// read before any service is reached, so the refusal is the handler's own.
func TestIntArgs_HandlersRefuseOutOfRange(t *testing.T) {
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	// The secrets tools refuse a nil Secrets service before reading any
	// argument, so they need a mock that provides one.
	secretsRegistry := NewToolRegistry(testutil.NewMockQuicBaaS(), newTestLogger(t))
	ctx := context.Background()

	tests := []struct {
		name string
		key  string
		call func() (interface{}, error)
	}{
		{
			name: "kms list keys negative limit",
			key:  "limit",
			call: func() (interface{}, error) {
				return registry.executeKMSListKeys(ctx, map[string]interface{}{"limit": float64(-1)})
			},
		},
		{
			name: "kms delete key pending window beyond int",
			key:  "pending_window_days",
			call: func() (interface{}, error) {
				return registry.executeKMSDeleteKey(ctx, map[string]interface{}{
					"key_id":              "k1",
					"pending_window_days": "18446744073709551615",
				})
			},
		},
		{
			name: "vector namespace negative dimensions",
			key:  "dimensions",
			call: func() (interface{}, error) {
				return registry.handleVectorCreateNamespace(ctx, nil, map[string]interface{}{
					"namespace":  "ns",
					"dimensions": float64(-128),
				})
			},
		},
		{
			name: "vector namespace dimensions beyond uint32",
			key:  "dimensions",
			call: func() (interface{}, error) {
				return registry.handleVectorCreateNamespace(ctx, nil, map[string]interface{}{
					"namespace":  "ns",
					"dimensions": float64(1 << 32),
				})
			},
		},
		{
			name: "secrets watch timeout beyond time.Duration",
			key:  "timeout_seconds",
			call: func() (interface{}, error) {
				return secretsRegistry.executeSecretsWatch(ctx, map[string]interface{}{
					"prefix":          "app/",
					"timeout_seconds": "9223372036854775807",
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.call()
			require.Nil(t, result)
			requireInvalidIntArg(t, err, tt.key)
		})
	}
}

func TestMCPServer_ParseInt32Param(t *testing.T) {
	s := &MCPServer{}
	tests := []struct {
		name    string
		value   interface{}
		want    int32
		wantErr bool
	}{
		{name: "absent uses default", value: nil, want: 100},
		{name: "float", value: float64(512), want: 512},
		{name: "string", value: "64", want: 64},
		{name: "largest int32", value: float64(math.MaxInt32), want: math.MaxInt32},
		{name: "beyond int32", value: float64(math.MaxInt32 + 1), wantErr: true},
		{name: "below int32", value: "-2147483649", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{}
			if tt.value != nil {
				params["page_size"] = tt.value
			}
			got, err := s.parseInt32Param(params, "page_size", 100)

			if tt.wantErr {
				requireInvalidIntArg(t, err, "page_size")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMCPServer_ParseIntParam(t *testing.T) {
	s := &MCPServer{}
	tests := []struct {
		name    string
		value   interface{}
		want    int
		wantErr bool
	}{
		{name: "absent uses default", value: nil, want: 5000},
		{name: "empty string uses default", value: "", want: 5000},
		{name: "float", value: float64(250), want: 250},
		{name: "string", value: "750", want: 750},
		{name: "negative float", value: float64(-1), want: -1},
		{name: "float beyond int", value: float64(1 << 63), wantErr: true},
		{name: "infinite float", value: math.Inf(1), wantErr: true},
		{name: "NaN", value: math.NaN(), wantErr: true},
		{name: "string beyond int", value: "9223372036854775808", wantErr: true},
		{name: "string not a number", value: "soon", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{}
			if tt.value != nil {
				params["timeout_ms"] = tt.value
			}
			got, err := s.parseIntParam(params, "timeout_ms", 5000)

			if tt.wantErr {
				requireInvalidIntArg(t, err, "timeout_ms")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMCPServer_ParseUint64Param(t *testing.T) {
	s := &MCPServer{}

	got, err := s.parseUint64Param(map[string]interface{}{"version": float64(3)}, "version")
	require.NoError(t, err)
	require.Equal(t, uint64(3), got)

	got, err = s.parseUint64Param(map[string]interface{}{}, "version")
	require.NoError(t, err)
	require.Zero(t, got)

	_, err = s.parseUint64Param(map[string]interface{}{"shard_id": "-1"}, "shard_id")
	requireInvalidIntArg(t, err, "shard_id")
}

// TestKMSKeyVersion_OverflowIsInternal proves a key version from the service
// that the response cannot hold is reported as a server fault, sanitized on
// the wire, never narrowed into a wrong version.
func TestKMSKeyVersion_OverflowIsInternal(t *testing.T) {
	v, err := kmsKeyVersion(4)
	require.NoError(t, err)
	require.Equal(t, 4, v)

	resp, err := svcMetadataToResponse(&service.KeyMetadata{KeyID: "k1", Version: math.MaxUint64})
	require.Nil(t, resp)

	var qbErr *qberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, qberrors.ErrInternal, qbErr.Code)
	var overflow *conv.ConversionOverflowError
	require.ErrorAs(t, err, &overflow)

	result := ToolErrorResult(nil, "quicbaas_kms_get_key", err)
	require.True(t, result.IsError)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Equal(t, qberrors.ServerFailureMessage, text.Text)
}
