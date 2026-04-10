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

package websocket

import (
	stderrors "errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// WebSocket-specific error codes.
const (
	// ErrCodeSubprotocolNotSupported indicates the requested subprotocol is not supported.
	ErrCodeSubprotocolNotSupported errors.ErrorCode = 1200 + iota

	// ErrCodeUpgradeFailed indicates the WebSocket upgrade failed.
	ErrCodeUpgradeFailed

	// ErrCodeAuthenticationFailed indicates authentication failed.
	ErrCodeAuthenticationFailed

	// ErrCodeSessionNotFound indicates the session was not found.
	ErrCodeSessionNotFound

	// ErrCodeSessionClosed indicates the session is closed.
	ErrCodeSessionClosed

	// ErrCodeMessageTooLarge indicates the message exceeds the size limit.
	ErrCodeMessageTooLarge

	// ErrCodeWriteTimeout indicates a write operation timed out.
	ErrCodeWriteTimeout

	// ErrCodeReadTimeout indicates a read operation timed out.
	ErrCodeReadTimeout

	// ErrCodeServerClosed indicates the server has been closed.
	ErrCodeServerClosed

	// ErrCodeInvalidMessage indicates an invalid message was received.
	ErrCodeInvalidMessage
)

// SubprotocolNotSupportedError indicates the requested subprotocol is not supported.
type SubprotocolNotSupportedError struct {
	Requested string
	Supported []string
}

func (e *SubprotocolNotSupportedError) Error() string {
	return "subprotocol not supported: " + e.Requested
}

// UpgradeFailedError indicates the WebSocket upgrade failed.
type UpgradeFailedError struct {
	Cause string
}

func (e *UpgradeFailedError) Error() string {
	return "websocket upgrade failed: " + e.Cause
}

// AuthenticationFailedError indicates authentication failed.
type AuthenticationFailedError struct {
	Reason string
}

func (e *AuthenticationFailedError) Error() string {
	return "authentication failed: " + e.Reason
}

// SessionNotFoundError indicates the session was not found.
type SessionNotFoundError struct {
	SessionID string
}

func (e *SessionNotFoundError) Error() string {
	return "session not found: " + e.SessionID
}

// SessionClosedError indicates the session is closed.
type SessionClosedError struct {
	SessionID string
}

func (e *SessionClosedError) Error() string {
	return "session closed: " + e.SessionID
}

// MessageTooLargeError indicates the message exceeds the size limit.
type MessageTooLargeError struct {
	Size    int64
	MaxSize int64
}

func (e *MessageTooLargeError) Error() string {
	return "message too large"
}

// WriteTimeoutError indicates a write operation timed out.
type WriteTimeoutError struct {
	SessionID string
}

func (e *WriteTimeoutError) Error() string {
	return "write timeout for session: " + e.SessionID
}

// ReadTimeoutError indicates a read operation timed out.
type ReadTimeoutError struct {
	SessionID string
}

func (e *ReadTimeoutError) Error() string {
	return "read timeout for session: " + e.SessionID
}

// ServerClosedError indicates the server has been closed.
type ServerClosedError struct{}

func (e *ServerClosedError) Error() string {
	return "websocket server closed"
}

// InvalidMessageError indicates an invalid message was received.
type InvalidMessageError struct {
	Reason string
}

func (e *InvalidMessageError) Error() string {
	return "invalid message: " + e.Reason
}

// NewSubprotocolNotSupportedError creates a new DragonError for unsupported subprotocol.
func NewSubprotocolNotSupportedError(op, requested string, supported []string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeSubprotocolNotSupported,
		Op:   op,
		Err: &SubprotocolNotSupportedError{
			Requested: requested,
			Supported: supported,
		},
		Context: map[string]interface{}{
			"requested": requested,
			"supported": supported,
		},
	}
}

// NewUpgradeFailedError creates a new DragonError for upgrade failure.
func NewUpgradeFailedError(op, cause string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeUpgradeFailed,
		Op:   op,
		Err: &UpgradeFailedError{
			Cause: cause,
		},
		Context: map[string]interface{}{
			"cause": cause,
		},
	}
}

// NewAuthenticationFailedError creates a new DragonError for auth failure.
func NewAuthenticationFailedError(op, reason string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeAuthenticationFailed,
		Op:   op,
		Err: &AuthenticationFailedError{
			Reason: reason,
		},
		Context: map[string]interface{}{
			"reason": reason,
		},
	}
}

// NewSessionNotFoundError creates a new DragonError for session not found.
func NewSessionNotFoundError(op, sessionID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeSessionNotFound,
		Op:   op,
		Err: &SessionNotFoundError{
			SessionID: sessionID,
		},
		Context: map[string]interface{}{
			"session_id": sessionID,
		},
	}
}

// NewSessionClosedError creates a new DragonError for closed session.
func NewSessionClosedError(op, sessionID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeSessionClosed,
		Op:   op,
		Err: &SessionClosedError{
			SessionID: sessionID,
		},
		Context: map[string]interface{}{
			"session_id": sessionID,
		},
	}
}

// NewMessageTooLargeError creates a new DragonError for oversized message.
func NewMessageTooLargeError(op string, size, maxSize int64) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeMessageTooLarge,
		Op:   op,
		Err: &MessageTooLargeError{
			Size:    size,
			MaxSize: maxSize,
		},
		Context: map[string]interface{}{
			"size":     size,
			"max_size": maxSize,
		},
	}
}

// NewWriteTimeoutError creates a new DragonError for write timeout.
func NewWriteTimeoutError(op, sessionID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeWriteTimeout,
		Op:   op,
		Err: &WriteTimeoutError{
			SessionID: sessionID,
		},
		Context: map[string]interface{}{
			"session_id": sessionID,
		},
	}
}

// NewReadTimeoutError creates a new DragonError for read timeout.
func NewReadTimeoutError(op, sessionID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeReadTimeout,
		Op:   op,
		Err: &ReadTimeoutError{
			SessionID: sessionID,
		},
		Context: map[string]interface{}{
			"session_id": sessionID,
		},
	}
}

// NewServerClosedError creates a new DragonError for server closed.
func NewServerClosedError(op string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeServerClosed,
		Op:   op,
		Err:  &ServerClosedError{},
	}
}

// NewInvalidMessageError creates a new DragonError for invalid message.
func NewInvalidMessageError(op, reason string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeInvalidMessage,
		Op:   op,
		Err: &InvalidMessageError{
			Reason: reason,
		},
		Context: map[string]interface{}{
			"reason": reason,
		},
	}
}

// IsSubprotocolNotSupported returns true if the error is a subprotocol not supported error.
func IsSubprotocolNotSupported(err error) bool {
	if err == nil {
		return false
	}
	var sns *SubprotocolNotSupportedError
	if stderrors.As(err, &sns) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeSubprotocolNotSupported
	}
	return false
}

// IsUpgradeFailed returns true if the error is an upgrade failed error.
func IsUpgradeFailed(err error) bool {
	if err == nil {
		return false
	}
	var uf *UpgradeFailedError
	if stderrors.As(err, &uf) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeUpgradeFailed
	}
	return false
}

// IsAuthenticationFailed returns true if the error is an authentication failed error.
func IsAuthenticationFailed(err error) bool {
	if err == nil {
		return false
	}
	var af *AuthenticationFailedError
	if stderrors.As(err, &af) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeAuthenticationFailed
	}
	return false
}

// IsSessionNotFound returns true if the error is a session not found error.
func IsSessionNotFound(err error) bool {
	if err == nil {
		return false
	}
	var snf *SessionNotFoundError
	if stderrors.As(err, &snf) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeSessionNotFound
	}
	return false
}

// IsSessionClosed returns true if the error is a session closed error.
func IsSessionClosed(err error) bool {
	if err == nil {
		return false
	}
	var sc *SessionClosedError
	if stderrors.As(err, &sc) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeSessionClosed
	}
	return false
}

// IsMessageTooLarge returns true if the error is a message too large error.
func IsMessageTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var mtl *MessageTooLargeError
	if stderrors.As(err, &mtl) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeMessageTooLarge
	}
	return false
}

// IsServerClosed returns true if the error is a server closed error.
func IsServerClosed(err error) bool {
	if err == nil {
		return false
	}
	var sc *ServerClosedError
	if stderrors.As(err, &sc) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeServerClosed
	}
	return false
}
