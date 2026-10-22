// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// auditDeleteServer records the one audit delete request it receives and
// counts every request, so a refused delete can prove nothing was sent.
type auditDeleteServer struct {
	server   *httptest.Server
	requests atomic.Int32
	method   string
	path     string
	query    map[string][]string
}

func newAuditDeleteServer(t *testing.T, status int, body string) *auditDeleteServer {
	t.Helper()
	s := &auditDeleteServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.method = r.Method
		s.path = r.URL.Path
		s.query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func auditDelete(t *testing.T, s *auditDeleteServer, req *iam.AuditDeleteRequest) (interface{}, error) {
	t.Helper()
	tr := newTestTransport(t, s.server)
	return tr.Request(context.Background(), iamAuditServicePrefix, "Delete",
		map[string]interface{}{"request": req})
}

func TestIAMHandlerAuditDeleteSendsTenantAndBefore(t *testing.T) {
	s := newAuditDeleteServer(t, http.StatusOK, `{"deleted_count":3,"before":"2026-01-02T03:04:05Z"}`)
	before := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("plus2", 2*60*60))

	resp, err := auditDelete(t, s, &iam.AuditDeleteRequest{TenantID: "t1", Before: before, Confirm: true})
	if err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}
	if s.method != http.MethodDelete || s.path != "/api/v1/iam/audit" {
		t.Fatalf("request = %s %s, want DELETE /api/v1/iam/audit", s.method, s.path)
	}
	if got := s.query["tenant_id"]; len(got) != 1 || got[0] != "t1" {
		t.Fatalf("tenant_id = %v, want [t1]", got)
	}
	got := s.query["before"]
	if len(got) != 1 {
		t.Fatalf("before = %v, want one value", got)
	}
	parsed, err := time.Parse(time.RFC3339, got[0])
	if err != nil {
		t.Fatalf("before %q does not parse as RFC3339: %v", got[0], err)
	}
	if !parsed.Equal(before) {
		t.Fatalf("before = %v, want %v", parsed, before)
	}
	body, ok := resp.(map[string]interface{})
	if !ok || body["deleted_count"] != float64(3) {
		t.Fatalf("response = %#v, want deleted_count 3", resp)
	}
}

func TestIAMHandlerAuditDeleteRefusesUnsupportedScope(t *testing.T) {
	before := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name string
		req  *iam.AuditDeleteRequest
		want func(error) bool
	}{
		{
			name: "dry run",
			req:  &iam.AuditDeleteRequest{TenantID: "t1", Before: before, DryRun: true, Confirm: true},
			want: func(err error) bool {
				var refusal *iam.AuditDeleteDryRunUnsupportedError
				return stderrors.As(err, &refusal)
			},
		},
		{
			name: "event types",
			req: &iam.AuditDeleteRequest{TenantID: "t1", Before: before, Confirm: true,
				EventTypes: []iam.AuditEventType{iam.AuditEventType(1)}},
			want: func(err error) bool {
				var refusal *iam.AuditDeleteEventTypesUnsupportedError
				return stderrors.As(err, &refusal)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAuditDeleteServer(t, http.StatusOK, `{}`)
			_, err := auditDelete(t, s, tt.req)
			if !tt.want(err) {
				t.Fatalf("Delete() = %v, want the typed refusal", err)
			}
			if n := s.requests.Load(); n != 0 {
				t.Fatalf("server received %d requests, want 0", n)
			}
		})
	}
}

func TestIAMHandlerAuditDeleteRejectsMissingCriteria(t *testing.T) {
	tests := []struct {
		name string
		req  map[string]interface{}
	}{
		{name: "no request", req: map[string]interface{}{}},
		{name: "no before", req: map[string]interface{}{"request": &iam.AuditDeleteRequest{TenantID: "t1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAuditDeleteServer(t, http.StatusOK, `{}`)
			tr := newTestTransport(t, s.server)
			_, err := tr.Request(context.Background(), iamAuditServicePrefix, "Delete", tt.req)
			if !errors.IsInvalidInput(err) {
				t.Fatalf("Delete() = %v, want invalid input", err)
			}
			if n := s.requests.Load(); n != 0 {
				t.Fatalf("server received %d requests, want 0", n)
			}
		})
	}
}

func TestIAMHandlerAuditDeleteMapsServerErrors(t *testing.T) {
	tests := []struct {
		status int
		check  func(error) bool
	}{
		{status: http.StatusForbidden, check: errors.IsForbidden},
		{status: http.StatusInternalServerError, check: func(err error) bool {
			return errors.Code(err) == errors.CodeInternal
		}},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			s := newAuditDeleteServer(t, tt.status, `{"error":"denied","message":"denied"}`)
			_, err := auditDelete(t, s, &iam.AuditDeleteRequest{
				TenantID: "t1", Before: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Confirm: true,
			})
			if !tt.check(err) {
				t.Fatalf("Delete() = %v, want the typed error for HTTP %d", err, tt.status)
			}
			if n := s.requests.Load(); n != 1 {
				t.Fatalf("server received %d requests, want 1", n)
			}
		})
	}
}
