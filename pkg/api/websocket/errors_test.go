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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestNewErrors checks, once per constructor, the wire code a client depends
// on, the operation, the context fields, the wrapped typed error and the
// message the wrapped error contributes to the QBError message.
func TestNewErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         *errors.QBError
		wantCode    errors.ErrorCode
		wantOp      string
		wantContext map[string]interface{}
		wantInner   error
		wantMsg     string
	}{
		{
			name:     "subprotocol not supported",
			err:      NewSubprotocolNotSupportedError("server.upgrade", "mqtt", []string{"echo", "stomp"}),
			wantCode: 1200,
			wantOp:   "server.upgrade",
			wantContext: map[string]interface{}{
				"requested": "mqtt",
				"supported": []string{"echo", "stomp"},
			},
			wantInner: &SubprotocolNotSupportedError{Requested: "mqtt", Supported: []string{"echo", "stomp"}},
			wantMsg:   "subprotocol not supported: mqtt",
		},
		{
			name:        "upgrade failed",
			err:         NewUpgradeFailedError("server.accept", "connection refused"),
			wantCode:    1201,
			wantOp:      "server.accept",
			wantContext: map[string]interface{}{"cause": "connection refused"},
			wantInner:   &UpgradeFailedError{Cause: "connection refused"},
			wantMsg:     "websocket upgrade failed: connection refused",
		},
		{
			name:        "authentication failed",
			err:         NewAuthenticationFailedError("auth.validate", "invalid token"),
			wantCode:    1202,
			wantOp:      "auth.validate",
			wantContext: map[string]interface{}{"reason": "invalid token"},
			wantInner:   &AuthenticationFailedError{Reason: "invalid token"},
			wantMsg:     "authentication failed: invalid token",
		},
		{
			name:        "session not found",
			err:         NewSessionNotFoundError("session.get", "session-123"),
			wantCode:    1203,
			wantOp:      "session.get",
			wantContext: map[string]interface{}{"session_id": "session-123"},
			wantInner:   &SessionNotFoundError{SessionID: "session-123"},
			wantMsg:     "session not found: session-123",
		},
		{
			name:        "session closed",
			err:         NewSessionClosedError("session.write", "session-456"),
			wantCode:    1204,
			wantOp:      "session.write",
			wantContext: map[string]interface{}{"session_id": "session-456"},
			wantInner:   &SessionClosedError{SessionID: "session-456"},
			wantMsg:     "session closed: session-456",
		},
		{
			name:     "message too large",
			err:      NewMessageTooLargeError("message.read", 2*1024*1024, 1024*1024),
			wantCode: 1205,
			wantOp:   "message.read",
			wantContext: map[string]interface{}{
				"size":     int64(2 * 1024 * 1024),
				"max_size": int64(1024 * 1024),
			},
			wantInner: &MessageTooLargeError{Size: 2 * 1024 * 1024, MaxSize: 1024 * 1024},
			wantMsg:   "message too large",
		},
		{
			name:        "write timeout",
			err:         NewWriteTimeoutError("session.write", "session-789"),
			wantCode:    1206,
			wantOp:      "session.write",
			wantContext: map[string]interface{}{"session_id": "session-789"},
			wantInner:   &WriteTimeoutError{SessionID: "session-789"},
			wantMsg:     "write timeout for session: session-789",
		},
		{
			name:        "read timeout",
			err:         NewReadTimeoutError("session.read", "session-abc"),
			wantCode:    1207,
			wantOp:      "session.read",
			wantContext: map[string]interface{}{"session_id": "session-abc"},
			wantInner:   &ReadTimeoutError{SessionID: "session-abc"},
			wantMsg:     "read timeout for session: session-abc",
		},
		{
			name:      "server closed",
			err:       NewServerClosedError("server.handle"),
			wantCode:  1208,
			wantOp:    "server.handle",
			wantInner: &ServerClosedError{},
			wantMsg:   "websocket server closed",
		},
		{
			name:        "invalid message",
			err:         NewInvalidMessageError("message.parse", "invalid JSON syntax"),
			wantCode:    1209,
			wantOp:      "message.parse",
			wantContext: map[string]interface{}{"reason": "invalid JSON syntax"},
			wantInner:   &InvalidMessageError{Reason: "invalid JSON syntax"},
			wantMsg:     "invalid message: invalid JSON syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", tt.err.Code, tt.wantCode)
			}
			if tt.err.Op != tt.wantOp {
				t.Errorf("Op = %q, want %q", tt.err.Op, tt.wantOp)
			}
			for key, want := range tt.wantContext {
				if got := tt.err.Context[key]; !reflect.DeepEqual(got, want) {
					t.Errorf("Context[%s] = %v, want %v", key, got, want)
				}
			}

			inner := stderrors.Unwrap(tt.err)
			if !reflect.DeepEqual(inner, tt.wantInner) {
				t.Fatalf("Unwrap() = %#v, want %#v", inner, tt.wantInner)
			}
			if inner.Error() != tt.wantMsg {
				t.Errorf("wrapped Error() = %q, want %q", inner.Error(), tt.wantMsg)
			}
			if !strings.Contains(tt.err.Error(), tt.wantMsg) {
				t.Errorf("Error() = %q, want to contain %q", tt.err.Error(), tt.wantMsg)
			}
		})
	}
}

// TestIsPredicates checks each Is* predicate against the typed error, the
// constructor's QBError, a QBError wrapped once more, a QBError that carries
// only the code, a QBError of another code, an unrelated error and nil.
func TestIsPredicates(t *testing.T) {
	tests := []struct {
		name  string
		is    func(error) bool
		match *errors.QBError
		other *errors.QBError
	}{
		{
			name:  "IsSubprotocolNotSupported",
			is:    IsSubprotocolNotSupported,
			match: NewSubprotocolNotSupportedError("op", "mqtt", []string{"echo"}),
			other: NewUpgradeFailedError("op", "cause"),
		},
		{
			name:  "IsUpgradeFailed",
			is:    IsUpgradeFailed,
			match: NewUpgradeFailedError("op", "cause"),
			other: NewAuthenticationFailedError("op", "reason"),
		},
		{
			name:  "IsAuthenticationFailed",
			is:    IsAuthenticationFailed,
			match: NewAuthenticationFailedError("op", "reason"),
			other: NewSessionNotFoundError("op", "session"),
		},
		{
			name:  "IsSessionNotFound",
			is:    IsSessionNotFound,
			match: NewSessionNotFoundError("op", "session"),
			other: NewSessionClosedError("op", "session"),
		},
		{
			name:  "IsSessionClosed",
			is:    IsSessionClosed,
			match: NewSessionClosedError("op", "session"),
			other: NewSessionNotFoundError("op", "session"),
		},
		{
			name:  "IsMessageTooLarge",
			is:    IsMessageTooLarge,
			match: NewMessageTooLargeError("op", 100, 50),
			other: NewInvalidMessageError("op", "reason"),
		},
		{
			name:  "IsServerClosed",
			is:    IsServerClosed,
			match: NewServerClosedError("op"),
			other: NewInvalidMessageError("op", "reason"),
		},
	}

	for _, tt := range tests {
		cases := []struct {
			name string
			err  error
			want bool
		}{
			{name: "typed error", err: stderrors.Unwrap(tt.match), want: true},
			{name: "constructor", err: tt.match, want: true},
			{name: "wrapped", err: fmt.Errorf("handler: %w", tt.match), want: true},
			{name: "code only", err: &errors.QBError{Code: tt.match.Code, Op: "op"}, want: true},
			{name: "other code", err: tt.other, want: false},
			{name: "unrelated", err: stderrors.New("some error"), want: false},
			{name: "nil", err: nil, want: false},
		}
		for _, c := range cases {
			t.Run(tt.name+"/"+c.name, func(t *testing.T) {
				if got := tt.is(c.err); got != c.want {
					t.Errorf("%s(%v) = %v, want %v", tt.name, c.err, got, c.want)
				}
			})
		}
	}
}
