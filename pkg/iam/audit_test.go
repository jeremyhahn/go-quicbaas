package iam

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditEvent_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		event   *AuditEvent
		wantErr bool
	}{
		{
			name: "valid event",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				TenantID:  "tenant1",
				Timestamp: now,
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			event: &AuditEvent{
				TenantID:  "tenant1",
				Timestamp: now,
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: true,
		},
		{
			name: "missing tenant ID",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				Timestamp: now,
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: true,
		},
		{
			name: "missing event type",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				TenantID:  "tenant1",
				Timestamp: now,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: true,
		},
		{
			name: "missing category",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				TenantID:  "tenant1",
				Timestamp: now,
				EventType: AuditEventAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: true,
		},
		{
			name: "missing action",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				TenantID:  "tenant1",
				Timestamp: now,
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: true,
		},
		{
			name: "missing outcome",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				TenantID:  "tenant1",
				Timestamp: now,
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
			},
			wantErr: true,
		},
		{
			name: "missing timestamp",
			event: &AuditEvent{
				ID:        uuid.New().String(),
				TenantID:  "tenant1",
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuditFilter_Validate(t *testing.T) {
	now := time.Now()
	later := now.Add(1 * time.Hour)
	earlier := now.Add(-1 * time.Hour)

	tests := []struct {
		name    string
		filter  *AuditFilter
		wantErr bool
	}{
		{
			name: "valid filter",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    100,
				Offset:   0,
			},
			wantErr: false,
		},
		{
			name: "missing tenant ID",
			filter: &AuditFilter{
				Limit:  100,
				Offset: 0,
			},
			wantErr: true,
		},
		{
			name: "negative limit",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    -1,
			},
			wantErr: true,
		},
		{
			name: "negative offset",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Offset:   -1,
			},
			wantErr: true,
		},
		{
			name: "end before start",
			filter: &AuditFilter{
				TenantID:  "tenant1",
				StartTime: &later,
				EndTime:   &earlier,
			},
			wantErr: true,
		},
		{
			name: "valid time range",
			filter: &AuditFilter{
				TenantID:  "tenant1",
				StartTime: &earlier,
				EndTime:   &later,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuditFilter_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name          string
		filter        *AuditFilter
		expectedLimit int
	}{
		{
			name: "zero limit gets default",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    0,
			},
			expectedLimit: 100,
		},
		{
			name: "custom limit preserved",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    50,
			},
			expectedLimit: 50,
		},
		{
			name: "limit over max capped",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    2000,
			},
			expectedLimit: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.filter.ApplyDefaults()
			assert.Equal(t, tt.expectedLimit, tt.filter.Limit)
		})
	}
}

func TestMemoryAuditStorage_SaveEvent(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAuditStorage()

	event := &AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  "tenant1",
		Timestamp: time.Now(),
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   AuditOutcomeSuccess,
		Actor: &AuditActor{
			UserID: "user1",
			Email:  "user@example.com",
		},
	}

	err := storage.SaveEvent(ctx, event)
	require.NoError(t, err)

	// Retrieve event
	retrieved, err := storage.GetEvent(ctx, "tenant1", event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved.ID)
	assert.Equal(t, event.TenantID, retrieved.TenantID)
	assert.Equal(t, event.Action, retrieved.Action)
}

func TestMemoryAuditStorage_Errors(t *testing.T) {
	stored := &AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  "tenant1",
		Timestamp: time.Now(),
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   AuditOutcomeSuccess,
	}

	tests := []struct {
		name    string
		run     func(ctx context.Context, storage *MemoryAuditStorage) error
		wantErr error
	}{
		{
			name: "save event missing required fields",
			run: func(ctx context.Context, storage *MemoryAuditStorage) error {
				return storage.SaveEvent(ctx, &AuditEvent{TenantID: "tenant1"})
			},
		},
		{
			name: "get unknown event",
			run: func(ctx context.Context, storage *MemoryAuditStorage) error {
				_, err := storage.GetEvent(ctx, "tenant1", "nonexistent")
				return err
			},
			wantErr: ErrAuditEventNotFound,
		},
		{
			// Tenant isolation: an event saved under tenant1 is not visible to tenant2.
			name: "get event of another tenant",
			run: func(ctx context.Context, storage *MemoryAuditStorage) error {
				_, err := storage.GetEvent(ctx, "tenant2", stored.ID)
				return err
			},
			wantErr: ErrAuditEventNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			storage := NewMemoryAuditStorage()
			require.NoError(t, storage.SaveEvent(ctx, stored))

			err := tt.run(ctx, storage)
			if tt.wantErr != nil {
				assert.Equal(t, tt.wantErr, err)
				return
			}
			assert.Error(t, err)
		})
	}
}

func TestMemoryAuditStorage_QueryEvents(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAuditStorage()

	now := time.Now()

	// Create events for tenant1
	events := []*AuditEvent{
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-3 * time.Hour),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "login",
			Outcome:   AuditOutcomeSuccess,
			Actor:     &AuditActor{UserID: "user1"},
		},
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-2 * time.Hour),
			EventType: AuditEventAuthorization,
			Category:  AuditCategoryAuthorization,
			Action:    "check_permission",
			Outcome:   AuditOutcomeDenied,
			Actor:     &AuditActor{UserID: "user1"},
		},
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-1 * time.Hour),
			EventType: AuditEventUserManagement,
			Category:  AuditCategoryAdministration,
			Action:    "create_user",
			Outcome:   AuditOutcomeSuccess,
			Actor:     &AuditActor{UserID: "admin1"},
			Resource:  &AuditResource{Type: "user", ID: "user2"},
		},
	}

	for _, event := range events {
		err := storage.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Create event for tenant2
	tenant2Event := &AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  "tenant2",
		Timestamp: now,
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   AuditOutcomeSuccess,
	}
	err := storage.SaveEvent(ctx, tenant2Event)
	require.NoError(t, err)

	tests := []struct {
		name          string
		filter        *AuditFilter
		expectedCount int
	}{
		{
			name: "all events for tenant1",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    100,
			},
			expectedCount: 3,
		},
		{
			name: "filter by event type",
			filter: &AuditFilter{
				TenantID:   "tenant1",
				EventTypes: []AuditEventType{AuditEventAuthentication},
				Limit:      100,
			},
			expectedCount: 1,
		},
		{
			name: "filter by category",
			filter: &AuditFilter{
				TenantID:   "tenant1",
				Categories: []AuditCategory{AuditCategoryAuthorization},
				Limit:      100,
			},
			expectedCount: 1,
		},
		{
			name: "filter by outcome",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Outcomes: []AuditOutcome{AuditOutcomeSuccess},
				Limit:    100,
			},
			expectedCount: 2,
		},
		{
			name: "filter by actor",
			filter: &AuditFilter{
				TenantID: "tenant1",
				ActorID:  "user1",
				Limit:    100,
			},
			expectedCount: 2,
		},
		{
			name: "filter by resource",
			filter: &AuditFilter{
				TenantID:   "tenant1",
				ResourceID: "user2",
				Limit:      100,
			},
			expectedCount: 1,
		},
		{
			name: "pagination - page 1",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    2,
				Offset:   0,
			},
			expectedCount: 2,
		},
		{
			name: "pagination - page 2",
			filter: &AuditFilter{
				TenantID: "tenant1",
				Limit:    2,
				Offset:   2,
			},
			expectedCount: 1,
		},
		{
			name: "time range filter",
			filter: &AuditFilter{
				TenantID:  "tenant1",
				StartTime: timePtr(now.Add(-2*time.Hour - 30*time.Minute)),
				EndTime:   timePtr(now.Add(-30 * time.Minute)),
				Limit:     100,
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := storage.QueryEvents(ctx, tt.filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)
			assert.GreaterOrEqual(t, total, tt.expectedCount)
		})
	}
}

func TestMemoryAuditStorage_QueryEvents_Sorting(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAuditStorage()

	now := time.Now()

	// Create events with different timestamps
	events := []*AuditEvent{
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-3 * time.Hour),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "oldest",
			Outcome:   AuditOutcomeSuccess,
		},
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-1 * time.Hour),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "newest",
			Outcome:   AuditOutcomeSuccess,
		},
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-2 * time.Hour),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "middle",
			Outcome:   AuditOutcomeSuccess,
		},
	}

	for _, event := range events {
		err := storage.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	filter := &AuditFilter{
		TenantID: "tenant1",
		Limit:    100,
	}

	results, _, err := storage.QueryEvents(ctx, filter)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Should be sorted newest first
	assert.Equal(t, "newest", results[0].Action)
	assert.Equal(t, "middle", results[1].Action)
	assert.Equal(t, "oldest", results[2].Action)
}

func TestMemoryAuditStorage_DeleteEventsBefore(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAuditStorage()

	now := time.Now()

	// Create events with different timestamps
	events := []*AuditEvent{
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-3 * time.Hour),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "old1",
			Outcome:   AuditOutcomeSuccess,
		},
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-2 * time.Hour),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "old2",
			Outcome:   AuditOutcomeSuccess,
		},
		{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: now.Add(-30 * time.Minute),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "recent",
			Outcome:   AuditOutcomeSuccess,
		},
	}

	for _, event := range events {
		err := storage.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Delete events older than 1 hour
	count, err := storage.DeleteEventsBefore(ctx, "tenant1", now.Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify only recent event remains
	filter := &AuditFilter{
		TenantID: "tenant1",
		Limit:    100,
	}
	results, total, err := storage.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "recent", results[0].Action)
}

func TestMemoryAuditStorage_MaxEventsPerTenant(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAuditStorage(WithMaxEventsPerTenant(5))

	// Create more events than the limit
	for i := 0; i < 10; i++ {
		event := &AuditEvent{
			ID:        uuid.New().String(),
			TenantID:  "tenant1",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "login",
			Outcome:   AuditOutcomeSuccess,
		}
		err := storage.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Should only have max events
	filter := &AuditFilter{
		TenantID: "tenant1",
		Limit:    100,
	}
	results, total, err := storage.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, results, 5)
}

func TestAuditLogger_Log(t *testing.T) {
	ctx := context.Background()
	logger, err := NewAuditLogger()
	require.NoError(t, err)
	defer logger.Close()

	event := &AuditEvent{
		TenantID:  "tenant1",
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   AuditOutcomeSuccess,
		Actor: &AuditActor{
			UserID: "user1",
			Email:  "user@example.com",
		},
	}

	err = logger.Log(ctx, event)
	require.NoError(t, err)

	// Event should have ID and timestamp set
	assert.NotEmpty(t, event.ID)
	assert.False(t, event.Timestamp.IsZero())

	// Should be retrievable
	retrieved, err := logger.GetEvent(ctx, "tenant1", event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved.ID)
}

// signallingAuditStorage reports every saved event on saved, so a test can
// wait for the async worker without sleeping.
type signallingAuditStorage struct {
	*MemoryAuditStorage
	saved chan string
}

func newSignallingAuditStorage() *signallingAuditStorage {
	return &signallingAuditStorage{
		MemoryAuditStorage: NewMemoryAuditStorage(),
		saved:              make(chan string, 1),
	}
}

func (s *signallingAuditStorage) SaveEvent(ctx context.Context, event *AuditEvent) error {
	err := s.MemoryAuditStorage.SaveEvent(ctx, event)
	s.saved <- event.ID
	return err
}

func TestAuditLogger_LogAsync(t *testing.T) {
	storage := newSignallingAuditStorage()
	logger, err := NewAuditLogger(WithAuditStorage(storage))
	require.NoError(t, err)
	defer logger.Close()

	event := &AuditEvent{
		TenantID:  "tenant1",
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   AuditOutcomeSuccess,
	}

	logger.LogAsync(event)

	// Event should have ID and timestamp set
	assert.NotEmpty(t, event.ID)
	assert.False(t, event.Timestamp.IsZero())

	select {
	case id := <-storage.saved:
		assert.Equal(t, event.ID, id)
	case <-time.After(5 * time.Second):
		t.Fatal("async worker did not save the event")
	}

	retrieved, err := logger.GetEvent(context.Background(), "tenant1", event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved.ID)
}

func TestAuditLogger_Query(t *testing.T) {
	ctx := context.Background()
	logger, err := NewAuditLogger()
	require.NoError(t, err)
	defer logger.Close()

	// Log some events
	for i := 0; i < 3; i++ {
		event := &AuditEvent{
			TenantID:  "tenant1",
			EventType: AuditEventAuthentication,
			Category:  AuditCategoryAuthentication,
			Action:    "login",
			Outcome:   AuditOutcomeSuccess,
		}
		err := logger.Log(ctx, event)
		require.NoError(t, err)
	}

	filter := &AuditFilter{
		TenantID: "tenant1",
	}

	results, total, err := logger.Query(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, results, 3)
}

func TestAuditLogger_DeleteOlderThan(t *testing.T) {
	ctx := context.Background()
	logger, err := NewAuditLogger()
	require.NoError(t, err)
	defer logger.Close()

	now := time.Now()

	// Log old event
	oldEvent := &AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  "tenant1",
		Timestamp: now.Add(-2 * time.Hour),
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "old",
		Outcome:   AuditOutcomeSuccess,
	}
	err = logger.Log(ctx, oldEvent)
	require.NoError(t, err)

	// Log recent event
	recentEvent := &AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  "tenant1",
		Timestamp: now.Add(-30 * time.Minute),
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "recent",
		Outcome:   AuditOutcomeSuccess,
	}
	err = logger.Log(ctx, recentEvent)
	require.NoError(t, err)

	// Delete old events
	count, err := logger.DeleteOlderThan(ctx, "tenant1", now.Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Recent event should still exist
	filter := &AuditFilter{
		TenantID: "tenant1",
	}
	results, total, err := logger.Query(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "recent", results[0].Action)
}

func TestAuditLogger_LogAuthentication(t *testing.T) {
	ctx := context.Background()
	logger, err := NewAuditLogger()
	require.NoError(t, err)
	defer logger.Close()

	actor := &AuditActor{
		UserID: "user1",
		Email:  "user@example.com",
	}

	details := map[string]interface{}{
		"method": "password",
	}

	err = logger.LogAuthentication(ctx, "tenant1", actor, "login", AuditOutcomeSuccess, details)
	require.NoError(t, err)

	// Verify event was logged
	filter := &AuditFilter{
		TenantID:   "tenant1",
		EventTypes: []AuditEventType{AuditEventAuthentication},
	}
	results, total, err := logger.Query(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "login", results[0].Action)
	assert.Equal(t, AuditCategoryAuthentication, results[0].Category)
}

func TestAuditLogger_LogAuthorization(t *testing.T) {
	ctx := context.Background()
	logger, err := NewAuditLogger()
	require.NoError(t, err)
	defer logger.Close()

	actor := &AuditActor{
		UserID: "user1",
	}

	resource := &AuditResource{
		Type: "document",
		ID:   "doc123",
		Name: "secret.txt",
	}

	err = logger.LogAuthorization(ctx, "tenant1", actor, resource, "read", AuditOutcomeDenied)
	require.NoError(t, err)

	// Verify event was logged
	filter := &AuditFilter{
		TenantID:   "tenant1",
		EventTypes: []AuditEventType{AuditEventAuthorization},
	}
	results, total, err := logger.Query(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "read", results[0].Action)
	assert.Equal(t, AuditCategoryAuthorization, results[0].Category)
	assert.Equal(t, AuditOutcomeDenied, results[0].Outcome)
}

func TestAuditLogger_LogAdminAction(t *testing.T) {
	ctx := context.Background()
	logger, err := NewAuditLogger()
	require.NoError(t, err)
	defer logger.Close()

	tests := []struct {
		name              string
		resourceType      string
		expectedEventType AuditEventType
	}{
		{"user management", "user", AuditEventUserManagement},
		{"an unmapped resource type is user management", "group", AuditEventUserManagement},
		{"role management", "role", AuditEventRoleManagement},
		{"policy change", "policy", AuditEventPolicyChange},
		{"tenant event", "tenant", AuditEventTenantEvent},
		{"app event", "app", AuditEventAppEvent},
		{"application event", "application", AuditEventAppEvent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor := &AuditActor{
				UserID: "admin1",
			}

			resource := &AuditResource{
				Type: tt.resourceType,
				ID:   "res123",
			}

			err = logger.LogAdminAction(ctx, "tenant1", actor, resource, "create", nil)
			require.NoError(t, err)

			// Verify event type
			filter := &AuditFilter{
				TenantID:   "tenant1",
				EventTypes: []AuditEventType{tt.expectedEventType},
			}
			results, total, err := logger.Query(ctx, filter)
			require.NoError(t, err)
			assert.Greater(t, total, 0)
			assert.Greater(t, len(results), 0)
		})
	}
}

func TestAuditLogger_Close(t *testing.T) {
	storage := newSignallingAuditStorage()
	logger, err := NewAuditLogger(WithAuditStorage(storage))
	require.NoError(t, err)

	event := &AuditEvent{
		TenantID:  "tenant1",
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Action:    "login",
		Outcome:   AuditOutcomeSuccess,
	}
	logger.LogAsync(event)

	// Close returns only after the queued event has been saved.
	require.NoError(t, logger.Close())
	select {
	case id := <-storage.saved:
		assert.Equal(t, event.ID, id)
	default:
		t.Fatal("Close returned before the queued event was saved")
	}
}

// Helper function to create time pointers
func timePtr(t time.Time) *time.Time {
	return &t
}

// =============================================================================
// Tests for audit.go - WithAuditStorage and WithAsyncChannelSize options
// =============================================================================

func TestAuditLoggerOptions(t *testing.T) {
	tests := []struct {
		name          string
		customStorage bool
		chanSize      int
		wantChanSize  int
	}{
		{name: "custom storage", customStorage: true, wantChanSize: 1000},
		{name: "custom channel size", chanSize: 500, wantChanSize: 500},
		{name: "custom storage and channel size", customStorage: true, chanSize: 250, wantChanSize: 250},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []AuditLoggerOption
			customStorage := NewMemoryAuditStorage()
			if tt.customStorage {
				opts = append(opts, WithAuditStorage(customStorage))
			}
			if tt.chanSize > 0 {
				opts = append(opts, WithAsyncChannelSize(tt.chanSize))
			}

			logger, err := NewAuditLogger(opts...)
			require.NoError(t, err)
			defer logger.Close()

			assert.Equal(t, tt.wantChanSize, logger.asyncChanSize)

			if !tt.customStorage {
				return
			}
			ctx := context.Background()
			event := &AuditEvent{
				TenantID:  "test-tenant",
				EventType: AuditEventAuthentication,
				Category:  AuditCategoryAuthentication,
				Action:    "login",
				Outcome:   AuditOutcomeSuccess,
			}
			require.NoError(t, logger.Log(ctx, event))

			retrieved, err := customStorage.GetEvent(ctx, "test-tenant", event.ID)
			require.NoError(t, err)
			assert.Equal(t, event.ID, retrieved.ID)
		})
	}
}

// =============================================================================
// Tests for audit_types.go - AuditPolicy.ApplyDefaults and AuditPolicy.Validate
// =============================================================================

func TestAuditPolicy_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name                    string
		policy                  *AuditPolicy
		expectedRetentionDays   int
		expectedLogAuthEvents   bool
		expectedLogAdminEvents  bool
		expectedLogPolicyChecks bool
	}{
		{
			name:                    "zero retention gets default",
			policy:                  &AuditPolicy{Enabled: true},
			expectedRetentionDays:   90,
			expectedLogAuthEvents:   true,
			expectedLogAdminEvents:  true,
			expectedLogPolicyChecks: true,
		},
		{
			name:                    "custom retention preserved",
			policy:                  &AuditPolicy{Enabled: true, RetentionDays: 30},
			expectedRetentionDays:   30,
			expectedLogAuthEvents:   true,
			expectedLogAdminEvents:  true,
			expectedLogPolicyChecks: true,
		},
		{
			name: "explicit settings preserved",
			policy: &AuditPolicy{
				Enabled:         true,
				RetentionDays:   60,
				LogAuthEvents:   true,
				LogAdminEvents:  false,
				LogPolicyChecks: false,
			},
			expectedRetentionDays:   60,
			expectedLogAuthEvents:   true,
			expectedLogAdminEvents:  false,
			expectedLogPolicyChecks: false,
		},
		{
			name: "one explicit setting preserves all",
			policy: &AuditPolicy{
				Enabled:        true,
				LogAdminEvents: true,
			},
			expectedRetentionDays:   90,
			expectedLogAuthEvents:   false,
			expectedLogAdminEvents:  true,
			expectedLogPolicyChecks: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.policy.ApplyDefaults()
			assert.Equal(t, tt.expectedRetentionDays, tt.policy.RetentionDays)
			assert.Equal(t, tt.expectedLogAuthEvents, tt.policy.LogAuthEvents)
			assert.Equal(t, tt.expectedLogAdminEvents, tt.policy.LogAdminEvents)
			assert.Equal(t, tt.expectedLogPolicyChecks, tt.policy.LogPolicyChecks)
		})
	}
}

func TestAuditPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *AuditPolicy
		wantErr bool
	}{
		{
			name:    "valid policy",
			policy:  &AuditPolicy{Enabled: true, RetentionDays: 90},
			wantErr: false,
		},
		{
			name:    "negative retention days",
			policy:  &AuditPolicy{Enabled: true, RetentionDays: -1},
			wantErr: true,
		},
		{
			name:    "retention days over 10 years",
			policy:  &AuditPolicy{Enabled: true, RetentionDays: 3651},
			wantErr: true,
		},
		{
			name:    "retention at max boundary",
			policy:  &AuditPolicy{Enabled: true, RetentionDays: 3650},
			wantErr: false,
		},
		{
			name:    "zero retention is valid",
			policy:  &AuditPolicy{Enabled: true, RetentionDays: 0},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Tests for oidc_app.go - ValidateClientCredentials
// =============================================================================

func TestAppManager_ValidateClientCredentials(t *testing.T) {
	t.Parallel()

	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app
	opts := RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	require.NoError(t, err)

	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		wantValid    bool
		wantErr      bool
	}{
		{
			name:         "valid credentials",
			clientID:     registered.ClientID,
			clientSecret: registered.ClientSecret,
			wantValid:    true,
			wantErr:      false,
		},
		{
			name:         "invalid secret",
			clientID:     registered.ClientID,
			clientSecret: "wrong-secret",
			wantValid:    false,
			wantErr:      true,
		},
		{
			name:         "invalid client ID",
			clientID:     "nonexistent",
			clientSecret: registered.ClientSecret,
			wantValid:    false,
			wantErr:      true,
		},
		{
			name:         "empty credentials",
			clientID:     "",
			clientSecret: "",
			wantValid:    false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := manager.ValidateClientCredentials(ctx, tt.clientID, tt.clientSecret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantValid, valid)
		})
	}
}

// =============================================================================
// Tests for oidc_app.go - validateRedirectURI
// =============================================================================

func TestValidateRedirectURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{
			name:    "valid https URI",
			uri:     "https://app.example.com/callback",
			wantErr: false,
		},
		{
			name:    "valid http localhost",
			uri:     "http://localhost/callback",
			wantErr: false,
		},
		{
			name:    "valid http 127.0.0.1",
			uri:     "http://127.0.0.1/callback",
			wantErr: false,
		},
		{
			name:    "valid http ::1",
			uri:     "http://[::1]/callback",
			wantErr: false,
		},
		{
			name:    "empty URI",
			uri:     "",
			wantErr: true,
		},
		{
			name:    "no scheme",
			uri:     "app.example.com/callback",
			wantErr: true,
		},
		{
			name:    "http non-localhost",
			uri:     "http://example.com/callback",
			wantErr: true,
		},
		{
			name:    "ftp scheme",
			uri:     "ftp://example.com/callback",
			wantErr: true,
		},
		{
			name:    "invalid URI format",
			uri:     "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRedirectURI(tt.uri)
			if tt.wantErr {
				// Every refusal is the one request-class type, so a
				// transport can answer it as the caller's mistake.
				var uriErr *InvalidRedirectURIError
				require.ErrorAs(t, err, &uriErr)
				assert.Equal(t, tt.uri, uriErr.URI)
				assert.NotEmpty(t, uriErr.Reason)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateRedirectURI_Fragment pins draft-ietf-oauth-v2-1 §2.3: a
// redirect URI must not contain a fragment component, not even an empty one.
func TestValidateRedirectURI_Fragment(t *testing.T) {
	for _, uri := range []string{"https://a.example/cb#x", "https://a.example/cb#"} {
		t.Run(uri, func(t *testing.T) {
			err := validateRedirectURI(uri)
			var uriErr *InvalidRedirectURIError
			require.ErrorAs(t, err, &uriErr)
			assert.Equal(t, uri, uriErr.URI)
			assert.Equal(t, "redirect URI must not contain a fragment", uriErr.Reason)
			var fragErr *RedirectURIFragmentError
			require.ErrorAs(t, err, &fragErr)
			assert.Equal(t, uri, fragErr.URI)
		})
	}

	t.Run("no fragment accepted", func(t *testing.T) {
		assert.NoError(t, validateRedirectURI("https://a.example/cb"))
	})
}

// TestInvalidRedirectURIError_FoundThroughWrapping pins go-quicbaas#1463: the
// refusal keeps its type through any wrapping a caller adds, and the parse
// failure keeps its cause.
func TestInvalidRedirectURIError_FoundThroughWrapping(t *testing.T) {
	t.Run("wrapped refusal", func(t *testing.T) {
		err := validateRedirectURIs([]string{"https://ok.example/cb", "ftp://a.example/cb"})
		wrapped := &IAMContextError{Message: "failed to update app", Cause: err}

		var uriErr *InvalidRedirectURIError
		require.ErrorAs(t, wrapped, &uriErr)
		assert.Equal(t, "ftp://a.example/cb", uriErr.URI)
		assert.Equal(t, "URI scheme must be HTTPS (or HTTP for localhost)", uriErr.Reason)
		assert.Contains(t, wrapped.Error(), uriErr.Reason)
	})

	t.Run("parse failure keeps its cause", func(t *testing.T) {
		err := validateRedirectURI("://invalid")
		var uriErr *InvalidRedirectURIError
		require.ErrorAs(t, err, &uriErr)
		assert.Equal(t, "invalid URI format", uriErr.Reason)
		require.Error(t, uriErr.Unwrap())
	})

	t.Run("empty list", func(t *testing.T) {
		err := validateRedirectURIs(nil)
		var uriErr *InvalidRedirectURIError
		require.ErrorAs(t, err, &uriErr)
		assert.Equal(t, "at least one redirect URI is required", uriErr.Reason)
		assert.Equal(t, "at least one redirect URI is required", err.Error())
		assert.NoError(t, uriErr.Unwrap())
	})
}

// =============================================================================
// Tests for oidc_app.go - UpdateApp with edge cases
// =============================================================================

func TestAppManager_UpdateApp_EdgeCases(t *testing.T) {
	t.Parallel()

	manager := createTestAppManager(t)
	ctx := context.Background()

	// Register an app first
	opts := RegisterAppOptions{
		Name:           "Test App",
		RedirectURIs:   []string{"https://app.example.com/callback"},
		ResourcePrefix: "app://",
	}

	registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
	require.NoError(t, err)

	t.Run("update nonexistent app", func(t *testing.T) {
		newName := "Updated"
		_, err := manager.UpdateApp(ctx, "nonexistent", UpdateAppOptions{Name: &newName})
		assert.Error(t, err)
	})

	t.Run("update with empty redirect URIs", func(t *testing.T) {
		emptyURIs := []string{}
		_, err := manager.UpdateApp(ctx, registered.ClientID, UpdateAppOptions{RedirectURIs: &emptyURIs})
		assert.Error(t, err)
	})

	t.Run("update with all optional fields", func(t *testing.T) {
		newName := "Updated App"
		newURIs := []string{"https://new.example.com/callback"}
		newPrefix := "newapp://"
		newGrantTypes := []string{GrantTypeAuthorizationCode, GrantTypeClientCredentials}
		newResponseTypes := []string{ResponseTypeCode}

		updated, err := manager.UpdateApp(ctx, registered.ClientID, UpdateAppOptions{
			Name:           &newName,
			RedirectURIs:   &newURIs,
			ResourcePrefix: &newPrefix,
			GrantTypes:     &newGrantTypes,
			ResponseTypes:  &newResponseTypes,
		})
		require.NoError(t, err)
		assert.Equal(t, newName, updated.Name)
		assert.Equal(t, newURIs, updated.RedirectURIs)
		assert.Equal(t, newPrefix, updated.ResourcePrefix)
		assert.Equal(t, newGrantTypes, updated.GrantTypes)
		assert.Equal(t, newResponseTypes, updated.ResponseTypes)
	})
}

// TestAppManager_UpdateApp_ValidatesRedirectURIs pins draft-ietf-oauth-v2-1
// §2.3 on the update path: a redirect URI that registration refuses is
// refused by an update with the same error, and the app is left unchanged.
func TestAppManager_UpdateApp_ValidatesRedirectURIs(t *testing.T) {
	t.Parallel()

	manager := createTestAppManager(t)
	ctx := context.Background()

	originalURIs := []string{"https://app.example.com/callback"}
	registered, err := manager.RegisterApp(ctx, "tenant-1", RegisterAppOptions{
		Name:         "Test App",
		RedirectURIs: originalURIs,
	})
	require.NoError(t, err)

	tests := []struct {
		name string
		uri  string
	}{
		{name: "fragment", uri: "https://app.example.com/callback#frag"},
		{name: "empty fragment", uri: "https://app.example.com/callback#"},
		{name: "bad scheme", uri: "ftp://app.example.com/callback"},
		{name: "http on a public host", uri: "http://app.example.com/callback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, regErr := manager.RegisterApp(ctx, "tenant-1", RegisterAppOptions{
				Name:         "Refused App",
				RedirectURIs: []string{tt.uri},
			})
			require.Error(t, regErr)

			newName := "Renamed"
			newURIs := []string{"https://app.example.com/other", tt.uri}
			_, err := manager.UpdateApp(ctx, registered.ClientID, UpdateAppOptions{
				Name:         &newName,
				RedirectURIs: &newURIs,
			})
			require.Error(t, err)
			assert.IsType(t, regErr, err)
			assert.Equal(t, regErr.Error(), err.Error())

			stored, getErr := manager.GetApp(ctx, registered.ClientID)
			require.NoError(t, getErr)
			assert.Equal(t, "Test App", stored.Name)
			assert.Equal(t, originalURIs, stored.RedirectURIs)
		})
	}

	t.Run("fragment error is typed", func(t *testing.T) {
		badURIs := []string{"https://app.example.com/callback#frag"}
		_, err := manager.UpdateApp(ctx, registered.ClientID, UpdateAppOptions{RedirectURIs: &badURIs})
		var uriErr *InvalidRedirectURIError
		require.ErrorAs(t, err, &uriErr)
		assert.Equal(t, badURIs[0], uriErr.URI)
		var fragErr *RedirectURIFragmentError
		require.ErrorAs(t, err, &fragErr)
		assert.Equal(t, badURIs[0], fragErr.URI)
	})

	t.Run("valid redirect URIs are stored", func(t *testing.T) {
		newURIs := []string{"https://app.example.com/new", "http://127.0.0.1:8080/cb"}
		updated, err := manager.UpdateApp(ctx, registered.ClientID, UpdateAppOptions{RedirectURIs: &newURIs})
		require.NoError(t, err)
		assert.Equal(t, newURIs, updated.RedirectURIs)
	})
}

// =============================================================================
// Tests for oidc_app.go - DeleteApp with edge cases
// =============================================================================

func TestAppManager_DeleteApp_EdgeCases(t *testing.T) {
	t.Parallel()

	manager := createTestAppManager(t)
	ctx := context.Background()

	t.Run("delete nonexistent app", func(t *testing.T) {
		err := manager.DeleteApp(ctx, "nonexistent")
		assert.Error(t, err)
	})

	t.Run("delete twice", func(t *testing.T) {
		// Register an app
		opts := RegisterAppOptions{
			Name:         "Test App",
			RedirectURIs: []string{"https://app.example.com/callback"},
		}

		registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
		require.NoError(t, err)

		// First delete should succeed
		err = manager.DeleteApp(ctx, registered.ClientID)
		require.NoError(t, err)

		// Second delete should fail
		err = manager.DeleteApp(ctx, registered.ClientID)
		assert.Error(t, err)
	})
}

// =============================================================================
// Tests for oidc_app.go - RotateSecret edge cases
// =============================================================================

func TestAppManager_RotateSecret_EdgeCases(t *testing.T) {
	t.Parallel()

	manager := createTestAppManager(t)
	ctx := context.Background()

	t.Run("rotate nonexistent app", func(t *testing.T) {
		_, err := manager.RotateSecret(ctx, "nonexistent")
		assert.Error(t, err)
	})

	t.Run("rotate multiple times", func(t *testing.T) {
		// Register an app
		opts := RegisterAppOptions{
			Name:         "Test App",
			RedirectURIs: []string{"https://app.example.com/callback"},
		}

		registered, err := manager.RegisterApp(ctx, "tenant-1", opts)
		require.NoError(t, err)

		// Rotate multiple times and verify each gives a new secret
		secrets := make(map[string]bool)
		secrets[registered.ClientSecret] = true

		for i := 0; i < 3; i++ {
			rotated, err := manager.RotateSecret(ctx, registered.ClientID)
			require.NoError(t, err)
			assert.False(t, secrets[rotated.ClientSecret], "secret should be unique")
			secrets[rotated.ClientSecret] = true
		}
	})
}

// =============================================================================
// Tests for gdpr.go - generateID
// =============================================================================

func TestGenerateID(t *testing.T) {
	id, err := generateID()
	require.NoError(t, err)
	assert.Len(t, id, 32) // 16 bytes = 32 hex chars
}

// =============================================================================
// Tests for gdpr.go - GrantConsent edge cases
// =============================================================================

func TestGDPRManager_GrantConsent_EdgeCases(t *testing.T) {
	deps := createCoverageTestGDPRDeps()
	manager, err := NewGDPRManager(deps)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("invalid consent purpose", func(t *testing.T) {
		_, err := manager.GrantConsent(ctx, "tenant-1", 10001, ConsentPurpose("invalid"), "v1", nil, "", "")
		assert.Error(t, err)
	})

	t.Run("update existing consent", func(t *testing.T) {
		// Grant initial consent
		consent1, err := manager.GrantConsent(ctx, "tenant-1", 10001, ConsentPurposeMarketing, "v1", nil, "192.168.1.1", "Mozilla/5.0")
		require.NoError(t, err)

		// Grant consent again with new version (should update)
		consent2, err := manager.GrantConsent(ctx, "tenant-1", 10001, ConsentPurposeMarketing, "v2", nil, "192.168.1.2", "Chrome/100")
		require.NoError(t, err)

		// Should be the same consent record updated
		assert.Equal(t, consent1.ID, consent2.ID)
		assert.Equal(t, "v2", consent2.Version)
	})

	t.Run("consent with expiration", func(t *testing.T) {
		expiresAt := time.Now().Add(24 * time.Hour)
		consent, err := manager.GrantConsent(ctx, "tenant-1", 10002, ConsentPurposeAnalytics, "v1", &expiresAt, "", "")
		require.NoError(t, err)
		assert.NotNil(t, consent.ExpiresAt)
	})
}

// =============================================================================
// Tests for gdpr.go - ProcessDeletion edge cases
// =============================================================================

func TestGDPRManager_ProcessDeletion_EdgeCases(t *testing.T) {
	deps := createCoverageTestGDPRDeps()
	manager, err := NewGDPRManager(deps)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("process nonexistent deletion", func(t *testing.T) {
		_, err := manager.ProcessDeletion(ctx, "nonexistent")
		assert.Error(t, err)
	})

	t.Run("process deletion not in pending status", func(t *testing.T) {
		// Create a user first
		user := &User{
			ID:       33,
			TenantID: "tenant-1",
			Email:    "delete@example.com",
			Name:     "Delete User",
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		err := deps.UserStorage.CreateUser(ctx, user)
		require.NoError(t, err)

		// Request deletion
		request, err := manager.RequestDeletion(ctx, "tenant-1", user.ID, 30040, "Test", nil, nil, 1)
		require.NoError(t, err)

		// Set grace period to past time so we can process
		pastTime := time.Now().Add(-1 * time.Hour)
		request.GracePeriodEndsAt = &pastTime
		err = deps.DeletionStorage.UpdateDeletionRequest(ctx, request)
		require.NoError(t, err)

		// Process to completion
		summary, err := manager.ProcessDeletion(ctx, request.ID)
		require.NoError(t, err)
		assert.NotNil(t, summary)

		// Try to process again (should fail - not in pending status)
		_, err = manager.ProcessDeletion(ctx, request.ID)
		assert.Error(t, err)
	})

	t.Run("process deletion during grace period", func(t *testing.T) {
		// Create a user
		user := &User{
			ID:       32,
			TenantID: "tenant-1",
			Email:    "grace@example.com",
			Name:     "Grace User",
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		err := deps.UserStorage.CreateUser(ctx, user)
		require.NoError(t, err)

		// Request deletion with 30 day grace period
		request, err := manager.RequestDeletion(ctx, "tenant-1", user.ID, 30040, "Test", nil, nil, 30)
		require.NoError(t, err)

		// Try to process immediately (should fail due to grace period)
		_, err = manager.ProcessDeletion(ctx, request.ID)
		assert.ErrorIs(t, err, ErrGracePeriodActive)
	})
}

// =============================================================================
// Tests for admin.go - DeleteAssignment edge cases
// =============================================================================

func TestAdminManager_DeleteAssignment_CoverageEdgeCases(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	t.Run("delete empty ID", func(t *testing.T) {
		err := manager.DeleteAssignment(ctx, "")
		assert.Error(t, err)
	})

	t.Run("delete nonexistent assignment", func(t *testing.T) {
		err := manager.DeleteAssignment(ctx, "nonexistent")
		assert.ErrorIs(t, err, ErrAdminAssignmentNotFound)
	})

	t.Run("delete existing assignment", func(t *testing.T) {
		// Create an assignment first
		opts := CreateAdminAssignmentOptions{
			UserID:    1,
			AdminRole: AdminRoleUserAdmin,
			GrantedBy: "admin",
		}
		assignment, err := manager.CreateAssignment(ctx, opts)
		require.NoError(t, err)

		// Delete it
		err = manager.DeleteAssignment(ctx, assignment.ID)
		require.NoError(t, err)

		// Verify it's deleted
		_, err = manager.GetAssignment(ctx, assignment.ID)
		assert.ErrorIs(t, err, ErrAdminAssignmentNotFound)
	})
}

// =============================================================================
// Tests for admin.go - EndImpersonation edge cases
// =============================================================================

func TestAdminManager_EndImpersonation_CoverageEdgeCases(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	t.Run("end empty session ID", func(t *testing.T) {
		err := manager.EndImpersonation(ctx, "")
		assert.Error(t, err)
	})

	t.Run("end nonexistent session", func(t *testing.T) {
		err := manager.EndImpersonation(ctx, "nonexistent")
		assert.ErrorIs(t, err, ErrImpersonationSessionNotFound)
	})

	t.Run("end already ended session", func(t *testing.T) {
		// Create an assignment with impersonation permission, explicitly
		// scoped to the target tenant — empty TenantIDs no longer matches
		// a tenant-scoped impersonation check (#357).
		assignmentOpts := CreateAdminAssignmentOptions{
			UserID:      30041,
			AdminRole:   AdminRoleSuperAdmin,
			TenantIDs:   []string{"tenant-1"},
			GrantedBy:   "system",
			Permissions: []AdminPermission{PermUserImpersonate},
		}
		_, err := manager.CreateAssignment(ctx, assignmentOpts)
		require.NoError(t, err)

		// Start impersonation
		session, err := manager.StartImpersonation(ctxWithActor(30041), &StartImpersonationOptions{
			TargetUserID: 40005,
			TenantID:     "tenant-1",
			Reason:       "Testing",
			Duration:     time.Hour,
		})
		require.NoError(t, err)

		// End it once
		err = manager.EndImpersonation(ctx, session.ID)
		require.NoError(t, err)

		// Try to end again
		err = manager.EndImpersonation(ctx, session.ID)
		assert.Error(t, err)
	})
}

// =============================================================================
// Helper functions
// =============================================================================

// createCoverageTestGDPRDeps creates test dependencies for GDPRManager (coverage tests)
func createCoverageTestGDPRDeps() GDPRManagerDependencies {
	return GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: NewMemoryDataExportStorage(),
		DeletionStorage:   NewMemoryDeletionStorage(),
		UserStorage:       NewMemoryStorage(), // MemoryStorage implements UserStorage
	}
}
