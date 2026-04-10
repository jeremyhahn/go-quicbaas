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

func TestNewGDPRClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewGDPRClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestGDPRClient_GrantConsent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != gdprServiceName {
					t.Errorf("expected service %s, got %s", gdprServiceName, service)
				}
				if method != "GrantConsent" {
					t.Errorf("expected method GrantConsent, got %s", method)
				}
				grantReq, ok := req.(*grantConsentRequest)
				if !ok {
					t.Error("expected grantConsentRequest")
				}
				if grantReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", grantReq.TenantID)
				}
				if grantReq.UserID != "user-456" {
					t.Errorf("expected user ID user-456, got %s", grantReq.UserID)
				}
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.GrantConsent(context.Background(), "tenant-123", "user-456", &GrantConsentRequest{
			Purpose:    "marketing",
			LegalBasis: LegalBasisConsent,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &grantConsentResponse{}, nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.GrantConsent(context.Background(), "tenant-123", "user-456", &GrantConsentRequest{
			Purpose: "marketing",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.GrantConsent(nil, "tenant-123", "user-456", &GrantConsentRequest{Purpose: "marketing"})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.GrantConsent(context.Background(), "", "user-456", &GrantConsentRequest{Purpose: "marketing"})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.GrantConsent(context.Background(), "tenant-123", "", &GrantConsentRequest{Purpose: "marketing"})
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.GrantConsent(context.Background(), "tenant-123", "user-456", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty purpose", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.GrantConsent(context.Background(), "tenant-123", "user-456", &GrantConsentRequest{})
		if err == nil {
			t.Error("expected error for empty purpose")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "service unavailable")
			},
		}

		client := NewGDPRClient(mt)
		err := client.GrantConsent(context.Background(), "tenant-123", "user-456", &GrantConsentRequest{Purpose: "marketing"})
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsUnavailable(err) {
			t.Errorf("expected unavailable error, got %v", err)
		}
	})

	t.Run("any response type accepted for void operation", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "any-value", nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.GrantConsent(context.Background(), "tenant-123", "user-456", &GrantConsentRequest{Purpose: "marketing"})
		if err != nil {
			t.Errorf("void operation should accept any response type, got error: %v", err)
		}
	})
}

func TestGDPRClient_WithdrawConsent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "WithdrawConsent" {
					t.Errorf("expected method WithdrawConsent, got %s", method)
				}
				withdrawReq, ok := req.(*withdrawConsentRequest)
				if !ok {
					t.Error("expected withdrawConsentRequest")
				}
				if withdrawReq.Purpose != "marketing" {
					t.Errorf("expected purpose marketing, got %s", withdrawReq.Purpose)
				}
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.WithdrawConsent(context.Background(), "tenant-123", "user-456", "marketing")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &withdrawConsentResponse{}, nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.WithdrawConsent(context.Background(), "tenant-123", "user-456", "marketing")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.WithdrawConsent(nil, "tenant-123", "user-456", "marketing")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.WithdrawConsent(context.Background(), "", "user-456", "marketing")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.WithdrawConsent(context.Background(), "tenant-123", "", "marketing")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty purpose", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.WithdrawConsent(context.Background(), "tenant-123", "user-456", "")
		if err == nil {
			t.Error("expected error for empty purpose")
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

		client := NewGDPRClient(mt)
		err := client.WithdrawConsent(context.Background(), "tenant-123", "user-456", "marketing")
		if err != nil {
			t.Errorf("void operation should accept any response type, got error: %v", err)
		}
	})
}

func TestGDPRClient_GetConsent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GetConsent" {
					t.Errorf("expected method GetConsent, got %s", method)
				}
				getReq, ok := req.(*getConsentRequest)
				if !ok {
					t.Error("expected getConsentRequest")
				}
				if getReq.Purpose != "marketing" {
					t.Errorf("expected purpose marketing, got %s", getReq.Purpose)
				}
				return &Consent{
					ID:         "consent-123",
					TenantID:   getReq.TenantID,
					UserID:     getReq.UserID,
					Purpose:    getReq.Purpose,
					Status:     ConsentStatusGranted,
					LegalBasis: LegalBasisConsent,
					GrantedAt:  now,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		consent, err := client.GetConsent(context.Background(), "tenant-123", "user-456", "marketing")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if consent.ID != "consent-123" {
			t.Errorf("expected consent ID consent-123, got %s", consent.ID)
		}
		if consent.Status != ConsentStatusGranted {
			t.Errorf("expected status granted, got %s", consent.Status)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetConsent(nil, "tenant-123", "user-456", "marketing")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetConsent(context.Background(), "", "user-456", "marketing")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetConsent(context.Background(), "tenant-123", "", "marketing")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty purpose", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetConsent(context.Background(), "tenant-123", "user-456", "")
		if err == nil {
			t.Error("expected error for empty purpose")
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

		client := NewGDPRClient(mt)
		_, err := client.GetConsent(context.Background(), "tenant-123", "user-456", "marketing")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.GetConsent(context.Background(), "tenant-123", "user-456", "marketing")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestGDPRClient_ListConsents(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "ListConsents" {
					t.Errorf("expected method ListConsents, got %s", method)
				}
				return &ListConsentsResponse{
					Consents: []Consent{
						{ID: "consent-1", Purpose: "marketing"},
						{ID: "consent-2", Purpose: "analytics"},
					},
					TotalCount: 2,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.ListConsents(context.Background(), "tenant-123", "user-456")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Consents) != 2 {
			t.Errorf("expected 2 consents, got %d", len(resp.Consents))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.ListConsents(nil, "tenant-123", "user-456")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.ListConsents(context.Background(), "", "user-456")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.ListConsents(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.ListConsents(context.Background(), "tenant-123", "user-456")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Consents) != 0 {
			t.Errorf("expected 0 consents, got %d", len(resp.Consents))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.ListConsents(context.Background(), "tenant-123", "user-456")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestGDPRClient_RequestExport(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RequestExport" {
					t.Errorf("expected method RequestExport, got %s", method)
				}
				exportReq, ok := req.(*requestExportRequest)
				if !ok {
					t.Error("expected requestExportRequest")
				}
				if exportReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", exportReq.TenantID)
				}
				return &DataExportRequest{
					ID:          "export-123",
					TenantID:    exportReq.TenantID,
					UserID:      exportReq.UserID,
					Status:      ExportStatusPending,
					Format:      ExportFormatJSON,
					RequestedAt: now,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.RequestExport(context.Background(), "tenant-123", "user-456", &ExportRequest{
			Format: ExportFormatJSON,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != "export-123" {
			t.Errorf("expected export ID export-123, got %s", resp.ID)
		}
		if resp.Status != ExportStatusPending {
			t.Errorf("expected status pending, got %s", resp.Status)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.RequestExport(nil, "tenant-123", "user-456", &ExportRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.RequestExport(context.Background(), "", "user-456", &ExportRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.RequestExport(context.Background(), "tenant-123", "", &ExportRequest{})
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.RequestExport(context.Background(), "tenant-123", "user-456", &ExportRequest{})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.RequestExport(context.Background(), "tenant-123", "user-456", &ExportRequest{})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestGDPRClient_GetExportStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GetExportStatus" {
					t.Errorf("expected method GetExportStatus, got %s", method)
				}
				getReq, ok := req.(*getExportStatusRequest)
				if !ok {
					t.Error("expected getExportStatusRequest")
				}
				if getReq.ExportID != "export-123" {
					t.Errorf("expected export ID export-123, got %s", getReq.ExportID)
				}
				return &DataExportRequest{
					ID:     "export-123",
					Status: ExportStatusCompleted,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.GetExportStatus(context.Background(), "tenant-123", "user-456", "export-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Status != ExportStatusCompleted {
			t.Errorf("expected status completed, got %s", resp.Status)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetExportStatus(nil, "tenant-123", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetExportStatus(context.Background(), "", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetExportStatus(context.Background(), "tenant-123", "", "export-123")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty export ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetExportStatus(context.Background(), "tenant-123", "user-456", "")
		if err == nil {
			t.Error("expected error for empty export ID")
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

		client := NewGDPRClient(mt)
		_, err := client.GetExportStatus(context.Background(), "tenant-123", "user-456", "export-123")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.GetExportStatus(context.Background(), "tenant-123", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestGDPRClient_GenerateExport(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GenerateExport" {
					t.Errorf("expected method GenerateExport, got %s", method)
				}
				return &UserDataExport{
					RequestID:   "export-123",
					UserID:      "user-456",
					Format:      ExportFormatJSON,
					Data:        []byte(`{"user": "data"}`),
					GeneratedAt: now,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.GenerateExport(context.Background(), "tenant-123", "user-456", "export-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.RequestID != "export-123" {
			t.Errorf("expected request ID export-123, got %s", resp.RequestID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GenerateExport(nil, "tenant-123", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GenerateExport(context.Background(), "", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GenerateExport(context.Background(), "tenant-123", "", "export-123")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty export ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GenerateExport(context.Background(), "tenant-123", "user-456", "")
		if err == nil {
			t.Error("expected error for empty export ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.GenerateExport(context.Background(), "tenant-123", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.GenerateExport(context.Background(), "tenant-123", "user-456", "export-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestGDPRClient_RequestDeletion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RequestDeletion" {
					t.Errorf("expected method RequestDeletion, got %s", method)
				}
				return &DataDeletionRequest{
					ID:          "deletion-123",
					TenantID:    "tenant-123",
					UserID:      "user-456",
					Status:      DeletionStatusPending,
					Reason:      "user request",
					RequestedAt: now,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.RequestDeletion(context.Background(), "tenant-123", "user-456", &DeletionRequest{
			Reason: "user request",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != "deletion-123" {
			t.Errorf("expected deletion ID deletion-123, got %s", resp.ID)
		}
		if resp.Status != DeletionStatusPending {
			t.Errorf("expected status pending, got %s", resp.Status)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.RequestDeletion(nil, "tenant-123", "user-456", &DeletionRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.RequestDeletion(context.Background(), "", "user-456", &DeletionRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.RequestDeletion(context.Background(), "tenant-123", "", &DeletionRequest{})
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.RequestDeletion(context.Background(), "tenant-123", "user-456", &DeletionRequest{})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.RequestDeletion(context.Background(), "tenant-123", "user-456", &DeletionRequest{})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestGDPRClient_CancelDeletion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "CancelDeletion" {
					t.Errorf("expected method CancelDeletion, got %s", method)
				}
				cancelReq, ok := req.(*cancelDeletionRequest)
				if !ok {
					t.Error("expected cancelDeletionRequest")
				}
				if cancelReq.DeletionID != "deletion-123" {
					t.Errorf("expected deletion ID deletion-123, got %s", cancelReq.DeletionID)
				}
				return nil, nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.CancelDeletion(context.Background(), "tenant-123", "user-456", "deletion-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &cancelDeletionResponse{}, nil
			},
		}

		client := NewGDPRClient(mt)
		err := client.CancelDeletion(context.Background(), "tenant-123", "user-456", "deletion-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.CancelDeletion(nil, "tenant-123", "user-456", "deletion-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.CancelDeletion(context.Background(), "", "user-456", "deletion-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.CancelDeletion(context.Background(), "tenant-123", "", "deletion-123")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty deletion ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		err := client.CancelDeletion(context.Background(), "tenant-123", "user-456", "")
		if err == nil {
			t.Error("expected error for empty deletion ID")
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

		client := NewGDPRClient(mt)
		err := client.CancelDeletion(context.Background(), "tenant-123", "user-456", "deletion-123")
		if err != nil {
			t.Errorf("void operation should accept any response type, got error: %v", err)
		}
	})
}

func TestGDPRClient_GetDeletionStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GetDeletionStatus" {
					t.Errorf("expected method GetDeletionStatus, got %s", method)
				}
				return &DataDeletionRequest{
					ID:     "deletion-123",
					Status: DeletionStatusCompleted,
				}, nil
			},
		}

		client := NewGDPRClient(mt)
		resp, err := client.GetDeletionStatus(context.Background(), "tenant-123", "user-456", "deletion-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Status != DeletionStatusCompleted {
			t.Errorf("expected status completed, got %s", resp.Status)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetDeletionStatus(nil, "tenant-123", "user-456", "deletion-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetDeletionStatus(context.Background(), "", "user-456", "deletion-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetDeletionStatus(context.Background(), "tenant-123", "", "deletion-123")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty deletion ID", func(t *testing.T) {
		client := NewGDPRClient(&mockTransport{})
		_, err := client.GetDeletionStatus(context.Background(), "tenant-123", "user-456", "")
		if err == nil {
			t.Error("expected error for empty deletion ID")
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

		client := NewGDPRClient(mt)
		_, err := client.GetDeletionStatus(context.Background(), "tenant-123", "user-456", "deletion-123")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewGDPRClient(mt)
		_, err := client.GetDeletionStatus(context.Background(), "tenant-123", "user-456", "deletion-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestLegalBasis_String(t *testing.T) {
	tests := []struct {
		basis    LegalBasis
		expected string
	}{
		{LegalBasisConsent, "consent"},
		{LegalBasisContract, "contract"},
		{LegalBasisLegalObligation, "legal_obligation"},
		{LegalBasisVitalInterests, "vital_interests"},
		{LegalBasisPublicTask, "public_task"},
		{LegalBasisLegitimateInterests, "legitimate_interests"},
		{LegalBasis(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.basis.String(); got != tt.expected {
				t.Errorf("LegalBasis.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConsentStatus_String(t *testing.T) {
	tests := []struct {
		status   ConsentStatus
		expected string
	}{
		{ConsentStatusGranted, "granted"},
		{ConsentStatusWithdrawn, "withdrawn"},
		{ConsentStatusExpired, "expired"},
		{ConsentStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("ConsentStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExportFormat_String(t *testing.T) {
	tests := []struct {
		format   ExportFormat
		expected string
	}{
		{ExportFormatJSON, "json"},
		{ExportFormatCSV, "csv"},
		{ExportFormatXML, "xml"},
		{ExportFormat(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.format.String(); got != tt.expected {
				t.Errorf("ExportFormat.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExportStatus_String(t *testing.T) {
	tests := []struct {
		status   ExportStatus
		expected string
	}{
		{ExportStatusPending, "pending"},
		{ExportStatusProcessing, "processing"},
		{ExportStatusCompleted, "completed"},
		{ExportStatusFailed, "failed"},
		{ExportStatusExpired, "expired"},
		{ExportStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("ExportStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDeletionStatus_String(t *testing.T) {
	tests := []struct {
		status   DeletionStatus
		expected string
	}{
		{DeletionStatusPending, "pending"},
		{DeletionStatusScheduled, "scheduled"},
		{DeletionStatusProcessing, "processing"},
		{DeletionStatusCompleted, "completed"},
		{DeletionStatusCancelled, "cancelled"},
		{DeletionStatusFailed, "failed"},
		{DeletionStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("DeletionStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
