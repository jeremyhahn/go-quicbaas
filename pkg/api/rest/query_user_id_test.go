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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// The user_id query tolerated on the self-service routes must accept every
// identifier the caller owns: the subject ID, the username, and — under a
// username login, where the subject carries the name — the caller's own
// numeric account ID. Anything else is a foreign identity.

// aliceAccountID is the account a username login as "alice" verified.
const aliceAccountID = 7

// aliceLogin is a username login whose credential verified account 7.
func aliceLogin(tenantID string) interfaces.Subject {
	return interfaces.Subject{
		ID: "alice", IDKind: interfaces.SubjectIDUsername, Username: "alice",
		UserID: "7", TenantID: tenantID, Authenticated: true,
	}
}

// userIDQueryCases are the user_id values every route family must judge
// alike for aliceLogin.
var userIDQueryCases = []struct {
	name    string
	userID  string
	allowed bool
}{
	{"own numeric account id", "7", true},
	{"own username", "alice", true},
	{"foreign numeric account id", "42", false},
	{"foreign username", "bob", false},
}

// resolvingService adds caller account resolution to the agent-account
// mock, which does not otherwise provide it.
type resolvingService struct {
	*mocks.MockQuicBaaS
	resolve func(ctx context.Context, subject interfaces.Subject) (uint64, error)
}

func (s *resolvingService) ResolveCallerAccount(ctx context.Context, subject interfaces.Subject) (uint64, error) {
	return s.resolve(ctx, subject)
}

// resolveAlice resolves aliceLogin to account 7 and refuses anyone else.
func resolveAlice(_ context.Context, subject interfaces.Subject) (uint64, error) {
	if subject.ID == "alice" {
		return aliceAccountID, nil
	}
	return 0, &service.CallerAccountRefusedError{Reason: "the subject names no user account"}
}

// failResolution is a resolver whose identity store is unavailable.
func failResolution(context.Context, interfaces.Subject) (uint64, error) {
	return 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test.ResolveCallerAccount"}
}

// accountRoute is one agent-account route that tolerates a user_id query.
type accountRoute struct {
	name   string
	method string
	path   string
	body   string
}

var accountRoutes = []accountRoute{
	{"reserve", http.MethodPost, "/api/v1/agent-accounts/acct-1/reservations", `{}`},
	{"release", http.MethodDelete, "/api/v1/agent-accounts/acct-1/reservations/r1", ""},
	{"materialize", http.MethodPost, "/api/v1/agent-accounts/acct-1/materialize", ""},
}

// newAccountRouteServer mounts every account route over an account service
// that counts the calls the routes make into it.
func newAccountRouteServer(
	subject interfaces.Subject,
	resolve func(context.Context, interfaces.Subject) (uint64, error),
	calls *int,
) *Server {
	accountSvc := &mocks.MockAccountService{
		ReserveAccountSessionFunc: func(context.Context, *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			*calls++
			return &service.ReserveAccountSessionResponse{ReservationID: "r1", Timestamp: time.Now().UTC()}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			*calls++
			return &service.ReleaseAccountSessionResponse{ReservationID: req.ReservationID}, nil
		},
	}
	captured := new(*service.MaterializeAccountCredentialRequest)
	materializing := releasingAccountService(captured)
	accountSvc.MaterializeAccountCredentialFunc = func(
		ctx context.Context, req *service.MaterializeAccountCredentialRequest,
	) (*service.MaterializeAccountCredentialResponse, error) {
		*calls++
		return materializing.MaterializeAccountCredentialFunc(ctx, req)
	}

	mock := mocks.NewMockQuicBaaS()
	mock.AccountServiceFunc = func() service.AccountService { return accountSvc }
	router := chi.NewRouter()
	srv := &Server{service: &resolvingService{MockQuicBaaS: mock, resolve: resolve}, router: router}
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(injectPrincipal(r.Context(), subject, nil)))
		})
	})
	router.Route("/api/v1/agent-accounts", func(r chi.Router) {
		r.Post("/{id}/reservations", srv.HandleReserveAccountSession)
		r.Delete("/{id}/reservations/{rid}", srv.HandleReleaseAccountSession)
		r.Post("/{id}/materialize", srv.HandleMaterializeAccountCredential)
	})
	return srv
}

func TestAccountRoutes_UserIDQueryAcceptsOnlyTheCallersOwnIdentifiers(t *testing.T) {
	for _, route := range accountRoutes {
		for _, tc := range userIDQueryCases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				var calls int
				srv := newAccountRouteServer(aliceLogin("tenant-1"), resolveAlice, &calls)

				w := doReservation(srv, route.method, route.path+"?user_id="+tc.userID, route.body)
				if tc.allowed {
					assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
					assert.Equal(t, 1, calls)
					return
				}
				assert.Equal(t, http.StatusForbidden, w.Code)
				assert.Contains(t, w.Body.String(), "user_id parameter does not match")
				assert.Zero(t, calls)
			})
		}
	}
}

// An agent caller has no user account; the routes serve it without
// resolving one unless it names a numeric user_id only an account could
// match, and then the refusal stands.
func TestAccountRoutes_AnAgentCallerNeedsNoAccountUnlessItNamesOne(t *testing.T) {
	for _, route := range accountRoutes {
		t.Run(route.name, func(t *testing.T) {
			var calls int
			srv := newAccountRouteServer(daemonSubject("tenant-1"), resolveAlice, &calls)

			w := doReservation(srv, route.method, route.path+"?user_id=agent-agent-04", route.body)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

			w = doReservation(srv, route.method, route.path+"?user_id=7", route.body)
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Equal(t, 1, calls, "the numeric query must not reach the service")
		})
	}
}

// A resolver failure is never a match: the request is refused and the
// account service is never reached.
func TestAccountRoutes_AResolverFailureIsRefused(t *testing.T) {
	for _, route := range accountRoutes {
		t.Run(route.name, func(t *testing.T) {
			var calls int
			srv := newAccountRouteServer(aliceLogin("tenant-1"), failResolution, &calls)

			w := doReservation(srv, route.method, route.path+"?user_id=7", route.body)
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Zero(t, calls)
		})
	}
}

// sessionRoute is one self-service session route that tolerates a user_id
// query.
type sessionRoute struct {
	name   string
	method string
	status int
	call   func(h *IAMHandler, w http.ResponseWriter, r *http.Request)
}

var sessionRoutes = []sessionRoute{
	{"list", http.MethodGet, http.StatusOK, (*IAMHandler).ListSessions},
	{"revoke all", http.MethodDelete, http.StatusNoContent, (*IAMHandler).RevokeAllSessions},
}

func TestSessionRoutes_UserIDQueryAcceptsOnlyTheCallersOwnIdentifiers(t *testing.T) {
	for _, route := range sessionRoutes {
		for _, tc := range userIDQueryCases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				mgr := &mockSessionManager{}
				svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
				handler, cleanup := setupHandlerWithService(t, svc)
				defer cleanup()

				req := httptest.NewRequest(route.method, "/api/v1/iam/sessions?user_id="+tc.userID, nil).
					WithContext(auth.SubjectIntoContext(context.Background(), aliceLogin("t1")))
				w := httptest.NewRecorder()
				route.call(handler, w, req)

				if tc.allowed {
					assert.Equal(t, route.status, w.Code, w.Body.String())
					assert.Equal(t, uint64(aliceAccountID), mgr.listedUserID+mgr.revokedUserID)
					return
				}
				assert.Equal(t, http.StatusForbidden, w.Code)
				assert.Contains(t, w.Body.String(), "user_id parameter does not match")
				assert.Zero(t, mgr.listedUserID+mgr.revokedUserID)
			})
		}
	}
}

// A caller with no verified account is refused even when its user_id
// spells a real account.
func TestSessionRoutes_AResolverFailureIsRefused(t *testing.T) {
	for _, route := range sessionRoutes {
		t.Run(route.name, func(t *testing.T) {
			mgr := &mockSessionManager{}
			svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
			handler, cleanup := setupHandlerWithService(t, svc)
			defer cleanup()

			req := httptest.NewRequest(route.method, "/api/v1/iam/sessions?user_id=7", nil).
				WithContext(auth.SubjectIntoContext(context.Background(), interfaces.Subject{
					ID: "alice", TenantID: "t1", Authenticated: true,
				}))
			w := httptest.NewRecorder()
			route.call(handler, w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Zero(t, mgr.listedUserID+mgr.revokedUserID)
		})
	}
}
