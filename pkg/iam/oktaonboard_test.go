//go:build iam

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

package iam

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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/oktaprovision"
)

// onboardTestTransport rewrites outgoing https://<org>.okta.com requests to the
// test server, mirroring the unexported transport in the oktaprovision package
// so the provisioner builds real URLs while the request lands on httptest.
type onboardTestTransport struct {
	server *httptest.Server
}

func (t *onboardTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target := t.server.URL + req.URL.Path
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, target, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.URL.RawQuery = req.URL.RawQuery
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

// onboardMockOkta is a configurable Okta Management-API mock covering every
// endpoint ProvisionApp exercises.
type onboardMockOkta struct {
	t *testing.T
}

func (m *onboardMockOkta) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/v1/apps" && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/apps" && r.Method == http.MethodPost:
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{
			"id": "app-1",
			"credentials": map[string]interface{}{
				"oauthClient": map[string]interface{}{
					"client_id":     "client-1",
					"client_secret": "secret-1",
				},
			},
		})
	case path == "/api/v1/groups" && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/groups" && r.Method == http.MethodPost:
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := body["profile"].(map[string]interface{})["name"].(string)
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{
			"id":      "grp-" + strings.ReplaceAll(name, " ", "-"),
			"profile": map[string]interface{}{"name": name},
		})
	case strings.HasPrefix(path, "/api/v1/apps/app-1/groups/") && r.Method == http.MethodPut:
		w.WriteHeader(http.StatusOK)
	case path == "/api/v1/users/me" && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{"id": "owner-1"})
	case strings.HasPrefix(path, "/api/v1/groups/") && strings.HasSuffix(path, "/users") && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case strings.Contains(path, "/api/v1/groups/") && strings.Contains(path, "/users/") && r.Method == http.MethodPut:
		w.WriteHeader(http.StatusNoContent)
	case path == "/api/v1/authorizationServers/default/scopes" && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/authorizationServers/default/scopes" && r.Method == http.MethodPost:
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{"id": "scope-1", "name": "groups"})
	case path == "/api/v1/authorizationServers/default/claims" && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/authorizationServers/default/claims" && r.Method == http.MethodPost:
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{"id": "claim-1", "name": "groups"})
	case path == "/api/v1/authorizationServers/default/policies" && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case path == "/api/v1/authorizationServers/default/policies" && r.Method == http.MethodPost:
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{"id": "pol-1", "name": "ignored"})
	case strings.HasPrefix(path, "/api/v1/authorizationServers/default/policies/pol-1/rules") && r.Method == http.MethodGet:
		writeOnboardJSON(w, http.StatusOK, []interface{}{})
	case strings.HasPrefix(path, "/api/v1/authorizationServers/default/policies/pol-1/rules") && r.Method == http.MethodPost:
		writeOnboardJSON(w, http.StatusOK, map[string]interface{}{"id": "rule-1"})
	default:
		m.t.Errorf("unexpected request %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func writeOnboardJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newOnboardProvisioner(t *testing.T) *oktaprovision.Provisioner {
	t.Helper()
	server := httptest.NewServer(&onboardMockOkta{t: t})
	t.Cleanup(server.Close)
	return oktaprovision.New(oktaprovision.Config{
		HTTPClient: &http.Client{
			Transport: &onboardTestTransport{server: server},
			Timeout:   5 * time.Second,
		},
	}, slog.Default())
}

// Gap 5: the onboarding orchestrator. ProvisionApp -> RegisterProvider ->
// RegisterDomain in one call produces a provider whose GroupRoleMapping +
// AllowedEmailDomains are set and whose domain resolves via the HRD.
func TestOnboardTenant_EndToEnd(t *testing.T) {
	ctx := context.Background()

	manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)

	shared := newMockKVStore()
	hrd, err := NewHomeRealmDirectory(shared)
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}

	prov := newOnboardProvisioner(t)

	onboarder, err := NewOktaOnboarder(prov, manager, hrd)
	if err != nil {
		t.Fatalf("NewOktaOnboarder: %v", err)
	}

	req := OktaOnboardingRequest{
		TenantID:     "tenant-acme",
		ProviderName: "okta",
		APIToken:     "ssws-token",
		OrgName:      "acme",
		AppLabel:     "Acme App",
		CallbackURL:  "https://app.acme.com/cb",
		AppURL:       "https://app.acme.com",
		EmailDomains: []string{"Acme.com", "acme.example"},
		RoleGroups: []OktaRoleGroup{
			{Name: "Acme Administrators", Description: "admins", RoleID: "role-admin"},
			{Name: "Acme Auditors", Description: "auditors", RoleID: "role-auditor"},
		},
		SeedAdminGroupName: "Acme Administrators",
		AutoProvision:      true,
		AllowLinking:       true,
	}

	res, err := onboarder.OnboardTenant(ctx, req)
	if err != nil {
		t.Fatalf("OnboardTenant: %v", err)
	}

	// (a) The provisioning result carries the org credentials.
	if res.OktaResult.Issuer != "https://acme.okta.com" || res.OktaResult.ClientID != "client-1" {
		t.Errorf("okta result = %+v", res.OktaResult)
	}

	// (b) The provider was persisted with GroupRoleMapping + AllowedEmailDomains.
	stored, err := providerStorage.GetProviderByName(ctx, "tenant-acme", "okta")
	if err != nil {
		t.Fatalf("get stored provider: %v", err)
	}
	if stored.IssuerURL != "https://acme.okta.com" || stored.ClientID != "client-1" || stored.ClientSecret != "secret-1" {
		t.Errorf("stored provider creds = %q/%q/%q", stored.IssuerURL, stored.ClientID, stored.ClientSecret)
	}
	if !stored.AutoProvision || !stored.AllowLinking {
		t.Error("expected AutoProvision and AllowLinking set on provider")
	}

	wantDomains := map[string]bool{"acme.com": true, "acme.example": true}
	if len(stored.AllowedEmailDomains) != len(wantDomains) {
		t.Fatalf("allowed domains = %v", stored.AllowedEmailDomains)
	}
	for _, d := range stored.AllowedEmailDomains {
		if !wantDomains[d] {
			t.Errorf("unexpected allowed domain %q", d)
		}
	}

	mappings := stored.ClaimMappings.GroupRoleMappings
	if len(mappings) != 2 {
		t.Fatalf("group role mappings = %v", mappings)
	}
	got := map[string]string{}
	for _, m := range mappings {
		got[m.Group] = m.RoleID
	}
	if got["Acme Administrators"] != "role-admin" || got["Acme Auditors"] != "role-auditor" {
		t.Errorf("group->role mappings = %v", got)
	}
	if stored.ClaimMappings.Groups != "groups" {
		t.Errorf("groups claim = %q, want groups", stored.ClaimMappings.Groups)
	}

	// (c) Each email domain resolves to the tenant via the HRD.
	for domain := range wantDomains {
		tenantID, rerr := hrd.Resolve(ctx, RealmHints{Email: "user@" + domain})
		if rerr != nil {
			t.Fatalf("HRD resolve %q: %v", domain, rerr)
		}
		if tenantID != "tenant-acme" {
			t.Errorf("domain %q resolved to %q, want tenant-acme", domain, tenantID)
		}
	}
	if len(res.RegisteredDomains) != 2 {
		t.Errorf("registered domains = %v", res.RegisteredDomains)
	}
}

// Gap 5 (error handling): a request missing required inputs is rejected before
// any side effect (fail-closed validation).
func TestOnboardTenant_InvalidRequestRejected(t *testing.T) {
	ctx := context.Background()
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	shared := newMockKVStore()
	hrd, err := NewHomeRealmDirectory(shared)
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	prov := newOnboardProvisioner(t)
	onboarder, err := NewOktaOnboarder(prov, manager, hrd)
	if err != nil {
		t.Fatalf("NewOktaOnboarder: %v", err)
	}

	base := OktaOnboardingRequest{
		TenantID:     "tenant-acme",
		ProviderName: "okta",
		APIToken:     "ssws-token",
		OrgName:      "acme",
		AppLabel:     "Acme App",
		CallbackURL:  "https://app.acme.com/cb",
		EmailDomains: []string{"acme.com"},
		RoleGroups:   []OktaRoleGroup{{Name: "Acme Administrators", RoleID: "role-admin"}},
	}

	cases := []struct {
		name   string
		mutate func(*OktaOnboardingRequest)
	}{
		{"no tenant", func(r *OktaOnboardingRequest) { r.TenantID = "" }},
		{"no provider name", func(r *OktaOnboardingRequest) { r.ProviderName = "" }},
		{"no domains", func(r *OktaOnboardingRequest) { r.EmailDomains = nil }},
		{"no role groups", func(r *OktaOnboardingRequest) { r.RoleGroups = nil }},
		{"role group missing role id", func(r *OktaOnboardingRequest) {
			r.RoleGroups = []OktaRoleGroup{{Name: "Acme Administrators"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			_, err := onboarder.OnboardTenant(ctx, req)
			if err == nil {
				t.Fatal("expected validation error")
			}
			var oErr *OktaOnboardingError
			if !errors.As(err, &oErr) {
				t.Fatalf("error = %v, want *OktaOnboardingError", err)
			}
			if oErr.Step != "validate" {
				t.Errorf("step = %q, want validate", oErr.Step)
			}
		})
	}
}

// Gap 5 (nil collaborators): the constructor fails closed.
func TestNewOktaOnboarder_NilCollaborators(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	shared := newMockKVStore()
	hrd, _ := NewHomeRealmDirectory(shared)
	prov := newOnboardProvisioner(t)

	if _, err := NewOktaOnboarder(nil, manager, hrd); err == nil {
		t.Error("expected error for nil provisioner")
	}
	if _, err := NewOktaOnboarder(prov, nil, hrd); err == nil {
		t.Error("expected error for nil federation manager")
	}
	if _, err := NewOktaOnboarder(prov, manager, nil); err == nil {
		t.Error("expected error for nil home-realm directory")
	}
}
