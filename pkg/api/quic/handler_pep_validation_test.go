//go:build quic

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

package quic

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// PEP Validation Function Tests - Full Coverage
// Tests all branches of validateCheckRequest, validateBatchCheckRequest, and
// validateSubscribeRequest functions
// =============================================================================

func TestValidateCheckRequest_AllValidationBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       *service.CheckRequest
		expectErr bool
	}{
		{
			name:      "NilRequest",
			req:       nil,
			expectErr: true,
		},
		{
			name: "MissingSubjectID",
			req: &service.CheckRequest{
				Subject:  iam.Subject{ID: ""},
				Action:   "read",
				Resource: "resource-1",
			},
			expectErr: true,
		},
		{
			name: "MissingAction",
			req: &service.CheckRequest{
				Subject:  iam.Subject{ID: "user-1"},
				Action:   "",
				Resource: "resource-1",
			},
			expectErr: true,
		},
		{
			name: "MissingResource",
			req: &service.CheckRequest{
				Subject:  iam.Subject{ID: "user-1"},
				Action:   "read",
				Resource: "",
			},
			expectErr: true,
		},
		{
			name: "ValidRequest",
			req: &service.CheckRequest{
				Subject:  iam.Subject{ID: "user-1"},
				Action:   "read",
				Resource: "resource-1",
			},
			expectErr: false,
		},
		{
			name: "ValidRequestWithContext",
			req: &service.CheckRequest{
				Subject:  iam.Subject{ID: "user-1", TenantID: "tenant-1"},
				Action:   "write",
				Resource: "data:123",
				Context:  map[string]interface{}{"ip": "192.168.1.1"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCheckRequest(tt.req)
			if tt.expectErr && err == nil {
				t.Fatal("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidateBatchCheckRequest_AllValidationBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       *service.BatchCheckRequest
		expectErr bool
	}{
		{
			name:      "NilRequest",
			req:       nil,
			expectErr: true,
		},
		{
			name: "EmptyChecks",
			req: &service.BatchCheckRequest{
				Checks: []*service.CheckRequest{},
			},
			expectErr: true,
		},
		{
			name: "FirstCheckInvalid",
			req: &service.BatchCheckRequest{
				Checks: []*service.CheckRequest{
					{
						Subject:  iam.Subject{ID: ""}, // Invalid
						Action:   "read",
						Resource: "resource-1",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "SecondCheckInvalid",
			req: &service.BatchCheckRequest{
				Checks: []*service.CheckRequest{
					{
						Subject:  iam.Subject{ID: "user-1"},
						Action:   "read",
						Resource: "resource-1",
					},
					{
						Subject:  iam.Subject{ID: "user-2"},
						Action:   "", // Invalid
						Resource: "resource-2",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "ThirdCheckInvalid",
			req: &service.BatchCheckRequest{
				Checks: []*service.CheckRequest{
					{
						Subject:  iam.Subject{ID: "user-1"},
						Action:   "read",
						Resource: "resource-1",
					},
					{
						Subject:  iam.Subject{ID: "user-2"},
						Action:   "write",
						Resource: "resource-2",
					},
					{
						Subject:  iam.Subject{ID: "user-3"},
						Action:   "delete",
						Resource: "", // Invalid
					},
				},
			},
			expectErr: true,
		},
		{
			name: "ValidSingleCheck",
			req: &service.BatchCheckRequest{
				Checks: []*service.CheckRequest{
					{
						Subject:  iam.Subject{ID: "user-1"},
						Action:   "read",
						Resource: "resource-1",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "ValidMultipleChecks",
			req: &service.BatchCheckRequest{
				Checks: []*service.CheckRequest{
					{
						Subject:  iam.Subject{ID: "user-1"},
						Action:   "read",
						Resource: "resource-1",
					},
					{
						Subject:  iam.Subject{ID: "user-2"},
						Action:   "write",
						Resource: "resource-2",
					},
					{
						Subject:  iam.Subject{ID: "user-3"},
						Action:   "delete",
						Resource: "resource-3",
					},
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBatchCheckRequest(tt.req)
			if tt.expectErr && err == nil {
				t.Fatal("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidateSubscribeRequest_AllValidationBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       *service.SubscribePolicyUpdatesRequest
		expectErr bool
	}{
		{
			name:      "NilRequest",
			req:       nil,
			expectErr: true,
		},
		{
			name: "EmptyTenantID",
			req: &service.SubscribePolicyUpdatesRequest{
				TenantID: "",
			},
			expectErr: true,
		},
		{
			name: "ValidRequest",
			req: &service.SubscribePolicyUpdatesRequest{
				TenantID: "tenant-1",
			},
			expectErr: false,
		},
		{
			name: "ValidRequestWithResourcePatterns",
			req: &service.SubscribePolicyUpdatesRequest{
				TenantID:         "tenant-2",
				ResourcePatterns: []string{"resource:*", "data:*"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubscribeRequest(tt.req)
			if tt.expectErr && err == nil {
				t.Fatal("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}
