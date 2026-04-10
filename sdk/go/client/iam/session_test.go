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

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestNewSessionClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewSessionClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestSessionClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != sessionServiceName {
					t.Errorf("expected service %s, got %s", sessionServiceName, service)
				}
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getSessionRequest)
				if !ok {
					t.Error("expected getSessionRequest")
				}
				if getReq.SessionID != "session-123" {
					t.Errorf("expected session ID session-123, got %s", getReq.SessionID)
				}
				return &Session{
					ID:          "session-123",
					UserID:      "user-456",
					TenantID:    "tenant-789",
					CreatedAt:   now,
					LastActive:  now,
					ExpiresAt:   now.Add(24 * time.Hour),
					MFAVerified: true,
					IPAddress:   "192.168.1.1",
					UserAgent:   "Mozilla/5.0",
					AuthMethod:  "password",
				}, nil
			},
		}

		client := NewSessionClient(mt)
		session, err := client.Get(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.ID != "session-123" {
			t.Errorf("expected session ID session-123, got %s", session.ID)
		}
		if session.UserID != "user-456" {
			t.Errorf("expected user ID user-456, got %s", session.UserID)
		}
		if !session.MFAVerified {
			t.Error("expected MFA to be verified")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		_, err := client.Get(nil, "session-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty session ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		_, err := client.Get(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewSessionClient(mt)
		_, err := client.Get(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewInternal("test", "database error")
			},
		}

		client := NewSessionClient(mt)
		_, err := client.Get(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error from transport")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewSessionClient(mt)
		_, err := client.Get(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestSessionClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				listReq, ok := req.(*listSessionsRequestInternal)
				if !ok {
					t.Error("expected listSessionsRequestInternal")
				}
				if listReq.Request.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", listReq.Request.TenantID)
				}
				if listReq.Request.UserID != "user-456" {
					t.Errorf("expected user ID user-456, got %s", listReq.Request.UserID)
				}
				return &ListSessionsResponse{
					Sessions: []Session{
						{ID: "session-1", UserID: "user-456"},
						{ID: "session-2", UserID: "user-456"},
					},
					TotalCount: 2,
					HasMore:    false,
				}, nil
			},
		}

		client := NewSessionClient(mt)
		resp, err := client.List(context.Background(), &ListSessionsRequest{
			TenantID: "tenant-123",
			UserID:   "user-456",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Sessions) != 2 {
			t.Errorf("expected 2 sessions, got %d", len(resp.Sessions))
		}
		if resp.TotalCount != 2 {
			t.Errorf("expected total count 2, got %d", resp.TotalCount)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		_, err := client.List(nil, &ListSessionsRequest{
			TenantID: "tenant-123",
			UserID:   "user-456",
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		_, err := client.List(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		_, err := client.List(context.Background(), &ListSessionsRequest{
			UserID: "user-456",
		})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		_, err := client.List(context.Background(), &ListSessionsRequest{
			TenantID: "tenant-123",
		})
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("limit defaults to 100", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				listReq := req.(*listSessionsRequestInternal)
				if listReq.Request.Limit != 100 {
					t.Errorf("expected default limit 100, got %d", listReq.Request.Limit)
				}
				return &ListSessionsResponse{Sessions: []Session{}}, nil
			},
		}

		client := NewSessionClient(mt)
		_, err := client.List(context.Background(), &ListSessionsRequest{
			TenantID: "tenant-123",
			UserID:   "user-456",
			Limit:    0,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("limit capped at 1000", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				listReq := req.(*listSessionsRequestInternal)
				if listReq.Request.Limit != 1000 {
					t.Errorf("expected max limit 1000, got %d", listReq.Request.Limit)
				}
				return &ListSessionsResponse{Sessions: []Session{}}, nil
			},
		}

		client := NewSessionClient(mt)
		_, err := client.List(context.Background(), &ListSessionsRequest{
			TenantID: "tenant-123",
			UserID:   "user-456",
			Limit:    5000,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewSessionClient(mt)
		resp, err := client.List(context.Background(), &ListSessionsRequest{
			TenantID: "tenant-123",
			UserID:   "user-456",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Sessions) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(resp.Sessions))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewSessionClient(mt)
		_, err := client.List(context.Background(), &ListSessionsRequest{
			TenantID: "tenant-123",
			UserID:   "user-456",
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestSessionClient_Revoke(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Revoke" {
					t.Errorf("expected method Revoke, got %s", method)
				}
				revokeReq, ok := req.(*revokeSessionRequest)
				if !ok {
					t.Error("expected revokeSessionRequest")
				}
				if revokeReq.SessionID != "session-123" {
					t.Errorf("expected session ID session-123, got %s", revokeReq.SessionID)
				}
				return nil, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.Revoke(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &revokeSessionResponse{}, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.Revoke(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.Revoke(nil, "session-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty session ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.Revoke(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "session not found")
			},
		}

		client := NewSessionClient(mt)
		err := client.Revoke(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("any response type accepted for void operation", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "any-value", nil
			},
		}

		client := NewSessionClient(mt)
		err := client.Revoke(context.Background(), "session-123")
		if err != nil {
			t.Errorf("void operation should accept any non-error response, got %v", err)
		}
	})
}

func TestSessionClient_RevokeAll(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RevokeAll" {
					t.Errorf("expected method RevokeAll, got %s", method)
				}
				revokeReq, ok := req.(*revokeAllSessionsRequest)
				if !ok {
					t.Error("expected revokeAllSessionsRequest")
				}
				if revokeReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", revokeReq.TenantID)
				}
				if revokeReq.UserID != "user-456" {
					t.Errorf("expected user ID user-456, got %s", revokeReq.UserID)
				}
				return nil, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.RevokeAll(context.Background(), "tenant-123", "user-456")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &revokeAllSessionsResponse{RevokedCount: 5}, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.RevokeAll(context.Background(), "tenant-123", "user-456")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.RevokeAll(nil, "tenant-123", "user-456")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.RevokeAll(context.Background(), "", "user-456")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.RevokeAll(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("any response type accepted for void operation", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "any-value", nil
			},
		}

		client := NewSessionClient(mt)
		err := client.RevokeAll(context.Background(), "tenant-123", "user-456")
		if err != nil {
			t.Errorf("void operation should accept any non-error response, got %v", err)
		}
	})
}

func TestSessionClient_RevokeOthers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RevokeOthers" {
					t.Errorf("expected method RevokeOthers, got %s", method)
				}
				revokeReq, ok := req.(*revokeOthersRequest)
				if !ok {
					t.Error("expected revokeOthersRequest")
				}
				if revokeReq.KeepSessionID != "session-123" {
					t.Errorf("expected keep session ID session-123, got %s", revokeReq.KeepSessionID)
				}
				return nil, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.RevokeOthers(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &revokeOthersResponse{RevokedCount: 3}, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.RevokeOthers(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.RevokeOthers(nil, "session-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty keep session ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.RevokeOthers(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty keep session ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("any response type accepted for void operation", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "any-value", nil
			},
		}

		client := NewSessionClient(mt)
		err := client.RevokeOthers(context.Background(), "session-123")
		if err != nil {
			t.Errorf("void operation should accept any non-error response, got %v", err)
		}
	})
}

func TestSessionClient_Touch(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Touch" {
					t.Errorf("expected method Touch, got %s", method)
				}
				touchReq, ok := req.(*touchSessionRequest)
				if !ok {
					t.Error("expected touchSessionRequest")
				}
				if touchReq.SessionID != "session-123" {
					t.Errorf("expected session ID session-123, got %s", touchReq.SessionID)
				}
				if touchReq.Timestamp.IsZero() {
					t.Error("expected non-zero timestamp")
				}
				return nil, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.Touch(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &touchSessionResponse{}, nil
			},
		}

		client := NewSessionClient(mt)
		err := client.Touch(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.Touch(nil, "session-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty session ID", func(t *testing.T) {
		client := NewSessionClient(&mockTransport{})
		err := client.Touch(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "session not found")
			},
		}

		client := NewSessionClient(mt)
		err := client.Touch(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("any response type accepted for void operation", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "any-value", nil
			},
		}

		client := NewSessionClient(mt)
		err := client.Touch(context.Background(), "session-123")
		if err != nil {
			t.Errorf("void operation should accept any non-error response, got %v", err)
		}
	})
}
