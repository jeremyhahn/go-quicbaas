//go:build unix

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

package scheduler

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestOpcodeToString(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		expected string
	}{
		{name: "CreateJob", opcode: OpSchedulerCreateJob, expected: "CreateJob"},
		{name: "GetJob", opcode: OpSchedulerGetJob, expected: "GetJob"},
		{name: "UpdateJob", opcode: OpSchedulerUpdateJob, expected: "UpdateJob"},
		{name: "DeleteJob", opcode: OpSchedulerDeleteJob, expected: "DeleteJob"},
		{name: "ListJobs", opcode: OpSchedulerListJobs, expected: "ListJobs"},
		{name: "TriggerJob", opcode: OpSchedulerTriggerJob, expected: "TriggerJob"},
		{name: "PauseJob", opcode: OpSchedulerPauseJob, expected: "PauseJob"},
		{name: "ResumeJob", opcode: OpSchedulerResumeJob, expected: "ResumeJob"},
		{name: "CancelJob", opcode: OpSchedulerCancelJob, expected: "CancelJob"},
		{name: "GetJobHistory", opcode: OpSchedulerGetJobHistory, expected: "GetJobHistory"},
		{name: "GetStatus", opcode: OpSchedulerGetStatus, expected: "GetStatus"},
		{name: "Unknown", opcode: 0xFF, expected: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestWireConstants pins the opcode, status and version bytes a peer puts on
// the wire; changing one breaks every client built against the old value.
func TestWireConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      byte
		expected byte
	}{
		{name: "ProtocolVersion", got: ProtocolVersion, expected: 0x01},
		{name: "OpSchedulerCreateJob", got: OpSchedulerCreateJob, expected: 0xB0},
		{name: "OpSchedulerGetJob", got: OpSchedulerGetJob, expected: 0xB1},
		{name: "OpSchedulerUpdateJob", got: OpSchedulerUpdateJob, expected: 0xB2},
		{name: "OpSchedulerDeleteJob", got: OpSchedulerDeleteJob, expected: 0xB3},
		{name: "OpSchedulerListJobs", got: OpSchedulerListJobs, expected: 0xB4},
		{name: "OpSchedulerTriggerJob", got: OpSchedulerTriggerJob, expected: 0xB5},
		{name: "OpSchedulerPauseJob", got: OpSchedulerPauseJob, expected: 0xB6},
		{name: "OpSchedulerResumeJob", got: OpSchedulerResumeJob, expected: 0xB7},
		{name: "OpSchedulerCancelJob", got: OpSchedulerCancelJob, expected: 0xB8},
		{name: "OpSchedulerGetJobHistory", got: OpSchedulerGetJobHistory, expected: 0xB9},
		{name: "OpSchedulerGetStatus", got: OpSchedulerGetStatus, expected: 0xBA},
		{name: "StatusOK", got: StatusOK, expected: 0x00},
		{name: "StatusInvalidRequest", got: StatusInvalidRequest, expected: 0x01},
		{name: "StatusNotFound", got: StatusNotFound, expected: 0x02},
		{name: "StatusUnauthorized", got: StatusUnauthorized, expected: 0x03},
		{name: "StatusInternalError", got: StatusInternalError, expected: 0x04},
		{name: "StatusTimeout", got: StatusTimeout, expected: 0x05},
		{name: "StatusAlreadyExists", got: StatusAlreadyExists, expected: 0x06},
		{name: "StatusPermissionDenied", got: StatusPermissionDenied, expected: 0x07},
		{name: "StatusResourceExhausted", got: StatusResourceExhausted, expected: 0x08},
		{name: "StatusInvalidArgument", got: StatusInvalidArgument, expected: 0x09},
		{name: "StatusUnavailable", got: StatusUnavailable, expected: 0x0A},
		{name: "StatusForbidden", got: StatusForbidden, expected: 0x0B},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.got)
		})
	}
}

func TestWriteReadRequest(t *testing.T) {
	tests := []struct {
		name string
		req  *Request
	}{
		{
			name: "basic request",
			req: &Request{
				Opcode:  OpSchedulerGetJob,
				Token:   "test-token",
				Payload: []byte(`{"job_id": 123}`),
			},
		},
		{
			name: "empty payload",
			req: &Request{
				Opcode:  OpSchedulerGetStatus,
				Token:   "token",
				Payload: nil,
			},
		},
		{
			name: "empty token",
			req: &Request{
				Opcode:  OpSchedulerListJobs,
				Token:   "",
				Payload: []byte(`{"limit": 10}`),
			},
		},
		{
			// Regression: RFC 9068 at+jwt tokens carrying qrdb-resolved RBAC
			// roles + scp claims legitimately exceed the old 8KB cap
			// (observed: 11,060 bytes).
			name: "token above old 8KB limit",
			req: &Request{
				Opcode: OpSchedulerGetJob,
				Token:  strings.Repeat("t", 11060),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}

			err := WriteRequest(buf, tt.req)
			require.NoError(t, err)

			readReq, err := ReadRequest(buf)
			require.NoError(t, err)

			assert.Equal(t, tt.req.Opcode, readReq.Opcode)
			assert.Equal(t, tt.req.Token, readReq.Token)
			assert.Equal(t, tt.req.Payload, readReq.Payload)
		})
	}
}

// assertErrorCode checks that err is a QBError carrying the wanted code.
func assertErrorCode(t *testing.T, err error, want dberrors.ErrorCode) {
	t.Helper()
	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr), "error %v is not a QBError", err)
	assert.Equal(t, want, qbErr.Code)
}

func TestRequestErrors(t *testing.T) {
	readRequest := func(wire ...byte) func() error {
		return func() error {
			_, err := ReadRequest(bytes.NewReader(wire))
			return err
		}
	}

	tests := []struct {
		name     string
		run      func() error
		wantCode dberrors.ErrorCode
	}{
		{
			name:     "write nil request",
			run:      func() error { return WriteRequest(&bytes.Buffer{}, nil) },
			wantCode: dberrors.ErrInvalidInput,
		},
		{
			name: "write payload over MaxPayloadSize",
			run: func() error {
				return WriteRequest(&bytes.Buffer{}, &Request{
					Opcode:  OpSchedulerCreateJob,
					Token:   "token",
					Payload: make([]byte, MaxPayloadSize+1),
				})
			},
			wantCode: dberrors.ErrInvalidInput,
		},
		{
			name: "write token over MaxTokenSize",
			run: func() error {
				return WriteRequest(&bytes.Buffer{}, &Request{
					Opcode:  OpSchedulerCreateJob,
					Token:   strings.Repeat("a", int(MaxTokenSize)+1),
					Payload: []byte("{}"),
				})
			},
			wantCode: dberrors.ErrInvalidInput,
		},
		{
			name: "write to failing writer",
			run: func() error {
				return WriteRequest(&errorWriter{}, &Request{
					Opcode:  OpSchedulerGetJob,
					Token:   "token",
					Payload: []byte(`{}`),
				})
			},
			wantCode: dberrors.ErrInternal,
		},
		{
			name:     "read empty stream",
			run:      readRequest(),
			wantCode: dberrors.ErrInternal,
		},
		{
			// A token length prefix at the 2-byte wire ceiling (65535, which
			// equals MaxTokenSize) with no token body must fail the read.
			name:     "read token length at wire ceiling with no token body",
			run:      readRequest(OpSchedulerGetJob, 0xFF, 0xFF),
			wantCode: dberrors.ErrInternal,
		},
		{
			// Payload length 0x02000000 (32MB) exceeds MaxPayloadSize (16MB).
			name:     "read payload length over MaxPayloadSize",
			run:      readRequest(OpSchedulerGetJob, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00),
			wantCode: dberrors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrorCode(t, tt.run(), tt.wantCode)
		})
	}
}

func TestWriteReadResponse(t *testing.T) {
	tests := []struct {
		name string
		resp *Response
	}{
		{
			name: "success response",
			resp: &Response{
				Status:  StatusOK,
				Payload: []byte(`{"job_id": 123}`),
			},
		},
		{
			name: "error response",
			resp: &Response{
				Status:  StatusNotFound,
				Payload: []byte(`{"error": "job not found"}`),
			},
		},
		{
			name: "empty payload",
			resp: &Response{
				Status:  StatusOK,
				Payload: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}

			err := WriteResponse(buf, tt.resp)
			require.NoError(t, err)

			readResp, err := ReadResponse(buf)
			require.NoError(t, err)

			assert.Equal(t, tt.resp.Status, readResp.Status)
			assert.Equal(t, tt.resp.Payload, readResp.Payload)
		})
	}
}

func TestResponseErrors(t *testing.T) {
	readResponse := func(wire ...byte) func() error {
		return func() error {
			_, err := ReadResponse(bytes.NewReader(wire))
			return err
		}
	}

	tests := []struct {
		name     string
		run      func() error
		wantCode dberrors.ErrorCode
	}{
		{
			name:     "write nil response",
			run:      func() error { return WriteResponse(&bytes.Buffer{}, nil) },
			wantCode: dberrors.ErrInvalidInput,
		},
		{
			name: "write payload over MaxPayloadSize",
			run: func() error {
				return WriteResponse(&bytes.Buffer{}, &Response{
					Status:  StatusOK,
					Payload: make([]byte, MaxPayloadSize+1),
				})
			},
			wantCode: dberrors.ErrInvalidInput,
		},
		{
			name: "write to failing writer",
			run: func() error {
				return WriteResponse(&errorWriter{}, &Response{
					Status:  StatusOK,
					Payload: []byte(`{}`),
				})
			},
			wantCode: dberrors.ErrInternal,
		},
		{
			name:     "read empty stream",
			run:      readResponse(),
			wantCode: dberrors.ErrInternal,
		},
		{
			name:     "read truncated payload length",
			run:      readResponse(StatusOK, 0x00, 0x00),
			wantCode: dberrors.ErrInternal,
		},
		{
			// Payload length 0x02000000 (32MB) exceeds MaxPayloadSize (16MB).
			name:     "read payload length over MaxPayloadSize",
			run:      readResponse(StatusOK, 0x02, 0x00, 0x00, 0x00),
			wantCode: dberrors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrorCode(t, tt.run(), tt.wantCode)
		})
	}
}

func TestEncodeDecodePayload(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name string
		// run calls EncodePayload or DecodePayload and checks any output.
		run func(t *testing.T) error
		// wantCode is the error code expected, or 0 for success.
		wantCode dberrors.ErrorCode
	}{
		{
			name: "round trip",
			run: func(t *testing.T) error {
				original := testStruct{Name: "test", Value: 123}
				encoded, err := EncodePayload(&original)
				require.NoError(t, err)

				var decoded testStruct
				require.NoError(t, DecodePayload(encoded, &decoded))
				assert.Equal(t, original, decoded)
				return nil
			},
		},
		{
			name: "encode nil entity yields no payload",
			run: func(t *testing.T) error {
				encoded, err := EncodePayload(nil)
				assert.Nil(t, encoded)
				return err
			},
		},
		{
			name: "decode nil data leaves entity untouched",
			run: func(t *testing.T) error {
				decoded := testStruct{Name: "kept"}
				err := DecodePayload(nil, &decoded)
				assert.Equal(t, "kept", decoded.Name)
				return err
			},
		},
		{
			name: "decode empty data with nil entity is a no-op",
			run: func(t *testing.T) error {
				return DecodePayload([]byte{}, nil)
			},
		},
		{
			name: "encode unmarshalable entity",
			run: func(t *testing.T) error {
				_, err := EncodePayload(func() {})
				return err
			},
			wantCode: dberrors.ErrInternal,
		},
		{
			name: "decode into nil entity",
			run: func(t *testing.T) error {
				return DecodePayload([]byte(`{"test": "value"}`), nil)
			},
			wantCode: dberrors.ErrInvalidInput,
		},
		{
			name: "decode invalid JSON",
			run: func(t *testing.T) error {
				var decoded testStruct
				return DecodePayload([]byte("invalid json"), &decoded)
			},
			wantCode: dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			if tt.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			assertErrorCode(t, err, tt.wantCode)
		})
	}
}

func TestErrorStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		code   dberrors.ErrorCode
		status byte
		// toStatus and toError name the directions the pair holds in; the
		// status set is wider than the error set, so some pairs are one-way.
		toStatus bool
		toError  bool
	}{
		{name: "not found", code: dberrors.ErrNotFound, status: StatusNotFound, toStatus: true, toError: true},
		{name: "unauthorized", code: dberrors.ErrUnauthorized, status: StatusUnauthorized, toStatus: true, toError: true},
		{name: "invalid argument", code: dberrors.ErrInvalidInput, status: StatusInvalidArgument, toStatus: true, toError: true},
		{name: "already exists", code: dberrors.ErrAlreadyExists, status: StatusAlreadyExists, toStatus: true, toError: true},
		{name: "timeout", code: dberrors.ErrTimeout, status: StatusTimeout, toStatus: true, toError: true},
		{name: "forbidden", code: dberrors.ErrForbidden, status: StatusForbidden, toStatus: true, toError: true},
		{name: "resource exhausted", code: dberrors.ErrResourceExhausted, status: StatusResourceExhausted, toStatus: true, toError: true},
		{name: "unavailable", code: dberrors.ErrUnavailable, status: StatusUnavailable, toStatus: true, toError: true},
		{name: "internal error", code: dberrors.ErrInternal, status: StatusInternalError, toStatus: true, toError: true},
		{name: "invalid request", code: dberrors.ErrInvalidInput, status: StatusInvalidRequest, toError: true},
		{name: "permission denied", code: dberrors.ErrForbidden, status: StatusPermissionDenied, toError: true},
		{name: "unknown status", code: dberrors.ErrInternal, status: 0xFE, toError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.toStatus {
				assert.Equal(t, tt.status, ErrorToStatus(&dberrors.QBError{Code: tt.code}))
			}
			if tt.toError {
				assertErrorCode(t, StatusToError(tt.status, tt.name), tt.code)
			}
		})
	}

	t.Run("nil error and StatusOK", func(t *testing.T) {
		assert.Equal(t, StatusOK, ErrorToStatus(nil))
		assert.NoError(t, StatusToError(StatusOK, "success"))
	})

	t.Run("non-QBError maps to internal error", func(t *testing.T) {
		assert.Equal(t, StatusInternalError, ErrorToStatus(io.EOF))
	})
}

// errorWriter is a writer that always returns an error.
type errorWriter struct{}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}
