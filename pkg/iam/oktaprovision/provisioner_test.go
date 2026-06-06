// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package oktaprovision

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testTransport redirects all outgoing requests to the test server, preserving
// path, method, headers, body, and query parameters. This lets the provisioner
// build real https://<org>.okta.com URLs while the request lands on httptest.
type testTransport struct {
	server *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target := t.server.URL + req.URL.Path
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, target, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.URL.RawQuery = req.URL.RawQuery
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

func newTestProvisioner(t *testing.T, handler http.Handler) *Provisioner {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Config{
		HTTPClient: &http.Client{
			Transport: &testTransport{server: server},
			Timeout:   5 * time.Second,
		},
	}, slog.Default())
}

// mockOkta is a configurable Okta Management-API mock covering the endpoints
// ProvisionApp exercises.
type mockOkta struct {
	t              *testing.T
	appCreateFail  bool
	groupCreateErr bool
}

func (m *mockOkta) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/v1/apps" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{}) // no existing app
	case path == "/api/v1/apps" && r.Method == http.MethodPost:
		if m.appCreateFail {
			writeOktaErr(w, http.StatusBadRequest, "E0000001", "app create failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id": "app-1",
			"credentials": map[string]interface{}{
				"oauthClient": map[string]interface{}{
					"client_id":     "client-1",
					"client_secret": "secret-1",
				},
			},
		})
	case path == "/api/v1/groups" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{}) // no existing groups
	case path == "/api/v1/groups" && r.Method == http.MethodPost:
		if m.groupCreateErr {
			writeOktaErr(w, http.StatusForbidden, "E0000006", "no group permission")
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := body["profile"].(map[string]interface{})["name"].(string)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":      "grp-" + strings.ReplaceAll(name, " ", "-"),
			"profile": map[string]interface{}{"name": name},
		})
	case strings.HasPrefix(path, "/api/v1/apps/app-1/groups/") && r.Method == http.MethodPut:
		w.WriteHeader(http.StatusOK)
	case path == "/api/v1/users/me" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": "owner-1"})
	case strings.HasPrefix(path, "/api/v1/groups/") && strings.HasSuffix(path, "/users") && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{})
	case strings.Contains(path, "/api/v1/groups/") && strings.Contains(path, "/users/") && r.Method == http.MethodPut:
		w.WriteHeader(http.StatusNoContent)
	case path == "/api/v1/authorizationServers/default/scopes" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/authorizationServers/default/scopes" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": "scope-1", "name": "groups"})
	case path == "/api/v1/authorizationServers/default/claims" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/authorizationServers/default/claims" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": "claim-1", "name": "groups"})
	case path == "/api/v1/authorizationServers/default/policies" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/authorizationServers/default/policies" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": "pol-1", "name": "ignored"})
	case strings.HasPrefix(path, "/api/v1/authorizationServers/default/policies/pol-1/rules") && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{})
	case strings.HasPrefix(path, "/api/v1/authorizationServers/default/policies/pol-1/rules") && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": "rule-1"})
	default:
		m.t.Errorf("unexpected request %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOktaErr(w http.ResponseWriter, status int, code, summary string) {
	writeJSON(w, status, map[string]interface{}{"errorCode": code, "errorSummary": summary})
}

func sampleRequest() ProvisionRequest {
	return ProvisionRequest{
		APIToken:    "ssws-token",
		OrgName:     "acme",
		AppLabel:    "Acme App",
		CallbackURL: "https://app.acme.com/cb",
		AppURL:      "https://app.acme.com",
		RoleGroups: []RoleGroup{
			{Name: "Acme Administrators", Description: "admins"},
			{Name: "Acme Auditors", Description: "auditors"},
		},
		GroupsClaimFilter:  GroupsClaimFilter{Type: "STARTS_WITH", Value: "Acme"},
		SeedAdminGroupName: "Acme Administrators",
	}
}

func TestProvisionApp_Success(t *testing.T) {
	p := newTestProvisioner(t, &mockOkta{t: t})
	res, err := p.ProvisionApp(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if res.Issuer != "https://acme.okta.com" {
		t.Errorf("issuer = %q", res.Issuer)
	}
	if res.ClientID != "client-1" || res.ClientSecret != "secret-1" {
		t.Errorf("client creds = %q/%q", res.ClientID, res.ClientSecret)
	}
	if res.Groups["Acme Administrators"] != "grp-Acme-Administrators" {
		t.Errorf("admin group id = %q", res.Groups["Acme Administrators"])
	}
	if res.Groups["Acme Auditors"] != "grp-Acme-Auditors" {
		t.Errorf("auditor group id = %q", res.Groups["Acme Auditors"])
	}
}

func TestProvisionApp_InvalidOrgNameRejected(t *testing.T) {
	p := newTestProvisioner(t, &mockOkta{t: t})
	req := sampleRequest()
	req.OrgName = "legit.okta.com@attacker.example"
	_, err := p.ProvisionApp(context.Background(), req)
	if err == nil {
		t.Fatal("expected SSRF-guard rejection of crafted org name")
	}
	if !errors.Is(err, ErrInvalidOrgName) {
		t.Errorf("error = %v, want ErrInvalidOrgName", err)
	}
}

func TestProvisionApp_MissingRequiredFields(t *testing.T) {
	p := newTestProvisioner(t, &mockOkta{t: t})
	cases := []struct {
		name   string
		mutate func(*ProvisionRequest)
	}{
		{"missing token", func(r *ProvisionRequest) { r.APIToken = "" }},
		{"missing label", func(r *ProvisionRequest) { r.AppLabel = "" }},
		{"missing callback", func(r *ProvisionRequest) { r.CallbackURL = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := sampleRequest()
			tc.mutate(&req)
			_, err := p.ProvisionApp(context.Background(), req)
			if err == nil {
				t.Fatal("expected config error")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestProvisionApp_AppCreateFailure(t *testing.T) {
	p := newTestProvisioner(t, &mockOkta{t: t, appCreateFail: true})
	_, err := p.ProvisionApp(context.Background(), sampleRequest())
	if err == nil {
		t.Fatal("expected provisioning error on app create failure")
	}
	if !errors.Is(err, ErrProvisioning) {
		t.Errorf("error = %v, want ErrProvisioning", err)
	}
	var provErr *ProvisioningError
	if errors.As(err, &provErr) && provErr.Operation != "create_app" {
		t.Errorf("operation = %q, want create_app", provErr.Operation)
	}
}

func TestProvisionApp_GroupCreateFailure(t *testing.T) {
	p := newTestProvisioner(t, &mockOkta{t: t, groupCreateErr: true})
	_, err := p.ProvisionApp(context.Background(), sampleRequest())
	if err == nil {
		t.Fatal("expected provisioning error on group create failure")
	}
	if !errors.Is(err, ErrProvisioning) {
		t.Errorf("error = %v, want ErrProvisioning", err)
	}
}

func TestValidateOrgName(t *testing.T) {
	valid := []string{"acme", "acme-corp", "a", "org123"}
	invalid := []string{"", "-bad", "has space", "UPPER", "dot.org", "a/b", "x@y"}
	for _, v := range valid {
		if err := ValidateOrgName(v); err != nil {
			t.Errorf("ValidateOrgName(%q) = %v, want nil", v, err)
		}
	}
	for _, v := range invalid {
		if err := ValidateOrgName(v); err == nil {
			t.Errorf("ValidateOrgName(%q) = nil, want error", v)
		}
	}
}
