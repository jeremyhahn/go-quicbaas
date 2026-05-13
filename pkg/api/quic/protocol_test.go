//go:build quic

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

package quic

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Request/Response Write/Read Tests
// =============================================================================

func TestWriteRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *Request
		wantErr bool
		errType string
	}{
		{
			name: "valid Put request",
			req: &Request{
				Opcode:  OpPut,
				Payload: []byte("test payload"),
			},
			wantErr: false,
		},
		{
			name: "valid request with empty payload",
			req: &Request{
				Opcode:  OpGet,
				Payload: nil,
			},
			wantErr: false,
		},
		{
			name: "valid request with token",
			req: &Request{
				Opcode:  OpGet,
				Token:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test",
				Payload: []byte("test"),
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "payload exceeds maximum size",
			req: &Request{
				Opcode:  OpPut,
				Payload: make([]byte, MaxPayloadSize+1),
			},
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "maximum allowed payload size",
			req: &Request{
				Opcode:  OpPut,
				Payload: make([]byte, MaxPayloadSize),
			},
			wantErr: false,
		},
		{
			name: "all opcodes",
			req: &Request{
				Opcode:  OpBatchPut,
				Payload: []byte("batch data"),
			},
			wantErr: false,
		},
		{
			name: "token exceeds maximum size",
			req: &Request{
				Opcode: OpGet,
				Token:  string(make([]byte, MaxTokenSize+1)),
			},
			wantErr: true,
			errType: "ErrInvalidInput",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteRequest(buf, tt.req)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("WriteRequest() expected error but got nil")
				}
				if tt.errType != "" {
					var qbErr *qberrors.QBError
					if !errors.As(err, &qbErr) {
						t.Fatalf("expected QBError, got %T", err)
					}
					if tt.errType == "ErrInvalidInput" && qbErr.Code != qberrors.ErrInvalidInput {
						t.Errorf("expected error code ErrInvalidInput, got %v", qbErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("WriteRequest() unexpected error: %v", err)
			}

			// Verify buffer contains expected data
			if buf.Len() == 0 {
				t.Error("WriteRequest() wrote nothing to buffer")
			}

			// Verify opcode
			opcode := buf.Bytes()[0]
			if opcode != tt.req.Opcode {
				t.Errorf("expected opcode %d, got %d", tt.req.Opcode, opcode)
			}
		})
	}
}

func TestReadRequest(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() io.Reader
		want    *Request
		wantErr bool
		errType string
	}{
		{
			name: "valid request with payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteRequest(buf, &Request{
					Opcode:  OpPut,
					Payload: []byte("test data"),
				})
				return buf
			},
			want: &Request{
				Opcode:  OpPut,
				Payload: []byte("test data"),
			},
			wantErr: false,
		},
		{
			name: "valid request with empty payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteRequest(buf, &Request{
					Opcode:  OpGet,
					Payload: nil,
				})
				return buf
			},
			want: &Request{
				Opcode:  OpGet,
				Payload: nil,
			},
			wantErr: false,
		},
		{
			name: "valid request with token",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteRequest(buf, &Request{
					Opcode:  OpGet,
					Token:   "test-token-12345",
					Payload: []byte("payload"),
				})
				return buf
			},
			want: &Request{
				Opcode:  OpGet,
				Token:   "test-token-12345",
				Payload: []byte("payload"),
			},
			wantErr: false,
		},
		{
			name: "valid request with token and empty payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteRequest(buf, &Request{
					Opcode: OpHealth,
					Token:  "auth-token",
				})
				return buf
			},
			want: &Request{
				Opcode: OpHealth,
				Token:  "auth-token",
			},
			wantErr: false,
		},
		{
			name: "EOF reading opcode",
			setup: func() io.Reader {
				return &bytes.Buffer{}
			},
			wantErr: true,
			errType: "EOF",
		},
		{
			name: "EOF reading token length",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(OpPut) // Write opcode only
				return buf
			},
			wantErr: true,
		},
		{
			name: "EOF reading payload length",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(OpPut)
				// Write token length (0)
				buf.WriteByte(0)
				buf.WriteByte(0)
				return buf
			},
			wantErr: true,
		},
		{
			name: "payload size exceeds maximum",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(OpPut)
				// Write token length (0)
				buf.WriteByte(0)
				buf.WriteByte(0)
				// Write length exceeding max
				lenBuf := make([]byte, 4)
				lenBuf[0] = 0xFF
				lenBuf[1] = 0xFF
				lenBuf[2] = 0xFF
				lenBuf[3] = 0xFF
				buf.Write(lenBuf)
				return buf
			},
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "token size exceeds maximum",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(OpPut)
				// Write token length exceeding max (>8KB)
				buf.WriteByte(0xFF)
				buf.WriteByte(0xFF)
				return buf
			},
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "EOF reading payload data",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(OpPut)
				// Write token length (0)
				buf.WriteByte(0)
				buf.WriteByte(0)
				// Write length indicating 100 bytes
				lenBuf := make([]byte, 4)
				lenBuf[3] = 100
				buf.Write(lenBuf)
				// But write only 10 bytes
				buf.Write(make([]byte, 10))
				return buf
			},
			wantErr: true,
		},
		{
			name: "EOF reading token data",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(OpPut)
				// Write token length indicating 50 bytes
				buf.WriteByte(0)
				buf.WriteByte(50)
				// But write only 10 bytes
				buf.Write(make([]byte, 10))
				return buf
			},
			wantErr: true,
		},
		{
			name: "maximum allowed payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteRequest(buf, &Request{
					Opcode:  OpBatchPut,
					Payload: make([]byte, MaxPayloadSize),
				})
				return buf
			},
			want: &Request{
				Opcode:  OpBatchPut,
				Payload: make([]byte, MaxPayloadSize),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.setup()
			got, err := ReadRequest(r)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadRequest() expected error but got nil")
				}
				if tt.errType == "ErrInvalidInput" {
					var qbErr *qberrors.QBError
					if errors.As(err, &qbErr) && qbErr.Code != qberrors.ErrInvalidInput {
						t.Errorf("expected error code ErrInvalidInput, got %v", qbErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadRequest() unexpected error: %v", err)
			}

			if got.Opcode != tt.want.Opcode {
				t.Errorf("Opcode = %d, want %d", got.Opcode, tt.want.Opcode)
			}

			if got.Token != tt.want.Token {
				t.Errorf("Token = %q, want %q", got.Token, tt.want.Token)
			}

			if !bytes.Equal(got.Payload, tt.want.Payload) {
				t.Errorf("Payload mismatch")
			}
		})
	}
}

func TestWriteResponse(t *testing.T) {
	tests := []struct {
		name    string
		resp    *Response
		wantErr bool
		errType string
	}{
		{
			name: "valid response with payload",
			resp: &Response{
				Status:  StatusOK,
				Payload: []byte("success"),
			},
			wantErr: false,
		},
		{
			name: "valid response with empty payload",
			resp: &Response{
				Status:  StatusNotFound,
				Payload: nil,
			},
			wantErr: false,
		},
		{
			name:    "nil response",
			resp:    nil,
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "payload exceeds maximum size",
			resp: &Response{
				Status:  StatusOK,
				Payload: make([]byte, MaxPayloadSize+1),
			},
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "all status codes",
			resp: &Response{
				Status:  StatusInternalError,
				Payload: []byte("error message"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteResponse(buf, tt.resp)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("WriteResponse() expected error but got nil")
				}
				if tt.errType == "ErrInvalidInput" {
					var qbErr *qberrors.QBError
					if errors.As(err, &qbErr) && qbErr.Code != qberrors.ErrInvalidInput {
						t.Errorf("expected error code ErrInvalidInput, got %v", qbErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("WriteResponse() unexpected error: %v", err)
			}

			// Verify status code
			status := buf.Bytes()[0]
			if status != tt.resp.Status {
				t.Errorf("expected status %d, got %d", tt.resp.Status, status)
			}
		})
	}
}

func TestReadResponse(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() io.Reader
		want    *Response
		wantErr bool
		errType string
	}{
		{
			name: "valid response with payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteResponse(buf, &Response{
					Status:  StatusOK,
					Payload: []byte("success"),
				})
				return buf
			},
			want: &Response{
				Status:  StatusOK,
				Payload: []byte("success"),
			},
			wantErr: false,
		},
		{
			name: "valid response with empty payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				_ = WriteResponse(buf, &Response{
					Status:  StatusNotFound,
					Payload: nil,
				})
				return buf
			},
			want: &Response{
				Status:  StatusNotFound,
				Payload: nil,
			},
			wantErr: false,
		},
		{
			name: "EOF reading status",
			setup: func() io.Reader {
				return &bytes.Buffer{}
			},
			wantErr: true,
		},
		{
			name: "EOF reading payload length",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(StatusOK)
				return buf
			},
			wantErr: true,
		},
		{
			name: "payload size exceeds maximum",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(StatusOK)
				// Write length exceeding max
				lenBuf := make([]byte, 4)
				lenBuf[0] = 0xFF
				lenBuf[1] = 0xFF
				lenBuf[2] = 0xFF
				lenBuf[3] = 0xFF
				buf.Write(lenBuf)
				return buf
			},
			wantErr: true,
			errType: "ErrInvalidInput",
		},
		{
			name: "EOF reading payload",
			setup: func() io.Reader {
				buf := &bytes.Buffer{}
				buf.WriteByte(StatusOK)
				lenBuf := make([]byte, 4)
				lenBuf[3] = 100
				buf.Write(lenBuf)
				buf.Write(make([]byte, 10)) // Only 10 bytes instead of 100
				return buf
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.setup()
			got, err := ReadResponse(r)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadResponse() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadResponse() unexpected error: %v", err)
			}

			if got.Status != tt.want.Status {
				t.Errorf("Status = %d, want %d", got.Status, tt.want.Status)
			}

			if !bytes.Equal(got.Payload, tt.want.Payload) {
				t.Errorf("Payload mismatch")
			}
		})
	}
}

// =============================================================================
// Token Serialization Tests
// =============================================================================

func TestRequestTokenSerialization(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"short token", "abc123"},
		{"JWT-like token", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
		{"max length token", string(make([]byte, MaxTokenSize))},
		{"token with special chars", "token-with_special.chars!@#$%"},
		{"unicode token", "token-中文-test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &Request{
				Opcode:  OpGet,
				Token:   tt.token,
				Payload: []byte("test payload"),
			}

			buf := &bytes.Buffer{}
			if err := WriteRequest(buf, original); err != nil {
				t.Fatalf("WriteRequest() error: %v", err)
			}

			decoded, err := ReadRequest(buf)
			if err != nil {
				t.Fatalf("ReadRequest() error: %v", err)
			}

			if decoded.Token != original.Token {
				t.Errorf("Token = %q, want %q", decoded.Token, original.Token)
			}
			if decoded.Opcode != original.Opcode {
				t.Errorf("Opcode = %d, want %d", decoded.Opcode, original.Opcode)
			}
			if !bytes.Equal(decoded.Payload, original.Payload) {
				t.Errorf("Payload mismatch")
			}
		})
	}
}

func TestRequestTokenRoundTrip(t *testing.T) {
	// Test all combinations of token and payload presence
	testCases := []struct {
		name    string
		opcode  byte
		token   string
		payload []byte
	}{
		{"no token, no payload", OpHealth, "", nil},
		{"token only", OpGet, "auth-token", nil},
		{"payload only", OpPut, "", []byte("data")},
		{"token and payload", OpPut, "auth-token", []byte("data")},
		{"large token and payload", OpBatchPut, strings.Repeat("x", 1000), make([]byte, 10000)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := &Request{
				Opcode:  tc.opcode,
				Token:   tc.token,
				Payload: tc.payload,
			}

			buf := &bytes.Buffer{}
			if err := WriteRequest(buf, original); err != nil {
				t.Fatalf("WriteRequest() error: %v", err)
			}

			decoded, err := ReadRequest(buf)
			if err != nil {
				t.Fatalf("ReadRequest() error: %v", err)
			}

			if decoded.Opcode != original.Opcode {
				t.Errorf("Opcode = %d, want %d", decoded.Opcode, original.Opcode)
			}
			if decoded.Token != original.Token {
				t.Errorf("Token = %q, want %q", decoded.Token, original.Token)
			}
			if !bytes.Equal(decoded.Payload, original.Payload) {
				t.Errorf("Payload mismatch: got %d bytes, want %d bytes", len(decoded.Payload), len(original.Payload))
			}
		})
	}
}

// =============================================================================
// Payload Encoding/Decoding Tests
// =============================================================================

func TestEncodePayload(t *testing.T) {
	type testStruct struct {
		Field1 string
		Field2 int
		Field3 bool
	}

	tests := []struct {
		name    string
		entity  interface{}
		wantErr bool
		wantNil bool
	}{
		{
			name: "encode struct",
			entity: testStruct{
				Field1: "test",
				Field2: 42,
				Field3: true,
			},
			wantErr: false,
		},
		{
			name:    "encode string",
			entity:  "test string",
			wantErr: false,
		},
		{
			name:    "encode int",
			entity:  12345,
			wantErr: false,
		},
		{
			name:    "encode slice",
			entity:  []byte{1, 2, 3, 4, 5},
			wantErr: false,
		},
		{
			name:    "encode map",
			entity:  map[string]string{"key": "value"},
			wantErr: false,
		},
		{
			name:    "nil entity",
			entity:  nil,
			wantErr: false,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodePayload(tt.entity)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("EncodePayload() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("EncodePayload() unexpected error: %v", err)
			}

			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil payload for nil entity")
				}
				return
			}

			if len(got) == 0 {
				t.Error("EncodePayload() returned empty payload")
			}
		})
	}
}

func TestDecodePayload(t *testing.T) {
	type testStruct struct {
		Field1 string
		Field2 int
		Field3 bool
	}

	tests := []struct {
		name    string
		setup   func() ([]byte, interface{})
		wantErr bool
	}{
		{
			name: "decode struct",
			setup: func() ([]byte, interface{}) {
				original := testStruct{Field1: "test", Field2: 42, Field3: true}
				data, _ := EncodePayload(original)
				target := &testStruct{}
				return data, target
			},
			wantErr: false,
		},
		{
			name: "decode string",
			setup: func() ([]byte, interface{}) {
				data, _ := EncodePayload("test string")
				var target string
				return data, &target
			},
			wantErr: false,
		},
		{
			name: "empty data with non-nil entity",
			setup: func() ([]byte, interface{}) {
				target := &testStruct{}
				return []byte{}, target
			},
			wantErr: false,
		},
		{
			name: "nil entity",
			setup: func() ([]byte, interface{}) {
				data, _ := EncodePayload("test")
				return data, nil
			},
			wantErr: true,
		},
		{
			name: "malformed data",
			setup: func() ([]byte, interface{}) {
				target := &testStruct{}
				return []byte{0xFF, 0xFF, 0xFF}, target
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, entity := tt.setup()
			err := DecodePayload(data, entity)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodePayload() expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("DecodePayload() unexpected error: %v", err)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	type testStruct struct {
		Name  string
		Value int
		Tags  []string
	}

	original := testStruct{
		Name:  "test",
		Value: 123,
		Tags:  []string{"a", "b", "c"},
	}

	// Encode
	data, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("EncodePayload() error: %v", err)
	}

	// Decode
	var decoded testStruct
	err = DecodePayload(data, &decoded)
	if err != nil {
		t.Fatalf("DecodePayload() error: %v", err)
	}

	// Compare
	if decoded.Name != original.Name {
		t.Errorf("Name = %s, want %s", decoded.Name, original.Name)
	}
	if decoded.Value != original.Value {
		t.Errorf("Value = %d, want %d", decoded.Value, original.Value)
	}
	if len(decoded.Tags) != len(original.Tags) {
		t.Errorf("Tags length = %d, want %d", len(decoded.Tags), len(original.Tags))
	}
}

// =============================================================================
// Error Conversion Tests
// =============================================================================

func TestErrorToStatus(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus byte
	}{
		{
			name:       "nil error",
			err:        nil,
			wantStatus: StatusOK,
		},
		{
			name: "ErrNotFound",
			err: &qberrors.QBError{
				Code: qberrors.ErrNotFound,
			},
			wantStatus: StatusNotFound,
		},
		{
			name: "ErrUnauthorized",
			err: &qberrors.QBError{
				Code: qberrors.ErrUnauthorized,
			},
			wantStatus: StatusUnauthorized,
		},
		{
			name: "ErrInvalidInput",
			err: &qberrors.QBError{
				Code: qberrors.ErrInvalidInput,
			},
			wantStatus: StatusInvalidArgument,
		},
		{
			name: "ErrAlreadyExists",
			err: &qberrors.QBError{
				Code: qberrors.ErrAlreadyExists,
			},
			wantStatus: StatusAlreadyExists,
		},
		{
			name: "ErrTimeout",
			err: &qberrors.QBError{
				Code: qberrors.ErrTimeout,
			},
			wantStatus: StatusTimeout,
		},
		{
			name: "ErrForbidden",
			err: &qberrors.QBError{
				Code: qberrors.ErrForbidden,
			},
			wantStatus: StatusPermissionDenied,
		},
		{
			name: "ErrResourceExhausted",
			err: &qberrors.QBError{
				Code: qberrors.ErrResourceExhausted,
			},
			wantStatus: StatusResourceExhausted,
		},
		{
			name: "ErrUnavailable",
			err: &qberrors.QBError{
				Code: qberrors.ErrUnavailable,
			},
			wantStatus: StatusUnavailable,
		},
		{
			name: "unknown QBError code",
			err: &qberrors.QBError{
				Code: 9999,
			},
			wantStatus: StatusInternalError,
		},
		{
			name:       "non-QBError",
			err:        errors.New("generic error"),
			wantStatus: StatusInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorToStatus(tt.err)
			if got != tt.wantStatus {
				t.Errorf("ErrorToStatus() = %d, want %d", got, tt.wantStatus)
			}
		})
	}
}

func TestStatusToError(t *testing.T) {
	tests := []struct {
		name       string
		status     byte
		message    string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "StatusOK",
			status:  StatusOK,
			message: "success",
			wantErr: false,
		},
		{
			name:       "StatusNotFound",
			status:     StatusNotFound,
			message:    "not found",
			wantErr:    true,
			wantErrMsg: "not found",
		},
		{
			name:       "StatusUnauthorized",
			status:     StatusUnauthorized,
			message:    "unauthorized",
			wantErr:    true,
			wantErrMsg: "unauthorized",
		},
		{
			name:       "StatusInvalidArgument",
			status:     StatusInvalidArgument,
			message:    "invalid argument",
			wantErr:    true,
			wantErrMsg: "invalid argument",
		},
		{
			name:       "StatusInternalError",
			status:     StatusInternalError,
			message:    "internal error",
			wantErr:    true,
			wantErrMsg: "internal error",
		},
		{
			name:       "StatusPermissionDenied",
			status:     StatusPermissionDenied,
			message:    "forbidden",
			wantErr:    true,
			wantErrMsg: "forbidden",
		},
		{
			name:       "unknown status",
			status:     0xFF,
			message:    "unknown",
			wantErr:    true,
			wantErrMsg: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := StatusToError(tt.status, tt.message)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("StatusToError() expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error message = %q, want to contain %q", err.Error(), tt.wantErrMsg)
				}
			} else {
				if err != nil {
					t.Errorf("StatusToError() unexpected error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// Opcode String Conversion Tests
// =============================================================================

func TestOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode byte
		want   string
	}{
		// KV Operations
		{OpPut, "Put"},
		{OpGet, "Get"},
		{OpDelete, "Delete"},
		{OpList, "List"},

		// Batch Operations
		{OpBatchPut, "BatchPut"},
		{OpBatchGet, "BatchGet"},
		{OpBatchDelete, "BatchDelete"},

		// Transaction Operations
		{OpTransaction, "Transaction"},

		// Query Operations
		{OpQuery, "Query"},
		{OpScanIndex, "ScanIndex"},

		// Cluster Operations
		{OpClusterStatus, "ClusterStatus"},
		{OpGetLeader, "GetLeader"},
		{OpAddNode, "AddNode"},
		{OpRemoveNode, "RemoveNode"},
		{OpCreateShard, "CreateShard"},
		{OpRebalanceCluster, "RebalanceCluster"},
		{OpListNodes, "ListNodes"},
		{OpRebalanceStatus, "RebalanceStatus"},
		{OpRebalancePlan, "RebalancePlan"},
		{OpCancelMigration, "CancelMigration"},
		{OpNodeReplaceRequest, "NodeReplaceRequest"},

		// Backup Operations
		{OpCreateBackup, "CreateBackup"},
		{OpListBackups, "ListBackups"},
		{OpRestoreBackup, "RestoreBackup"},
		{OpDeleteBackup, "DeleteBackup"},
		{OpVerifyBackup, "VerifyBackup"},

		// Auth Operations
		{OpLogout, "Logout"},
		{OpRefreshToken, "RefreshToken"},
		{OpValidateToken, "ValidateToken"},

		// RBAC Operations
		{OpCreateUser, "CreateUser"},
		{OpDeleteUser, "DeleteUser"},
		{OpListUsers, "ListUsers"},
		{OpCreateRole, "CreateRole"},
		{OpDeleteRole, "DeleteRole"},
		{OpListRoles, "ListRoles"},
		{OpAssignRole, "AssignRole"},
		{OpRevokeRole, "RevokeRole"},

		// Cache Operations
		{OpGetCacheStats, "GetCacheStats"},
		{OpClearCache, "ClearCache"},
		{OpGetCacheConfig, "GetCacheConfig"},
		{OpUpdateCacheConfig, "UpdateCacheConfig"},

		// Health Operations
		{OpHealth, "Health"},
		{OpLiveness, "Liveness"},
		{OpReadiness, "Readiness"},

		// Time-Series Operations
		{OpAggregate, "Aggregate"},
		{OpTimeSeriesQuery, "TimeSeriesQuery"},
		{OpTSWritePoint, "TSWritePoint"},
		{OpTSWriteBatch, "TSWriteBatch"},
		{OpTSQuery, "TSQuery"},
		{OpTSDeleteSeries, "TSDeleteSeries"},

		// Operations Management
		{OpMigrateData, "MigrateData"},
		{OpGetMigrationProgress, "GetMigrationProgress"},
		{OpReplaceNode, "ReplaceNode"},
		{OpUpgradeNode, "UpgradeNode"},
		{OpGetUpgradeStatus, "GetUpgradeStatus"},

		// Seal Operations
		{OpSealStatus, "SealStatus"},
		{OpSealInit, "SealInit"},
		{OpUnseal, "Unseal"},
		{OpSeal, "Seal"},
		{OpSealRekey, "SealRekey"},

		// KMS Operations
		{OpKMSGenerateKey, "KMSGenerateKey"},
		{OpKMSImportKey, "KMSImportKey"},
		{OpKMSGetKey, "KMSGetKey"},
		{OpKMSListKeys, "KMSListKeys"},
		{OpKMSRotateKey, "KMSRotateKey"},
		{OpKMSDeleteKey, "KMSDeleteKey"},
		{OpKMSEncrypt, "KMSEncrypt"},
		{OpKMSDecrypt, "KMSDecrypt"},

		// Secrets Operations
		{OpSecretsStore, "SecretsStore"},
		{OpSecretsGet, "SecretsGet"},
		{OpSecretsList, "SecretsList"},
		{OpSecretsDelete, "SecretsDelete"},
		{OpSecretsListVersions, "SecretsListVersions"},
		{OpSecretsRollback, "SecretsRollback"},
		{OpSecretsRotate, "SecretsRotate"},

		// Crypto Operations
		{OpCryptoEncrypt, "CryptoEncrypt"},
		{OpCryptoDecrypt, "CryptoDecrypt"},
		{OpCryptoRewrap, "CryptoRewrap"},
		{OpCryptoSign, "CryptoSign"},
		{OpCryptoVerify, "CryptoVerify"},
		{OpCryptoHash, "CryptoHash"},
		{OpCryptoRandom, "CryptoRandom"},
		{OpCryptoDataKey, "CryptoDataKey"},
		// Note: All 256 opcodes are defined when full build tags are enabled,
		// so we cannot test the "Unknown" fallback case here.
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := OpcodeToString(tt.opcode)
			if got != tt.want {
				t.Errorf("OpcodeToString(%d) = %s, want %s", tt.opcode, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkWriteRequest(b *testing.B) {
	req := &Request{
		Opcode:  OpPut,
		Payload: make([]byte, 1024),
	}
	buf := &bytes.Buffer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = WriteRequest(buf, req)
	}
}

func BenchmarkWriteRequestWithToken(b *testing.B) {
	req := &Request{
		Opcode:  OpPut,
		Token:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		Payload: make([]byte, 1024),
	}
	buf := &bytes.Buffer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = WriteRequest(buf, req)
	}
}

func BenchmarkReadRequest(b *testing.B) {
	buf := &bytes.Buffer{}
	req := &Request{
		Opcode:  OpPut,
		Payload: make([]byte, 1024),
	}
	_ = WriteRequest(buf, req)
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, _ = ReadRequest(r)
	}
}

func BenchmarkReadRequestWithToken(b *testing.B) {
	buf := &bytes.Buffer{}
	req := &Request{
		Opcode:  OpPut,
		Token:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		Payload: make([]byte, 1024),
	}
	_ = WriteRequest(buf, req)
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, _ = ReadRequest(r)
	}
}

func BenchmarkEncodePayload(b *testing.B) {
	type testStruct struct {
		Name  string
		Value int
		Tags  []string
	}
	entity := testStruct{
		Name:  "benchmark",
		Value: 123456,
		Tags:  []string{"tag1", "tag2", "tag3"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodePayload(entity)
	}
}

func BenchmarkDecodePayload(b *testing.B) {
	type testStruct struct {
		Name  string
		Value int
		Tags  []string
	}
	entity := testStruct{
		Name:  "benchmark",
		Value: 123456,
		Tags:  []string{"tag1", "tag2", "tag3"},
	}
	data, _ := EncodePayload(entity)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var target testStruct
		_ = DecodePayload(data, &target)
	}
}
