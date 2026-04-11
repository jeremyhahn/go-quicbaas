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

package objectstorage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestWriteReadRequest(t *testing.T) {
	tests := []struct {
		name    string
		request *Request
	}{
		{
			name: "simple request",
			request: &Request{
				Opcode:  OpPutObject,
				Token:   "test-token",
				Payload: []byte(`{"key":"test"}`),
			},
		},
		{
			name: "request without token",
			request: &Request{
				Opcode:  OpGetObject,
				Token:   "",
				Payload: []byte(`{"key":"test"}`),
			},
		},
		{
			name: "request without payload",
			request: &Request{
				Opcode:  OpDeleteObject,
				Token:   "auth-token",
				Payload: nil,
			},
		},
		{
			name: "minimal request",
			request: &Request{
				Opcode:  OpHeadObject,
				Token:   "",
				Payload: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := WriteRequest(&buf, tt.request)
			require.NoError(t, err)

			readReq, err := ReadRequest(&buf)
			require.NoError(t, err)

			assert.Equal(t, tt.request.Opcode, readReq.Opcode)
			assert.Equal(t, tt.request.Token, readReq.Token)
			assert.Equal(t, tt.request.Payload, readReq.Payload)
		})
	}
}

func TestWriteRequestNilRequest(t *testing.T) {
	var buf bytes.Buffer
	err := WriteRequest(&buf, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteRequestPayloadTooLarge(t *testing.T) {
	var buf bytes.Buffer
	req := &Request{
		Opcode:  OpPutObject,
		Payload: make([]byte, MaxPayloadSize+1),
	}
	err := WriteRequest(&buf, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteRequestTokenTooLarge(t *testing.T) {
	var buf bytes.Buffer
	req := &Request{
		Opcode: OpPutObject,
		Token:  strings.Repeat("a", int(MaxTokenSize)+1),
	}
	err := WriteRequest(&buf, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestReadRequestTruncated(t *testing.T) {
	// Test empty reader
	_, err := ReadRequest(bytes.NewReader(nil))
	require.Error(t, err)

	// Test truncated token length
	buf := []byte{OpPutObject}
	_, err = ReadRequest(bytes.NewReader(buf))
	require.Error(t, err)

	// Test truncated payload length
	buf = []byte{OpPutObject, 0x00, 0x00} // opcode + token length (0)
	_, err = ReadRequest(bytes.NewReader(buf))
	require.Error(t, err)
}

func TestWriteReadResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
	}{
		{
			name: "success response",
			response: &Response{
				Status:  StatusOK,
				Payload: []byte(`{"result":"success"}`),
			},
		},
		{
			name: "error response",
			response: &Response{
				Status:  StatusNotFound,
				Payload: []byte(`{"error":"not found"}`),
			},
		},
		{
			name: "response without payload",
			response: &Response{
				Status:  StatusOK,
				Payload: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := WriteResponse(&buf, tt.response)
			require.NoError(t, err)

			readResp, err := ReadResponse(&buf)
			require.NoError(t, err)

			assert.Equal(t, tt.response.Status, readResp.Status)
			assert.Equal(t, tt.response.Payload, readResp.Payload)
		})
	}
}

func TestWriteResponseNilResponse(t *testing.T) {
	var buf bytes.Buffer
	err := WriteResponse(&buf, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteResponsePayloadTooLarge(t *testing.T) {
	var buf bytes.Buffer
	resp := &Response{
		Status:  StatusOK,
		Payload: make([]byte, MaxPayloadSize+1),
	}
	err := WriteResponse(&buf, resp)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestReadResponseTruncated(t *testing.T) {
	// Test empty reader
	_, err := ReadResponse(bytes.NewReader(nil))
	require.Error(t, err)

	// Test truncated payload length
	buf := []byte{StatusOK}
	_, err = ReadResponse(bytes.NewReader(buf))
	require.Error(t, err)
}

func TestEncodeDecodePayload(t *testing.T) {
	type TestStruct struct {
		Key   string `json:"key"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name   string
		entity interface{}
	}{
		{
			name: "struct",
			entity: &TestStruct{
				Key:   "test-key",
				Value: 42,
			},
		},
		{
			name:   "map",
			entity: map[string]string{"key": "value"},
		},
		{
			name:   "slice",
			entity: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodePayload(tt.entity)
			require.NoError(t, err)
			require.NotEmpty(t, encoded)

			// For struct test, decode back
			if ts, ok := tt.entity.(*TestStruct); ok {
				var decoded TestStruct
				err = DecodePayload(encoded, &decoded)
				require.NoError(t, err)
				assert.Equal(t, ts.Key, decoded.Key)
				assert.Equal(t, ts.Value, decoded.Value)
			}
		})
	}
}

func TestEncodePayloadNil(t *testing.T) {
	encoded, err := EncodePayload(nil)
	require.NoError(t, err)
	assert.Nil(t, encoded)
}

func TestDecodePayloadEmpty(t *testing.T) {
	var result map[string]string
	err := DecodePayload(nil, &result)
	require.NoError(t, err)

	err = DecodePayload([]byte{}, &result)
	require.NoError(t, err)
}

func TestDecodePayloadNilEntity(t *testing.T) {
	err := DecodePayload([]byte(`{"key":"value"}`), nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestDecodePayloadInvalidJSON(t *testing.T) {
	var result map[string]string
	err := DecodePayload([]byte("not json"), &result)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
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
			name: "storage",
			err: &dberrors.QBError{
				Code: dberrors.ErrStorage,
			},
			expected: StatusStorage,
		},
		{
			name: "internal",
			err: &dberrors.QBError{
				Code: dberrors.ErrInternal,
			},
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
			name:         "OK",
			status:       StatusOK,
			message:      "",
			expectedCode: 0,
		},
		{
			name:         "not found",
			status:       StatusNotFound,
			message:      "object not found",
			expectedCode: dberrors.ErrNotFound,
		},
		{
			name:         "unauthorized",
			status:       StatusUnauthorized,
			message:      "authentication required",
			expectedCode: dberrors.ErrUnauthorized,
		},
		{
			name:         "invalid request",
			status:       StatusInvalidRequest,
			message:      "bad request",
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "invalid argument",
			status:       StatusInvalidArgument,
			message:      "invalid parameter",
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "already exists",
			status:       StatusAlreadyExists,
			message:      "key exists",
			expectedCode: dberrors.ErrAlreadyExists,
		},
		{
			name:         "timeout",
			status:       StatusTimeout,
			message:      "operation timed out",
			expectedCode: dberrors.ErrTimeout,
		},
		{
			name:         "permission denied",
			status:       StatusPermissionDenied,
			message:      "access denied",
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
			name:         "storage",
			status:       StatusStorage,
			message:      "storage error",
			expectedCode: dberrors.ErrStorage,
		},
		{
			name:         "internal error",
			status:       StatusInternalError,
			message:      "internal error",
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "unknown status",
			status:       0xFF,
			message:      "unknown error",
			expectedCode: dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := StatusToError(tt.status, tt.message)
			if tt.status == StatusOK {
				assert.Nil(t, err)
			} else {
				require.Error(t, err)
				var qbErr *dberrors.QBError
				require.True(t, dberrors.As(err, &qbErr))
				assert.Equal(t, tt.expectedCode, qbErr.Code)
			}
		})
	}
}

func TestBatchTypes(t *testing.T) {
	t.Run("BatchPutRequest", func(t *testing.T) {
		req := &BatchPutRequest{
			Objects: []*PutObjectItem{
				{
					Key:         "key1",
					Value:       []byte("value1"),
					ContentType: "text/plain",
					Tags:        map[string]string{"env": "test"},
				},
				{
					Key:   "key2",
					Value: []byte("value2"),
				},
			},
		}

		encoded, err := EncodePayload(req)
		require.NoError(t, err)

		var decoded BatchPutRequest
		err = DecodePayload(encoded, &decoded)
		require.NoError(t, err)

		assert.Len(t, decoded.Objects, 2)
		assert.Equal(t, "key1", decoded.Objects[0].Key)
		assert.Equal(t, "text/plain", decoded.Objects[0].ContentType)
	})

	t.Run("BatchGetRequest", func(t *testing.T) {
		req := &BatchGetRequest{
			Keys:            []string{"key1", "key2", "key3"},
			TenantID:        "tenant-1",
			IncludeMetadata: true,
		}

		encoded, err := EncodePayload(req)
		require.NoError(t, err)

		var decoded BatchGetRequest
		err = DecodePayload(encoded, &decoded)
		require.NoError(t, err)

		assert.Equal(t, []string{"key1", "key2", "key3"}, decoded.Keys)
		assert.Equal(t, "tenant-1", decoded.TenantID)
		assert.True(t, decoded.IncludeMetadata)
	})

	t.Run("BatchDeleteRequest", func(t *testing.T) {
		req := &BatchDeleteRequest{
			Keys:     []string{"key1", "key2"},
			TenantID: "tenant-1",
		}

		encoded, err := EncodePayload(req)
		require.NoError(t, err)

		var decoded BatchDeleteRequest
		err = DecodePayload(encoded, &decoded)
		require.NoError(t, err)

		assert.Equal(t, []string{"key1", "key2"}, decoded.Keys)
		assert.Equal(t, "tenant-1", decoded.TenantID)
	})

	t.Run("BatchPutResponse", func(t *testing.T) {
		resp := &BatchPutResponse{
			Results: []*BatchPutResult{
				{Key: "key1", Success: true, ObjectID: 1},
				{Key: "key2", Success: false, Error: "error"},
			},
			Succeeded: 1,
			Failed:    1,
		}

		encoded, err := EncodePayload(resp)
		require.NoError(t, err)

		var decoded BatchPutResponse
		err = DecodePayload(encoded, &decoded)
		require.NoError(t, err)

		assert.Len(t, decoded.Results, 2)
		assert.Equal(t, 1, decoded.Succeeded)
		assert.Equal(t, 1, decoded.Failed)
	})

	t.Run("BatchGetResponse", func(t *testing.T) {
		resp := &BatchGetResponse{
			Results: []*BatchGetResult{
				{Key: "key1", Success: true, Value: []byte("value1")},
				{Key: "key2", Success: false, Error: "not found"},
			},
			Succeeded: 1,
			Failed:    1,
		}

		encoded, err := EncodePayload(resp)
		require.NoError(t, err)

		var decoded BatchGetResponse
		err = DecodePayload(encoded, &decoded)
		require.NoError(t, err)

		assert.Len(t, decoded.Results, 2)
		assert.Equal(t, 1, decoded.Succeeded)
		assert.Equal(t, 1, decoded.Failed)
	})

	t.Run("BatchDeleteResponse", func(t *testing.T) {
		resp := &BatchDeleteResponse{
			Results: []*BatchDeleteResult{
				{Key: "key1", Success: true, Size: 100},
				{Key: "key2", Success: false, Error: "not found"},
			},
			Succeeded: 1,
			Failed:    1,
		}

		encoded, err := EncodePayload(resp)
		require.NoError(t, err)

		var decoded BatchDeleteResponse
		err = DecodePayload(encoded, &decoded)
		require.NoError(t, err)

		assert.Len(t, decoded.Results, 2)
		assert.Equal(t, 1, decoded.Succeeded)
		assert.Equal(t, 1, decoded.Failed)
	})
}

func TestErrorResponse(t *testing.T) {
	resp := &ErrorResponse{
		Error:   "operation failed",
		Code:    "NOT_FOUND",
		Details: "object does not exist",
	}

	encoded, err := EncodePayload(resp)
	require.NoError(t, err)

	var decoded ErrorResponse
	err = DecodePayload(encoded, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "operation failed", decoded.Error)
	assert.Equal(t, "NOT_FOUND", decoded.Code)
	assert.Equal(t, "object does not exist", decoded.Details)
}
