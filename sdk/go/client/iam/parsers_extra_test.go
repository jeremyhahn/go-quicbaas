// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"testing"
	"time"
)

func TestParseTenantResponse(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, err := parseTenantResponse(nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &Tenant{ID: "t1"}
		got, err := parseTenantResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseTenantResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("full nested map", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339)
		resp := map[string]interface{}{
			"tenant": map[string]interface{}{
				"id":          "t1",
				"name":        "acme",
				"description": "desc",
				"status":      "active",
				"settings": map[string]interface{}{
					"key1": "v",
					"num":  float64(3),
					"flag": true,
				},
				"created_at": now,
				"updated_at": now,
				"quotas": map[string]interface{}{
					"max_users":        float64(10),
					"max_groups":       float64(5),
					"max_roles":        float64(4),
					"max_policies":     float64(3),
					"max_applications": float64(2),
				},
			},
		}
		got, err := parseTenantResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "t1" || got.Status != TenantStatusActive {
			t.Errorf("basic wrong: %+v", got)
		}
		if got.DisplayName != "acme" {
			t.Error("display name fallback wrong")
		}
		if got.Settings["key1"] != "v" || got.Settings["num"] != "3" || got.Settings["flag"] != "true" {
			t.Errorf("settings wrong: %+v", got.Settings)
		}
		if got.Quotas == nil || got.Quotas.MaxUsers != 10 || got.Quotas.MaxApplications != 2 {
			t.Errorf("quotas wrong: %+v", got.Quotas)
		}
	})
}

func TestParseTenantStatus(t *testing.T) {
	cases := map[string]TenantStatus{
		"active":    TenantStatusActive,
		"suspended": TenantStatusSuspended,
		"pending":   TenantStatusUnspecified,
		"deleted":   TenantStatusDeleted,
		"other":     TenantStatusActive,
	}
	for in, want := range cases {
		if got := parseTenantStatus(in); got != want {
			t.Errorf("parseTenantStatus(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseTime(t *testing.T) {
	t.Run("rfc3339", func(t *testing.T) {
		if _, err := parseTime("2024-01-02T03:04:05Z"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("rfc3339nano", func(t *testing.T) {
		if _, err := parseTime("2024-01-02T03:04:05.123456789Z"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		if _, err := parseTime("not-a-time"); err == nil {
			t.Error("expected error")
		}
	})
}

func TestParseListTenantsResponse(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got, err := parseListTenantsResponse(nil)
		if err != nil || len(got.Tenants) != 0 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &ListTenantsResponse{}
		got, err := parseListTenantsResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseListTenantsResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map with tenants and pagination", func(t *testing.T) {
		resp := map[string]interface{}{
			"tenants": []interface{}{
				map[string]interface{}{"id": "t1", "name": "a"},
				map[string]interface{}{"id": "t2", "name": "b"},
			},
			"pagination": map[string]interface{}{
				"total_count":     float64(2),
				"page_size":       float64(10),
				"next_page_token": "tok",
			},
		}
		got, err := parseListTenantsResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Tenants) != 2 || got.Pagination.TotalCount != 2 || got.Pagination.NextPageToken != "tok" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestParseSessionFromMap(t *testing.T) {
	now := time.Now().UTC()
	m := map[string]interface{}{
		"id":           "s1",
		"user_id":      "u1",
		"tenant_id":    "t1",
		"mfa_verified": true,
		"ip_address":   "1.2.3.4",
		"user_agent":   "agent",
		"device_info":  "device",
		"created_at":   now,
		"last_active":  now.Format(time.RFC3339),
		"expires_at":   now.Format(time.RFC3339Nano),
	}
	s := parseSessionFromMap(m)
	if s.ID != "s1" || s.UserID != "u1" || !s.MFAVerified {
		t.Errorf("basic wrong: %+v", s)
	}
	if s.IPAddress != "1.2.3.4" || s.DeviceInfo != "device" {
		t.Error("ip/device wrong")
	}
	if s.Created.IsZero() {
		t.Error("created time should be set from time.Time")
	}
}

func TestParseSessionResponse(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, err := parseSessionResponse(nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &Session{ID: "s1"}
		got, err := parseSessionResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseSessionResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map", func(t *testing.T) {
		got, err := parseSessionResponse(map[string]interface{}{"id": "s1"})
		if err != nil || got.ID != "s1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestParseListSessionsResponse(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got, err := parseListSessionsResponse(nil)
		if err != nil || len(got.Sessions) != 0 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &ListSessionsResponse{}
		got, err := parseListSessionsResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseListSessionsResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map with sessions and total", func(t *testing.T) {
		resp := map[string]interface{}{
			"sessions": []interface{}{
				map[string]interface{}{"id": "s1"},
			},
			"total_count": float64(1),
		}
		got, err := parseListSessionsResponse(resp)
		if err != nil || len(got.Sessions) != 1 || got.TotalCount != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map with pagination total", func(t *testing.T) {
		resp := map[string]interface{}{
			"sessions":   []interface{}{},
			"pagination": map[string]interface{}{"total_count": float64(5)},
		}
		got, err := parseListSessionsResponse(resp)
		if err != nil || got.TotalCount != 5 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestParseTimeFromMap(t *testing.T) {
	now := time.Now().UTC()
	t.Run("time.Time value", func(t *testing.T) {
		got := parseTimeFromMap(map[string]interface{}{"a": now}, "a")
		if !got.Equal(now) {
			t.Error("expected time.Time pass-through")
		}
	})
	t.Run("rfc3339 string", func(t *testing.T) {
		got := parseTimeFromMap(map[string]interface{}{"a": now.Format(time.RFC3339)}, "a")
		if got.IsZero() {
			t.Error("expected parsed time")
		}
	})
	t.Run("second key", func(t *testing.T) {
		got := parseTimeFromMap(map[string]interface{}{"b": now.Format(time.RFC3339Nano)}, "a", "b")
		if got.IsZero() {
			t.Error("expected parse via second key")
		}
	})
	t.Run("missing returns zero", func(t *testing.T) {
		got := parseTimeFromMap(map[string]interface{}{}, "a")
		if !got.IsZero() {
			t.Error("expected zero time")
		}
	})
	t.Run("unparseable string returns zero", func(t *testing.T) {
		got := parseTimeFromMap(map[string]interface{}{"a": "garbage"}, "a")
		if !got.IsZero() {
			t.Error("expected zero time for unparseable")
		}
	})
}

func TestParseAuditEventTypeFromString(t *testing.T) {
	cases := map[string]AuditEventType{
		"authentication":    AuditEventTypeAuthentication,
		"authorization":     AuditEventTypeAuthorization,
		"user_management":   AuditEventTypeUserManagement,
		"role_management":   AuditEventTypeRoleManagement,
		"policy_management": AuditEventTypePolicyManagement,
		"data_access":       AuditEventTypeDataAccess,
		"configuration":     AuditEventTypeConfiguration,
		"system":            AuditEventTypeSystem,
		"gdpr":              AuditEventTypeGDPR,
		"admin":             AuditEventTypeAdmin,
		"unknown":           AuditEventTypeSystem,
	}
	for in, want := range cases {
		if got := parseAuditEventTypeFromString(in); got != want {
			t.Errorf("parseAuditEventTypeFromString(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseAuditOutcomeFromString(t *testing.T) {
	cases := map[string]AuditOutcome{
		"success": AuditOutcomeSuccess,
		"failure": AuditOutcomeFailure,
		"denied":  AuditOutcomeDenied,
		"error":   AuditOutcomeError,
		"other":   AuditOutcomeSuccess,
	}
	for in, want := range cases {
		if got := parseAuditOutcomeFromString(in); got != want {
			t.Errorf("parseAuditOutcomeFromString(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseAuditEventFromMap(t *testing.T) {
	now := time.Now().UTC()
	m := map[string]interface{}{
		"id":          "e1",
		"tenant_id":   "t1",
		"event_type":  "authentication",
		"action":      "login",
		"outcome":     "success",
		"actor_id":    "a1",
		"actor_type":  "user",
		"resource_id": "r1",
		"ip_address":  "1.2.3.4",
		"user_agent":  "agent",
		"timestamp":   now,
		"details":     map[string]interface{}{"k": "v"},
	}
	got := parseAuditEventFromMap(m)
	if got.ID != "e1" || got.Action != "login" {
		t.Errorf("basic wrong: %+v", got)
	}
	if got.Actor == nil || got.Actor.ID != "a1" || got.Actor.IPAddress != "1.2.3.4" {
		t.Errorf("actor wrong: %+v", got.Actor)
	}
	if got.Resource == nil || got.Resource.ID != "r1" {
		t.Errorf("resource wrong: %+v", got.Resource)
	}
	if got.Details["k"] != "v" {
		t.Error("details wrong")
	}
}

func TestParseAuditEventFromMap_StringDetailsAndCategory(t *testing.T) {
	m := map[string]interface{}{
		"category": "authorization",
		"details":  map[string]string{"x": "y"},
	}
	got := parseAuditEventFromMap(m)
	if got.EventType != AuditEventTypeAuthorization {
		t.Error("category not parsed")
	}
	if got.Details["x"] != "y" {
		t.Error("string details not converted")
	}
}

func TestConvertSdkAuditEventToAuditEvent(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got := convertSdkAuditEventToAuditEvent(nil)
		if got.ID != "" {
			t.Error("expected empty event")
		}
	})
	t.Run("full", func(t *testing.T) {
		in := &sdkAuditEventResponse{
			ID:         "e1",
			TenantID:   "t1",
			EventType:  "authentication",
			Outcome:    "success",
			ActorID:    "a1",
			ActorType:  "user",
			ResourceID: "r1",
			Action:     "login",
			IPAddress:  "1.2.3.4",
			UserAgent:  "agent",
			Details:    map[string]string{"k": "v"},
		}
		got := convertSdkAuditEventToAuditEvent(in)
		if got.ID != "e1" || got.Actor == nil || got.Actor.ID != "a1" {
			t.Errorf("wrong: %+v", got)
		}
		if got.Resource == nil || got.Resource.ID != "r1" {
			t.Error("resource wrong")
		}
		if got.Details["k"] != "v" {
			t.Error("details wrong")
		}
	})
}

func TestParseAuditEventResponse(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if _, err := parseAuditEventResponse(nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &AuditEvent{ID: "e1"}
		got, err := parseAuditEventResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("sdk response type", func(t *testing.T) {
		got, err := parseAuditEventResponse(&sdkAuditEventResponse{ID: "e1"})
		if err != nil || got.ID != "e1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseAuditEventResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map", func(t *testing.T) {
		got, err := parseAuditEventResponse(map[string]interface{}{"id": "e1"})
		if err != nil || got.ID != "e1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestParseAuditQueryResponse(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got, err := parseAuditQueryResponse(nil)
		if err != nil || len(got.Events) != 0 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &AuditQueryResponse{}
		got, err := parseAuditQueryResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseAuditQueryResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map with events from map and sdk type", func(t *testing.T) {
		resp := map[string]interface{}{
			"events": []interface{}{
				map[string]interface{}{"id": "e1"},
				&sdkAuditEventResponse{ID: "e2"},
			},
			"pagination": map[string]interface{}{
				"total_count": float64(2),
				"limit":       float64(2),
				"offset":      float64(0),
			},
		}
		got, err := parseAuditQueryResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Events) != 2 || got.TotalCount != 2 {
			t.Errorf("got %+v", got)
		}
		if !got.HasMore {
			t.Error("expected HasMore true when len >= limit")
		}
	})
	t.Run("direct total_count", func(t *testing.T) {
		resp := map[string]interface{}{
			"events":      []interface{}{},
			"total_count": float64(7),
		}
		got, err := parseAuditQueryResponse(resp)
		if err != nil || got.TotalCount != 7 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestParseCheckResponse(t *testing.T) {
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &CheckResponse{Allowed: true}
		got, err := parseCheckResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseCheckResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("allowed map", func(t *testing.T) {
		resp := map[string]interface{}{
			"allowed":            true,
			"matched_policy":     "p1",
			"matched_rule":       "r1",
			"evaluation_time_ns": float64(100),
			"reason":             "ok",
			"cache_hit":          true,
		}
		got, err := parseCheckResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Allowed || got.Effect != EffectAllow || got.MatchedPolicy != "p1" {
			t.Errorf("got %+v", got)
		}
		if got.EvaluationTimeNs != 100 || !got.CacheHit {
			t.Error("eval/cache wrong")
		}
	})
	t.Run("denied map", func(t *testing.T) {
		got, err := parseCheckResponse(map[string]interface{}{"allowed": false})
		if err != nil || got.Effect != EffectDeny {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestParseCheckBatchResponse(t *testing.T) {
	t.Run("pointer pass-through", func(t *testing.T) {
		want := &CheckBatchResponse{}
		got, err := parseCheckBatchResponse(want)
		if err != nil || got != want {
			t.Error("expected pass-through")
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if _, err := parseCheckBatchResponse(42); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("results bool map", func(t *testing.T) {
		resp := map[string]interface{}{
			"results": map[string]bool{"a": true, "b": false},
		}
		got, err := parseCheckBatchResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Results) != 2 || got.AllowedCount != 1 || got.DeniedCount != 1 {
			t.Errorf("got %+v", got)
		}
		if got.AllAllowed {
			t.Error("should not be all allowed")
		}
	})
	t.Run("results iface slice", func(t *testing.T) {
		resp := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"allowed": true, "matched_policy": "p1", "matched_rule": "r1"},
				map[string]interface{}{"allowed": false},
			},
			"evaluation_time_ns": float64(500),
		}
		got, err := parseCheckBatchResponse(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Results) != 2 || got.AllowedCount != 1 || got.TotalEvaluationTimeNs != 500 {
			t.Errorf("got %+v", got)
		}
	})
}
