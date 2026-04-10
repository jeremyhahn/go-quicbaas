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
	"encoding/binary"
	"encoding/json"
	"io"
	"math"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

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
		return api.NewNilInputError("unix.objectstorage.WriteRequest", "request")
	}

	// Validate payload length before conversion
	if len(req.Payload) > math.MaxUint32 || len(req.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("unix.objectstorage.WriteRequest", "payload",
			uint32(len(req.Payload)), MaxPayloadSize) //#nosec G115 -- bounds checked above
	}
	payloadLen := uint32(len(req.Payload)) //#nosec G115 -- bounds checked above

	// Validate token length before conversion
	if len(req.Token) > math.MaxUint16 || len(req.Token) > int(MaxTokenSize) {
		return api.NewSizeLimitError("unix.objectstorage.WriteRequest", "token",
			uint32(len(req.Token)), uint32(MaxTokenSize)) //#nosec G115 -- bounds checked above
	}
	tokenLen := uint16(len(req.Token)) //#nosec G115 -- bounds checked above

	// Write opcode
	if _, err := w.Write([]byte{req.Opcode}); err != nil {
		return api.NewWriteError("unix.objectstorage.WriteRequest", "opcode", err)
	}

	// Write token length (big-endian, 2 bytes)
	tokenLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(tokenLenBuf, tokenLen)
	if _, err := w.Write(tokenLenBuf); err != nil {
		return api.NewWriteError("unix.objectstorage.WriteRequest", "token length", err)
	}

	// Write token
	if tokenLen > 0 {
		if _, err := w.Write([]byte(req.Token)); err != nil {
			return api.NewWriteError("unix.objectstorage.WriteRequest", "token", err)
		}
	}

	// Write payload length (big-endian, 4 bytes)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("unix.objectstorage.WriteRequest", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(req.Payload); err != nil {
			return api.NewWriteError("unix.objectstorage.WriteRequest", "payload", err)
		}
	}

	return nil
}

// ReadRequest reads a request from the reader.
func ReadRequest(r io.Reader) (*Request, error) {
	// Read opcode
	opcodeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, opcodeBuf); err != nil {
		return nil, api.NewReadError("unix.objectstorage.ReadRequest", "opcode", err)
	}

	// Read token length
	tokenLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, tokenLenBuf); err != nil {
		return nil, api.NewReadError("unix.objectstorage.ReadRequest", "token length", err)
	}
	tokenLen := binary.BigEndian.Uint16(tokenLenBuf)

	if tokenLen > MaxTokenSize {
		return nil, api.NewSizeLimitError("unix.objectstorage.ReadRequest", "token",
			uint32(tokenLen), uint32(MaxTokenSize))
	}

	// Read token
	var token string
	if tokenLen > 0 {
		tokenBuf := make([]byte, tokenLen)
		if _, err := io.ReadFull(r, tokenBuf); err != nil {
			return nil, api.NewReadError("unix.objectstorage.ReadRequest", "token", err)
		}
		token = string(tokenBuf)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("unix.objectstorage.ReadRequest", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("unix.objectstorage.ReadRequest", "payload",
			payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("unix.objectstorage.ReadRequest", "payload", err)
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
		return api.NewNilInputError("unix.objectstorage.WriteResponse", "response")
	}

	// Validate payload length before conversion
	if len(resp.Payload) > math.MaxUint32 || len(resp.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("unix.objectstorage.WriteResponse", "payload",
			uint32(len(resp.Payload)), MaxPayloadSize) //#nosec G115 -- bounds checked above
	}
	payloadLen := uint32(len(resp.Payload)) //#nosec G115 -- bounds checked above

	// Write status
	if _, err := w.Write([]byte{resp.Status}); err != nil {
		return api.NewWriteError("unix.objectstorage.WriteResponse", "status", err)
	}

	// Write payload length (big-endian, 4 bytes)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("unix.objectstorage.WriteResponse", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(resp.Payload); err != nil {
			return api.NewWriteError("unix.objectstorage.WriteResponse", "payload", err)
		}
	}

	return nil
}

// ReadResponse reads a response from the reader.
func ReadResponse(r io.Reader) (*Response, error) {
	// Read status
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, statusBuf); err != nil {
		return nil, api.NewReadError("unix.objectstorage.ReadResponse", "status", err)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("unix.objectstorage.ReadResponse", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("unix.objectstorage.ReadResponse", "payload",
			payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("unix.objectstorage.ReadResponse", "payload", err)
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
		return nil, api.NewEncodeError("unix.objectstorage.EncodePayload", "payload", err)
	}

	return buf, nil
}

// DecodePayload decodes JSON bytes into an entity.
func DecodePayload(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return nil
	}

	if entity == nil {
		return api.NewNilInputError("unix.objectstorage.DecodePayload", "entity")
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("unix.objectstorage.DecodePayload", "payload", err)
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
		case dberrors.ErrStorage:
			return StatusStorage
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
	case StatusStorage:
		code = dberrors.ErrStorage
	default:
		code = dberrors.ErrInternal
	}

	return api.NewProtocolStatusError("unix.objectstorage.protocol", code, message)
}

// ErrorResponse represents an error response payload.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// BatchPutRequest represents a batch put objects request.
type BatchPutRequest struct {
	Objects []*PutObjectItem `json:"objects"`
}

// PutObjectItem represents a single object in a batch put request.
type PutObjectItem struct {
	Key               string                     `json:"key"`
	Value             []byte                     `json:"value"`
	ContentType       string                     `json:"content_type,omitempty"`
	TenantID          string                     `json:"tenant_id,omitempty"`
	ShardID           uint64                     `json:"shard_id,omitempty"`
	Tags              map[string]string          `json:"tags,omitempty"`
	TargetBackend     string                     `json:"target_backend,omitempty"`
	Checksum          []byte                     `json:"checksum,omitempty"`
	ReplicationPolicy *catalog.ReplicationPolicy `json:"replication_policy,omitempty"`
	ArchivePolicy     *catalog.ArchivePolicy     `json:"archive_policy,omitempty"`
	RetentionPolicy   *catalog.RetentionPolicy   `json:"retention_policy,omitempty"`
}

// BatchPutResponse represents a batch put objects response.
type BatchPutResponse struct {
	Results   []*BatchPutResult `json:"results"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
}

// BatchPutResult represents a single object result in a batch put response.
type BatchPutResult struct {
	Key       string `json:"key"`
	ObjectID  uint64 `json:"object_id,omitempty"`
	Checksum  []byte `json:"checksum,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Backend   string `json:"backend,omitempty"`
	Version   uint64 `json:"version,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// BatchGetRequest represents a batch get objects request.
type BatchGetRequest struct {
	Keys            []string `json:"keys"`
	TenantID        string   `json:"tenant_id,omitempty"`
	IncludeMetadata bool     `json:"include_metadata,omitempty"`
}

// BatchGetResponse represents a batch get objects response.
type BatchGetResponse struct {
	Results   []*BatchGetResult `json:"results"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
}

// BatchGetResult represents a single object result in a batch get response.
type BatchGetResult struct {
	Key         string            `json:"key"`
	Value       []byte            `json:"value,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Size        int64             `json:"size,omitempty"`
	Checksum    []byte            `json:"checksum,omitempty"`
	Version     uint64            `json:"version,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	TenantID    string            `json:"tenant_id,omitempty"`
	CreatedAt   int64             `json:"created_at,omitempty"`
	ModifiedAt  int64             `json:"modified_at,omitempty"`
	Backend     string            `json:"backend,omitempty"`
	Success     bool              `json:"success"`
	Error       string            `json:"error,omitempty"`
}

// BatchDeleteRequest represents a batch delete objects request.
type BatchDeleteRequest struct {
	Keys     []string `json:"keys"`
	TenantID string   `json:"tenant_id,omitempty"`
}

// BatchDeleteResponse represents a batch delete objects response.
type BatchDeleteResponse struct {
	Results   []*BatchDeleteResult `json:"results"`
	Succeeded int                  `json:"succeeded"`
	Failed    int                  `json:"failed"`
}

// BatchDeleteResult represents a single object result in a batch delete response.
type BatchDeleteResult struct {
	Key       string `json:"key"`
	Size      int64  `json:"size,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// FetchObjectRequest represents a request to fetch object data from a peer.
// Used for peer-to-peer object data transfer during lazy snapshot recovery.
type FetchObjectRequest struct {
	// ShardID identifies the Raft shard owning the object.
	ShardID uint64 `json:"shard_id"`

	// ObjectRef is the storage path/key in the peer's object store.
	ObjectRef string `json:"object_ref"`

	// Checksum is the expected SHA-256 for verification.
	// If provided, the peer will verify before sending.
	Checksum []byte `json:"checksum,omitempty"`

	// RequestID for tracing and deduplication.
	RequestID string `json:"request_id,omitempty"`
}

// FetchObjectResponse represents a response containing object data from a peer.
type FetchObjectResponse struct {
	// ObjectRef echoes back the requested reference.
	ObjectRef string `json:"object_ref"`

	// Data contains the object bytes.
	Data []byte `json:"data,omitempty"`

	// Checksum is the computed SHA-256 of data.
	Checksum []byte `json:"checksum,omitempty"`

	// Size is the uncompressed size in bytes.
	Size int64 `json:"size"`

	// Compressed indicates if data is compressed.
	Compressed bool `json:"compressed,omitempty"`

	// CompressionAlgo if compressed (e.g., "lz4", "zstd").
	CompressionAlgo string `json:"compression_algo,omitempty"`

	// Found indicates if the object exists.
	Found bool `json:"found"`

	// ErrorMessage if not found or other error.
	ErrorMessage string `json:"error_message,omitempty"`
}

// StreamFetchObjectsRequest represents a request to fetch multiple objects in a stream.
type StreamFetchObjectsRequest struct {
	// ShardID identifies the Raft shard.
	ShardID uint64 `json:"shard_id"`

	// Items lists the objects to fetch.
	Items []*ObjectFetchItem `json:"items"`
}

// ObjectFetchItem identifies a single object to fetch in a stream request.
type ObjectFetchItem struct {
	// ObjectRef is the storage path/key.
	ObjectRef string `json:"object_ref"`

	// Checksum is the expected SHA-256 for verification.
	Checksum []byte `json:"checksum,omitempty"`
}

// StreamFetchObjectsResponse represents a response containing multiple fetched objects.
type StreamFetchObjectsResponse struct {
	// Results contains the fetch results for each requested object.
	Results []*FetchObjectResponse `json:"results"`

	// Succeeded is the count of successfully fetched objects.
	Succeeded int `json:"succeeded"`

	// Failed is the count of objects that failed to fetch.
	Failed int `json:"failed"`
}
