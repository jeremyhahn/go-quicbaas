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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     errors.ErrorCode
		wantCode errors.ErrorCode
	}{
		{
			name:     "ErrCodeSubprotocolNotSupported",
			code:     ErrCodeSubprotocolNotSupported,
			wantCode: 1200,
		},
		{
			name:     "ErrCodeUpgradeFailed",
			code:     ErrCodeUpgradeFailed,
			wantCode: 1201,
		},
		{
			name:     "ErrCodeAuthenticationFailed",
			code:     ErrCodeAuthenticationFailed,
			wantCode: 1202,
		},
		{
			name:     "ErrCodeSessionNotFound",
			code:     ErrCodeSessionNotFound,
			wantCode: 1203,
		},
		{
			name:     "ErrCodeSessionClosed",
			code:     ErrCodeSessionClosed,
			wantCode: 1204,
		},
		{
			name:     "ErrCodeMessageTooLarge",
			code:     ErrCodeMessageTooLarge,
			wantCode: 1205,
		},
		{
			name:     "ErrCodeWriteTimeout",
			code:     ErrCodeWriteTimeout,
			wantCode: 1206,
		},
		{
			name:     "ErrCodeReadTimeout",
			code:     ErrCodeReadTimeout,
			wantCode: 1207,
		},
		{
			name:     "ErrCodeServerClosed",
			code:     ErrCodeServerClosed,
			wantCode: 1208,
		},
		{
			name:     "ErrCodeInvalidMessage",
			code:     ErrCodeInvalidMessage,
			wantCode: 1209,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.wantCode {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.wantCode)
			}
		})
	}
}

func TestSubprotocolNotSupportedError(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		supported []string
		wantMsg   string
	}{
		{
			name:      "single supported",
			requested: "mqtt",
			supported: []string{"echo"},
			wantMsg:   "subprotocol not supported: mqtt",
		},
		{
			name:      "multiple supported",
			requested: "stomp",
			supported: []string{"echo", "mqtt"},
			wantMsg:   "subprotocol not supported: stomp",
		},
		{
			name:      "empty requested",
			requested: "",
			supported: []string{"echo"},
			wantMsg:   "subprotocol not supported: ",
		},
		{
			name:      "empty supported",
			requested: "mqtt",
			supported: []string{},
			wantMsg:   "subprotocol not supported: mqtt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &SubprotocolNotSupportedError{
				Requested: tt.requested,
				Supported: tt.supported,
			}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}

			// Verify fields are stored
			if err.Requested != tt.requested {
				t.Errorf("Requested = %q, want %q", err.Requested, tt.requested)
			}

			if len(err.Supported) != len(tt.supported) {
				t.Errorf("Supported length = %d, want %d", len(err.Supported), len(tt.supported))
			}
		})
	}
}

func TestUpgradeFailedError(t *testing.T) {
	tests := []struct {
		name    string
		cause   string
		wantMsg string
	}{
		{
			name:    "connection refused",
			cause:   "connection refused",
			wantMsg: "websocket upgrade failed: connection refused",
		},
		{
			name:    "protocol error",
			cause:   "invalid protocol",
			wantMsg: "websocket upgrade failed: invalid protocol",
		},
		{
			name:    "empty cause",
			cause:   "",
			wantMsg: "websocket upgrade failed: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &UpgradeFailedError{Cause: tt.cause}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestAuthenticationFailedError(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		wantMsg string
	}{
		{
			name:    "invalid token",
			reason:  "invalid token",
			wantMsg: "authentication failed: invalid token",
		},
		{
			name:    "expired token",
			reason:  "token expired",
			wantMsg: "authentication failed: token expired",
		},
		{
			name:    "empty reason",
			reason:  "",
			wantMsg: "authentication failed: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &AuthenticationFailedError{Reason: tt.reason}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestSessionNotFoundError(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantMsg   string
	}{
		{
			name:      "uuid session",
			sessionID: "550e8400-e29b-41d4-a716-446655440000",
			wantMsg:   "session not found: 550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:      "empty session ID",
			sessionID: "",
			wantMsg:   "session not found: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &SessionNotFoundError{SessionID: tt.sessionID}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestSessionClosedError(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantMsg   string
	}{
		{
			name:      "uuid session",
			sessionID: "550e8400-e29b-41d4-a716-446655440000",
			wantMsg:   "session closed: 550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:      "empty session ID",
			sessionID: "",
			wantMsg:   "session closed: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &SessionClosedError{SessionID: tt.sessionID}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestMessageTooLargeError(t *testing.T) {
	tests := []struct {
		name    string
		size    int64
		maxSize int64
		wantMsg string
	}{
		{
			name:    "slightly over",
			size:    1024*1024 + 1,
			maxSize: 1024 * 1024,
			wantMsg: "message too large",
		},
		{
			name:    "much larger",
			size:    10 * 1024 * 1024,
			maxSize: 1024 * 1024,
			wantMsg: "message too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &MessageTooLargeError{
				Size:    tt.size,
				MaxSize: tt.maxSize,
			}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}

			// Verify fields
			if err.Size != tt.size {
				t.Errorf("Size = %d, want %d", err.Size, tt.size)
			}

			if err.MaxSize != tt.maxSize {
				t.Errorf("MaxSize = %d, want %d", err.MaxSize, tt.maxSize)
			}
		})
	}
}

func TestWriteTimeoutError(t *testing.T) {
	err := &WriteTimeoutError{SessionID: "session-123"}

	if !strings.Contains(err.Error(), "write timeout") {
		t.Errorf("Error() = %q, want to contain 'write timeout'", err.Error())
	}

	if !strings.Contains(err.Error(), "session-123") {
		t.Errorf("Error() = %q, want to contain 'session-123'", err.Error())
	}
}

func TestReadTimeoutError(t *testing.T) {
	err := &ReadTimeoutError{SessionID: "session-456"}

	if !strings.Contains(err.Error(), "read timeout") {
		t.Errorf("Error() = %q, want to contain 'read timeout'", err.Error())
	}

	if !strings.Contains(err.Error(), "session-456") {
		t.Errorf("Error() = %q, want to contain 'session-456'", err.Error())
	}
}

func TestServerClosedError(t *testing.T) {
	err := &ServerClosedError{}

	if err.Error() != "websocket server closed" {
		t.Errorf("Error() = %q, want %q", err.Error(), "websocket server closed")
	}
}

func TestInvalidMessageError(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		wantMsg string
	}{
		{
			name:    "malformed json",
			reason:  "malformed JSON",
			wantMsg: "invalid message: malformed JSON",
		},
		{
			name:    "missing field",
			reason:  "missing required field",
			wantMsg: "invalid message: missing required field",
		},
		{
			name:    "empty reason",
			reason:  "",
			wantMsg: "invalid message: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &InvalidMessageError{Reason: tt.reason}

			if err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestNewSubprotocolNotSupportedError(t *testing.T) {
	err := NewSubprotocolNotSupportedError("server.upgrade", "mqtt", []string{"echo", "stomp"})

	if err.Code != ErrCodeSubprotocolNotSupported {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeSubprotocolNotSupported)
	}

	if err.Op != "server.upgrade" {
		t.Errorf("Op = %q, want %q", err.Op, "server.upgrade")
	}

	// Check context
	requested, ok := err.Context["requested"]
	if !ok || requested != "mqtt" {
		t.Errorf("Context[requested] = %v, want mqtt", requested)
	}

	// Check underlying error
	var sns *SubprotocolNotSupportedError
	if !stderrors.As(err, &sns) {
		t.Error("Should unwrap to SubprotocolNotSupportedError")
	}
}

func TestNewUpgradeFailedError(t *testing.T) {
	err := NewUpgradeFailedError("server.accept", "connection refused")

	if err.Code != ErrCodeUpgradeFailed {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeUpgradeFailed)
	}

	if err.Op != "server.accept" {
		t.Errorf("Op = %q, want %q", err.Op, "server.accept")
	}

	cause, ok := err.Context["cause"]
	if !ok || cause != "connection refused" {
		t.Errorf("Context[cause] = %v, want connection refused", cause)
	}
}

func TestNewAuthenticationFailedError(t *testing.T) {
	err := NewAuthenticationFailedError("auth.validate", "invalid token")

	if err.Code != ErrCodeAuthenticationFailed {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeAuthenticationFailed)
	}

	if err.Op != "auth.validate" {
		t.Errorf("Op = %q, want %q", err.Op, "auth.validate")
	}

	reason, ok := err.Context["reason"]
	if !ok || reason != "invalid token" {
		t.Errorf("Context[reason] = %v, want invalid token", reason)
	}
}

func TestNewSessionNotFoundError(t *testing.T) {
	err := NewSessionNotFoundError("session.get", "session-123")

	if err.Code != ErrCodeSessionNotFound {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeSessionNotFound)
	}

	if err.Op != "session.get" {
		t.Errorf("Op = %q, want %q", err.Op, "session.get")
	}

	sessionID, ok := err.Context["session_id"]
	if !ok || sessionID != "session-123" {
		t.Errorf("Context[session_id] = %v, want session-123", sessionID)
	}
}

func TestNewSessionClosedError(t *testing.T) {
	err := NewSessionClosedError("session.write", "session-456")

	if err.Code != ErrCodeSessionClosed {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeSessionClosed)
	}

	if err.Op != "session.write" {
		t.Errorf("Op = %q, want %q", err.Op, "session.write")
	}

	sessionID, ok := err.Context["session_id"]
	if !ok || sessionID != "session-456" {
		t.Errorf("Context[session_id] = %v, want session-456", sessionID)
	}
}

func TestNewMessageTooLargeError(t *testing.T) {
	err := NewMessageTooLargeError("message.read", 2*1024*1024, 1024*1024)

	if err.Code != ErrCodeMessageTooLarge {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeMessageTooLarge)
	}

	size, ok := err.Context["size"]
	if !ok || size != int64(2*1024*1024) {
		t.Errorf("Context[size] = %v, want %d", size, 2*1024*1024)
	}

	maxSize, ok := err.Context["max_size"]
	if !ok || maxSize != int64(1024*1024) {
		t.Errorf("Context[max_size] = %v, want %d", maxSize, 1024*1024)
	}
}

func TestNewWriteTimeoutError(t *testing.T) {
	err := NewWriteTimeoutError("session.write", "session-789")

	if err.Code != ErrCodeWriteTimeout {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeWriteTimeout)
	}

	sessionID, ok := err.Context["session_id"]
	if !ok || sessionID != "session-789" {
		t.Errorf("Context[session_id] = %v, want session-789", sessionID)
	}
}

func TestNewReadTimeoutError(t *testing.T) {
	err := NewReadTimeoutError("session.read", "session-abc")

	if err.Code != ErrCodeReadTimeout {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeReadTimeout)
	}

	sessionID, ok := err.Context["session_id"]
	if !ok || sessionID != "session-abc" {
		t.Errorf("Context[session_id] = %v, want session-abc", sessionID)
	}
}

func TestNewServerClosedError(t *testing.T) {
	err := NewServerClosedError("server.handle")

	if err.Code != ErrCodeServerClosed {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeServerClosed)
	}

	if err.Op != "server.handle" {
		t.Errorf("Op = %q, want %q", err.Op, "server.handle")
	}
}

func TestNewInvalidMessageError(t *testing.T) {
	err := NewInvalidMessageError("message.parse", "invalid JSON syntax")

	if err.Code != ErrCodeInvalidMessage {
		t.Errorf("Code = %d, want %d", err.Code, ErrCodeInvalidMessage)
	}

	reason, ok := err.Context["reason"]
	if !ok || reason != "invalid JSON syntax" {
		t.Errorf("Context[reason] = %v, want invalid JSON syntax", reason)
	}
}

func TestIsSubprotocolNotSupported(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &SubprotocolNotSupportedError{Requested: "mqtt"},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewSubprotocolNotSupportedError("op", "mqtt", []string{}),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewUpgradeFailedError("op", "cause"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSubprotocolNotSupported(tt.err); got != tt.want {
				t.Errorf("IsSubprotocolNotSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUpgradeFailed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &UpgradeFailedError{Cause: "test"},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewUpgradeFailedError("op", "cause"),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewSessionNotFoundError("op", "id"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUpgradeFailed(tt.err); got != tt.want {
				t.Errorf("IsUpgradeFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAuthenticationFailed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &AuthenticationFailedError{Reason: "test"},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewAuthenticationFailedError("op", "reason"),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewSessionClosedError("op", "id"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthenticationFailed(tt.err); got != tt.want {
				t.Errorf("IsAuthenticationFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSessionNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &SessionNotFoundError{SessionID: "test"},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewSessionNotFoundError("op", "id"),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewServerClosedError("op"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSessionNotFound(tt.err); got != tt.want {
				t.Errorf("IsSessionNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSessionClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &SessionClosedError{SessionID: "test"},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewSessionClosedError("op", "id"),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewMessageTooLargeError("op", 100, 50),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSessionClosed(tt.err); got != tt.want {
				t.Errorf("IsSessionClosed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMessageTooLarge(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &MessageTooLargeError{Size: 100, MaxSize: 50},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewMessageTooLargeError("op", 100, 50),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewReadTimeoutError("op", "id"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMessageTooLarge(tt.err); got != tt.want {
				t.Errorf("IsMessageTooLarge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsServerClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "direct error type",
			err:  &ServerClosedError{},
			want: true,
		},
		{
			name: "dragon error with correct code",
			err:  NewServerClosedError("op"),
			want: true,
		},
		{
			name: "dragon error with different code",
			err:  NewInvalidMessageError("op", "reason"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  stderrors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsServerClosed(tt.err); got != tt.want {
				t.Errorf("IsServerClosed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQBErrorUnwrap(t *testing.T) {
	// Test that errors.As works through QBError
	underlyingErr := &SessionNotFoundError{SessionID: "test-session"}
	qbErr := NewSessionNotFoundError("op", "test-session")

	var snf *SessionNotFoundError
	if !stderrors.As(qbErr, &snf) {
		t.Error("errors.As should find SessionNotFoundError")
	}

	if snf.SessionID != underlyingErr.SessionID {
		t.Errorf("SessionID = %q, want %q", snf.SessionID, underlyingErr.SessionID)
	}
}

func TestQBErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		err     *errors.QBError
		wantSub string
	}{
		{
			name:    "subprotocol not supported",
			err:     NewSubprotocolNotSupportedError("server.upgrade", "mqtt", []string{"echo"}),
			wantSub: "subprotocol not supported: mqtt",
		},
		{
			name:    "upgrade failed",
			err:     NewUpgradeFailedError("server.accept", "connection refused"),
			wantSub: "websocket upgrade failed: connection refused",
		},
		{
			name:    "auth failed",
			err:     NewAuthenticationFailedError("auth.validate", "invalid"),
			wantSub: "authentication failed: invalid",
		},
		{
			name:    "session not found",
			err:     NewSessionNotFoundError("session.get", "abc"),
			wantSub: "session not found: abc",
		},
		{
			name:    "session closed",
			err:     NewSessionClosedError("session.write", "xyz"),
			wantSub: "session closed: xyz",
		},
		{
			name:    "message too large",
			err:     NewMessageTooLargeError("message.read", 100, 50),
			wantSub: "message too large",
		},
		{
			name:    "write timeout",
			err:     NewWriteTimeoutError("session.write", "sess"),
			wantSub: "write timeout",
		},
		{
			name:    "read timeout",
			err:     NewReadTimeoutError("session.read", "sess"),
			wantSub: "read timeout",
		},
		{
			name:    "server closed",
			err:     NewServerClosedError("server.handle"),
			wantSub: "websocket server closed",
		},
		{
			name:    "invalid message",
			err:     NewInvalidMessageError("message.parse", "bad json"),
			wantSub: "invalid message: bad json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantSub) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantSub)
			}
		})
	}
}
