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
			name: "both empty",
			req: &Request{
				Opcode:  OpSchedulerGetStatus,
				Token:   "",
				Payload: nil,
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

func TestWriteRequestNil(t *testing.T) {
	buf := &bytes.Buffer{}
	err := WriteRequest(buf, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteRequestPayloadTooLarge(t *testing.T) {
	buf := &bytes.Buffer{}
	req := &Request{
		Opcode:  OpSchedulerCreateJob,
		Token:   "token",
		Payload: make([]byte, MaxPayloadSize+1),
	}

	err := WriteRequest(buf, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteRequestTokenTooLarge(t *testing.T) {
	buf := &bytes.Buffer{}
	req := &Request{
		Opcode:  OpSchedulerCreateJob,
		Token:   strings.Repeat("a", int(MaxTokenSize)+1),
		Payload: []byte("{}"),
	}

	err := WriteRequest(buf, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestReadRequestReadError(t *testing.T) {
	// Test empty reader
	_, err := ReadRequest(&bytes.Buffer{})
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestReadRequestTokenAtWireCeilingTruncated(t *testing.T) {
	buf := &bytes.Buffer{}
	// Write opcode
	buf.WriteByte(OpSchedulerGetJob)
	// Token length prefix at the 2-byte wire ceiling (65535, which equals
	// MaxTokenSize) with no token body must fail the read.
	buf.WriteByte(0xFF)
	buf.WriteByte(0xFF)

	_, err := ReadRequest(buf)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestReadRequestTokenAboveOld8KBLimit(t *testing.T) {
	// Regression: RFC 9068 at+jwt tokens carrying qrdb-resolved RBAC roles +
	// scp claims legitimately exceed the old 8KB cap (observed: 11,060 bytes).
	req := &Request{
		Opcode: OpSchedulerGetJob,
		Token:  strings.Repeat("t", 11060),
	}
	buf := &bytes.Buffer{}
	require.NoError(t, WriteRequest(buf, req))

	decoded, err := ReadRequest(buf)
	require.NoError(t, err)
	assert.Equal(t, req.Token, decoded.Token)
}

func TestReadRequestPayloadSizeExceeded(t *testing.T) {
	buf := &bytes.Buffer{}
	// Write opcode
	buf.WriteByte(OpSchedulerGetJob)
	// Write token length (0)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	// Write payload length that exceeds max (> 16MB)
	buf.WriteByte(0x02) // 33MB
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)

	_, err := ReadRequest(buf)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
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

func TestWriteResponseNil(t *testing.T) {
	buf := &bytes.Buffer{}
	err := WriteResponse(buf, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteResponsePayloadTooLarge(t *testing.T) {
	buf := &bytes.Buffer{}
	resp := &Response{
		Status:  StatusOK,
		Payload: make([]byte, MaxPayloadSize+1),
	}

	err := WriteResponse(buf, resp)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestReadResponseReadError(t *testing.T) {
	// Test empty reader
	_, err := ReadResponse(&bytes.Buffer{})
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestReadResponsePayloadSizeExceeded(t *testing.T) {
	buf := &bytes.Buffer{}
	// Write status
	buf.WriteByte(StatusOK)
	// Write payload length that exceeds max (> 16MB)
	buf.WriteByte(0x02) // 33MB
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)

	_, err := ReadResponse(buf)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
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

			var qbErr *dberrors.QBError
			require.True(t, dberrors.As(err, &qbErr))
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
}

func TestErrorToStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected byte
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: StatusOK,
		},
		{
			name: "not found",
			err: &dberrors.QBError{
				Code: dberrors.ErrNotFound,
			},
			expected: StatusNotFound,
		},
		{
			name: "unauthorized",
			err: &dberrors.QBError{
				Code: dberrors.ErrUnauthorized,
			},
			expected: StatusUnauthorized,
		},
		{
			name: "invalid input",
			err: &dberrors.QBError{
				Code: dberrors.ErrInvalidInput,
			},
			expected: StatusInvalidArgument,
		},
		{
			name: "already exists",
			err: &dberrors.QBError{
				Code: dberrors.ErrAlreadyExists,
			},
			expected: StatusAlreadyExists,
		},
		{
			name: "timeout",
			err: &dberrors.QBError{
				Code: dberrors.ErrTimeout,
			},
			expected: StatusTimeout,
		},
		{
			name: "forbidden",
			err: &dberrors.QBError{
				Code: dberrors.ErrForbidden,
			},
			expected: StatusForbidden,
		},
		{
			name: "resource exhausted",
			err: &dberrors.QBError{
				Code: dberrors.ErrResourceExhausted,
			},
			expected: StatusResourceExhausted,
		},
		{
			name: "unavailable",
			err: &dberrors.QBError{
				Code: dberrors.ErrUnavailable,
			},
			expected: StatusUnavailable,
		},
		{
			name: "internal error",
			err: &dberrors.QBError{
				Code: dberrors.ErrInternal,
			},
			expected: StatusInternalError,
		},
		{
			name:     "non-qb error",
			err:      io.EOF,
			expected: StatusInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ErrorToStatus(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStatusToError(t *testing.T) {
	tests := []struct {
		name         string
		status       byte
		message      string
		expectedCode dberrors.ErrorCode
	}{
		{
			name:         "OK returns nil",
			status:       StatusOK,
			message:      "success",
			expectedCode: 0, // nil error
		},
		{
			name:         "not found",
			status:       StatusNotFound,
			message:      "job not found",
			expectedCode: dberrors.ErrNotFound,
		},
		{
			name:         "unauthorized",
			status:       StatusUnauthorized,
			message:      "unauthorized",
			expectedCode: dberrors.ErrUnauthorized,
		},
		{
			name:         "invalid argument",
			status:       StatusInvalidArgument,
			message:      "invalid input",
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "invalid request",
			status:       StatusInvalidRequest,
			message:      "invalid request",
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "already exists",
			status:       StatusAlreadyExists,
			message:      "job exists",
			expectedCode: dberrors.ErrAlreadyExists,
		},
		{
			name:         "timeout",
			status:       StatusTimeout,
			message:      "timed out",
			expectedCode: dberrors.ErrTimeout,
		},
		{
			name:         "permission denied",
			status:       StatusPermissionDenied,
			message:      "permission denied",
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "forbidden",
			status:       StatusForbidden,
			message:      "forbidden",
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "resource exhausted",
			status:       StatusResourceExhausted,
			message:      "quota exceeded",
			expectedCode: dberrors.ErrResourceExhausted,
		},
		{
			name:         "unavailable",
			status:       StatusUnavailable,
			message:      "service unavailable",
			expectedCode: dberrors.ErrUnavailable,
		},
		{
			name:         "internal error",
			status:       StatusInternalError,
			message:      "internal error",
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "unknown status",
			status:       0xFE,
			message:      "unknown error",
			expectedCode: dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := StatusToError(tt.status, tt.message)

			if tt.status == StatusOK {
				assert.Nil(t, err)
				return
			}

			require.NotNil(t, err)
			var qbErr *dberrors.QBError
			require.True(t, dberrors.As(err, &qbErr))
			assert.Equal(t, tt.expectedCode, qbErr.Code)
		})
	}
}

// errorWriter is a writer that always returns an error.
type errorWriter struct{}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}

func TestWriteRequestWriteErrors(t *testing.T) {
	w := &errorWriter{}

	req := &Request{
		Opcode:  OpSchedulerGetJob,
		Token:   "token",
		Payload: []byte(`{}`),
	}

	err := WriteRequest(w, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestWriteResponseWriteErrors(t *testing.T) {
	w := &errorWriter{}

	resp := &Response{
		Status:  StatusOK,
		Payload: []byte(`{}`),
	}

	err := WriteResponse(w, resp)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

// partialReader returns data only up to a specified point.
type partialReader struct {
	data   []byte
	offset int
	limit  int
}

func (r *partialReader) Read(p []byte) (n int, err error) {
	if r.offset >= r.limit || r.offset >= len(r.data) {
		return 0, io.EOF
	}
	remaining := r.limit - r.offset
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > len(r.data)-r.offset {
		remaining = len(r.data) - r.offset
	}
	n = copy(p, r.data[r.offset:r.offset+remaining])
	r.offset += n
	if r.offset >= r.limit {
		return n, io.EOF
	}
	return n, nil
}

func TestReadRequestPartialRead(t *testing.T) {
	// Write a valid request
	buf := &bytes.Buffer{}
	req := &Request{
		Opcode:  OpSchedulerGetJob,
		Token:   "token",
		Payload: []byte(`{"job_id": 123}`),
	}
	err := WriteRequest(buf, req)
	require.NoError(t, err)

	// Read with partial data (cut off in the middle of token)
	reader := &partialReader{
		data:  buf.Bytes(),
		limit: 5, // Only read opcode and part of token length
	}

	_, err = ReadRequest(reader)
	require.Error(t, err)
}

func TestReadResponsePartialRead(t *testing.T) {
	// Write a valid response
	buf := &bytes.Buffer{}
	resp := &Response{
		Status:  StatusOK,
		Payload: []byte(`{"job_id": 123}`),
	}
	err := WriteResponse(buf, resp)
	require.NoError(t, err)

	// Read with partial data
	reader := &partialReader{
		data:  buf.Bytes(),
		limit: 3, // Only read status and part of payload length
	}

	_, err = ReadResponse(reader)
	require.Error(t, err)
}
