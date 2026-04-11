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

package messaging

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	parentmsg "github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// =============================================================================
// OpcodeToString Tests
// =============================================================================

func TestOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpPushCreateChannel, "CreateChannel"},
		{OpPushDeleteChannel, "DeleteChannel"},
		{OpPushGetChannel, "GetChannel"},
		{OpPushListChannels, "ListChannels"},
		{OpPushSubscribe, "Subscribe"},
		{OpPushUnsubscribe, "Unsubscribe"},
		{OpPushPublish, "Publish"},
		{OpPushPublishDelta, "PublishDelta"},
		{OpPushGetStats, "GetStats"},
		{OpPushDedupStats, "DedupStats"},
		{0xFF, "Unknown"},
		{0x00, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// WriteRequest / ReadRequest Tests
// =============================================================================

func TestWriteRequest_NilRequest(t *testing.T) {
	var buf bytes.Buffer
	err := WriteRequest(&buf, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteReadRequest_RoundTrip(t *testing.T) {
	t.Run("request with payload and token", func(t *testing.T) {
		original := &Request{
			Opcode:  OpPushPublish,
			Token:   "bearer-token-123",
			Payload: []byte(`{"channel":"test","data":"hello"}`),
		}

		var buf bytes.Buffer
		err := WriteRequest(&buf, original)
		require.NoError(t, err)

		decoded, err := ReadRequest(&buf)
		require.NoError(t, err)
		assert.Equal(t, original.Opcode, decoded.Opcode)
		assert.Equal(t, original.Token, decoded.Token)
		assert.Equal(t, original.Payload, decoded.Payload)
	})

	t.Run("request with empty token and payload", func(t *testing.T) {
		original := &Request{
			Opcode: OpPushGetStats,
		}

		var buf bytes.Buffer
		err := WriteRequest(&buf, original)
		require.NoError(t, err)

		decoded, err := ReadRequest(&buf)
		require.NoError(t, err)
		assert.Equal(t, original.Opcode, decoded.Opcode)
		assert.Equal(t, "", decoded.Token)
		assert.Nil(t, decoded.Payload)
	})
}

func TestWriteRequest_PayloadTooLarge(t *testing.T) {
	largePayload := make([]byte, MaxPayloadSize+1)
	req := &Request{
		Opcode:  OpPushPublish,
		Payload: largePayload,
	}

	var buf bytes.Buffer
	err := WriteRequest(&buf, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteRequest_TokenTooLarge(t *testing.T) {
	largeToken := strings.Repeat("x", int(MaxTokenSize)+1)
	req := &Request{
		Opcode: OpPushPublish,
		Token:  largeToken,
	}

	var buf bytes.Buffer
	err := WriteRequest(&buf, req)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteRequest_WriteErrors(t *testing.T) {
	t.Run("opcode write error", func(t *testing.T) {
		w := &failWriter{failAfter: 0}
		err := WriteRequest(w, &Request{Opcode: 0x01})
		require.Error(t, err)
	})

	t.Run("token length write error", func(t *testing.T) {
		w := &failWriter{failAfter: 1}
		err := WriteRequest(w, &Request{Opcode: 0x01})
		require.Error(t, err)
	})

	t.Run("token write error", func(t *testing.T) {
		w := &failWriter{failAfter: 2}
		err := WriteRequest(w, &Request{Opcode: 0x01, Token: "tok"})
		require.Error(t, err)
	})

	t.Run("payload length write error", func(t *testing.T) {
		w := &failWriter{failAfter: 2}
		err := WriteRequest(w, &Request{Opcode: 0x01})
		require.Error(t, err)
	})

	t.Run("payload write error", func(t *testing.T) {
		w := &failWriter{failAfter: 3}
		err := WriteRequest(w, &Request{Opcode: 0x01, Payload: []byte("data")})
		require.Error(t, err)
	})
}

func TestReadRequest_ReadErrors(t *testing.T) {
	t.Run("empty reader fails on opcode", func(t *testing.T) {
		buf := bytes.NewReader(nil)
		_, err := ReadRequest(buf)
		require.Error(t, err)
	})

	t.Run("short reader fails on token length", func(t *testing.T) {
		buf := bytes.NewReader([]byte{0x01}) // only opcode
		_, err := ReadRequest(buf)
		require.Error(t, err)
	})

	t.Run("token exceeds max size", func(t *testing.T) {
		var b bytes.Buffer
		b.WriteByte(0x01)           // opcode
		b.Write([]byte{0xFF, 0xFF}) // token length = 65535 > MaxTokenSize
		_, err := ReadRequest(bytes.NewReader(b.Bytes()))
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("short reader fails on payload length", func(t *testing.T) {
		var b bytes.Buffer
		b.WriteByte(0x01)           // opcode
		b.Write([]byte{0x00, 0x00}) // token length = 0
		// missing payload length
		_, err := ReadRequest(bytes.NewReader(b.Bytes()))
		require.Error(t, err)
	})

	t.Run("payload exceeds max size", func(t *testing.T) {
		var b bytes.Buffer
		b.WriteByte(0x01)                       // opcode
		b.Write([]byte{0x00, 0x00})             // token length = 0
		b.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}) // payload length = MaxUint32
		_, err := ReadRequest(bytes.NewReader(b.Bytes()))
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})
}

// =============================================================================
// WriteResponse / ReadResponse Tests
// =============================================================================

func TestWriteResponse_NilResponse(t *testing.T) {
	var buf bytes.Buffer
	err := WriteResponse(&buf, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteReadResponse_RoundTrip(t *testing.T) {
	t.Run("response with payload", func(t *testing.T) {
		original := &Response{
			Status:  StatusOK,
			Payload: []byte(`{"success":true}`),
		}

		var buf bytes.Buffer
		err := WriteResponse(&buf, original)
		require.NoError(t, err)

		decoded, err := ReadResponse(&buf)
		require.NoError(t, err)
		assert.Equal(t, original.Status, decoded.Status)
		assert.Equal(t, original.Payload, decoded.Payload)
	})

	t.Run("response with empty payload", func(t *testing.T) {
		original := &Response{
			Status: StatusNotFound,
		}

		var buf bytes.Buffer
		err := WriteResponse(&buf, original)
		require.NoError(t, err)

		decoded, err := ReadResponse(&buf)
		require.NoError(t, err)
		assert.Equal(t, original.Status, decoded.Status)
		assert.Nil(t, decoded.Payload)
	})
}

func TestWriteResponse_PayloadTooLarge(t *testing.T) {
	largePayload := make([]byte, MaxPayloadSize+1)
	resp := &Response{
		Status:  StatusOK,
		Payload: largePayload,
	}

	var buf bytes.Buffer
	err := WriteResponse(&buf, resp)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestWriteResponse_WriteErrors(t *testing.T) {
	t.Run("status write error", func(t *testing.T) {
		w := &failWriter{failAfter: 0}
		err := WriteResponse(w, &Response{Status: StatusOK})
		require.Error(t, err)
	})

	t.Run("payload length write error", func(t *testing.T) {
		w := &failWriter{failAfter: 1}
		err := WriteResponse(w, &Response{Status: StatusOK})
		require.Error(t, err)
	})

	t.Run("payload write error", func(t *testing.T) {
		w := &failWriter{failAfter: 2}
		err := WriteResponse(w, &Response{Status: StatusOK, Payload: []byte("data")})
		require.Error(t, err)
	})
}

func TestReadResponse_ReadErrors(t *testing.T) {
	t.Run("empty reader fails on status", func(t *testing.T) {
		buf := bytes.NewReader(nil)
		_, err := ReadResponse(buf)
		require.Error(t, err)
	})

	t.Run("short reader fails on payload length", func(t *testing.T) {
		buf := bytes.NewReader([]byte{StatusOK})
		_, err := ReadResponse(buf)
		require.Error(t, err)
	})

	t.Run("payload exceeds max size", func(t *testing.T) {
		var b bytes.Buffer
		b.WriteByte(StatusOK)
		b.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}) // payload length = MaxUint32
		_, err := ReadResponse(bytes.NewReader(b.Bytes()))
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})
}

// =============================================================================
// EncodePayload / DecodePayload Tests
// =============================================================================

func TestEncodePayload(t *testing.T) {
	t.Run("encodes nil returns nil", func(t *testing.T) {
		data, err := EncodePayload(nil)
		require.NoError(t, err)
		assert.Nil(t, data)
	})

	t.Run("encodes struct to JSON", func(t *testing.T) {
		data, err := EncodePayload(&CreateChannelRequest{Name: "test"})
		require.NoError(t, err)
		assert.Contains(t, string(data), `"name":"test"`)
	})

	t.Run("returns error for unencodable value", func(t *testing.T) {
		_, err := EncodePayload(make(chan int))
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
	})
}

func TestDecodePayload(t *testing.T) {
	t.Run("decodes empty data is no-op", func(t *testing.T) {
		var target CreateChannelRequest
		err := DecodePayload(nil, &target)
		require.NoError(t, err)
	})

	t.Run("decodes empty bytes is no-op", func(t *testing.T) {
		var target CreateChannelRequest
		err := DecodePayload([]byte{}, &target)
		require.NoError(t, err)
	})

	t.Run("returns error for nil entity", func(t *testing.T) {
		err := DecodePayload([]byte(`{"name":"test"}`), nil)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		var target CreateChannelRequest
		err := DecodePayload([]byte("not-json"), &target)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
	})

	t.Run("decodes valid JSON successfully", func(t *testing.T) {
		var target CreateChannelRequest
		err := DecodePayload([]byte(`{"name":"test","buffer_size":100}`), &target)
		require.NoError(t, err)
		assert.Equal(t, "test", target.Name)
		assert.Equal(t, 100, target.BufferSize)
	})
}

// =============================================================================
// ErrorToStatus Tests
// =============================================================================

func TestErrorToStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected byte
	}{
		{"nil error returns OK", nil, StatusOK},
		{"NotFound", &dberrors.QBError{Code: dberrors.ErrNotFound}, StatusNotFound},
		{"Unauthorized", &dberrors.QBError{Code: dberrors.ErrUnauthorized}, StatusUnauthorized},
		{"InvalidInput", &dberrors.QBError{Code: dberrors.ErrInvalidInput}, StatusInvalidArgument},
		{"AlreadyExists", &dberrors.QBError{Code: dberrors.ErrAlreadyExists}, StatusAlreadyExists},
		{"Timeout", &dberrors.QBError{Code: dberrors.ErrTimeout}, StatusTimeout},
		{"Forbidden", &dberrors.QBError{Code: dberrors.ErrForbidden}, StatusForbidden},
		{"ResourceExhausted", &dberrors.QBError{Code: dberrors.ErrResourceExhausted}, StatusResourceExhausted},
		{"Unavailable", &dberrors.QBError{Code: dberrors.ErrUnavailable}, StatusUnavailable},
		{"Internal", &dberrors.QBError{Code: dberrors.ErrInternal}, StatusInternalError},
		{"non-qb error returns Internal", errors.New("generic error"), StatusInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ErrorToStatus(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// StatusToError Tests
// =============================================================================

func TestStatusToError(t *testing.T) {
	t.Run("OK status returns nil", func(t *testing.T) {
		err := StatusToError(StatusOK, "")
		assert.Nil(t, err)
	})

	tests := []struct {
		status       byte
		expectedCode dberrors.ErrorCode
	}{
		{StatusNotFound, dberrors.ErrNotFound},
		{StatusUnauthorized, dberrors.ErrUnauthorized},
		{StatusInvalidArgument, dberrors.ErrInvalidInput},
		{StatusInvalidRequest, dberrors.ErrInvalidInput},
		{StatusAlreadyExists, dberrors.ErrAlreadyExists},
		{StatusTimeout, dberrors.ErrTimeout},
		{StatusPermissionDenied, dberrors.ErrForbidden},
		{StatusForbidden, dberrors.ErrForbidden},
		{StatusResourceExhausted, dberrors.ErrResourceExhausted},
		{StatusUnavailable, dberrors.ErrUnavailable},
		{StatusInternalError, dberrors.ErrInternal},
		{0xFE, dberrors.ErrInternal}, // unknown status
	}

	for _, tt := range tests {
		t.Run(tt.expectedCode.String(), func(t *testing.T) {
			err := StatusToError(tt.status, "test message")
			require.Error(t, err)

			var qbErr *dberrors.QBError
			require.True(t, errors.As(err, &qbErr))
			assert.Equal(t, tt.expectedCode, qbErr.Code)
		})
	}
}

// =============================================================================
// Types Conversion Tests
// =============================================================================

func TestChannelToInfo(t *testing.T) {
	t.Run("nil channel returns nil", func(t *testing.T) {
		assert.Nil(t, channelToInfo(nil))
	})

	t.Run("converts channel to info", func(t *testing.T) {
		ch := &parentmsg.Channel{
			Name:        "ch-1",
			Codec:       "json",
			Subscribers: 5,
			Persistent:  true,
			MaxHistory:  100,
			BufferSize:  256,
		}
		info := channelToInfo(ch)
		require.NotNil(t, info)
		assert.Equal(t, "ch-1", info.Name)
		assert.Equal(t, "json", info.Codec)
		assert.Equal(t, int64(5), info.Subscribers)
		assert.True(t, info.Persistent)
	})
}

func TestStatsToInfo(t *testing.T) {
	t.Run("nil stats returns nil", func(t *testing.T) {
		assert.Nil(t, statsToInfo(nil))
	})

	t.Run("converts stats to info", func(t *testing.T) {
		stats := &parentmsg.Stats{
			TotalChannels:      10,
			TotalSubscriptions: 20,
			MessagesPublished:  1000,
			MessagesDelivered:  900,
			DedupHits:          50,
			DroppedMessages:    5,
			BufferUtilization:  0.75,
		}
		info := statsToInfo(stats)
		require.NotNil(t, info)
		assert.Equal(t, int64(10), info.TotalChannels)
		assert.Equal(t, int64(20), info.TotalSubscriptions)
		assert.Equal(t, int64(1000), info.MessagesPublished)
		assert.InDelta(t, 0.75, info.BufferUtilization, 0.001)
	})
}

func TestDedupStatsToInfo(t *testing.T) {
	t.Run("nil dedup stats returns nil", func(t *testing.T) {
		assert.Nil(t, dedupStatsToInfo(nil))
	})

	t.Run("converts dedup stats without top duplicates", func(t *testing.T) {
		stats := &parentmsg.DedupStats{
			Channel:         "ch-1",
			TotalMessages:   100,
			TotalDuplicates: 10,
		}
		info := dedupStatsToInfo(stats)
		require.NotNil(t, info)
		assert.Equal(t, "ch-1", info.Channel)
		assert.Equal(t, int64(100), info.TotalMessages)
		assert.Nil(t, info.TopDuplicates)
	})

	t.Run("converts dedup stats with top duplicates", func(t *testing.T) {
		stats := &parentmsg.DedupStats{
			Channel:         "ch-1",
			TotalMessages:   100,
			TotalDuplicates: 10,
			TopDuplicates: []*parentmsg.DedupEntry{
				{Count: 5, MessageID: "msg-1", PayloadHash: "hash-1"},
				{Count: 3, MessageID: "msg-2", PayloadHash: "hash-2"},
			},
		}
		info := dedupStatsToInfo(stats)
		require.NotNil(t, info)
		assert.Len(t, info.TopDuplicates, 2)
		assert.Equal(t, int64(5), info.TopDuplicates[0].Count)
		assert.Equal(t, "msg-1", info.TopDuplicates[0].MessageID)
	})
}

func TestSubscriptionToResponse(t *testing.T) {
	t.Run("nil subscription returns nil", func(t *testing.T) {
		assert.Nil(t, subscriptionToResponse(nil))
	})

	t.Run("converts subscription without filters", func(t *testing.T) {
		sub := &parentmsg.Subscription{
			ID:       "sub-1",
			Channel:  "ch-1",
			ClientID: "client-1",
		}
		resp := subscriptionToResponse(sub)
		require.NotNil(t, resp)
		assert.Equal(t, "sub-1", resp.SubscriptionID)
		assert.Nil(t, resp.Filter)
	})

	t.Run("converts subscription with filters", func(t *testing.T) {
		sub := &parentmsg.Subscription{
			ID:       "sub-2",
			Channel:  "ch-2",
			ClientID: "client-2",
			Filters: &parentmsg.SubscriptionFilter{
				KeyPrefix: "user:",
				EventTypes: []parentmsg.EventType{
					parentmsg.EventTypePut,
					parentmsg.EventTypeDelete,
				},
			},
		}
		resp := subscriptionToResponse(sub)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Filter)
		assert.Equal(t, "user:", resp.Filter.KeyPrefix)
		assert.Len(t, resp.Filter.EventTypes, 2)
		assert.Contains(t, resp.Filter.EventTypes, "PUT")
		assert.Contains(t, resp.Filter.EventTypes, "DELETE")
	})

	t.Run("converts subscription with empty event types", func(t *testing.T) {
		sub := &parentmsg.Subscription{
			ID:      "sub-3",
			Channel: "ch-3",
			Filters: &parentmsg.SubscriptionFilter{
				KeyPrefix: "prefix:",
			},
		}
		resp := subscriptionToResponse(sub)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Filter)
		assert.Equal(t, "prefix:", resp.Filter.KeyPrefix)
		assert.Nil(t, resp.Filter.EventTypes)
	})
}

// =============================================================================
// failWriter - a writer that fails after N writes
// =============================================================================

type failWriter struct {
	writeCount int
	failAfter  int
}

func (w *failWriter) Write(p []byte) (int, error) {
	if w.writeCount >= w.failAfter {
		return 0, errors.New("write failed")
	}
	w.writeCount++
	return len(p), nil
}
