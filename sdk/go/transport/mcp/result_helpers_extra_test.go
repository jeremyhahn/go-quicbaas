// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// result_helpers_extra_test.go covers the shared MCP result-decoding helpers
// and the JSON-RPC-to-SDK error-code mapping across their numeric/json.Number
// and edge-case branches.

package mcp

import (
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestGetResultInt64Variants(t *testing.T) {
	if getResultInt64(map[string]interface{}{"k": float64(5)}, "k") != 5 {
		t.Error("float64 case")
	}
	if getResultInt64(map[string]interface{}{"k": int64(7)}, "k") != 7 {
		t.Error("int64 case")
	}
	if getResultInt64(map[string]interface{}{"k": json.Number("9")}, "k") != 9 {
		t.Error("json.Number case")
	}
	if getResultInt64(map[string]interface{}{"k": json.Number("nan")}, "k") != 0 {
		t.Error("invalid json.Number should yield 0")
	}
	if getResultInt64(map[string]interface{}{"k": "x"}, "k") != 0 {
		t.Error("string should yield 0")
	}
}

func TestGetResultUint64Variants(t *testing.T) {
	if getResultUint64(map[string]interface{}{"k": float64(5)}, "k") != 5 {
		t.Error("float64 case")
	}
	if getResultUint64(map[string]interface{}{"k": float64(-1)}, "k") != 0 {
		t.Error("negative float64 should clamp to 0")
	}
	if getResultUint64(map[string]interface{}{"k": uint64(8)}, "k") != 8 {
		t.Error("uint64 case")
	}
	if getResultUint64(map[string]interface{}{"k": int64(3)}, "k") != 3 {
		t.Error("int64 case")
	}
	if getResultUint64(map[string]interface{}{"k": int64(-2)}, "k") != 0 {
		t.Error("negative int64 should clamp to 0")
	}
	if getResultUint64(map[string]interface{}{"k": json.Number("11")}, "k") != 11 {
		t.Error("json.Number case")
	}
	if getResultUint64(map[string]interface{}{"k": json.Number("-1")}, "k") != 0 {
		t.Error("negative json.Number should yield 0")
	}
	if getResultUint64(map[string]interface{}{"k": "x"}, "k") != 0 {
		t.Error("string should yield 0")
	}
}

func TestGetResultFloat64AndTime(t *testing.T) {
	if getResultFloat64(map[string]interface{}{"k": 1.5}, "k") != 1.5 {
		t.Error("float64 case")
	}
	if getResultFloat64(map[string]interface{}{"k": "x"}, "k") != 0 {
		t.Error("non-float should yield 0")
	}

	if getResultTime(map[string]interface{}{"k": "2026-05-29T00:00:00Z"}, "k").IsZero() {
		t.Error("RFC3339 string should parse")
	}
	unix := getResultTime(map[string]interface{}{"k": float64(1_700_000_000)}, "k")
	if unix.IsZero() {
		t.Error("unix-seconds float should parse")
	}
	if !getResultTime(map[string]interface{}{"k": true}, "k").IsZero() {
		t.Error("bool should yield zero time")
	}
}

func TestTrimServiceNameAndParseDuration(t *testing.T) {
	if trimServiceName("quicbaas.Messaging") != "Messaging" {
		t.Error("dotted name trim failed")
	}
	if trimServiceName("Plain") != "Plain" {
		t.Error("plain name should be unchanged")
	}
	if d, err := parseDurationLoose("1h30m"); err != nil || d.Minutes() != 90 {
		t.Errorf("parseDurationLoose = %v, %v", d, err)
	}
	if _, err := parseDurationLoose("garbage"); err == nil {
		t.Error("invalid duration must error")
	}
}

func TestAsResultMapNilAndScalar(t *testing.T) {
	m, err := asResultMap(nil, "op")
	if err != nil || m == nil || len(m) != 0 {
		t.Errorf("nil result = %v, %v", m, err)
	}
	if _, err := asResultMap("scalar", "op"); err == nil {
		t.Error("scalar should error")
	}
}

func TestPutJSONArrayArg(t *testing.T) {
	args := map[string]interface{}{}
	if err := putJSONArrayArg(args, "k", nil); err != nil {
		t.Fatalf("nil slice: %v", err)
	}
	if _, ok := args["k"]; ok {
		t.Error("nil slice should add nothing")
	}
	if err := putJSONArrayArg(args, "k", []string{"a", "b"}); err != nil {
		t.Fatalf("slice: %v", err)
	}
	if args["k"] != `["a","b"]` {
		t.Errorf("k = %v", args["k"])
	}
}

func TestMapToErrorCode(t *testing.T) {
	cases := map[int]errors.ErrorCode{
		JSONRPCParseError:         errors.CodeInvalidInput,
		JSONRPCInvalidRequest:     errors.CodeInvalidInput,
		JSONRPCMethodNotFound:     errors.CodeNotFound,
		JSONRPCInvalidParams:      errors.CodeInvalidInput,
		JSONRPCInternalError:      errors.CodeInternal,
		MCPAuthenticationRequired: errors.CodeUnauthorized,
		MCPInvalidToken:           errors.CodeUnauthorized,
		MCPPermissionDenied:       errors.CodeForbidden,
		MCPResourceNotFound:       errors.CodeNotFound,
		MCPResourceConflict:       errors.CodeConflict,
		MCPServiceUnavailable:     errors.CodeUnavailable,
		MCPRequestTimeout:         errors.CodeTimeout,
		MCPResourceExhausted:      errors.CodeResourceExhausted,
		MCPPreconditionFailed:     errors.CodePreconditionFailed,
		MCPOperationNotAllowed:    errors.CodeForbidden,
		MCPOperationCanceled:      errors.CodeCanceled,
		MCPInvalidInput:           errors.CodeInvalidInput,
		MCPAlreadyExists:          errors.CodeAlreadyExists,
		MCPUnimplemented:          errors.CodeUnimplemented,
	}
	for code, want := range cases {
		e := &JSONRPCError{Code: code}
		if got := e.mapToErrorCode(); got != want {
			t.Errorf("mapToErrorCode(%d) = %v, want %v", code, got, want)
		}
	}

	// Server-defined range maps to internal; everything else to unknown.
	if (&JSONRPCError{Code: -32050}).mapToErrorCode() != errors.CodeInternal {
		t.Error("server range should map to internal")
	}
	if (&JSONRPCError{Code: 12345}).mapToErrorCode() != errors.CodeUnknown {
		t.Error("out-of-range code should map to unknown")
	}
}

func TestJSONRPCErrorToSDKError(t *testing.T) {
	e := &JSONRPCError{Code: MCPResourceNotFound, Message: "missing", Data: "detail"}
	sdkErr := e.ToSDKError("op")
	if sdkErr.Code != errors.CodeNotFound {
		t.Errorf("code = %v", sdkErr.Code)
	}
	if sdkErr.Message != "missing: detail" {
		t.Errorf("message = %q", sdkErr.Message)
	}

	// No data: message is unchanged.
	e2 := &JSONRPCError{Code: MCPInvalidInput, Message: "bad"}
	if e2.ToSDKError("op").Message != "bad" {
		t.Error("message should be unchanged with no data")
	}
}
