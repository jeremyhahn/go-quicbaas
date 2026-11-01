// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package oktaprovision

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// routeHandler is a small router that dispatches on "METHOD PATH-prefix" so
// individual tests can stub only the endpoints they exercise without the strict
// mockOkta default-case t.Errorf tripping on benign extra calls.
type routeHandler struct {
	t       *testing.T
	routes  map[string]http.HandlerFunc
	prefix  map[string]http.HandlerFunc
	fail    bool // when true, default returns 500 instead of t.Errorf
	dfltErr bool
}

func newRouter(t *testing.T) *routeHandler {
	return &routeHandler{
		t:      t,
		routes: map[string]http.HandlerFunc{},
		prefix: map[string]http.HandlerFunc{},
	}
}

func (h *routeHandler) on(method, path string, fn http.HandlerFunc) *routeHandler {
	h.routes[method+" "+path] = fn
	return h
}

func (h *routeHandler) onPrefix(method, pathPrefix string, fn http.HandlerFunc) *routeHandler {
	h.prefix[method+" "+pathPrefix] = fn
	return h
}

func (h *routeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	if fn, ok := h.routes[key]; ok {
		fn(w, r)
		return
	}
	for k, fn := range h.prefix {
		parts := strings.SplitN(k, " ", 2)
		if r.Method == parts[0] && strings.HasPrefix(r.URL.Path, parts[1]) {
			fn(w, r)
			return
		}
	}
	if h.dfltErr {
		writeOktaErr(w, http.StatusInternalServerError, "E0000009", "internal error")
		return
	}
	h.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNotFound)
}

func okJSON(v interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, v) }
}

func errStatus(status int, code, summary string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeOktaErr(w, status, code, summary) }
}

func rawBody(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// -------------------- errors.go --------------------

func TestInvalidOrgNameError(t *testing.T) {
	e := &InvalidOrgNameError{OrgName: "bad name"}
	got := e.Error()
	if !strings.Contains(got, "bad name") {
		t.Errorf("Error() = %q, want it to contain the org name", got)
	}
	if !errors.Is(e, ErrInvalidOrgName) {
		t.Errorf("Is(ErrInvalidOrgName) = false, want true")
	}
	if errors.Is(e, ErrProvisioning) {
		t.Errorf("Is(ErrProvisioning) = true, want false")
	}
}

func TestProvisioningError(t *testing.T) {
	inner := errors.New("boom")
	e := &ProvisioningError{Operation: "create_app", OrgName: "acme", Err: inner}
	got := e.Error()
	for _, want := range []string{"create_app", "acme", "boom"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}
	if e.Unwrap() != inner {
		t.Errorf("Unwrap() = %v, want %v", e.Unwrap(), inner)
	}
	if !errors.Is(e, inner) {
		t.Errorf("errors.Is(e, inner) = false, want true (via Unwrap)")
	}
	if !errors.Is(e, ErrProvisioning) {
		t.Errorf("Is(ErrProvisioning) = false, want true")
	}
	if errors.Is(e, ErrInvalidConfig) {
		t.Errorf("Is(ErrInvalidConfig) = true, want false")
	}
}

func TestInvalidConfigError(t *testing.T) {
	e := &InvalidConfigError{Reason: "api token is required"}
	got := e.Error()
	if !strings.Contains(got, "api token is required") {
		t.Errorf("Error() = %q, want it to contain the reason", got)
	}
	if !errors.Is(e, ErrInvalidConfig) {
		t.Errorf("Is(ErrInvalidConfig) = false, want true")
	}
	if errors.Is(e, ErrProvisioning) {
		t.Errorf("Is(ErrProvisioning) = true, want false")
	}
}

// -------------------- ensureApp existing-app path (getApp/mergeAppUpdate/updateApp) --------------------

func TestProvisionApp_ExistingAppUpdated(t *testing.T) {
	r := newRouter(t)
	// findAppByLabel returns the matching existing app.
	r.on("GET", "/api/v1/apps", okJSON([]map[string]interface{}{
		{
			"id":    "app-existing",
			"label": "Acme App",
			"credentials": map[string]interface{}{
				"oauthClient": map[string]interface{}{"client_id": "existing-client"},
			},
		},
	}))
	// getApp returns a full representation (with server-managed fields to strip).
	r.on("GET", "/api/v1/apps/app-existing", okJSON(map[string]interface{}{
		"id":          "app-existing",
		"label":       "Old Label",
		"_links":      map[string]interface{}{"self": "x"},
		"created":     "2020-01-01",
		"lastUpdated": "2020-01-02",
		"credentials": map[string]interface{}{},
		"settings":    map[string]interface{}{},
	}))
	// updateApp (PUT) returns the updated app.
	r.on("PUT", "/api/v1/apps/app-existing", okJSON(map[string]interface{}{
		"id": "app-existing",
		"credentials": map[string]interface{}{
			"oauthClient": map[string]interface{}{"client_id": "existing-client", "client_secret": ""},
		},
	}))
	// groups + auth-server config endpoints.
	r.on("GET", "/api/v1/groups", okJSON([]interface{}{})).
		on("POST", "/api/v1/groups", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"id": "g1", "profile": map[string]interface{}{"name": "Acme Administrators"}})
		}).
		onPrefix("PUT", "/api/v1/apps/app-existing/groups/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }).
		on("GET", "/api/v1/users/me", okJSON(map[string]interface{}{"id": "owner-1"})).
		onPrefix("PUT", "/api/v1/groups/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }).
		on("GET", "/api/v1/authorizationServers/default/scopes", okJSON([]map[string]interface{}{{"id": "s1", "name": "groups"}})).
		on("GET", "/api/v1/authorizationServers/default/policies", okJSON([]map[string]interface{}{{"id": "pol-1", "name": "Acme App Access Policy"}})).
		onPrefix("GET", "/api/v1/authorizationServers/default/policies/pol-1/rules", okJSON([]map[string]interface{}{{"id": "r1", "name": "Acme App Default Rule"}}))

	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = []RoleGroup{{Name: "Acme Administrators", Description: "admins"}}
	req.GroupsClaimFilter = GroupsClaimFilter{} // skip claim config
	res, err := p.ProvisionApp(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.ClientID != "existing-client" {
		t.Errorf("ClientID = %q, want existing-client", res.ClientID)
	}
	if res.ClientSecret != "" {
		t.Errorf("ClientSecret = %q, want empty on update", res.ClientSecret)
	}
}

func TestEnsureApp_FindError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", errStatus(http.StatusForbidden, "E0000006", "no access"))
	p := newTestProvisioner(t, r)
	_, _, err := p.ensureApp(context.Background(), "https://acme.okta.com", sampleRequest())
	assertProvOp(t, err, "find_app")
}

func TestEnsureApp_GetAppError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]map[string]interface{}{{"id": "app-x", "label": "Acme App"}}))
	r.on("GET", "/api/v1/apps/app-x", errStatus(http.StatusInternalServerError, "E0000009", "boom"))
	p := newTestProvisioner(t, r)
	_, _, err := p.ensureApp(context.Background(), "https://acme.okta.com", sampleRequest())
	assertProvOp(t, err, "get_app")
}

func TestEnsureApp_UpdateAppError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]map[string]interface{}{{"id": "app-x", "label": "Acme App"}}))
	r.on("GET", "/api/v1/apps/app-x", okJSON(map[string]interface{}{"id": "app-x"}))
	r.on("PUT", "/api/v1/apps/app-x", errStatus(http.StatusBadRequest, "E0000001", "bad"))
	p := newTestProvisioner(t, r)
	_, _, err := p.ensureApp(context.Background(), "https://acme.okta.com", sampleRequest())
	assertProvOp(t, err, "update_app")
}

// -------------------- getApp / updateApp decode errors --------------------

func TestGetApp_DecodeError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps/app-x", rawBody(http.StatusOK, "not-json"))
	p := newTestProvisioner(t, r)
	_, err := p.getApp(context.Background(), "https://acme.okta.com", "tok", "app-x")
	assertProvOp(t, err, "get_app")
}

func TestUpdateApp_DecodeError(t *testing.T) {
	r := newRouter(t)
	r.on("PUT", "/api/v1/apps/app-x", rawBody(http.StatusOK, "not-json"))
	p := newTestProvisioner(t, r)
	_, err := p.updateApp(context.Background(), "https://acme.okta.com", "tok", "app-x", map[string]interface{}{})
	assertProvOp(t, err, "update_app")
}

// -------------------- mergeAppUpdate (nil/non-map branches) --------------------

func TestMergeAppUpdate_FillsMissingMaps(t *testing.T) {
	app := map[string]interface{}{
		"_links":      "x",
		"orn":         "y",
		"credentials": nil,
		"settings":    "not-a-map",
	}
	mergeAppUpdate(app, "New Label", "https://cb", "https://app")

	if app["label"] != "New Label" {
		t.Errorf("label = %v", app["label"])
	}
	if _, ok := app["_links"]; ok {
		t.Error("_links should have been deleted")
	}
	creds := app["credentials"].(map[string]interface{})
	oauthCreds := creds["oauthClient"].(map[string]interface{})
	if oauthCreds["token_endpoint_auth_method"] != "client_secret_post" {
		t.Errorf("token_endpoint_auth_method = %v", oauthCreds["token_endpoint_auth_method"])
	}
	settings := app["settings"].(map[string]interface{})
	oauthSet := settings["oauthClient"].(map[string]interface{})
	if oauthSet["client_uri"] != "https://app" {
		t.Errorf("client_uri = %v", oauthSet["client_uri"])
	}
	if _, ok := oauthSet["response_types"]; !ok {
		t.Error("response_types default not set")
	}
	if _, ok := oauthSet["idp_initiated_login"]; !ok {
		t.Error("idp_initiated_login default not set")
	}
}

func TestMergeAppUpdate_PreservesExistingOAuthSettings(t *testing.T) {
	app := map[string]interface{}{
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"existing": true}},
		"settings": map[string]interface{}{"oauthClient": map[string]interface{}{
			"response_types":      []interface{}{"token"},
			"grant_types":         []interface{}{"implicit"},
			"consent_method":      "TRUSTED",
			"idp_initiated_login": map[string]interface{}{"mode": "ENABLED"},
		}},
	}
	mergeAppUpdate(app, "L", "https://cb", "https://app")
	oauthSet := app["settings"].(map[string]interface{})["oauthClient"].(map[string]interface{})
	if rt := oauthSet["response_types"].([]interface{}); rt[0] != "token" {
		t.Errorf("response_types overwritten = %v", rt)
	}
	if oauthSet["consent_method"] != "TRUSTED" {
		t.Errorf("consent_method overwritten = %v", oauthSet["consent_method"])
	}
}

// -------------------- listGroupMembers --------------------

func TestListGroupMembers_Success(t *testing.T) {
	r := newRouter(t)
	r.onPrefix("GET", "/api/v1/groups/g1/users", okJSON([]map[string]interface{}{{"id": "u1"}, {"id": "u2"}}))
	p := newTestProvisioner(t, r)
	users, err := p.listGroupMembers(context.Background(), "https://acme.okta.com", "tok", "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 2 || users[0].ID != "u1" || users[1].ID != "u2" {
		t.Errorf("users = %+v", users)
	}
}

func TestListGroupMembers_HTTPError(t *testing.T) {
	r := newRouter(t)
	r.onPrefix("GET", "/api/v1/groups/g1/users", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	_, err := p.listGroupMembers(context.Background(), "https://acme.okta.com", "tok", "g1")
	assertProvOp(t, err, "list_group_members")
}

func TestListGroupMembers_DecodeError(t *testing.T) {
	r := newRouter(t)
	r.onPrefix("GET", "/api/v1/groups/g1/users", rawBody(http.StatusOK, "{not-an-array"))
	p := newTestProvisioner(t, r)
	_, err := p.listGroupMembers(context.Background(), "https://acme.okta.com", "tok", "g1")
	assertProvOp(t, err, "list_group_members")
}

func TestListGroupMembers_RequestError(t *testing.T) {
	p := New(Config{}, nil)
	_, err := p.listGroupMembers(context.Background(), "://bad-url", "tok", "g1")
	assertProvOp(t, err, "list_group_members")
}

// -------------------- getTokenOwner --------------------

func TestGetTokenOwner_Success(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", okJSON(map[string]interface{}{"id": "owner-9"}))
	p := newTestProvisioner(t, r)
	u, err := p.getTokenOwner(context.Background(), "https://acme.okta.com", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != "owner-9" {
		t.Errorf("id = %q", u.ID)
	}
}

func TestGetTokenOwner_HTTPError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", errStatus(http.StatusUnauthorized, "E0000011", "invalid token"))
	p := newTestProvisioner(t, r)
	_, err := p.getTokenOwner(context.Background(), "https://acme.okta.com", "tok")
	assertProvOp(t, err, "get_token_owner")
}

func TestGetTokenOwner_DecodeError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", rawBody(http.StatusOK, "garbage"))
	p := newTestProvisioner(t, r)
	_, err := p.getTokenOwner(context.Background(), "https://acme.okta.com", "tok")
	assertProvOp(t, err, "get_token_owner")
}

func TestGetTokenOwner_RequestError(t *testing.T) {
	p := New(Config{}, nil)
	_, err := p.getTokenOwner(context.Background(), "://bad", "tok")
	assertProvOp(t, err, "get_token_owner")
}

// -------------------- addUserToGroup --------------------

func TestAddUserToGroup_StatusOK(t *testing.T) {
	r := newRouter(t)
	r.onPrefix("PUT", "/api/v1/groups/g1/users/u1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	p := newTestProvisioner(t, r)
	if err := p.addUserToGroup(context.Background(), "https://acme.okta.com", "tok", "g1", "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddUserToGroup_HTTPError(t *testing.T) {
	r := newRouter(t)
	r.onPrefix("PUT", "/api/v1/groups/g1/users/u1", errStatus(http.StatusNotFound, "E0000007", "user not found"))
	p := newTestProvisioner(t, r)
	err := p.addUserToGroup(context.Background(), "https://acme.okta.com", "tok", "g1", "u1")
	assertProvOp(t, err, "add_user_to_group")
}

func TestAddUserToGroup_RequestError(t *testing.T) {
	p := New(Config{}, nil)
	err := p.addUserToGroup(context.Background(), "://bad", "tok", "g1", "u1")
	assertProvOp(t, err, "add_user_to_group")
}

// -------------------- assignGroupToApp --------------------

func TestAssignGroupToApp_HTTPError(t *testing.T) {
	r := newRouter(t)
	r.onPrefix("PUT", "/api/v1/apps/app-1/groups/g1", errStatus(http.StatusBadRequest, "E0000001", "bad"))
	p := newTestProvisioner(t, r)
	err := p.assignGroupToApp(context.Background(), "https://acme.okta.com", "tok", "app-1", "g1")
	assertProvOp(t, err, "assign_group")
}

func TestAssignGroupToApp_RequestError(t *testing.T) {
	p := New(Config{}, nil)
	err := p.assignGroupToApp(context.Background(), "://bad", "tok", "app-1", "g1")
	assertProvOp(t, err, "assign_group")
}

// -------------------- seedAdminFromTokenOwner / seedNamedAdminGroup --------------------

// admin group found, members copied into the target group.
func TestSeedAdminFromTokenOwner_CopiesNamedAdminMembers(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", okJSON(map[string]interface{}{"id": "owner-1"}))
	// findGroupByName: the first admin name "Administrators" exists.
	r.on("GET", "/api/v1/groups", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query().Get("q")
		if q == "Administrators" {
			writeJSON(w, http.StatusOK, []map[string]interface{}{{"id": "admins-grp", "profile": map[string]interface{}{"name": "Administrators"}}})
			return
		}
		writeJSON(w, http.StatusOK, []interface{}{})
	})
	r.onPrefix("GET", "/api/v1/groups/admins-grp/users", okJSON([]map[string]interface{}{{"id": "m1"}, {"id": "m2"}}))
	var added []string
	r.onPrefix("PUT", "/api/v1/groups/target/users/", func(w http.ResponseWriter, req *http.Request) {
		seg := strings.Split(req.URL.Path, "/")
		added = append(added, seg[len(seg)-1])
		w.WriteHeader(http.StatusNoContent)
	})
	p := newTestProvisioner(t, r)
	if err := p.seedAdminFromTokenOwner(context.Background(), "https://acme.okta.com", "tok", "target"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// owner-1 then m1, m2.
	want := map[string]bool{"owner-1": true, "m1": true, "m2": true}
	for _, a := range added {
		delete(want, a)
	}
	if len(want) != 0 {
		t.Errorf("missing expected adds: %v (got %v)", want, added)
	}
}

func TestSeedAdminFromTokenOwner_OwnerLookupError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", errStatus(http.StatusUnauthorized, "E0000011", "bad token"))
	p := newTestProvisioner(t, r)
	err := p.seedAdminFromTokenOwner(context.Background(), "https://acme.okta.com", "tok", "target")
	assertProvOp(t, err, "get_token_owner")
}

func TestSeedAdminFromTokenOwner_AddOwnerError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", okJSON(map[string]interface{}{"id": "owner-1"}))
	r.onPrefix("PUT", "/api/v1/groups/target/users/owner-1", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	err := p.seedAdminFromTokenOwner(context.Background(), "https://acme.okta.com", "tok", "target")
	assertProvOp(t, err, "add_user_to_group")
}

// seedNamedAdminGroup: a search error on one name is logged and skipped, then a
// later name is found and its members listed; a member-add failure is tolerated.
func TestSeedNamedAdminGroup_SearchErrorThenFound(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/groups", func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Query().Get("q") {
		case "Administrators":
			writeOktaErr(w, http.StatusInternalServerError, "E0000009", "transient")
		case "Super": // "Super Admins" -> searchTerm "Super"
			writeJSON(w, http.StatusOK, []map[string]interface{}{{"id": "sa", "profile": map[string]interface{}{"name": "Super Admins"}}})
		default:
			writeJSON(w, http.StatusOK, []interface{}{})
		}
	})
	r.onPrefix("GET", "/api/v1/groups/sa/users", okJSON([]map[string]interface{}{{"id": "m1"}}))
	// member add fails but is tolerated (logged, loop continues).
	r.onPrefix("PUT", "/api/v1/groups/target/users/m1", errStatus(http.StatusNotFound, "E0000007", "gone"))
	p := newTestProvisioner(t, r)
	// Should not panic / return; function is fire-and-forget.
	p.seedNamedAdminGroup(context.Background(), "https://acme.okta.com", "tok", "target")
}

// seedNamedAdminGroup: group found but listing its members fails -> returns.
func TestSeedNamedAdminGroup_ListMembersError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/groups", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("q") == "Administrators" {
			writeJSON(w, http.StatusOK, []map[string]interface{}{{"id": "ad", "profile": map[string]interface{}{"name": "Administrators"}}})
			return
		}
		writeJSON(w, http.StatusOK, []interface{}{})
	})
	r.onPrefix("GET", "/api/v1/groups/ad/users", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	p.seedNamedAdminGroup(context.Background(), "https://acme.okta.com", "tok", "target")
}

// seedNamedAdminGroup: no admin groups found at all -> no-op.
func TestSeedNamedAdminGroup_NoneFound(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/groups", okJSON([]interface{}{}))
	p := newTestProvisioner(t, r)
	p.seedNamedAdminGroup(context.Background(), "https://acme.okta.com", "tok", "target")
}

// -------------------- configureGroupsScope --------------------

func TestConfigureGroupsScope(t *testing.T) {
	const scopesPath = "/api/v1/authorizationServers/default/scopes"
	tests := []struct {
		name        string
		list        http.HandlerFunc
		create      http.HandlerFunc
		baseURL     string
		wantCreated bool
		wantOp      string
	}{
		{
			name: "already exists",
			list: okJSON([]map[string]interface{}{{"id": "s1", "name": "groups"}}),
		},
		{
			name: "creates when missing",
			list: okJSON([]map[string]interface{}{{"id": "s1", "name": "email"}}),
			create: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusCreated, map[string]interface{}{"id": "s2", "name": "groups"})
			},
			wantCreated: true,
		},
		{
			name:   "list error",
			list:   errStatus(http.StatusForbidden, "E0000006", "denied"),
			wantOp: "list_scopes",
		},
		{
			name:   "list decode error",
			list:   rawBody(http.StatusOK, "nope"),
			wantOp: "list_scopes",
		},
		{
			name:        "create error",
			list:        okJSON([]interface{}{}),
			create:      errStatus(http.StatusBadRequest, "E0000001", "bad"),
			wantCreated: true,
			wantOp:      "create_scope",
		},
		{
			name:    "request error",
			baseURL: "://bad",
			wantOp:  "list_scopes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t)
			if tc.list != nil {
				r.on("GET", scopesPath, tc.list)
			}
			created := false
			if tc.create != nil {
				r.on("POST", scopesPath, func(w http.ResponseWriter, req *http.Request) {
					created = true
					tc.create(w, req)
				})
			}
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = "https://acme.okta.com"
			}
			p := newTestProvisioner(t, r)
			err := p.configureGroupsScope(context.Background(), baseURL, "tok")
			if created != tc.wantCreated {
				t.Errorf("created = %v, want %v", created, tc.wantCreated)
			}
			if tc.wantOp != "" {
				assertProvOp(t, err, tc.wantOp)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// -------------------- configureGroupsClaim --------------------

func TestConfigureGroupsClaim(t *testing.T) {
	const claimsPath = "/api/v1/authorizationServers/default/claims"
	existing := okJSON([]map[string]interface{}{{"id": "c-existing", "name": "groups", "valueType": "GROUPS"}})
	tests := []struct {
		name        string
		list        http.HandlerFunc
		writeMethod string
		writePath   string
		write       http.HandlerFunc
		baseURL     string
		filter      GroupsClaimFilter
		wantWritten bool
		wantOp      string
	}{
		{
			name:        "creates when missing",
			list:        okJSON([]interface{}{}),
			writeMethod: "POST",
			writePath:   claimsPath,
			write: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusCreated, map[string]interface{}{"id": "c1"})
			},
			filter:      GroupsClaimFilter{Type: "STARTS_WITH", Value: "Acme"},
			wantWritten: true,
		},
		{
			name:        "updates existing via PUT",
			list:        existing,
			writeMethod: "PUT",
			writePath:   claimsPath + "/c-existing",
			write:       okJSON(map[string]interface{}{"id": "c-existing"}),
			filter:      GroupsClaimFilter{Type: "EQUALS", Value: "Acme"},
			wantWritten: true,
		},
		{
			name:        "update error",
			list:        existing,
			writeMethod: "PUT",
			writePath:   claimsPath + "/c-existing",
			write:       errStatus(http.StatusBadRequest, "E0000001", "bad"),
			filter:      GroupsClaimFilter{Type: "EQUALS", Value: "Acme"},
			wantWritten: true,
			wantOp:      "update_claim",
		},
		{
			name:   "list error",
			list:   errStatus(http.StatusForbidden, "E0000006", "denied"),
			filter: GroupsClaimFilter{Type: "EQUALS", Value: "x"},
			wantOp: "list_claims",
		},
		{
			name:   "list decode error",
			list:   rawBody(http.StatusOK, "nope"),
			filter: GroupsClaimFilter{Type: "EQUALS", Value: "x"},
			wantOp: "list_claims",
		},
		{
			name:        "create error",
			list:        okJSON([]interface{}{}),
			writeMethod: "POST",
			writePath:   claimsPath,
			write:       errStatus(http.StatusBadRequest, "E0000001", "bad"),
			filter:      GroupsClaimFilter{Type: "EQUALS", Value: "x"},
			wantWritten: true,
			wantOp:      "create_claim",
		},
		{
			name:    "request error",
			baseURL: "://bad",
			filter:  GroupsClaimFilter{Type: "EQUALS", Value: "x"},
			wantOp:  "list_claims",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t)
			if tc.list != nil {
				r.on("GET", claimsPath, tc.list)
			}
			written := false
			if tc.write != nil {
				r.on(tc.writeMethod, tc.writePath, func(w http.ResponseWriter, req *http.Request) {
					written = true
					tc.write(w, req)
				})
			}
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = "https://acme.okta.com"
			}
			p := newTestProvisioner(t, r)
			err := p.configureGroupsClaim(context.Background(), baseURL, "tok", tc.filter)
			if written != tc.wantWritten {
				t.Errorf("%s %s hit = %v, want %v", tc.writeMethod, tc.writePath, written, tc.wantWritten)
			}
			if tc.wantOp != "" {
				assertProvOp(t, err, tc.wantOp)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// -------------------- configureAccessPolicy --------------------

func TestConfigureAccessPolicy(t *testing.T) {
	const policiesPath = "/api/v1/authorizationServers/default/policies"
	existingPolicy := okJSON([]map[string]interface{}{{"id": "pol-1", "name": "Acme App Access Policy"}})
	tests := []struct {
		name            string
		listPolicies    http.HandlerFunc
		createPolicy    http.HandlerFunc
		policyID        string
		listRules       http.HandlerFunc
		createRule      http.HandlerFunc
		baseURL         string
		wantRuleCreated bool
		wantOp          string
	}{
		{
			name:         "creates policy and rule",
			listPolicies: okJSON([]interface{}{}),
			createPolicy: okJSON(map[string]interface{}{"id": "pol-new"}),
			policyID:     "pol-new",
			listRules:    okJSON([]interface{}{}),
			createRule: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusCreated, map[string]interface{}{"id": "rule-new"})
			},
			wantRuleCreated: true,
		},
		{
			name:         "reuses existing policy and rule",
			listPolicies: existingPolicy,
			policyID:     "pol-1",
			listRules:    okJSON([]map[string]interface{}{{"id": "r1", "name": "Acme App Default Rule"}}),
		},
		{
			name:         "list policies error",
			listPolicies: errStatus(http.StatusForbidden, "E0000006", "denied"),
			wantOp:       "list_policies",
		},
		{
			name:         "list policies decode error",
			listPolicies: rawBody(http.StatusOK, "nope"),
			wantOp:       "list_policies",
		},
		{
			name:         "create policy error",
			listPolicies: okJSON([]interface{}{}),
			createPolicy: errStatus(http.StatusBadRequest, "E0000001", "bad"),
			wantOp:       "create_policy",
		},
		{
			name:         "create policy decode error",
			listPolicies: okJSON([]interface{}{}),
			createPolicy: rawBody(http.StatusOK, "nope"),
			wantOp:       "create_policy",
		},
		{
			name:         "list rules error",
			listPolicies: existingPolicy,
			policyID:     "pol-1",
			listRules:    errStatus(http.StatusForbidden, "E0000006", "denied"),
			wantOp:       "list_policy_rules",
		},
		{
			name:         "list rules decode error",
			listPolicies: existingPolicy,
			policyID:     "pol-1",
			listRules:    rawBody(http.StatusOK, "nope"),
			wantOp:       "list_policy_rules",
		},
		{
			name:            "create rule error",
			listPolicies:    existingPolicy,
			policyID:        "pol-1",
			listRules:       okJSON([]interface{}{}),
			createRule:      errStatus(http.StatusBadRequest, "E0000001", "bad"),
			wantRuleCreated: true,
			wantOp:          "create_policy_rule",
		},
		{
			name:    "request error",
			baseURL: "://bad",
			wantOp:  "list_policies",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t)
			if tc.listPolicies != nil {
				r.on("GET", policiesPath, tc.listPolicies)
			}
			if tc.createPolicy != nil {
				r.on("POST", policiesPath, tc.createPolicy)
			}
			rulesPath := policiesPath + "/" + tc.policyID + "/rules"
			if tc.listRules != nil {
				r.onPrefix("GET", rulesPath, tc.listRules)
			}
			ruleCreated := false
			if tc.createRule != nil {
				r.onPrefix("POST", rulesPath, func(w http.ResponseWriter, req *http.Request) {
					ruleCreated = true
					tc.createRule(w, req)
				})
			}
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = "https://acme.okta.com"
			}
			p := newTestProvisioner(t, r)
			err := p.configureAccessPolicy(context.Background(), baseURL, "tok", "Acme App")
			if ruleCreated != tc.wantRuleCreated {
				t.Errorf("rule created = %v, want %v", ruleCreated, tc.wantRuleCreated)
			}
			if tc.wantOp != "" {
				assertProvOp(t, err, tc.wantOp)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// -------------------- findAppByLabel / findGroupByName --------------------

func TestFindAppByLabel(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		baseURL   string
		label     string
		wantQuery string
		wantID    string
		wantOp    string
	}{
		{
			name: "found",
			handler: okJSON([]map[string]interface{}{
				{"id": "a1", "label": "Other App"},
				{"id": "a2", "label": "Acme App"},
			}),
			label:  "Acme App",
			wantID: "a2",
		},
		{
			name:    "not found",
			handler: okJSON([]map[string]interface{}{{"id": "a1", "label": "Other"}}),
			label:   "Acme App",
		},
		{
			name:    "http error",
			handler: errStatus(http.StatusForbidden, "E0000006", "denied"),
			label:   "Acme App",
			wantOp:  "find_app",
		},
		{
			name:    "decode error",
			handler: rawBody(http.StatusOK, "nope"),
			label:   "Acme App",
			wantOp:  "find_app",
		},
		{
			name:    "request error",
			baseURL: "://bad",
			label:   "Acme App",
			wantOp:  "find_app",
		},
		{
			// A single-word label is sent as the whole search term.
			name:      "single-word label",
			handler:   okJSON([]map[string]interface{}{{"id": "a1", "label": "Acme"}}),
			label:     "Acme",
			wantQuery: "Acme",
			wantID:    "a1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t)
			if tc.handler != nil {
				r.on("GET", "/api/v1/apps", func(w http.ResponseWriter, req *http.Request) {
					if tc.wantQuery != "" && req.URL.Query().Get("q") != tc.wantQuery {
						t.Errorf("q = %q, want %q", req.URL.Query().Get("q"), tc.wantQuery)
					}
					tc.handler(w, req)
				})
			}
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = "https://acme.okta.com"
			}
			p := newTestProvisioner(t, r)
			app, err := p.findAppByLabel(context.Background(), baseURL, "tok", tc.label)
			if tc.wantOp != "" {
				assertProvOp(t, err, tc.wantOp)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantID == "" {
				if app != nil {
					t.Errorf("app = %+v, want nil", app)
				}
				return
			}
			if app == nil || app.ID != tc.wantID {
				t.Errorf("app = %+v, want id %s", app, tc.wantID)
			}
		})
	}
}

func TestFindGroupByName(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		baseURL   string
		group     string
		wantQuery string
		wantID    string
		wantOp    string
	}{
		{
			name: "found",
			handler: okJSON([]map[string]interface{}{
				{"id": "g1", "profile": map[string]interface{}{"name": "Acme Auditors"}},
				{"id": "g2", "profile": map[string]interface{}{"name": "Acme Administrators"}},
			}),
			group:  "Acme Administrators",
			wantID: "g2",
		},
		{
			name:    "not found",
			handler: okJSON([]map[string]interface{}{{"id": "g1", "profile": map[string]interface{}{"name": "Other"}}}),
			group:   "Acme Administrators",
		},
		{
			name:    "http error",
			handler: errStatus(http.StatusForbidden, "E0000006", "denied"),
			group:   "Acme Administrators",
			wantOp:  "find_group",
		},
		{
			name:    "decode error",
			handler: rawBody(http.StatusOK, "nope"),
			group:   "Acme Administrators",
			wantOp:  "find_group",
		},
		{
			name:    "request error",
			baseURL: "://bad",
			group:   "Acme Administrators",
			wantOp:  "find_group",
		},
		{
			// A single-word name is sent as the whole search term.
			name:      "single-word name",
			handler:   okJSON([]map[string]interface{}{{"id": "g1", "profile": map[string]interface{}{"name": "Admins"}}}),
			group:     "Admins",
			wantQuery: "Admins",
			wantID:    "g1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter(t)
			if tc.handler != nil {
				r.on("GET", "/api/v1/groups", func(w http.ResponseWriter, req *http.Request) {
					if tc.wantQuery != "" && req.URL.Query().Get("q") != tc.wantQuery {
						t.Errorf("q = %q, want %q", req.URL.Query().Get("q"), tc.wantQuery)
					}
					tc.handler(w, req)
				})
			}
			baseURL := tc.baseURL
			if baseURL == "" {
				baseURL = "https://acme.okta.com"
			}
			p := newTestProvisioner(t, r)
			g, err := p.findGroupByName(context.Background(), baseURL, "tok", tc.group)
			if tc.wantOp != "" {
				assertProvOp(t, err, tc.wantOp)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantID == "" {
				if g != nil {
					t.Errorf("group = %+v, want nil", g)
				}
				return
			}
			if g == nil || g.ID != tc.wantID {
				t.Errorf("group = %+v, want id %s", g, tc.wantID)
			}
		})
	}
}

// -------------------- createApp / createGroup decode & request errors --------------------

func TestCreateApp_DecodeError(t *testing.T) {
	r := newRouter(t)
	r.on("POST", "/api/v1/apps", rawBody(http.StatusOK, "nope"))
	p := newTestProvisioner(t, r)
	_, err := p.createApp(context.Background(), "https://acme.okta.com", "tok", "L", "https://cb", "https://app")
	assertProvOp(t, err, "create_app")
}

func TestCreateApp_RequestError(t *testing.T) {
	p := New(Config{}, nil)
	_, err := p.createApp(context.Background(), "://bad", "tok", "L", "https://cb", "https://app")
	assertProvOp(t, err, "create_app")
}

func TestCreateGroup_DecodeError(t *testing.T) {
	r := newRouter(t)
	r.on("POST", "/api/v1/groups", rawBody(http.StatusOK, "nope"))
	p := newTestProvisioner(t, r)
	_, err := p.createGroup(context.Background(), "https://acme.okta.com", "tok", "G", "desc")
	assertProvOp(t, err, "create_group")
}

func TestCreateGroup_RequestError(t *testing.T) {
	p := New(Config{}, nil)
	_, err := p.createGroup(context.Background(), "://bad", "tok", "G", "desc")
	assertProvOp(t, err, "create_group")
}

// -------------------- ProvisionApp: findGroup/assignGroup error propagation --------------------

func TestProvisionApp_FindGroupError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/groups", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	_, err := p.ProvisionApp(context.Background(), sampleRequest())
	assertProvOp(t, err, "find_group")
}

func TestProvisionApp_AssignGroupError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/groups", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/groups", okJSON(map[string]interface{}{"id": "g1", "profile": map[string]interface{}{"name": "Acme Administrators"}}))
	r.onPrefix("PUT", "/api/v1/apps/app-1/groups/", errStatus(http.StatusBadRequest, "E0000001", "bad"))
	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = []RoleGroup{{Name: "Acme Administrators"}}
	_, err := p.ProvisionApp(context.Background(), req)
	assertProvOp(t, err, "assign_group")
}

func TestProvisionApp_SeedAdminError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/groups", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/groups", okJSON(map[string]interface{}{"id": "g1", "profile": map[string]interface{}{"name": "Acme Administrators"}}))
	r.onPrefix("PUT", "/api/v1/apps/app-1/groups/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.on("GET", "/api/v1/users/me", errStatus(http.StatusUnauthorized, "E0000011", "bad token"))
	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = []RoleGroup{{Name: "Acme Administrators"}}
	_, err := p.ProvisionApp(context.Background(), req)
	assertProvOp(t, err, "get_token_owner")
}

func TestProvisionApp_ConfigureScopeError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/authorizationServers/default/scopes", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = nil
	req.SeedAdminGroupName = ""
	_, err := p.ProvisionApp(context.Background(), req)
	assertProvOp(t, err, "list_scopes")
}

func TestProvisionApp_ConfigureClaimError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/authorizationServers/default/scopes", okJSON([]map[string]interface{}{{"id": "s", "name": "groups"}}))
	r.on("GET", "/api/v1/authorizationServers/default/claims", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = nil
	req.SeedAdminGroupName = ""
	_, err := p.ProvisionApp(context.Background(), req)
	assertProvOp(t, err, "list_claims")
}

func TestProvisionApp_ConfigureAccessPolicyError(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/authorizationServers/default/scopes", okJSON([]map[string]interface{}{{"id": "s", "name": "groups"}}))
	r.on("GET", "/api/v1/authorizationServers/default/policies", errStatus(http.StatusForbidden, "E0000006", "denied"))
	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = nil
	req.SeedAdminGroupName = ""
	req.GroupsClaimFilter = GroupsClaimFilter{}
	_, err := p.ProvisionApp(context.Background(), req)
	assertProvOp(t, err, "list_policies")
}

// SeedAdminGroupName references a group not in the created set -> seed skipped.
func TestProvisionApp_SeedAdminGroupNotInSet(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/apps", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/apps", okJSON(map[string]interface{}{
		"id":          "app-1",
		"credentials": map[string]interface{}{"oauthClient": map[string]interface{}{"client_id": "c", "client_secret": "s"}},
	}))
	r.on("GET", "/api/v1/groups", okJSON([]interface{}{}))
	r.on("POST", "/api/v1/groups", okJSON(map[string]interface{}{"id": "g1", "profile": map[string]interface{}{"name": "Acme Auditors"}}))
	r.onPrefix("PUT", "/api/v1/apps/app-1/groups/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.on("GET", "/api/v1/authorizationServers/default/scopes", okJSON([]map[string]interface{}{{"id": "s", "name": "groups"}}))
	r.on("GET", "/api/v1/authorizationServers/default/policies", okJSON([]map[string]interface{}{{"id": "pol-1", "name": "Acme App Access Policy"}}))
	r.onPrefix("GET", "/api/v1/authorizationServers/default/policies/pol-1/rules", okJSON([]map[string]interface{}{{"id": "r", "name": "Acme App Default Rule"}}))
	p := newTestProvisioner(t, r)
	req := sampleRequest()
	req.RoleGroups = []RoleGroup{{Name: "Acme Auditors"}}
	req.SeedAdminGroupName = "Nonexistent Group" // not in the created groups map
	req.GroupsClaimFilter = GroupsClaimFilter{}
	if _, err := p.ProvisionApp(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// -------------------- doRequest / parseError / orgNameFromURL --------------------

func TestDoRequest_BadURL(t *testing.T) {
	p := New(Config{}, nil)
	resp, err := p.doRequest(context.Background(), http.MethodGet, "://bad", "tok", nil)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected request-creation error for bad URL")
	}
}

func TestParseError_PlainTextBody(t *testing.T) {
	r := newRouter(t)
	r.on("GET", "/api/v1/users/me", rawBody(http.StatusBadGateway, "upstream down"))
	p := newTestProvisioner(t, r)
	_, err := p.getTokenOwner(context.Background(), "https://acme.okta.com", "tok")
	if err == nil || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("err = %v, want plain-text body in message", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("err = %v, want status code 502", err)
	}
}

func TestParseError_OktaJSONWithoutSummary(t *testing.T) {
	r := newRouter(t)
	// Valid JSON object but no errorSummary field -> falls through to raw body.
	r.on("GET", "/api/v1/users/me", rawBody(http.StatusTeapot, `{"errorCode":"E1","other":"x"}`))
	p := newTestProvisioner(t, r)
	_, err := p.getTokenOwner(context.Background(), "https://acme.okta.com", "tok")
	if err == nil || !strings.Contains(err.Error(), "418") {
		t.Fatalf("err = %v, want status 418", err)
	}
}

func TestOrgNameFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://acme.okta.com", "acme"},
		{"https://my-org.okta.com", "my-org"},
		{"short", "short"}, // too short -> returned as-is
		{"", ""},
	}
	for _, c := range cases {
		if got := orgNameFromURL(c.in); got != c.want {
			t.Errorf("orgNameFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// -------------------- New defaults --------------------

func TestNew_DefaultsApplied(t *testing.T) {
	p := New(Config{}, nil)
	if p.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if p.httpClient.Timeout <= 0 {
		t.Errorf("default timeout not applied: %v", p.httpClient.Timeout)
	}
	if p.logger == nil {
		t.Fatal("logger is nil")
	}
}

// assertProvOp asserts err is a *ProvisioningError wrapping ErrProvisioning with
// the expected Operation.
func assertProvOp(t *testing.T, err error, wantOp string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with operation %q, got nil", wantOp)
	}
	if !errors.Is(err, ErrProvisioning) {
		t.Errorf("error %v does not match ErrProvisioning", err)
	}
	var pe *ProvisioningError
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is not *ProvisioningError", err)
	}
	if pe.Operation != wantOp {
		t.Errorf("operation = %q, want %q", pe.Operation, wantOp)
	}
}
