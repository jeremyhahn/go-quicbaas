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

func TestNewAuditClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewAuditClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestAuditClient_Query(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != auditServiceName {
					t.Errorf("expected service %s, got %s", auditServiceName, service)
				}
				if method != "Query" {
					t.Errorf("expected method Query, got %s", method)
				}
				queryReq, ok := req.(*queryAuditRequest)
				if !ok {
					t.Error("expected queryAuditRequest")
				}
				if queryReq.Filter.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", queryReq.Filter.TenantID)
				}
				return &AuditQueryResponse{
					Events: []AuditEvent{
						{
							ID:        "event-1",
							TenantID:  "tenant-123",
							Timestamp: now,
							EventType: AuditEventTypeAuthentication,
							Action:    "login",
							Outcome:   AuditOutcomeSuccess,
						},
						{
							ID:        "event-2",
							TenantID:  "tenant-123",
							Timestamp: now,
							EventType: AuditEventTypeAuthorization,
							Action:    "access",
							Outcome:   AuditOutcomeDenied,
						},
					},
					TotalCount: 2,
					HasMore:    false,
				}, nil
			},
		}

		client := NewAuditClient(mt)
		resp, err := client.Query(context.Background(), &AuditFilter{
			TenantID: "tenant-123",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Events) != 2 {
			t.Errorf("expected 2 events, got %d", len(resp.Events))
		}
		if resp.TotalCount != 2 {
			t.Errorf("expected total count 2, got %d", resp.TotalCount)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		_, err := client.Query(nil, &AuditFilter{TenantID: "tenant-123"})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil filter", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		_, err := client.Query(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil filter")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("limit defaults to 100", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				queryReq := req.(*queryAuditRequest)
				if queryReq.Filter.Limit != 100 {
					t.Errorf("expected default limit 100, got %d", queryReq.Filter.Limit)
				}
				return &AuditQueryResponse{Events: []AuditEvent{}}, nil
			},
		}

		client := NewAuditClient(mt)
		_, err := client.Query(context.Background(), &AuditFilter{
			TenantID: "tenant-123",
			Limit:    0,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("limit capped at 1000", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				queryReq := req.(*queryAuditRequest)
				if queryReq.Filter.Limit != 1000 {
					t.Errorf("expected max limit 1000, got %d", queryReq.Filter.Limit)
				}
				return &AuditQueryResponse{Events: []AuditEvent{}}, nil
			},
		}

		client := NewAuditClient(mt)
		_, err := client.Query(context.Background(), &AuditFilter{
			TenantID: "tenant-123",
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

		client := NewAuditClient(mt)
		resp, err := client.Query(context.Background(), &AuditFilter{TenantID: "tenant-123"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Events) != 0 {
			t.Errorf("expected 0 events, got %d", len(resp.Events))
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "service unavailable")
			},
		}

		client := NewAuditClient(mt)
		_, err := client.Query(context.Background(), &AuditFilter{TenantID: "tenant-123"})
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsUnavailable(err) {
			t.Errorf("expected unavailable error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAuditClient(mt)
		_, err := client.Query(context.Background(), &AuditFilter{TenantID: "tenant-123"})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAuditClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getAuditEventRequest)
				if !ok {
					t.Error("expected getAuditEventRequest")
				}
				if getReq.EventID != "event-123" {
					t.Errorf("expected event ID event-123, got %s", getReq.EventID)
				}
				return &AuditEvent{
					ID:        "event-123",
					TenantID:  "tenant-123",
					Timestamp: now,
					EventType: AuditEventTypeUserManagement,
					Action:    "create_user",
					Outcome:   AuditOutcomeSuccess,
					Severity:  AuditSeverityInfo,
					Message:   "User created successfully",
				}, nil
			},
		}

		client := NewAuditClient(mt)
		event, err := client.Get(context.Background(), "tenant-123", "event-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event.ID != "event-123" {
			t.Errorf("expected event ID event-123, got %s", event.ID)
		}
		if event.Action != "create_user" {
			t.Errorf("expected action create_user, got %s", event.Action)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		_, err := client.Get(nil, "tenant-123", "event-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		_, err := client.Get(context.Background(), "", "event-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty event ID", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		_, err := client.Get(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty event ID")
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

		client := NewAuditClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "nonexistent")
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

		client := NewAuditClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "event-123")
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

		client := NewAuditClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "event-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAuditClient_Delete(t *testing.T) {
	t.Run("success with dry run", func(t *testing.T) {
		before := time.Now().Add(-30 * 24 * time.Hour)
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				deleteReq, ok := req.(*deleteAuditRequest)
				if !ok {
					t.Error("expected deleteAuditRequest")
				}
				if deleteReq.Request.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", deleteReq.Request.TenantID)
				}
				if !deleteReq.Request.DryRun {
					t.Error("expected dry run to be true")
				}
				return nil, nil
			},
		}

		client := NewAuditClient(mt)
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			TenantID: "tenant-123",
			Before:   before,
			DryRun:   true,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with confirm", func(t *testing.T) {
		before := time.Now().Add(-30 * 24 * time.Hour)
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &deleteAuditResponse{}, nil
			},
		}

		client := NewAuditClient(mt)
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			TenantID: "tenant-123",
			Before:   before,
			Confirm:  true,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		err := client.Delete(nil, &AuditDeleteRequest{
			TenantID: "tenant-123",
			Before:   time.Now(),
			Confirm:  true,
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		err := client.Delete(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			Before:  time.Now(),
			Confirm: true,
		})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("zero before time", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			TenantID: "tenant-123",
			Confirm:  true,
		})
		if err == nil {
			t.Error("expected error for zero before time")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("requires confirm or dry run", func(t *testing.T) {
		client := NewAuditClient(&mockTransport{})
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			TenantID: "tenant-123",
			Before:   time.Now(),
		})
		if err == nil {
			t.Error("expected error when neither confirm nor dry run is set")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnauthorized("test", "not authorized")
			},
		}

		client := NewAuditClient(mt)
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			TenantID: "tenant-123",
			Before:   time.Now(),
			Confirm:  true,
		})
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsUnauthorized(err) {
			t.Errorf("expected unauthorized error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAuditClient(mt)
		err := client.Delete(context.Background(), &AuditDeleteRequest{
			TenantID: "tenant-123",
			Before:   time.Now(),
			Confirm:  true,
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAuditEventType_String(t *testing.T) {
	tests := []struct {
		eventType AuditEventType
		expected  string
	}{
		{AuditEventTypeAuthentication, "authentication"},
		{AuditEventTypeAuthorization, "authorization"},
		{AuditEventTypeUserManagement, "user_management"},
		{AuditEventTypeRoleManagement, "role_management"},
		{AuditEventTypePolicyManagement, "policy_management"},
		{AuditEventTypeDataAccess, "data_access"},
		{AuditEventTypeConfiguration, "configuration"},
		{AuditEventTypeSystem, "system"},
		{AuditEventTypeGDPR, "gdpr"},
		{AuditEventTypeAdmin, "admin"},
		{AuditEventType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.expected {
				t.Errorf("AuditEventType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAuditOutcome_String(t *testing.T) {
	tests := []struct {
		outcome  AuditOutcome
		expected string
	}{
		{AuditOutcomeSuccess, "success"},
		{AuditOutcomeFailure, "failure"},
		{AuditOutcomeDenied, "denied"},
		{AuditOutcomeError, "error"},
		{AuditOutcome(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.outcome.String(); got != tt.expected {
				t.Errorf("AuditOutcome.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAuditSeverity_String(t *testing.T) {
	tests := []struct {
		severity AuditSeverity
		expected string
	}{
		{AuditSeverityInfo, "info"},
		{AuditSeverityLow, "low"},
		{AuditSeverityMedium, "medium"},
		{AuditSeverityHigh, "high"},
		{AuditSeverityCritical, "critical"},
		{AuditSeverity(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.expected {
				t.Errorf("AuditSeverity.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
