//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// newReservationTestServer mounts the two reservation routes with a principal
// injected the way the authz middleware injects one.
func newReservationTestServer(accountSvc service.AccountService, subject interfaces.Subject) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.AccountServiceFunc = func() service.AccountService { return accountSvc }
	router := chi.NewRouter()
	srv := &Server{service: mock, router: router}
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(injectPrincipal(r.Context(), subject, nil)))
		})
	})
	router.Route("/api/v1/agent-accounts", func(r chi.Router) {
		r.Post("/{id}/reservations", srv.HandleReserveAccountSession)
		r.Delete("/{id}/reservations/{rid}", srv.HandleReleaseAccountSession)
	})
	return srv
}

func doReservation(srv *Server, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)
	return w
}

func TestReservationREST_ReservesForThePrincipalsTenant(t *testing.T) {
	var captured *service.ReserveAccountSessionRequest
	srv := newReservationTestServer(&mocks.MockAccountService{
		ReserveAccountSessionFunc: func(_ context.Context, req *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			captured = req
			return &service.ReserveAccountSessionResponse{ReservationID: "r1", Timestamp: time.Now().UTC()}, nil
		},
	}, daemonSubject("tenant-1"))

	w := doReservation(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/reservations?tenant_id=tenant-1", `{"expected_live":2}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp ReserveAccountSessionRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.ReservationID != "r1" {
		t.Fatalf("response = %+v (%v), want r1", resp, err)
	}
	if captured == nil || captured.TenantID != "tenant-1" || captured.AccountID != "acct-1" ||
		captured.ExpectedLive == nil || *captured.ExpectedLive != 2 {
		t.Fatalf("service asked %+v", captured)
	}
}

func TestReservationREST_AFullAccountIsA409(t *testing.T) {
	srv := newReservationTestServer(&mocks.MockAccountService{
		ReserveAccountSessionFunc: func(context.Context, *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			return nil, &orchestration.AccountsSaturatedError{TenantID: "tenant-1", Candidates: 1}
		},
	}, daemonSubject("tenant-1"))
	w := doReservation(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/reservations?tenant_id=tenant-1", `{}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for an account at its cap", w.Code)
	}
}

func TestReservationREST_AMovedCountIsA412(t *testing.T) {
	srv := newReservationTestServer(&mocks.MockAccountService{
		ReserveAccountSessionFunc: func(context.Context, *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			return nil, &orchestration.AccountSessionCountChangedError{AccountID: "acct-1", Expected: 0, Actual: 1}
		},
	}, daemonSubject("tenant-1"))
	w := doReservation(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/reservations?tenant_id=tenant-1", `{"expected_live":0}`)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412 for a conditional claim whose count moved", w.Code)
	}
}

// TestReservationREST_APrincipalCannotReachAnotherTenant is the round-3 P1
// guard: a principal of tenant-1 can neither reserve nor release on tenant-2,
// and the refused call never reaches the service.
func TestReservationREST_APrincipalCannotReachAnotherTenant(t *testing.T) {
	reached := false
	srv := newReservationTestServer(&mocks.MockAccountService{
		ReserveAccountSessionFunc: func(context.Context, *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			reached = true
			return &service.ReserveAccountSessionResponse{}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			reached = true
			return &service.ReleaseAccountSessionResponse{ReservationID: req.ReservationID}, nil
		},
	}, daemonSubject("tenant-1"))
	if w := doReservation(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/reservations?tenant_id=tenant-2", `{}`); w.Code == http.StatusOK {
		t.Errorf("reserve on another tenant: status = %d, want a refusal", w.Code)
	}
	if w := doReservation(srv, http.MethodDelete, "/api/v1/agent-accounts/acct-1/reservations/r1?tenant_id=tenant-2", ""); w.Code == http.StatusOK {
		t.Errorf("release on another tenant: status = %d, want a refusal", w.Code)
	}
	if reached {
		t.Fatal("a cross-tenant call reached the service")
	}
}

func TestReservationREST_Releases(t *testing.T) {
	var captured *service.ReleaseAccountSessionRequest
	srv := newReservationTestServer(&mocks.MockAccountService{
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			captured = req
			return &service.ReleaseAccountSessionResponse{ReservationID: req.ReservationID}, nil
		},
	}, daemonSubject("tenant-1"))
	w := doReservation(srv, http.MethodDelete, "/api/v1/agent-accounts/acct-1/reservations/r9?tenant_id=tenant-1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if captured == nil || captured.ReservationID != "r9" || captured.TenantID != "tenant-1" {
		t.Fatalf("service asked %+v", captured)
	}
}
