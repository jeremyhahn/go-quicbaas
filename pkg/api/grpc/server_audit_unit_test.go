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

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Mock Audit Service
// =============================================================================

type mockAuditService struct {
	mock.Mock
}

func (m *mockAuditService) QueryAuditEvents(ctx context.Context, tenantID string, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	args := m.Called(ctx, tenantID, filter)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.AuditEvent), args.Int(1), args.Error(2)
}

func (m *mockAuditService) GetAuditEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	args := m.Called(ctx, tenantID, eventID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.AuditEvent), args.Error(1)
}

// =============================================================================
// Helper Functions
// =============================================================================

func createTestAuditServer(t *testing.T) (*AuditServer, *mockAuditService) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "audit-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := &mockAuditService{}
	srv := NewAuditServer(mockSvc, log)
	return srv, mockSvc
}

func createTestAuditEvent(tenantID, eventID, eventType string) *iam.AuditEvent {
	now := time.Now()
	return &iam.AuditEvent{
		ID:        eventID,
		TenantID:  tenantID,
		EventType: iam.AuditEventType(eventType),
		Category:  iam.AuditCategorySecurity,
		Outcome:   iam.AuditOutcomeSuccess,
		Actor: &iam.AuditActor{
			UserID:   "user-1",
			ClientID: "",
		},
		Resource: &iam.AuditResource{
			ID:   "resource-1",
			Type: "document",
		},
		Action:    "read",
		Details:   map[string]interface{}{"key": "value"},
		IPAddress: "192.168.1.1",
		UserAgent: "test-client/1.0",
		Timestamp: now,
	}
}

// =============================================================================
// NewAuditServer Tests
// =============================================================================

func TestNewAuditServer(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "audit-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := &mockAuditService{}
	srv := NewAuditServer(mockSvc, log)

	require.NotNil(t, srv)
	assert.NotNil(t, srv.auditService)
	assert.NotNil(t, srv.logger)
}

// =============================================================================
// QueryAuditEvents Tests
// =============================================================================

func TestAuditServer_QueryAuditEvents(t *testing.T) {
	tests := []struct {
		name        string
		request     *iampb.QueryAuditEventsRequest
		mockSetup   func(m *mockAuditService)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *iampb.QueryAuditEventsResponse)
	}{
		{
			name: "successful_query",
			request: &iampb.QueryAuditEventsRequest{
				TenantId:   "tenant-1",
				EventTypes: []string{"login"},
				Limit:      10,
				Offset:     0,
			},
			mockSetup: func(m *mockAuditService) {
				events := []*iam.AuditEvent{
					createTestAuditEvent("tenant-1", "event-1", "login"),
					createTestAuditEvent("tenant-1", "event-2", "login"),
				}
				m.On("QueryAuditEvents", mock.Anything, "tenant-1", mock.AnythingOfType("*iam.AuditFilter")).
					Return(events, 2, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *iampb.QueryAuditEventsResponse) {
				assert.Len(t, resp.Events, 2)
				assert.Equal(t, int32(2), resp.Total)
				assert.Equal(t, int32(10), resp.Limit)
				assert.Equal(t, int32(0), resp.Offset)
			},
		},
		{
			name: "query_with_time_range",
			request: &iampb.QueryAuditEventsRequest{
				TenantId:  "tenant-1",
				StartTime: timestamppb.New(time.Now().Add(-24 * time.Hour)),
				EndTime:   timestamppb.Now(),
				Limit:     50,
			},
			mockSetup: func(m *mockAuditService) {
				events := []*iam.AuditEvent{
					createTestAuditEvent("tenant-1", "event-1", "update"),
				}
				m.On("QueryAuditEvents", mock.Anything, "tenant-1", mock.AnythingOfType("*iam.AuditFilter")).
					Return(events, 1, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *iampb.QueryAuditEventsResponse) {
				assert.Len(t, resp.Events, 1)
				assert.Equal(t, int32(1), resp.Total)
			},
		},
		{
			name: "query_with_filters",
			request: &iampb.QueryAuditEventsRequest{
				TenantId:   "tenant-1",
				EventTypes: []string{"login", "logout"},
				Categories: []string{"security"},
				Outcomes:   []string{"success"},
				ActorId:    "user-123",
				ResourceId: "resource-456",
			},
			mockSetup: func(m *mockAuditService) {
				m.On("QueryAuditEvents", mock.Anything, "tenant-1", mock.MatchedBy(func(f *iam.AuditFilter) bool {
					return len(f.EventTypes) == 2 &&
						len(f.Categories) == 1 &&
						len(f.Outcomes) == 1 &&
						f.ActorID == "user-123" &&
						f.ResourceID == "resource-456"
				})).Return([]*iam.AuditEvent{}, 0, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *iampb.QueryAuditEventsResponse) {
				assert.Len(t, resp.Events, 0)
				assert.Equal(t, int32(0), resp.Total)
			},
		},
		{
			name: "missing_tenant_id",
			request: &iampb.QueryAuditEventsRequest{
				TenantId: "",
			},
			mockSetup:   func(m *mockAuditService) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &iampb.QueryAuditEventsRequest{
				TenantId: "tenant-1",
			},
			mockSetup: func(m *mockAuditService) {
				m.On("QueryAuditEvents", mock.Anything, "tenant-1", mock.AnythingOfType("*iam.AuditFilter")).
					Return(nil, 0, errors.New("database error")).Once()
			},
			expectError: true,
			errorCode:   codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestAuditServer(t)
			tt.mockSetup(mockSvc)

			resp, err := srv.QueryAuditEvents(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// GetAuditEvent Tests
// =============================================================================

func TestAuditServer_GetAuditEvent(t *testing.T) {
	tests := []struct {
		name        string
		request     *iampb.GetAuditEventRequest
		mockSetup   func(m *mockAuditService)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *iampb.AuditEvent)
	}{
		{
			name: "successful_get",
			request: &iampb.GetAuditEventRequest{
				TenantId: "tenant-1",
				EventId:  "event-123",
			},
			mockSetup: func(m *mockAuditService) {
				event := createTestAuditEvent("tenant-1", "event-123", "login")
				m.On("GetAuditEvent", mock.Anything, "tenant-1", "event-123").
					Return(event, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *iampb.AuditEvent) {
				assert.Equal(t, "event-123", resp.Id)
				assert.Equal(t, "tenant-1", resp.TenantId)
				assert.Equal(t, "login", resp.EventType)
				assert.Equal(t, "security", resp.Category)
				assert.Equal(t, "success", resp.Outcome)
				assert.Equal(t, "user-1", resp.ActorId)
				assert.Equal(t, "user", resp.ActorType)
				assert.Equal(t, "resource-1", resp.ResourceId)
				assert.Equal(t, "read", resp.Action)
				assert.Equal(t, "192.168.1.1", resp.IpAddress)
				assert.Equal(t, "test-client/1.0", resp.UserAgent)
			},
		},
		{
			name: "successful_get_service_actor",
			request: &iampb.GetAuditEventRequest{
				TenantId: "tenant-1",
				EventId:  "event-456",
			},
			mockSetup: func(m *mockAuditService) {
				event := &iam.AuditEvent{
					ID:        "event-456",
					TenantID:  "tenant-1",
					EventType: iam.AuditEventType("service_call"),
					Category:  iam.AuditCategorySecurity,
					Outcome:   iam.AuditOutcomeSuccess,
					Actor: &iam.AuditActor{
						UserID:   "service-account",
						ClientID: "client-123",
					},
					Action:    "invoke",
					Timestamp: time.Now(),
				}
				m.On("GetAuditEvent", mock.Anything, "tenant-1", "event-456").
					Return(event, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *iampb.AuditEvent) {
				assert.Equal(t, "event-456", resp.Id)
				assert.Equal(t, "service", resp.ActorType)
			},
		},
		{
			name: "missing_tenant_id",
			request: &iampb.GetAuditEventRequest{
				TenantId: "",
				EventId:  "event-123",
			},
			mockSetup:   func(m *mockAuditService) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_event_id",
			request: &iampb.GetAuditEventRequest{
				TenantId: "tenant-1",
				EventId:  "",
			},
			mockSetup:   func(m *mockAuditService) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "event_not_found",
			request: &iampb.GetAuditEventRequest{
				TenantId: "tenant-1",
				EventId:  "nonexistent",
			},
			mockSetup: func(m *mockAuditService) {
				m.On("GetAuditEvent", mock.Anything, "tenant-1", "nonexistent").
					Return(nil, errors.New("event not found")).Once()
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestAuditServer(t)
			tt.mockSetup(mockSvc)

			resp, err := srv.GetAuditEvent(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// auditEventToProto Tests
// =============================================================================

func TestAuditEventToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    *iam.AuditEvent
		validate func(t *testing.T, result *iampb.AuditEvent)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.AuditEvent) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_event_user_actor",
			input: &iam.AuditEvent{
				ID:        "event-1",
				TenantID:  "tenant-1",
				EventType: iam.AuditEventType("login"),
				Category:  iam.AuditCategorySecurity,
				Outcome:   iam.AuditOutcomeSuccess,
				Actor: &iam.AuditActor{
					UserID: "user-123",
				},
				Resource: &iam.AuditResource{
					ID: "resource-456",
				},
				Action:    "authenticate",
				Details:   map[string]interface{}{"method": "password", "count": 5},
				IPAddress: "10.0.0.1",
				UserAgent: "browser/1.0",
				Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			},
			validate: func(t *testing.T, result *iampb.AuditEvent) {
				require.NotNil(t, result)
				assert.Equal(t, "event-1", result.Id)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, "login", result.EventType)
				assert.Equal(t, "security", result.Category)
				assert.Equal(t, "success", result.Outcome)
				assert.Equal(t, "user-123", result.ActorId)
				assert.Equal(t, "user", result.ActorType)
				assert.Equal(t, "resource-456", result.ResourceId)
				assert.Equal(t, "authenticate", result.Action)
				assert.Equal(t, "password", result.Details["method"])
				assert.Equal(t, "10.0.0.1", result.IpAddress)
				assert.Equal(t, "browser/1.0", result.UserAgent)
				assert.NotNil(t, result.Timestamp)
			},
		},
		{
			name: "full_event_service_actor",
			input: &iam.AuditEvent{
				ID:        "event-2",
				TenantID:  "tenant-1",
				EventType: iam.AuditEventType("api_call"),
				Category:  iam.AuditCategorySecurity,
				Outcome:   iam.AuditOutcomeFailure,
				Actor: &iam.AuditActor{
					UserID:   "service-account",
					ClientID: "client-abc",
				},
				Action:    "invoke",
				Timestamp: time.Now(),
			},
			validate: func(t *testing.T, result *iampb.AuditEvent) {
				require.NotNil(t, result)
				assert.Equal(t, "service", result.ActorType)
				assert.Equal(t, "failure", result.Outcome)
			},
		},
		{
			name: "event_without_actor",
			input: &iam.AuditEvent{
				ID:        "event-3",
				TenantID:  "tenant-1",
				EventType: iam.AuditEventType("system"),
				Timestamp: time.Now(),
			},
			validate: func(t *testing.T, result *iampb.AuditEvent) {
				require.NotNil(t, result)
				assert.Empty(t, result.ActorId)
				assert.Empty(t, result.ActorType)
			},
		},
		{
			name: "event_without_resource",
			input: &iam.AuditEvent{
				ID:        "event-4",
				TenantID:  "tenant-1",
				EventType: iam.AuditEventType("logout"),
				Actor: &iam.AuditActor{
					UserID: "user-1",
				},
				Timestamp: time.Now(),
			},
			validate: func(t *testing.T, result *iampb.AuditEvent) {
				require.NotNil(t, result)
				assert.Empty(t, result.ResourceId)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := auditEventToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// AuditServiceAdapter Tests
// =============================================================================

func TestNewAuditServiceAdapter(t *testing.T) {
	adapter := NewAuditServiceAdapter(nil)
	require.NotNil(t, adapter)
}

func TestAuditServiceAdapter_QueryAuditEvents(t *testing.T) {
	adapter := NewAuditServiceAdapter(nil)
	ctx := context.Background()
	filter := &iam.AuditFilter{}

	events, total, err := adapter.QueryAuditEvents(ctx, "tenant-1", filter)

	require.NoError(t, err)
	assert.Len(t, events, 0)
	assert.Equal(t, 0, total)
}

func TestAuditServiceAdapter_GetAuditEvent(t *testing.T) {
	adapter := NewAuditServiceAdapter(nil)
	ctx := context.Background()

	event, err := adapter.GetAuditEvent(ctx, "tenant-1", "event-1")

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// MemoryAuditService Tests
// =============================================================================

func TestNewMemoryAuditService(t *testing.T) {
	svc := NewMemoryAuditService()
	require.NotNil(t, svc)
	assert.NotNil(t, svc.events)
}

func TestMemoryAuditService_AddEvent(t *testing.T) {
	svc := NewMemoryAuditService()
	event := &iam.AuditEvent{
		ID:        "event-1",
		TenantID:  "tenant-1",
		EventType: iam.AuditEventType("test"),
		Timestamp: time.Now(),
	}

	svc.AddEvent(event)

	retrieved, err := svc.GetAuditEvent(context.Background(), "tenant-1", "event-1")
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved.ID)
}

func TestMemoryAuditService_CreateTestEvent(t *testing.T) {
	svc := NewMemoryAuditService()

	event := svc.CreateTestEvent("tenant-1", "login")

	assert.Equal(t, "event-tenant-1-login", event.ID)
	assert.Equal(t, "tenant-1", event.TenantID)
	assert.Equal(t, iam.AuditEventType("login"), event.EventType)
	assert.Equal(t, iam.AuditCategorySecurity, event.Category)
	assert.Equal(t, iam.AuditOutcomeSuccess, event.Outcome)
	assert.Equal(t, "user-1", event.Actor.UserID)

	// Verify it's stored
	retrieved, err := svc.GetAuditEvent(context.Background(), "tenant-1", "event-tenant-1-login")
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved.ID)
}

func TestMemoryAuditService_GetAuditEvent(t *testing.T) {
	svc := NewMemoryAuditService()
	ctx := context.Background()

	// Add test events
	svc.CreateTestEvent("tenant-1", "login")
	svc.CreateTestEvent("tenant-2", "logout")

	tests := []struct {
		name        string
		tenantID    string
		eventID     string
		expectError bool
	}{
		{
			name:        "found",
			tenantID:    "tenant-1",
			eventID:     "event-tenant-1-login",
			expectError: false,
		},
		{
			name:        "not_found_wrong_tenant",
			tenantID:    "tenant-2",
			eventID:     "event-tenant-1-login",
			expectError: true,
		},
		{
			name:        "not_found_wrong_id",
			tenantID:    "tenant-1",
			eventID:     "nonexistent",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := svc.GetAuditEvent(ctx, tt.tenantID, tt.eventID)
			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, event)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, event)
			}
		})
	}
}

func TestMemoryAuditService_QueryAuditEvents(t *testing.T) {
	svc := NewMemoryAuditService()
	ctx := context.Background()
	now := time.Now()

	// Add test events
	events := []*iam.AuditEvent{
		{
			ID:        "event-1",
			TenantID:  "tenant-1",
			EventType: iam.AuditEventType("login"),
			Category:  iam.AuditCategorySecurity,
			Outcome:   iam.AuditOutcomeSuccess,
			Actor:     &iam.AuditActor{UserID: "user-1"},
			Resource:  &iam.AuditResource{ID: "resource-1"},
			Timestamp: now.Add(-1 * time.Hour),
		},
		{
			ID:        "event-2",
			TenantID:  "tenant-1",
			EventType: iam.AuditEventType("logout"),
			Category:  iam.AuditCategorySecurity,
			Outcome:   iam.AuditOutcomeSuccess,
			Actor:     &iam.AuditActor{UserID: "user-1"},
			Resource:  &iam.AuditResource{ID: "resource-2"},
			Timestamp: now.Add(-30 * time.Minute),
		},
		{
			ID:        "event-3",
			TenantID:  "tenant-1",
			EventType: iam.AuditEventType("login"),
			Category:  iam.AuditCategoryAuthorization,
			Outcome:   iam.AuditOutcomeFailure,
			Actor:     &iam.AuditActor{UserID: "user-2"},
			Timestamp: now,
		},
		{
			ID:        "event-4",
			TenantID:  "tenant-2",
			EventType: iam.AuditEventType("login"),
			Category:  iam.AuditCategorySecurity,
			Outcome:   iam.AuditOutcomeSuccess,
			Timestamp: now,
		},
	}

	for _, e := range events {
		svc.AddEvent(e)
	}

	tests := []struct {
		name          string
		tenantID      string
		filter        *iam.AuditFilter
		expectedCount int
		expectedTotal int
	}{
		{
			name:          "all_tenant_1_events",
			tenantID:      "tenant-1",
			filter:        &iam.AuditFilter{},
			expectedCount: 3,
			expectedTotal: 3,
		},
		{
			name:     "filter_by_event_type",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				EventTypes: []iam.AuditEventType{"login"},
			},
			expectedCount: 2,
			expectedTotal: 2,
		},
		{
			name:     "filter_by_category",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				Categories: []iam.AuditCategory{iam.AuditCategorySecurity},
			},
			expectedCount: 2,
			expectedTotal: 2,
		},
		{
			name:     "filter_by_outcome",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				Outcomes: []iam.AuditOutcome{iam.AuditOutcomeSuccess},
			},
			expectedCount: 2,
			expectedTotal: 2,
		},
		{
			name:     "filter_by_actor",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				ActorID: "user-1",
			},
			expectedCount: 2,
			expectedTotal: 2,
		},
		{
			name:     "filter_by_resource",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				ResourceID: "resource-1",
			},
			expectedCount: 1,
			expectedTotal: 1,
		},
		{
			name:     "filter_by_time_range",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				StartTime: timePtr(now.Add(-45 * time.Minute)),
				EndTime:   timePtr(now.Add(1 * time.Minute)),
			},
			expectedCount: 2,
			expectedTotal: 2,
		},
		{
			name:     "with_pagination_limit",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				Limit: 2,
			},
			expectedCount: 2,
			expectedTotal: 3,
		},
		{
			name:     "with_pagination_offset",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				Offset: 2,
			},
			expectedCount: 1,
			expectedTotal: 3,
		},
		{
			name:     "offset_exceeds_count",
			tenantID: "tenant-1",
			filter: &iam.AuditFilter{
				Offset: 10,
			},
			expectedCount: 0,
			expectedTotal: 3,
		},
		{
			name:          "different_tenant",
			tenantID:      "tenant-2",
			filter:        &iam.AuditFilter{},
			expectedCount: 1,
			expectedTotal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, total, err := svc.QueryAuditEvents(ctx, tt.tenantID, tt.filter)
			require.NoError(t, err)
			assert.Len(t, result, tt.expectedCount)
			assert.Equal(t, tt.expectedTotal, total)
		})
	}
}

// =============================================================================
// Helper Conversion Tests
// =============================================================================

func TestConvertToAuditEventTypes(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect int
	}{
		{"empty", []string{}, 0},
		{"nil", nil, 0},
		{"single", []string{"login"}, 1},
		{"multiple", []string{"login", "logout", "update"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToAuditEventTypes(tt.input)
			if tt.expect == 0 {
				assert.Nil(t, result)
			} else {
				assert.Len(t, result, tt.expect)
			}
		})
	}
}

func TestConvertToAuditCategories(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect int
	}{
		{"empty", []string{}, 0},
		{"nil", nil, 0},
		{"single", []string{"security"}, 1},
		{"multiple", []string{"security", "data", "admin"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToAuditCategories(tt.input)
			if tt.expect == 0 {
				assert.Nil(t, result)
			} else {
				assert.Len(t, result, tt.expect)
			}
		})
	}
}

func TestConvertToAuditOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect int
	}{
		{"empty", []string{}, 0},
		{"nil", nil, 0},
		{"single", []string{"success"}, 1},
		{"multiple", []string{"success", "failure"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToAuditOutcomes(tt.input)
			if tt.expect == 0 {
				assert.Nil(t, result)
			} else {
				assert.Len(t, result, tt.expect)
			}
		})
	}
}

func TestContainsEventType(t *testing.T) {
	slice := []iam.AuditEventType{"login", "logout", "update"}

	assert.True(t, containsEventType(slice, "login"))
	assert.True(t, containsEventType(slice, "logout"))
	assert.True(t, containsEventType(slice, "update"))
	assert.False(t, containsEventType(slice, "delete"))
	assert.False(t, containsEventType([]iam.AuditEventType{}, "login"))
}

func TestContainsCategory(t *testing.T) {
	slice := []iam.AuditCategory{iam.AuditCategorySecurity, iam.AuditCategoryAuthorization}

	assert.True(t, containsCategory(slice, iam.AuditCategorySecurity))
	assert.True(t, containsCategory(slice, iam.AuditCategoryAuthorization))
	assert.False(t, containsCategory(slice, iam.AuditCategoryAdministration))
	assert.False(t, containsCategory([]iam.AuditCategory{}, iam.AuditCategorySecurity))
}

func TestContainsOutcome(t *testing.T) {
	slice := []iam.AuditOutcome{iam.AuditOutcomeSuccess, iam.AuditOutcomeFailure}

	assert.True(t, containsOutcome(slice, iam.AuditOutcomeSuccess))
	assert.True(t, containsOutcome(slice, iam.AuditOutcomeFailure))
	assert.False(t, containsOutcome(slice, iam.AuditOutcome("unknown")))
	assert.False(t, containsOutcome([]iam.AuditOutcome{}, iam.AuditOutcomeSuccess))
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}
