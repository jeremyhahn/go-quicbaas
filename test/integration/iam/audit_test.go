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

//go:build integration
// +build integration

package iam_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AuditEventResponse represents an audit event in API responses.
type AuditEventResponse struct {
	ID        string                 `json:"id"`
	TenantID  string                 `json:"tenant_id"`
	Timestamp string                 `json:"timestamp"`
	EventType string                 `json:"event_type"`
	Category  string                 `json:"category"`
	Action    string                 `json:"action"`
	Outcome   string                 `json:"outcome"`
	Actor     *AuditActorResponse    `json:"actor,omitempty"`
	Resource  *AuditResourceResponse `json:"resource,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	IPAddress string                 `json:"ip_address,omitempty"`
	UserAgent string                 `json:"user_agent,omitempty"`
}

// AuditActorResponse represents the actor who performed an action.
type AuditActorResponse struct {
	UserID    string `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Email     string `json:"email,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
}

// AuditResourceResponse represents a resource affected by an action.
type AuditResourceResponse struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// AuditQueryResponse represents the response from querying audit events.
type AuditQueryResponse struct {
	Events []AuditEventResponse `json:"events"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

// setupTenantForAuditTests creates a test tenant and returns it with a view
// of cluster that calls as the tenant's auditor. Creating a tenant is a
// system-tenant route (tenants:create), so it is sent as the fixture system
// admin. The audit queries that follow are not: the audit routes answer only
// for the caller's own tenant (iam.ResolveRequestTenant) and check
// audit:query / audit:read, so they go through the returned view, a user of
// the tenant bound to go-qrdb's built-in Auditor role (asTenantAuditor).
// Setting the auditor up writes audit events of its own, including a
// user_management one; this waits until that one is visible, so a test that
// counts its own events from auditBaseline does not count it.
func setupTenantForAuditTests(t *testing.T, cluster *testCluster) (string, *testCluster) {
	t.Helper()
	tenantID := generateUniqueTenantID()
	createTenantReq := map[string]interface{}{
		"id":   tenantID,
		"name": "Audit Test Tenant",
	}
	require.NoError(t, cluster.asSystemAdmin().postJSON("/api/v1/tenants", createTenantReq, nil))
	auditor := cluster.asTenantAuditor(t, tenantID)
	waitForAuditEvents(t, auditor,
		fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenantID), 1)
	return tenantID, auditor
}

// cleanupAuditTests cleans up test resources. Deleting a tenant is a
// system-tenant route (tenants:delete), sent as the fixture system admin.
func cleanupAuditTests(cluster *testCluster, tenantID string) {
	cluster.asSystemAdmin().deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
}

// waitForAuditEvents polls the audit query endpoint until at least min events
// are visible for the tenant (or the deadline expires) and returns the last
// response. Audit events are written through DefaultAuditLogger.LogAsync — a
// buffered channel drained by a background worker — so an event emitted by
// the request that just returned is only EVENTUALLY queryable. Asserting on
// the first read races the async writer; polling matches the API contract.
func waitForAuditEvents(t *testing.T, cluster *testCluster, path string, min int) AuditQueryResponse {
	t.Helper()
	var resp AuditQueryResponse
	require.Eventually(t, func() bool {
		var attempt AuditQueryResponse
		if err := cluster.getJSON(path, &attempt); err != nil {
			t.Logf("audit query not ready: %v", err)
			return false
		}
		resp = attempt
		return attempt.Total >= min && len(attempt.Events) >= min
	}, 15*time.Second, 200*time.Millisecond,
		"at least %d audit event(s) should become visible at %s", min, path)
	return resp
}

// auditBaseline returns the number of audit events visible at path now: the
// count a test adds its own expected events to before it waits for them,
// since the auditor's own setup events are already in the tenant's chain.
func auditBaseline(t *testing.T, auditor *testCluster, path string) int {
	t.Helper()
	var resp AuditQueryResponse
	require.NoError(t, auditor.getJSON(path, &resp), "audit baseline at %s", path)
	return resp.Total
}

// TestAuditEventQuery tests querying audit events for a tenant.
func TestAuditEventQuery(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	// Query audit events for the tenant. The tenant creation just performed
	// must generate at least one audit event; the write is async (LogAsync),
	// so poll until it becomes visible.
	resp := waitForAuditEvents(t, auditor,
		fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenantID), 1)
	assert.Equal(t, 100, resp.Limit, "Default limit should be 100")
	assert.Equal(t, 0, resp.Offset, "Default offset should be 0")

	// Verify event structure
	if len(resp.Events) > 0 {
		event := resp.Events[0]
		assert.NotEmpty(t, event.ID, "Event should have an ID")
		assert.Equal(t, tenantID, event.TenantID, "Event should belong to the correct tenant")
		assert.NotEmpty(t, event.Timestamp, "Event should have a timestamp")
		assert.NotEmpty(t, event.EventType, "Event should have an event type")
		assert.NotEmpty(t, event.Category, "Event should have a category")
		assert.NotEmpty(t, event.Action, "Event should have an action")
		assert.NotEmpty(t, event.Outcome, "Event should have an outcome")
	}
}

// TestAuditEventQueryWithFilters tests querying audit events with various filters.
func TestAuditEventQueryWithFilters(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	userEvents := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenantID)
	before := auditBaseline(t, auditor, userEvents)

	// Create a user to generate more audit events
	email := generateUniqueEmail()
	createUserReq := map[string]interface{}{
		"email":    email,
		"username": generateUniqueUsername(),
		"name":     "Audit Filter Test User",
	}
	var user struct {
		ID string `json:"id"`
	}
	// The user create acts in the test's tenant as the system admin
	// (tenant lift, pkg/api/rest/tenant_target.go).
	err := cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createUserReq, &user)
	require.NoError(t, err)

	// Wait for the user creation event to become queryable
	waitForAuditEvents(t, auditor, userEvents, before+1)

	// Test filtering by event type
	var typeResp AuditQueryResponse
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenantID), &typeResp)
	require.NoError(t, err, "Should query by event type")
	assert.Greater(t, typeResp.Total, 0, "Should find user_management events")

	// Test filtering by category
	var categoryResp AuditQueryResponse
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&category=administration", tenantID), &categoryResp)
	require.NoError(t, err, "Should query by category")
	assert.Greater(t, categoryResp.Total, 0, "Should find administration events")

	// Test filtering by outcome
	var outcomeResp AuditQueryResponse
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&outcome=success", tenantID), &outcomeResp)
	require.NoError(t, err, "Should query by outcome")
	assert.Greater(t, outcomeResp.Total, 0, "Should find success events")

	// Verify all returned events match the filter
	for _, event := range outcomeResp.Events {
		assert.Equal(t, "success", event.Outcome, "All events should have success outcome")
	}

	// Test filtering by resource_id
	var resourceResp AuditQueryResponse
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&resource_id=%s", tenantID, user.ID), &resourceResp)
	require.NoError(t, err, "Should query by resource_id")
	if resourceResp.Total > 0 {
		for _, event := range resourceResp.Events {
			if event.Resource != nil {
				assert.Equal(t, user.ID, event.Resource.ID, "Resource ID should match filter")
			}
		}
	}
}

// TestAuditEventQueryPagination tests pagination parameters.
func TestAuditEventQueryPagination(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	userEvents := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenantID)
	before := auditBaseline(t, auditor, userEvents)

	// Create multiple users to generate multiple audit events
	for i := 0; i < 5; i++ {
		email := generateUniqueEmail()
		createUserReq := map[string]interface{}{
			"email":    email,
			"username": generateUniqueUsername(),
			"name":     fmt.Sprintf("Pagination Test User %d", i),
		}
		// The user create acts in the test's tenant as the system admin
		// (tenant lift, pkg/api/rest/tenant_target.go).
		err := cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createUserReq, nil)
		require.NoError(t, err)
	}

	// Wait for the five user creation events to become queryable
	waitForAuditEvents(t, auditor, userEvents, before+5)

	// Test with limit
	var limitResp AuditQueryResponse
	err := auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&limit=3", tenantID), &limitResp)
	require.NoError(t, err, "Should query with limit")
	assert.LessOrEqual(t, len(limitResp.Events), 3, "Should respect limit parameter")
	assert.Equal(t, 3, limitResp.Limit, "Limit should be set correctly")

	// Test with offset
	var offsetResp AuditQueryResponse
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&limit=3&offset=2", tenantID), &offsetResp)
	require.NoError(t, err, "Should query with offset")
	assert.Equal(t, 2, offsetResp.Offset, "Offset should be set correctly")

	// First page should be different from second page (if enough events exist)
	if limitResp.Total > 3 && len(limitResp.Events) > 0 && len(offsetResp.Events) > 0 {
		assert.NotEqual(t, limitResp.Events[0].ID, offsetResp.Events[0].ID, "Different pages should return different events")
	}
}

// TestAuditEventQueryTimeRange tests querying events within a time range.
func TestAuditEventQueryTimeRange(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	userEvents := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenantID)
	before := auditBaseline(t, auditor, userEvents)

	// Record the time before creating a user, in whole seconds: event
	// timestamps are RFC3339, so one in the same second must not compare
	// before it.
	beforeCreate := time.Now().Add(-1 * time.Second).UTC().Truncate(time.Second)

	// Create a user to generate audit events
	email := generateUniqueEmail()
	createUserReq := map[string]interface{}{
		"email":    email,
		"username": generateUniqueUsername(),
		"name":     "Time Range Test User",
	}
	// The user create acts in the test's tenant as the system admin
	// (tenant lift, pkg/api/rest/tenant_target.go).
	err := cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createUserReq, nil)
	require.NoError(t, err)

	// Wait for the user creation event to become queryable
	waitForAuditEvents(t, auditor, userEvents, before+1)

	afterCreate := time.Now().Add(1 * time.Second).UTC()

	// Query events within the time range
	var resp AuditQueryResponse
	startTimeParam := url.QueryEscape(beforeCreate.Format(time.RFC3339))
	endTimeParam := url.QueryEscape(afterCreate.Format(time.RFC3339))
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&start_time=%s&end_time=%s",
		tenantID, startTimeParam, endTimeParam), &resp)
	require.NoError(t, err, "Should query with time range")

	// Should find events in this time range
	assert.Greater(t, resp.Total, 0, "Should find events in time range")

	// Verify all events are within the time range
	for _, event := range resp.Events {
		eventTime, err := time.Parse(time.RFC3339, event.Timestamp)
		require.NoError(t, err, "Event timestamp should be valid")
		assert.True(t, eventTime.After(beforeCreate) || eventTime.Equal(beforeCreate),
			"Event should be after start time")
		assert.True(t, eventTime.Before(afterCreate) || eventTime.Equal(afterCreate),
			"Event should be before end time")
	}

	// Query events before tenant creation (should find nothing or just tenant creation)
	veryOldTime := time.Now().Add(-24 * time.Hour).UTC()
	veryOldParam := url.QueryEscape(veryOldTime.Format(time.RFC3339))
	beforeParam := url.QueryEscape(beforeCreate.Add(-10 * time.Minute).Format(time.RFC3339))
	var oldResp AuditQueryResponse
	err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&start_time=%s&end_time=%s",
		tenantID, veryOldParam, beforeParam), &oldResp)
	require.NoError(t, err, "Should query old time range")
	// Should find very few or no events
	assert.LessOrEqual(t, oldResp.Total, 1, "Should find few/no events in old time range")
}

// TestAuditEventGet tests getting a specific audit event by ID.
func TestAuditEventGet(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	// Query events to get an event ID. Audit writes flow through
	// DefaultAuditLogger.LogAsync (buffered channel + background worker),
	// so the tenant-creation event is only EVENTUALLY queryable — poll
	// instead of racing the async writer (same contract as
	// TestAuditEventQuery / waitForAuditEvents).
	listResp := waitForAuditEvents(t, auditor,
		fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&limit=1", tenantID), 1)

	eventID := listResp.Events[0].ID

	// Get the specific event
	var event AuditEventResponse
	err := auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit/%s?tenant_id=%s", eventID, tenantID), &event)
	require.NoError(t, err, "Should get specific audit event")

	// Verify the event details
	assert.Equal(t, eventID, event.ID, "Event ID should match")
	assert.Equal(t, tenantID, event.TenantID, "Tenant ID should match")
	assert.NotEmpty(t, event.Timestamp, "Event should have timestamp")
	assert.NotEmpty(t, event.EventType, "Event should have event type")
	assert.NotEmpty(t, event.Category, "Event should have category")
	assert.NotEmpty(t, event.Action, "Event should have action")
	assert.NotEmpty(t, event.Outcome, "Event should have outcome")
}

// TestAuditEventGetNotFound tests getting a non-existent audit event.
func TestAuditEventGetNotFound(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	// Try to get a non-existent event
	var event AuditEventResponse
	err := auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit/nonexistent-id?tenant_id=%s", tenantID), &event)
	assert.Error(t, err, "Should fail to get non-existent event")
	assert.Contains(t, err.Error(), "404", "Should return 404 status")
}

// TestAuditEventDelete tests deleting events older than a timestamp.
func TestAuditEventDelete(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	// The tenant is provisioned through its admin, the principal that must
	// be authorized for audit:delete in the tenant; the helper deletes the
	// tenant when t ends.
	// The auditor in the same tenant holds audit:read but not audit:delete.
	tenantID := generateUniqueTenantID()
	tenantAdmin := cluster.asTenantAdmin(t, tenantID)
	auditor := cluster.asTenantAuditor(t, tenantID)

	userEvents := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenantID)
	before := auditBaseline(t, auditor, userEvents)

	// Create some users to generate audit events
	for i := 0; i < 3; i++ {
		email := generateUniqueEmail()
		createUserReq := map[string]interface{}{
			"email":    email,
			"username": generateUniqueUsername(),
			"name":     fmt.Sprintf("Delete Test User %d", i),
		}
		// The user create acts in the test's tenant as the system admin
		// (tenant lift, pkg/api/rest/tenant_target.go).
		err := cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createUserReq, nil)
		require.NoError(t, err)
	}

	// Wait for the three user creation events to become queryable
	waitForAuditEvents(t, auditor, userEvents, before+3)

	// Read every event in the tenant's chain before the delete. The limit
	// is the endpoint's maximum so the ID set is the whole chain.
	allEvents := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&limit=1000", tenantID)
	var beforeResp AuditQueryResponse
	require.NoError(t, auditor.getJSON(allEvents, &beforeResp), "Should query events before delete")
	require.Len(t, beforeResp.Events, beforeResp.Total, "the before read should hold the whole chain")
	require.NotEmpty(t, beforeResp.Events, "the chain should hold the user creation events")

	// Send the audit delete the API serves: DELETE /api/v1/iam/audit with a
	// future cut-off, which names every event in the chain.
	futureTime := time.Now().Add(1 * time.Hour).UTC()
	deletePath := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&before=%s",
		tenantID, url.QueryEscape(futureTime.Format(time.RFC3339)))

	// The auditor holds no audit:delete grant and is refused.
	refusedResp, err := auditor.doHTTPRequest(http.MethodDelete, deletePath, nil)
	require.NoError(t, err, "Should send the auditor's audit delete")
	refusedBody, _ := io.ReadAll(refusedResp.Body)
	refusedResp.Body.Close()
	require.Equal(t, http.StatusForbidden, refusedResp.StatusCode,
		"an auditor without audit:delete should be refused: %s", refusedBody)

	// The tenant admin must be authorized for audit:delete here. The
	// audit log is append-only (go-qrdb's hash-chained audit sink), so an
	// authorized delete erases nothing and reports deleted_count 0.
	deleteResp, err := tenantAdmin.doHTTPRequest(http.MethodDelete, deletePath, nil)
	require.NoError(t, err, "Should send the tenant admin's audit delete")
	deleteBody, _ := io.ReadAll(deleteResp.Body)
	deleteResp.Body.Close()
	require.Equal(t, http.StatusOK, deleteResp.StatusCode,
		"the tenant admin's audit delete should be authorized: %s", deleteBody)
	var deleted struct {
		DeletedCount *int `json:"deleted_count"`
	}
	require.NoError(t, json.Unmarshal(deleteBody, &deleted), "audit delete response")
	require.NotNil(t, deleted.DeletedCount, "the audit delete response should carry deleted_count: %s", deleteBody)
	assert.Zero(t, *deleted.DeletedCount, "the append-only audit chain should delete nothing")

	// Every event read before the delete is still in the chain after it.
	// The count may grow: each authorized request, these reads and the
	// delete included, appends its authorization decision to the chain.
	var afterResp AuditQueryResponse
	require.NoError(t, auditor.getJSON(allEvents, &afterResp), "Should query events after delete")
	afterIDs := make(map[string]bool, len(afterResp.Events))
	for _, ev := range afterResp.Events {
		afterIDs[ev.ID] = true
	}
	for _, ev := range beforeResp.Events {
		assert.True(t, afterIDs[ev.ID], "audit event %s should survive the delete", ev.ID)
	}
	assert.GreaterOrEqual(t, afterResp.Total, beforeResp.Total, "the audit delete should not reduce the event count")
}

// TestAuditEventTenantIsolation tests that audit events are properly isolated between tenants.
func TestAuditEventTenantIsolation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	// Create two separate tenants, each with its own auditor
	tenant1ID, auditor1 := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenant1ID)

	tenant2ID, auditor2 := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenant2ID)

	tenant1UserEvents := fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=user_management", tenant1ID)
	before := auditBaseline(t, auditor1, tenant1UserEvents)

	// Create a user in tenant1
	email := generateUniqueEmail()
	createUserReq := map[string]interface{}{
		"email":    email,
		"username": generateUniqueUsername(),
		"name":     "Tenant 1 User",
	}
	var user1 struct {
		ID string `json:"id"`
	}
	// The user create acts in the test's tenant as the system admin
	// (tenant lift, pkg/api/rest/tenant_target.go).
	err := cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenant1ID), createUserReq, &user1)
	require.NoError(t, err)

	// Wait for the tenant1 user creation and the tenant2 creation events to become queryable
	waitForAuditEvents(t, auditor1, tenant1UserEvents, before+1)
	waitForAuditEvents(t, auditor2, fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenant2ID), 1)

	// Query audit events for tenant1
	var tenant1Resp AuditQueryResponse
	err = auditor1.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenant1ID), &tenant1Resp)
	require.NoError(t, err, "Should query tenant1 events")
	tenant1EventCount := tenant1Resp.Total

	// Query audit events for tenant2
	var tenant2Resp AuditQueryResponse
	err = auditor2.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenant2ID), &tenant2Resp)
	require.NoError(t, err, "Should query tenant2 events")
	tenant2EventCount := tenant2Resp.Total

	// Both tenants carry the same setup events (tenant creation and their
	// auditor's); tenant1 also has its extra user's creation
	assert.Greater(t, tenant1EventCount, tenant2EventCount,
		"Tenant1 should have more events than tenant2")

	// An auditor of one tenant cannot read another tenant's chain: the
	// request is rejected before it reaches storage (iam.ResolveRequestTenant)
	var crossResp AuditQueryResponse
	err = auditor1.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenant2ID), &crossResp)
	require.Error(t, err, "Tenant1's auditor should not read tenant2's events")
	assert.Contains(t, err.Error(), "403", "Should return 403 Forbidden")

	// Verify all tenant1 events belong to tenant1
	for _, event := range tenant1Resp.Events {
		assert.Equal(t, tenant1ID, event.TenantID,
			"All tenant1 events should belong to tenant1")
	}

	// Verify all tenant2 events belong to tenant2
	for _, event := range tenant2Resp.Events {
		assert.Equal(t, tenant2ID, event.TenantID,
			"All tenant2 events should belong to tenant2")
	}
}

// TestAuditEventGeneration tests that audit events are generated for various IAM operations.
func TestAuditEventGeneration(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	tests := []struct {
		name          string
		operation     func() error
		expectedEvent string
	}{
		{
			name: "User Creation",
			operation: func() error {
				email := generateUniqueEmail()
				createUserReq := map[string]interface{}{
					"email":    email,
					"username": generateUniqueUsername(),
					"name":     "Event Gen Test User",
				}
				// The user create acts in the test's tenant as the system admin
				// (tenant lift, pkg/api/rest/tenant_target.go).
				return cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createUserReq, nil)
			},
			expectedEvent: "user_management",
		},
		{
			name: "Group Creation",
			operation: func() error {
				createGroupReq := map[string]interface{}{
					"name":        fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
					"description": "Test group for audit events",
				}
				// The group create acts in the test's tenant as the system admin
				// (tenant lift, pkg/api/rest/tenant_target.go).
				return cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/groups", tenantID), createGroupReq, nil)
			},
			expectedEvent: "group_management",
		},
		{
			name: "Role Creation",
			operation: func() error {
				createRoleReq := map[string]interface{}{
					"name":        fmt.Sprintf("test-role-%d", time.Now().UnixNano()),
					"description": "Test role for audit events",
				}
				// The role create acts in the test's tenant as the system admin
				// (tenant lift, pkg/api/rest/tenant_target.go).
				return cluster.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/roles", tenantID), createRoleReq, nil)
			},
			expectedEvent: "role_management",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get event count before operation
			var beforeResp AuditQueryResponse
			err := auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenantID), &beforeResp)
			require.NoError(t, err)
			beforeCount := beforeResp.Total

			// Perform the operation
			err = tt.operation()
			require.NoError(t, err, "Operation should succeed")

			// Wait for the operation's audit event to become queryable
			waitForAuditEvents(t, auditor,
				fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenantID), beforeCount+1)

			// Get event count after operation
			var afterResp AuditQueryResponse
			err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s", tenantID), &afterResp)
			require.NoError(t, err)

			// Should have at least one more event
			assert.Greater(t, afterResp.Total, beforeCount,
				"Should have new audit event after %s", tt.name)

			// Check if the expected event type exists
			var eventTypeResp AuditQueryResponse
			err = auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&event_type=%s",
				tenantID, tt.expectedEvent), &eventTypeResp)
			require.NoError(t, err)
			assert.Greater(t, eventTypeResp.Total, 0,
				"Should have %s event type", tt.expectedEvent)
		})
	}
}

// TestAuditEventMissingTenantID tests that a query without tenant_id answers
// for the caller's own tenant (iam.ResolveRequestTenant defaults to it).
func TestAuditEventMissingTenantID(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	// Query without tenant_id
	resp := waitForAuditEvents(t, auditor, "/api/v1/iam/audit", 1)
	for _, event := range resp.Events {
		assert.Equal(t, tenantID, event.TenantID,
			"Events without tenant_id should belong to the caller's tenant")
	}
}

// TestAuditEventInvalidTimeFormat tests handling of invalid time format parameters.
func TestAuditEventInvalidTimeFormat(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	// The handler should gracefully handle invalid time formats by ignoring
	// them: the query must still succeed and return the tenant-creation
	// event. The audit write is async (LogAsync), so poll until visible.
	resp := waitForAuditEvents(t, auditor,
		fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&start_time=invalid-time", tenantID), 1)
	assert.Greater(t, resp.Total, 0, "Should still return events")
}

// TestAuditEventLargeLimit tests that limit parameter is capped at maximum.
func TestAuditEventLargeLimit(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID, auditor := setupTenantForAuditTests(t, cluster)
	defer cleanupAuditTests(cluster, tenantID)

	// Try to query with a very large limit
	var resp AuditQueryResponse
	err := auditor.getJSON(fmt.Sprintf("/api/v1/iam/audit?tenant_id=%s&limit=999999", tenantID), &resp)
	require.NoError(t, err, "Should query with large limit")

	// Limit should be capped at maximum (1000)
	assert.LessOrEqual(t, resp.Limit, 1000, "Limit should be capped at maximum")
	assert.LessOrEqual(t, len(resp.Events), 1000, "Should not return more than max limit")
}
