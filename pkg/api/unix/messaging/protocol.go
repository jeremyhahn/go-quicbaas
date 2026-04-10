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

// Package messaging provides Unix socket API handlers for messaging operations.
// It implements a binary protocol with JSON-encoded payloads for high-performance
// local inter-process communication.
package messaging

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Protocol constants for Push Unix socket API.
const (
	// ProtocolVersion is the current protocol version.
	ProtocolVersion byte = 0x01

	// MaxPayloadSize is the maximum payload size (16MB for message payloads).
	MaxPayloadSize uint32 = 16 * 1024 * 1024

	// MaxTokenSize is the maximum authentication token size (8KB).
	MaxTokenSize uint16 = 8 * 1024
)

// Operation codes for messaging operations.
// Starting at 0xC0 to avoid conflicts with other Unix socket APIs.
const (
	// Channel Operations (0xC0-0xC3).
	OpPushCreateChannel byte = 0xC0
	OpPushDeleteChannel byte = 0xC1
	OpPushGetChannel    byte = 0xC2
	OpPushListChannels  byte = 0xC3

	// Subscription Operations (0xC4-0xC5).
	OpPushSubscribe   byte = 0xC4
	OpPushUnsubscribe byte = 0xC5

	// Publish Operations (0xC6-0xC7).
	OpPushPublish      byte = 0xC6
	OpPushPublishDelta byte = 0xC7

	// Stats Operations (0xC8-0xC9).
	OpPushGetStats   byte = 0xC8
	OpPushDedupStats byte = 0xC9
)

// Status codes for responses.
const (
	StatusOK                byte = 0x00
	StatusInvalidRequest    byte = 0x01
	StatusNotFound          byte = 0x02
	StatusUnauthorized      byte = 0x03
	StatusInternalError     byte = 0x04
	StatusTimeout           byte = 0x05
	StatusAlreadyExists     byte = 0x06
	StatusPermissionDenied  byte = 0x07
	StatusResourceExhausted byte = 0x08
	StatusInvalidArgument   byte = 0x09
	StatusUnavailable       byte = 0x0A
	StatusForbidden         byte = 0x0B
)

// OpcodeToString returns a human-readable name for an opcode.
func OpcodeToString(opcode byte) string {
	names := map[byte]string{
		OpPushCreateChannel: "CreateChannel",
		OpPushDeleteChannel: "DeleteChannel",
		OpPushGetChannel:    "GetChannel",
		OpPushListChannels:  "ListChannels",
		OpPushSubscribe:     "Subscribe",
		OpPushUnsubscribe:   "Unsubscribe",
		OpPushPublish:       "Publish",
		OpPushPublishDelta:  "PublishDelta",
		OpPushGetStats:      "GetStats",
		OpPushDedupStats:    "DedupStats",
	}

	if name, ok := names[opcode]; ok {
		return name
	}
	return "Unknown"
}

// Request represents a Unix socket protocol request.
// Wire format: [opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M].
type Request struct {
	Opcode  byte
	Token   string
	Payload []byte
}

// Response represents a Unix socket protocol response.
// Wire format: [status:1][payloadLen:4][payload:N].
type Response struct {
	Status  byte
	Payload []byte
}

// WriteRequest writes a request to the writer.
func WriteRequest(w io.Writer, req *Request) error {
	if req == nil {
		return api.NewNilInputError("unix.messaging.WriteRequest", "request")
	}

	// Validate payload length before conversion
	if len(req.Payload) > math.MaxUint32 || len(req.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("unix.messaging.WriteRequest", "payload",
			uint32(len(req.Payload)), MaxPayloadSize) //#nosec G115 -- bounds checked above
	}
	payloadLen := uint32(len(req.Payload)) //#nosec G115 -- bounds checked above

	// Validate token length before conversion
	if len(req.Token) > math.MaxUint16 || len(req.Token) > int(MaxTokenSize) {
		return api.NewSizeLimitError("unix.messaging.WriteRequest", "token",
			uint32(len(req.Token)), uint32(MaxTokenSize)) //#nosec G115 -- bounds checked above
	}
	tokenLen := uint16(len(req.Token)) //#nosec G115 -- bounds checked above

	// Write opcode
	if _, err := w.Write([]byte{req.Opcode}); err != nil {
		return api.NewWriteError("unix.messaging.WriteRequest", "opcode", err)
	}

	// Write token length (big-endian, 2 bytes)
	tokenLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(tokenLenBuf, tokenLen)
	if _, err := w.Write(tokenLenBuf); err != nil {
		return api.NewWriteError("unix.messaging.WriteRequest", "token length", err)
	}

	// Write token
	if tokenLen > 0 {
		if _, err := w.Write([]byte(req.Token)); err != nil {
			return api.NewWriteError("unix.messaging.WriteRequest", "token", err)
		}
	}

	// Write payload length (big-endian, 4 bytes)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("unix.messaging.WriteRequest", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(req.Payload); err != nil {
			return api.NewWriteError("unix.messaging.WriteRequest", "payload", err)
		}
	}

	return nil
}

// ReadRequest reads a request from the reader.
func ReadRequest(r io.Reader) (*Request, error) {
	// Read opcode
	opcodeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, opcodeBuf); err != nil {
		return nil, api.NewReadError("unix.messaging.ReadRequest", "opcode", err)
	}

	// Read token length
	tokenLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, tokenLenBuf); err != nil {
		return nil, api.NewReadError("unix.messaging.ReadRequest", "token length", err)
	}
	tokenLen := binary.BigEndian.Uint16(tokenLenBuf)

	if tokenLen > MaxTokenSize {
		return nil, api.NewSizeLimitError("unix.messaging.ReadRequest", "token",
			uint32(tokenLen), uint32(MaxTokenSize))
	}

	// Read token
	var token string
	if tokenLen > 0 {
		tokenBuf := make([]byte, tokenLen)
		if _, err := io.ReadFull(r, tokenBuf); err != nil {
			return nil, api.NewReadError("unix.messaging.ReadRequest", "token", err)
		}
		token = string(tokenBuf)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("unix.messaging.ReadRequest", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("unix.messaging.ReadRequest", "payload",
			payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("unix.messaging.ReadRequest", "payload", err)
		}
	}

	return &Request{
		Opcode:  opcodeBuf[0],
		Token:   token,
		Payload: payload,
	}, nil
}

// WriteResponse writes a response to the writer.
func WriteResponse(w io.Writer, resp *Response) error {
	if resp == nil {
		return api.NewNilInputError("unix.messaging.WriteResponse", "response")
	}

	// Validate payload length before conversion
	if len(resp.Payload) > math.MaxUint32 || len(resp.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("unix.messaging.WriteResponse", "payload",
			uint32(len(resp.Payload)), MaxPayloadSize) //#nosec G115 -- bounds checked above
	}
	payloadLen := uint32(len(resp.Payload)) //#nosec G115 -- bounds checked above

	// Write status
	if _, err := w.Write([]byte{resp.Status}); err != nil {
		return api.NewWriteError("unix.messaging.WriteResponse", "status", err)
	}

	// Write payload length (big-endian, 4 bytes)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("unix.messaging.WriteResponse", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(resp.Payload); err != nil {
			return api.NewWriteError("unix.messaging.WriteResponse", "payload", err)
		}
	}

	return nil
}

// ReadResponse reads a response from the reader.
func ReadResponse(r io.Reader) (*Response, error) {
	// Read status
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, statusBuf); err != nil {
		return nil, api.NewReadError("unix.messaging.ReadResponse", "status", err)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("unix.messaging.ReadResponse", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("unix.messaging.ReadResponse", "payload",
			payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("unix.messaging.ReadResponse", "payload", err)
		}
	}

	return &Response{
		Status:  statusBuf[0],
		Payload: payload,
	}, nil
}

// EncodePayload encodes an entity to JSON bytes.
func EncodePayload(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, nil
	}

	buf, err := json.Marshal(entity)
	if err != nil {
		return nil, api.NewEncodeError("unix.messaging.EncodePayload", "payload", err)
	}

	return buf, nil
}

// DecodePayload decodes JSON bytes into an entity.
func DecodePayload(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return nil
	}

	if entity == nil {
		return api.NewNilInputError("unix.messaging.DecodePayload", "entity")
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("unix.messaging.DecodePayload", "payload", err)
	}

	return nil
}

// ErrorToStatus converts a QuicBaaS error to a status code.
func ErrorToStatus(err error) byte {
	if err == nil {
		return StatusOK
	}

	var dragonErr *dberrors.DragonError
	if dberrors.As(err, &dragonErr) {
		switch dragonErr.Code {
		case dberrors.ErrNotFound:
			return StatusNotFound
		case dberrors.ErrUnauthorized:
			return StatusUnauthorized
		case dberrors.ErrInvalidInput:
			return StatusInvalidArgument
		case dberrors.ErrAlreadyExists:
			return StatusAlreadyExists
		case dberrors.ErrTimeout:
			return StatusTimeout
		case dberrors.ErrForbidden:
			return StatusForbidden
		case dberrors.ErrResourceExhausted:
			return StatusResourceExhausted
		case dberrors.ErrUnavailable:
			return StatusUnavailable
		default:
			return StatusInternalError
		}
	}

	return StatusInternalError
}

// StatusToError converts a status code to a QuicBaaS error.
func StatusToError(status byte, message string) error {
	if status == StatusOK {
		return nil
	}

	var code dberrors.ErrorCode
	switch status {
	case StatusNotFound:
		code = dberrors.ErrNotFound
	case StatusUnauthorized:
		code = dberrors.ErrUnauthorized
	case StatusInvalidArgument, StatusInvalidRequest:
		code = dberrors.ErrInvalidInput
	case StatusAlreadyExists:
		code = dberrors.ErrAlreadyExists
	case StatusTimeout:
		code = dberrors.ErrTimeout
	case StatusPermissionDenied, StatusForbidden:
		code = dberrors.ErrForbidden
	case StatusResourceExhausted:
		code = dberrors.ErrResourceExhausted
	case StatusUnavailable:
		code = dberrors.ErrUnavailable
	default:
		code = dberrors.ErrInternal
	}

	return api.NewProtocolStatusError("unix.messaging.protocol", code, message)
}
