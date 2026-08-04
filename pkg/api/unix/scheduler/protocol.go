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

// Package scheduler provides Unix socket API handlers for scheduler operations.
// It implements a binary protocol with JSON-encoded payloads for high-performance
// local inter-process communication.
package scheduler

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Protocol constants for Scheduler Unix socket API.
const (
	// ProtocolVersion is the current protocol version.
	ProtocolVersion byte = 0x01

	// MaxPayloadSize is the maximum payload size (16MB for job payloads).
	MaxPayloadSize uint32 = 16 * 1024 * 1024

	// MaxTokenSize is the maximum authentication token size (64KB - 1).
	//
	// Production tokens are RFC 9068 at+jwt access tokens whose scp and
	// roles claims scale with the subject's qrdb RBAC grants; real tokens
	// have been observed at 11,060 bytes (past the previous 8KB cap).
	// 65535 is the ceiling of the 2-byte wire length prefix, so this is a
	// pure constant change with no frame-layout change. Must stay
	// identical to the QUIC codec bound in pkg/api/quic and
	// sdk/go/transport/quic.
	MaxTokenSize uint16 = 64*1024 - 1
)

// Operation codes for scheduler operations.
// Starting at 0xB0 to avoid conflicts with other Unix socket APIs.
const (
	// Job CRUD Operations (0xB0-0xB4).
	OpSchedulerCreateJob byte = 0xB0
	OpSchedulerGetJob    byte = 0xB1
	OpSchedulerUpdateJob byte = 0xB2
	OpSchedulerDeleteJob byte = 0xB3
	OpSchedulerListJobs  byte = 0xB4

	// Job Control Operations (0xB5-0xB8).
	OpSchedulerTriggerJob byte = 0xB5
	OpSchedulerPauseJob   byte = 0xB6
	OpSchedulerResumeJob  byte = 0xB7
	OpSchedulerCancelJob  byte = 0xB8

	// Status/History Operations (0xB9-0xBA).
	OpSchedulerGetJobHistory byte = 0xB9
	OpSchedulerGetStatus     byte = 0xBA
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
		OpSchedulerCreateJob:     "CreateJob",
		OpSchedulerGetJob:        "GetJob",
		OpSchedulerUpdateJob:     "UpdateJob",
		OpSchedulerDeleteJob:     "DeleteJob",
		OpSchedulerListJobs:      "ListJobs",
		OpSchedulerTriggerJob:    "TriggerJob",
		OpSchedulerPauseJob:      "PauseJob",
		OpSchedulerResumeJob:     "ResumeJob",
		OpSchedulerCancelJob:     "CancelJob",
		OpSchedulerGetJobHistory: "GetJobHistory",
		OpSchedulerGetStatus:     "GetStatus",
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
		return api.NewNilInputError("unix.scheduler.WriteRequest", "request")
	}

	// Validate payload length before conversion
	if len(req.Payload) > math.MaxUint32 || len(req.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("unix.scheduler.WriteRequest", "payload",
			uint32(len(req.Payload)), MaxPayloadSize) //#nosec G115 -- bounds checked above
	}
	payloadLen := uint32(len(req.Payload)) //#nosec G115 -- bounds checked above

	// Validate token length before conversion
	if len(req.Token) > math.MaxUint16 || len(req.Token) > int(MaxTokenSize) {
		return api.NewSizeLimitError("unix.scheduler.WriteRequest", "token",
			uint32(len(req.Token)), uint32(MaxTokenSize)) //#nosec G115 -- bounds checked above
	}
	tokenLen := uint16(len(req.Token)) //#nosec G115 -- bounds checked above

	// Write opcode
	if _, err := w.Write([]byte{req.Opcode}); err != nil {
		return api.NewWriteError("unix.scheduler.WriteRequest", "opcode", err)
	}

	// Write token length (big-endian, 2 bytes)
	tokenLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(tokenLenBuf, tokenLen)
	if _, err := w.Write(tokenLenBuf); err != nil {
		return api.NewWriteError("unix.scheduler.WriteRequest", "token length", err)
	}

	// Write token
	if tokenLen > 0 {
		if _, err := w.Write([]byte(req.Token)); err != nil {
			return api.NewWriteError("unix.scheduler.WriteRequest", "token", err)
		}
	}

	// Write payload length (big-endian, 4 bytes)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("unix.scheduler.WriteRequest", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(req.Payload); err != nil {
			return api.NewWriteError("unix.scheduler.WriteRequest", "payload", err)
		}
	}

	return nil
}

// ReadRequest reads a request from the reader.
func ReadRequest(r io.Reader) (*Request, error) {
	// Read opcode
	opcodeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, opcodeBuf); err != nil {
		return nil, api.NewReadError("unix.scheduler.ReadRequest", "opcode", err)
	}

	// Read token length
	tokenLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, tokenLenBuf); err != nil {
		return nil, api.NewReadError("unix.scheduler.ReadRequest", "token length", err)
	}
	tokenLen := binary.BigEndian.Uint16(tokenLenBuf)

	// With MaxTokenSize at the ceiling of the 2-byte wire prefix this cannot
	// trigger; it stays as defense in depth should the bound be lowered.
	if int(tokenLen) > int(MaxTokenSize) {
		return nil, api.NewSizeLimitError("unix.scheduler.ReadRequest", "token",
			uint32(tokenLen), uint32(MaxTokenSize))
	}

	// Read token
	var token string
	if tokenLen > 0 {
		tokenBuf := make([]byte, tokenLen)
		if _, err := io.ReadFull(r, tokenBuf); err != nil {
			return nil, api.NewReadError("unix.scheduler.ReadRequest", "token", err)
		}
		token = string(tokenBuf)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("unix.scheduler.ReadRequest", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("unix.scheduler.ReadRequest", "payload",
			payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("unix.scheduler.ReadRequest", "payload", err)
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
		return api.NewNilInputError("unix.scheduler.WriteResponse", "response")
	}

	// Validate payload length before conversion
	if len(resp.Payload) > math.MaxUint32 || len(resp.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("unix.scheduler.WriteResponse", "payload",
			uint32(len(resp.Payload)), MaxPayloadSize) //#nosec G115 -- bounds checked above
	}
	payloadLen := uint32(len(resp.Payload)) //#nosec G115 -- bounds checked above

	// Write status
	if _, err := w.Write([]byte{resp.Status}); err != nil {
		return api.NewWriteError("unix.scheduler.WriteResponse", "status", err)
	}

	// Write payload length (big-endian, 4 bytes)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("unix.scheduler.WriteResponse", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(resp.Payload); err != nil {
			return api.NewWriteError("unix.scheduler.WriteResponse", "payload", err)
		}
	}

	return nil
}

// ReadResponse reads a response from the reader.
func ReadResponse(r io.Reader) (*Response, error) {
	// Read status
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, statusBuf); err != nil {
		return nil, api.NewReadError("unix.scheduler.ReadResponse", "status", err)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("unix.scheduler.ReadResponse", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("unix.scheduler.ReadResponse", "payload",
			payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("unix.scheduler.ReadResponse", "payload", err)
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
		return nil, api.NewEncodeError("unix.scheduler.EncodePayload", "payload", err)
	}

	return buf, nil
}

// DecodePayload decodes JSON bytes into an entity.
func DecodePayload(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return nil
	}

	if entity == nil {
		return api.NewNilInputError("unix.scheduler.DecodePayload", "entity")
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("unix.scheduler.DecodePayload", "payload", err)
	}

	return nil
}

// ErrorToStatus converts a QuicBaaS error to a status code.
func ErrorToStatus(err error) byte {
	if err == nil {
		return StatusOK
	}

	var qbErr *dberrors.QBError
	if dberrors.As(err, &qbErr) {
		switch qbErr.Code {
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

	return api.NewProtocolStatusError("unix.scheduler.protocol", code, message)
}
