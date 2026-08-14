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
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/go-msgpack/v2/codec"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Default codec names.
const (
	defaultCodecName = CodecJSON
	codecTagName     = "quicbaas"
	codecTagKey      = "codec"
)

// CodecInterface abstracts codec operations for the push notification system.
type CodecInterface interface {
	// Name returns the codec name (e.g., "json", "msgpack").
	Name() string

	// Encode serializes an entity to bytes.
	Encode(entity interface{}) ([]byte, error)

	// Decode deserializes bytes into an entity.
	Decode(data []byte, entity interface{}) error
}

// CodecResolver resolves which codec to use based on priority:
// 1. Message-level override
// 2. Subscription-level preference
// 3. Channel default
// 4. Session negotiated codec
// 5. System default (json).
type CodecResolver struct {
	codecs       sync.Map // map[string]CodecInterface
	defaultCodec atomic.Value
	logger       *logger.Logger
}

// CodecResolverOption configures CodecResolver.
type CodecResolverOption func(*CodecResolver)

// WithResolverDefaultCodec sets the system default codec name for the resolver.
func WithResolverDefaultCodec(name string) CodecResolverOption {
	return func(r *CodecResolver) {
		r.defaultCodec.Store(name)
	}
}

// WithResolverLogger sets the logger for the resolver.
func WithResolverLogger(log *logger.Logger) CodecResolverOption {
	return func(r *CodecResolver) {
		r.logger = log
	}
}

// NewCodecResolver creates a new CodecResolver with built-in codecs registered.
func NewCodecResolver(log *logger.Logger, opts ...CodecResolverOption) *CodecResolver {
	r := &CodecResolver{
		logger: log,
	}
	r.defaultCodec.Store(defaultCodecName)

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Register built-in codecs
	r.codecs.Store(CodecJSON, &jsonPushCodec{})
	r.codecs.Store(CodecMsgpack, &msgpackPushCodec{})

	return r
}

// RegisterCodec registers a codec with the resolver.
// Returns an error if the codec is nil or has an empty name.
func (r *CodecResolver) RegisterCodec(c CodecInterface) error {
	if c == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.RegisterCodec",
			Err:  ErrNilCodec,
		}
	}

	name := c.Name()
	if name == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.RegisterCodec",
			Err:  ErrEmptyCodecName,
		}
	}

	r.codecs.Store(name, c)

	if r.logger != nil {
		r.logger.Debugf("registered codec: %s", name)
	}

	return nil
}

// GetCodec retrieves a codec by name.
// Returns an error if the codec is not registered.
func (r *CodecResolver) GetCodec(name string) (CodecInterface, error) {
	if name == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.GetCodec",
			Err:  ErrEmptyCodecName,
		}
	}

	val, ok := r.codecs.Load(name)
	if !ok {
		return nil, NewInvalidCodecError("CodecResolver.GetCodec", name)
	}

	c, ok := val.(CodecInterface)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CodecResolver.GetCodec",
			Err:  &CodecTypeAssertionError{Name: name},
		}
	}

	return c, nil
}

// ResolveCodec resolves the codec based on priority:
// 1. Message-level override (highest priority)
// 2. Subscription-level preference
// 3. Channel default
// 4. Session negotiated codec
// 5. System default (lowest priority)
//
// Empty strings are skipped in the priority chain.
// Returns the first available codec in priority order, falling back to
// system default if no specified codec is available.
func (r *CodecResolver) ResolveCodec(messageCodec, subscriptionCodec, channelCodec, sessionCodec string) (CodecInterface, error) {
	// Build priority list
	candidates := []string{messageCodec, subscriptionCodec, channelCodec, sessionCodec}

	for _, name := range candidates {
		if name == "" {
			continue
		}

		c, err := r.GetCodec(name)
		if err == nil {
			return c, nil
		}

		// Log warning if requested codec not found, continue to fallback
		if r.logger != nil {
			r.logger.Debugf("codec %q not available, trying next in priority", name)
		}
	}

	// Fall back to system default
	defaultName, ok := r.defaultCodec.Load().(string)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CodecResolver.ResolveCodec",
			Err:  &DefaultCodecNotFoundError{Name: "unknown"},
		}
	}
	c, err := r.GetCodec(defaultName)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CodecResolver.ResolveCodec",
			Err:  &DefaultCodecNotFoundError{Name: defaultName},
		}
	}

	return c, nil
}

// DetectCodec detects the preferred codec from struct tags.
// It looks for the quicbaas:"codec=xxx" tag on struct fields.
// Returns empty string if no codec preference is found.
// Returns an error only for invalid entity types.
func (r *CodecResolver) DetectCodec(entity interface{}) (string, error) {
	if entity == nil {
		return "", &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.DetectCodec",
			Err:  ErrNilEntity,
		}
	}

	t := reflect.TypeOf(entity)
	if t == nil {
		return "", &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.DetectCodec",
			Err:  ErrNilEntity,
		}
	}

	// Dereference pointer
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		// Non-struct types have no codec preference
		return "", nil
	}

	// Scan fields for quicbaas tag with codec specification
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get(codecTagName)
		if tag == "" {
			continue
		}

		codecName := parseCodecFromTag(tag)
		if codecName != "" {
			return codecName, nil
		}
	}

	return "", nil
}

// Encode encodes an entity using the named codec.
// If codecName is empty, the system default codec is used.
func (r *CodecResolver) Encode(entity interface{}, codecName string) ([]byte, error) {
	if entity == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.Encode",
			Err:  ErrNilEntity,
		}
	}

	name := codecName
	if name == "" {
		val, ok := r.defaultCodec.Load().(string)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "CodecResolver.Encode",
				Err:  &DefaultCodecNotFoundError{Name: "unknown"},
			}
		}
		name = val
	}

	c, err := r.GetCodec(name)
	if err != nil {
		return nil, err
	}

	data, err := c.Encode(entity)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CodecResolver.Encode",
			Err:  &EncodeError{Codec: name, Cause: err},
		}
	}

	return data, nil
}

// Decode decodes data into an entity using the named codec.
// If codecName is empty, the system default codec is used.
func (r *CodecResolver) Decode(data []byte, entity interface{}, codecName string) error {
	if len(data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.Decode",
			Err:  ErrEmptyData,
		}
	}

	if entity == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.Decode",
			Err:  ErrNilEntity,
		}
	}

	name := codecName
	if name == "" {
		val, ok := r.defaultCodec.Load().(string)
		if !ok {
			return &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "CodecResolver.Decode",
				Err:  ErrNilCodecName,
			}
		}
		name = val
	}

	c, err := r.GetCodec(name)
	if err != nil {
		return err
	}

	if err := c.Decode(data, entity); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CodecResolver.Decode",
			Err:  &DecodeError{Codec: name, Cause: err},
		}
	}

	return nil
}

// ListCodecs returns the names of all registered codecs.
func (r *CodecResolver) ListCodecs() []string {
	var names []string
	r.codecs.Range(func(key, value interface{}) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// DefaultCodecName returns the system default codec name.
func (r *CodecResolver) DefaultCodecName() string {
	name, _ := r.defaultCodec.Load().(string)
	return name
}

// SetDefaultCodec changes the system default codec.
// Returns an error if the codec is not registered.
func (r *CodecResolver) SetDefaultCodec(name string) error {
	if name == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "CodecResolver.SetDefaultCodec",
			Err:  ErrEmptyCodecName,
		}
	}

	// Verify codec exists
	_, err := r.GetCodec(name)
	if err != nil {
		return err
	}

	r.defaultCodec.Store(name)
	return nil
}

// parseCodecFromTag extracts the codec name from a quicbaas tag value.
// Tag format: "codec=json" or "codec=msgpack,other=value".
func parseCodecFromTag(tag string) string {
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, codecTagKey+"=") {
			return strings.TrimPrefix(part, codecTagKey+"=")
		}
	}
	return ""
}

// jsonPushCodec implements CodecInterface using encoding/json.
type jsonPushCodec struct{}

// Name returns the codec name.
func (c *jsonPushCodec) Name() string {
	return CodecJSON
}

// Encode serializes an entity to JSON bytes.
func (c *jsonPushCodec) Encode(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "jsonPushCodec.Encode",
			Err:  ErrNilEntity,
		}
	}

	data, err := json.Marshal(entity)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "jsonPushCodec.Encode",
			Err:  &EncodeError{Codec: CodecJSON, Cause: err},
		}
	}

	return data, nil
}

// Decode deserializes JSON bytes into an entity.
func (c *jsonPushCodec) Decode(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "jsonPushCodec.Decode",
			Err:  ErrEmptyData,
		}
	}

	if entity == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "jsonPushCodec.Decode",
			Err:  ErrNilEntity,
		}
	}

	// Verify entity is a pointer
	if reflect.TypeOf(entity).Kind() != reflect.Pointer {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "jsonPushCodec.Decode",
			Err:  &NotPointerError{Type: reflect.TypeOf(entity).String()},
		}
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "jsonPushCodec.Decode",
			Err:  &DecodeError{Codec: CodecJSON, Cause: err},
		}
	}

	return nil
}

// msgpackPushCodec implements CodecInterface using hashicorp/go-msgpack.
type msgpackPushCodec struct{}

// Name returns the codec name.
func (c *msgpackPushCodec) Name() string {
	return CodecMsgpack
}

// Encode serializes an entity to MessagePack bytes.
func (c *msgpackPushCodec) Encode(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "msgpackPushCodec.Encode",
			Err:  ErrNilEntity,
		}
	}

	var buf []byte
	enc := codec.NewEncoderBytes(&buf, &codec.MsgpackHandle{})

	if err := enc.Encode(entity); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "msgpackPushCodec.Encode",
			Err:  &EncodeError{Codec: CodecMsgpack, Cause: err},
		}
	}

	return buf, nil
}

// Decode deserializes MessagePack bytes into an entity.
func (c *msgpackPushCodec) Decode(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "msgpackPushCodec.Decode",
			Err:  ErrEmptyData,
		}
	}

	if entity == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "msgpackPushCodec.Decode",
			Err:  ErrNilEntity,
		}
	}

	// Verify entity is a pointer
	if reflect.TypeOf(entity).Kind() != reflect.Pointer {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "msgpackPushCodec.Decode",
			Err:  &NotPointerError{Type: reflect.TypeOf(entity).String()},
		}
	}

	dec := codec.NewDecoderBytes(data, &codec.MsgpackHandle{})

	if err := dec.Decode(entity); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "msgpackPushCodec.Decode",
			Err:  &DecodeError{Codec: CodecMsgpack, Cause: err},
		}
	}

	return nil
}

// Codec-related sentinel errors.
var (
	// ErrNilCodec indicates a nil codec was provided.
	ErrNilCodec = &NilCodecError{}

	// ErrEmptyCodecName indicates an empty codec name was provided.
	ErrEmptyCodecName = &EmptyCodecNameError{}

	// ErrNilCodecName indicates a nil or invalid codec name was loaded.
	ErrNilCodecName = &NilCodecNameError{}

	// ErrNilEntity indicates a nil entity was provided.
	ErrNilEntity = &NilEntityError{}

	// ErrEmptyData indicates empty data was provided for decoding.
	ErrEmptyData = &EmptyDataError{}
)

// NilCodecError represents an error when a nil codec is provided.
type NilCodecError struct{}

// Error implements the error interface.
func (e *NilCodecError) Error() string {
	return "codec is nil"
}

// EmptyCodecNameError represents an error when an empty codec name is provided.
type EmptyCodecNameError struct{}

// Error implements the error interface.
func (e *EmptyCodecNameError) Error() string {
	return "codec name is empty"
}

// NilCodecNameError represents an error when the default codec name cannot be loaded.
type NilCodecNameError struct{}

// Error implements the error interface.
func (e *NilCodecNameError) Error() string {
	return "default codec name is nil or invalid"
}

// NilEntityError represents an error when a nil entity is provided.
type NilEntityError struct{}

// Error implements the error interface.
func (e *NilEntityError) Error() string {
	return "entity is nil"
}

// EmptyDataError represents an error when empty data is provided for decoding.
type EmptyDataError struct{}

// Error implements the error interface.
func (e *EmptyDataError) Error() string {
	return "data is empty"
}

// CodecTypeAssertionError represents a type assertion failure for a codec.
type CodecTypeAssertionError struct {
	Name string
}

// Error implements the error interface.
func (e *CodecTypeAssertionError) Error() string {
	return "codec type assertion failed for: " + e.Name
}

// DefaultCodecNotFoundError represents an error when the default codec is not found.
type DefaultCodecNotFoundError struct {
	Name string
}

// Error implements the error interface.
func (e *DefaultCodecNotFoundError) Error() string {
	return "default codec not found: " + e.Name
}

// NotPointerError represents an error when a non-pointer is provided for decoding.
type NotPointerError struct {
	Type string
}

// Error implements the error interface.
func (e *NotPointerError) Error() string {
	return "entity must be a pointer, got: " + e.Type
}

// EncodeError represents an encoding failure.
type EncodeError struct {
	Codec string
	Cause error
}

// Error implements the error interface.
func (e *EncodeError) Error() string {
	if e.Cause != nil {
		return "failed to encode with " + e.Codec + ": " + e.Cause.Error()
	}
	return "failed to encode with " + e.Codec
}

// Unwrap returns the underlying error.
func (e *EncodeError) Unwrap() error {
	return e.Cause
}

// DecodeError represents a decoding failure.
type DecodeError struct {
	Codec string
	Cause error
}

// Error implements the error interface.
func (e *DecodeError) Error() string {
	if e.Cause != nil {
		return "failed to decode with " + e.Codec + ": " + e.Cause.Error()
	}
	return "failed to decode with " + e.Codec
}

// Unwrap returns the underlying error.
func (e *DecodeError) Unwrap() error {
	return e.Cause
}
