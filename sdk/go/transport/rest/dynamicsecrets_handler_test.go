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
	"testing"
	"time"

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingServer captures the method and path of the single request it
// serves and replies with body.
func recordingServer(t *testing.T, method, path string, body interface{}) (*httptest.Server, *string) {
	t.Helper()
	seen := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Method + " " + r.URL.Path
		if r.Method != method || r.URL.Path != path {
			t.Errorf("expected %s %s, got %s %s", method, path, r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	return srv, seen
}

func TestIsDynamicSecretsService(t *testing.T) {
	if !isDynamicSecretsService("quicbaas.DynamicSecrets") {
		t.Error("quicbaas.DynamicSecrets must route to the dynamic secrets handler")
	}
	if isDynamicSecretsService("quicbaas.Secrets") {
		t.Error("quicbaas.Secrets must not route to the dynamic secrets handler")
	}
}

func TestDynamicSecretsRESTHandler_CreateRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/secrets/dynamic/roles" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body dsCreateRoleWire
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.DefaultTTL != "1h0m0s" {
			t.Errorf("expected the SDK duration serialized as a Go duration string, got %q", body.DefaultTTL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(dsRoleWire{
			Name:          body.Name,
			GeneratorType: body.GeneratorType,
			Config:        body.Config,
			DefaultTTL:    body.DefaultTTL,
			MaxTTL:        body.MaxTTL,
			CreatedAt:     1767225600,
		})
	}))
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()

	out, err := tr.Request(context.Background(), "quicbaas.DynamicSecrets", "CreateRole",
		&dsclient.CreateRoleRequest{
			Name:          "readonly-db",
			GeneratorType: "postgres",
			Config:        map[string]string{"host": "db.internal"},
			DefaultTTL:    time.Hour,
			MaxTTL:        24 * time.Hour,
		})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	resp, ok := out.(*dsclient.CreateRoleResponse)
	if !ok {
		t.Fatalf("expected *CreateRoleResponse, got %T", out)
	}
	if resp.Role == nil || resp.Role.Name != "readonly-db" {
		t.Fatalf("unexpected role: %+v", resp.Role)
	}
	if resp.Role.DefaultTTL != time.Hour {
		t.Errorf("expected the wire duration string parsed back to 1h, got %s", resp.Role.DefaultTTL)
	}
	if resp.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be decoded from the Unix timestamp")
	}
}

func TestDynamicSecretsRESTHandler_GetAndDeleteRoleUsePathParam(t *testing.T) {
	srv, _ := recordingServer(t, http.MethodGet, "/api/v1/secrets/dynamic/roles/readonly-db",
		dsRoleWire{Name: "readonly-db", GeneratorType: "postgres", DefaultTTL: "1h0m0s", MaxTTL: "24h0m0s"})
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()

	out, err := tr.Request(context.Background(), "quicbaas.DynamicSecrets", "GetRole",
		&dsclient.GetRoleRequest{Name: "readonly-db"})
	if err != nil {
		t.Fatalf("GetRole failed: %v", err)
	}
	if out.(*dsclient.GetRoleResponse).Role.MaxTTL != 24*time.Hour {
		t.Error("MaxTTL round-trip failed")
	}

	delSrv, _ := recordingServer(t, http.MethodDelete, "/api/v1/secrets/dynamic/roles/readonly-db",
		dsDeleteRoleWire{Success: true, DeletedAt: 1767225600})
	defer delSrv.Close()
	delTr := newTestTransport(t, delSrv)
	defer delTr.Close()

	out, err = delTr.Request(context.Background(), "quicbaas.DynamicSecrets", "DeleteRole",
		&dsclient.DeleteRoleRequest{Name: "readonly-db"})
	if err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}
	if out.(*dsclient.DeleteRoleResponse).DeletedAt.IsZero() {
		t.Error("expected DeletedAt to be decoded")
	}
}

func TestDynamicSecretsRESTHandler_ListRolesAppliesPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("prefix"); got != "readonly-" {
			t.Errorf("expected prefix query param, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dsListRolesWire{
			Roles: []dsRoleWire{{Name: "readonly-db", DefaultTTL: "1h0m0s"}},
			Count: 1,
		})
	}))
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()

	out, err := tr.Request(context.Background(), "quicbaas.DynamicSecrets", "ListRoles",
		&dsclient.ListRolesRequest{Prefix: "readonly-"})
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}
	resp := out.(*dsclient.ListRolesResponse)
	if resp.Count != 1 || len(resp.Roles) != 1 {
		t.Fatalf("unexpected list response: %+v", resp)
	}
}

func TestDynamicSecretsRESTHandler_GenerateCredentialFoldsUsernamePassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/secrets/dynamic/credentials/readonly-db" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dsCredentialWire{
			CredentialID: "cred_1",
			Username:     "u1",
			Password:     "p1",
			Secret:       map[string]string{"dsn": "postgres://db"},
			LeaseID:      "lease_1",
			LeaseTTL:     "30m0s",
			LeaseExpiry:  1767225600,
			Renewable:    true,
		})
	}))
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()

	out, err := tr.Request(context.Background(), "quicbaas.DynamicSecrets", "GenerateCredential",
		&dsclient.GenerateCredentialRequest{RoleName: "readonly-db", TTL: 30 * time.Minute})
	if err != nil {
		t.Fatalf("GenerateCredential failed: %v", err)
	}
	resp := out.(*dsclient.GenerateCredentialResponse)
	if resp.Credential.Data["username"] != "u1" || resp.Credential.Data["password"] != "p1" {
		t.Errorf("username/password must be folded into the credential data map: %+v", resp.Credential.Data)
	}
	if resp.Credential.Data["dsn"] != "postgres://db" {
		t.Error("generator-specific secret fields must be preserved")
	}
	if resp.Lease == nil || resp.Lease.TTL != 30*time.Minute || !resp.Lease.Renewable {
		t.Errorf("unexpected lease: %+v", resp.Lease)
	}
}

func TestDynamicSecretsRESTHandler_LeaseVerbs(t *testing.T) {
	renewSrv, _ := recordingServer(t, http.MethodPost, "/api/v1/secrets/dynamic/leases/lease_1/renew",
		dsRenewedLeaseWire{LeaseID: "lease_1", LeaseTTL: "2h0m0s", LeaseExpiry: 1767225600, RenewCount: 2, RenewedAt: 1767222000})
	defer renewSrv.Close()
	renewTr := newTestTransport(t, renewSrv)
	defer renewTr.Close()

	out, err := renewTr.Request(context.Background(), "quicbaas.DynamicSecrets", "RenewLease",
		&dsclient.RenewLeaseRequest{LeaseID: "lease_1", Increment: time.Hour})
	if err != nil {
		t.Fatalf("RenewLease failed: %v", err)
	}
	if out.(*dsclient.RenewLeaseResponse).Lease.RenewCount != 2 {
		t.Error("renew count round-trip failed")
	}

	revokeSrv, _ := recordingServer(t, http.MethodDelete, "/api/v1/secrets/dynamic/leases/lease_1",
		dsRevokeLeaseWire{Success: true, RevokedAt: 1767225600})
	defer revokeSrv.Close()
	revokeTr := newTestTransport(t, revokeSrv)
	defer revokeTr.Close()

	out, err = revokeTr.Request(context.Background(), "quicbaas.DynamicSecrets", "RevokeLease",
		&dsclient.RevokeLeaseRequest{LeaseID: "lease_1"})
	if err != nil {
		t.Fatalf("RevokeLease failed: %v", err)
	}
	if out.(*dsclient.RevokeLeaseResponse).RevokedAt.IsZero() {
		t.Error("expected RevokedAt to be decoded")
	}

	listSrv, _ := recordingServer(t, http.MethodGet, "/api/v1/secrets/dynamic/leases",
		dsListLeasesWire{Leases: []dsLeaseWire{{LeaseID: "lease_1", Path: "database/readonly", TTL: "1h0m0s"}}, Count: 1})
	defer listSrv.Close()
	listTr := newTestTransport(t, listSrv)
	defer listTr.Close()

	out, err = listTr.Request(context.Background(), "quicbaas.DynamicSecrets", "ListLeases",
		&dsclient.ListLeasesRequest{})
	if err != nil {
		t.Fatalf("ListLeases failed: %v", err)
	}
	leases := out.(*dsclient.ListLeasesResponse)
	if leases.Count != 1 || leases.Leases[0].SecretPath != "database/readonly" {
		t.Errorf("unexpected lease list: %+v", leases)
	}
}

func TestDynamicSecretsRESTHandler_RejectsBadRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("handler must reject the request before dialing the server (%s %s)", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()

	h := newDynamicSecretsRESTHandler(tr)
	ctx := context.Background()

	cases := map[string]interface{}{
		"CreateRole":         nil,
		"GetRole":            &dsclient.GetRoleRequest{},
		"DeleteRole":         &dsclient.DeleteRoleRequest{},
		"GenerateCredential": &dsclient.GenerateCredentialRequest{},
		"RenewLease":         &dsclient.RenewLeaseRequest{},
		"RevokeLease":        &dsclient.RevokeLeaseRequest{},
	}
	for method, req := range cases {
		if _, err := h.handleRequest(ctx, method, req); err == nil {
			t.Errorf("%s must reject an empty request", method)
		}
	}
}

func TestDynamicSecretsRESTHandler_UnknownMethod(t *testing.T) {
	srv, _ := recordingServer(t, http.MethodGet, "/unused", struct{}{})
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()

	_, err := newDynamicSecretsRESTHandler(tr).handleRequest(context.Background(), "Nope", nil)
	if err == nil {
		t.Fatal("an unknown method must return a typed error")
	}
	if !sdkerrors.IsUnimplemented(err) {
		t.Errorf("expected CodeUnimplemented, got %v", sdkerrors.Code(err))
	}
}

func TestParseDurationOrZero(t *testing.T) {
	if got := parseDurationOrZero("1h30m"); got != 90*time.Minute {
		t.Errorf("expected 90m, got %s", got)
	}
	for _, bad := range []string{"", "not-a-duration"} {
		if got := parseDurationOrZero(bad); got != 0 {
			t.Errorf("expected zero for %q, got %s", bad, got)
		}
	}
}

func TestUnixToTime(t *testing.T) {
	if !unixToTime(0).IsZero() {
		t.Error("a zero timestamp must map to the zero time, not the epoch")
	}
	if unixToTime(1767225600).Unix() != 1767225600 {
		t.Error("non-zero timestamps must round-trip")
	}
}
