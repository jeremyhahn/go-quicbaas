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

package oktaprovision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds provisioner-wide settings (not per-call inputs).
type Config struct {
	// HTTPTimeout bounds each Okta Management-API request. Defaults to 30s.
	HTTPTimeout time.Duration

	// HTTPClient overrides the HTTP client. When nil a client with HTTPTimeout
	// is created. Inject an httptest client in tests.
	HTTPClient *http.Client
}

// RoleGroup describes an Okta group to create/reuse and assign to the app. The
// group name is what an operator maps to a quicbaas role via the federation
// ClaimMappings.GroupRoleMappings.
type RoleGroup struct {
	// Name is the Okta group name (e.g. "Acme Administrators").
	Name string

	// Description is the Okta group description.
	Description string
}

// ProvisionRequest is the per-tenant input to ProvisionApp. All product-specific
// values (app label, role-group names, groups-claim filter) are configurable
// here so the provisioner carries no hardcoding.
type ProvisionRequest struct {
	// APIToken is the Okta Management-API token (SSWS). Used only for this call;
	// never stored.
	APIToken string

	// OrgName is the Okta org subdomain label (e.g. "acme" for acme.okta.com).
	OrgName string

	// AppLabel is the OAuth app label to create or reuse.
	AppLabel string

	// CallbackURL is the OAuth redirect URI.
	CallbackURL string

	// AppURL is the application's base URL (client_uri / post-logout redirect).
	AppURL string

	// RoleGroups are the groups to create/reuse and assign to the app.
	RoleGroups []RoleGroup

	// GroupsClaimFilter configures the "groups" claim emitted by the default
	// Custom Authorization Server. When zero-valued, no group filter is applied
	// (the claim is skipped). Set Type=STARTS_WITH and Value to a tenant prefix
	// to scope which group names appear in the token.
	GroupsClaimFilter GroupsClaimFilter

	// SeedAdminGroupName, when non-empty, is the RoleGroup name into which the
	// API-token owner (and members of common Okta admin groups) are seeded so at
	// least one administrator can authenticate immediately.
	SeedAdminGroupName string
}

// GroupsClaimFilter configures the groups-claim group filter on the auth server.
type GroupsClaimFilter struct {
	// Type is the Okta group filter type: "STARTS_WITH", "EQUALS", "CONTAINS",
	// or "REGEX". Empty disables the filter (claim not configured).
	Type string

	// Value is the filter operand (e.g. the tenant group-name prefix).
	Value string
}

// Result contains the credentials and group info returned after provisioning.
type Result struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// Groups maps the requested RoleGroup name to its created/resolved Okta ID.
	Groups map[string]string
}

// Provisioner provisions Okta OAuth apps and role groups for tenants via the
// Okta Management API.
type Provisioner struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a Provisioner.
func New(cfg Config, logger *slog.Logger) *Provisioner {
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &Provisioner{
		httpClient: client,
		logger:     logger.With("component", "okta_provisioner"),
	}
}

// ProvisionApp provisions an Okta OAuth app and role groups in the tenant's org.
// Existing apps/groups are reused and updated rather than duplicated. The
// returned ClientSecret is empty when an existing app is updated (Okta only
// returns the secret on initial creation).
func (p *Provisioner) ProvisionApp(ctx context.Context, req ProvisionRequest) (*Result, error) {
	if err := ValidateOrgName(req.OrgName); err != nil {
		return nil, err
	}
	if req.APIToken == "" {
		return nil, &InvalidConfigError{Reason: "api token is required"}
	}
	if req.AppLabel == "" {
		return nil, &InvalidConfigError{Reason: "app label is required"}
	}
	if req.CallbackURL == "" {
		return nil, &InvalidConfigError{Reason: "callback URL is required"}
	}

	baseURL := fmt.Sprintf("https://%s.okta.com", req.OrgName)

	p.logger.InfoContext(ctx, "provisioning okta app", "org", req.OrgName, "label", req.AppLabel)

	// 1. Find or create/update the OAuth web application.
	app, clientID, err := p.ensureApp(ctx, baseURL, req)
	if err != nil {
		return nil, err
	}

	// 2. Find or create the role groups, then assign them to the app.
	groups := make(map[string]string, len(req.RoleGroups))
	for _, rg := range req.RoleGroups {
		g, err := p.findGroupByName(ctx, baseURL, req.APIToken, rg.Name)
		if err != nil {
			return nil, err
		}
		if g == nil {
			g, err = p.createGroup(ctx, baseURL, req.APIToken, rg.Name, rg.Description)
			if err != nil {
				return nil, err
			}
		}
		groups[rg.Name] = g.ID
		if err := p.assignGroupToApp(ctx, baseURL, req.APIToken, app.ID, g.ID); err != nil {
			return nil, err
		}
	}

	// 3. Seed the admin role group from the token owner (best-effort named-group copy).
	if req.SeedAdminGroupName != "" {
		if gid, ok := groups[req.SeedAdminGroupName]; ok {
			if err := p.seedAdminFromTokenOwner(ctx, baseURL, req.APIToken, gid); err != nil {
				return nil, err
			}
		}
	}

	// 4. Configure the "groups" scope, claim, and access policy on the default
	//    Custom Authorization Server.
	if err := p.configureGroupsScope(ctx, baseURL, req.APIToken); err != nil {
		return nil, err
	}
	if req.GroupsClaimFilter.Type != "" {
		if err := p.configureGroupsClaim(ctx, baseURL, req.APIToken, req.GroupsClaimFilter); err != nil {
			return nil, err
		}
	}
	if err := p.configureAccessPolicy(ctx, baseURL, req.APIToken, req.AppLabel); err != nil {
		return nil, err
	}

	p.logger.InfoContext(ctx, "okta provisioning complete", "org", req.OrgName, "app_id", app.ID)

	return &Result{
		Issuer:       baseURL,
		ClientID:     clientID,
		ClientSecret: app.Credentials.OAuthClient.ClientSecret,
		Groups:       groups,
	}, nil
}

// ensureApp finds-and-updates or creates the OAuth app, returning the app and
// its client ID.
func (p *Provisioner) ensureApp(ctx context.Context, baseURL string, req ProvisionRequest) (*appResponse, string, error) {
	existing, err := p.findAppByLabel(ctx, baseURL, req.APIToken, req.AppLabel)
	if err != nil {
		return nil, "", err
	}

	if existing != nil {
		clientID := existing.Credentials.OAuthClient.ClientID
		body, err := p.getApp(ctx, baseURL, req.APIToken, existing.ID)
		if err != nil {
			return nil, "", err
		}
		mergeAppUpdate(body, req.AppLabel, req.CallbackURL, req.AppURL)
		app, err := p.updateApp(ctx, baseURL, req.APIToken, existing.ID, body)
		if err != nil {
			return nil, "", err
		}
		p.logger.InfoContext(ctx, "okta app updated", "org", req.OrgName, "app_id", app.ID, "client_id", clientID)
		return app, clientID, nil
	}

	app, err := p.createApp(ctx, baseURL, req.APIToken, req.AppLabel, req.CallbackURL, req.AppURL)
	if err != nil {
		return nil, "", err
	}
	clientID := app.Credentials.OAuthClient.ClientID
	p.logger.InfoContext(ctx, "okta app created", "org", req.OrgName, "app_id", app.ID, "client_id", clientID)
	return app, clientID, nil
}

// oauthClientSettings builds the app settings map.
func oauthClientSettings(clientURI, callbackURL, appURL string) map[string]interface{} {
	return map[string]interface{}{
		"client_uri":                clientURI,
		"redirect_uris":             []string{callbackURL},
		"post_logout_redirect_uris": []string{appURL},
		"response_types":            []string{"code"},
		"grant_types":               []string{"authorization_code"},
		"consent_method":            "REQUIRED",
		"idp_initiated_login":       map[string]interface{}{"mode": "DISABLED"},
		"application_type":          "web",
	}
}

type appResponse struct {
	ID          string `json:"id"`
	Credentials struct {
		OAuthClient struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"oauthClient"`
	} `json:"credentials"`
}

type groupResponse struct {
	ID      string `json:"id"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

type userResponse struct {
	ID string `json:"id"`
}

type errorResponse struct {
	ErrorCode    string `json:"errorCode"`
	ErrorSummary string `json:"errorSummary"`
}

type scopeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type policyResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type policyRuleResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type claimResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ValueType string `json:"valueType"`
}

func (p *Provisioner) createApp(ctx context.Context, baseURL, token, label, callbackURL, appURL string) (*appResponse, error) {
	body := map[string]interface{}{
		"name":       "oidc_client",
		"label":      label,
		"signOnMode": "OPENID_CONNECT",
		"credentials": map[string]interface{}{
			"oauthClient": map[string]interface{}{
				"token_endpoint_auth_method": "client_secret_post",
			},
		},
		"settings": map[string]interface{}{
			"oauthClient": oauthClientSettings(appURL, callbackURL, appURL),
		},
	}
	resp, err := p.doRequest(ctx, http.MethodPost, baseURL+"/api/v1/apps", token, body)
	if err != nil {
		return nil, &ProvisioningError{Operation: "create_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "create_app", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var app appResponse
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, &ProvisioningError{Operation: "create_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	return &app, nil
}

func (p *Provisioner) createGroup(ctx context.Context, baseURL, token, name, description string) (*groupResponse, error) {
	body := map[string]interface{}{
		"profile": map[string]interface{}{"name": name, "description": description},
	}
	resp, err := p.doRequest(ctx, http.MethodPost, baseURL+"/api/v1/groups", token, body)
	if err != nil {
		return nil, &ProvisioningError{Operation: "create_group", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "create_group", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var group groupResponse
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		return nil, &ProvisioningError{Operation: "create_group", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	return &group, nil
}

func (p *Provisioner) assignGroupToApp(ctx context.Context, baseURL, token, appID, groupID string) error {
	u := fmt.Sprintf("%s/api/v1/apps/%s/groups/%s", baseURL, appID, groupID)
	resp, err := p.doRequest(ctx, http.MethodPut, u, token, nil)
	if err != nil {
		return &ProvisioningError{Operation: "assign_group", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ProvisioningError{Operation: "assign_group", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	return nil
}

// adminGroupNames lists common Okta admin group names to search for when seeding.
var adminGroupNames = []string{"Administrators", "Super Admins", "Okta Administrators", "Admin", "Admins"}

func (p *Provisioner) getTokenOwner(ctx context.Context, baseURL, token string) (*userResponse, error) {
	resp, err := p.doRequest(ctx, http.MethodGet, baseURL+"/api/v1/users/me", token, nil)
	if err != nil {
		return nil, &ProvisioningError{Operation: "get_token_owner", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "get_token_owner", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var user userResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, &ProvisioningError{Operation: "get_token_owner", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	return &user, nil
}

func (p *Provisioner) addUserToGroup(ctx context.Context, baseURL, token, groupID, userID string) error {
	u := fmt.Sprintf("%s/api/v1/groups/%s/users/%s", baseURL, groupID, userID)
	resp, err := p.doRequest(ctx, http.MethodPut, u, token, nil)
	if err != nil {
		return &ProvisioningError{Operation: "add_user_to_group", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return &ProvisioningError{Operation: "add_user_to_group", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	return nil
}

func (p *Provisioner) listGroupMembers(ctx context.Context, baseURL, token, groupID string) ([]userResponse, error) {
	u := fmt.Sprintf("%s/api/v1/groups/%s/users?limit=200", baseURL, groupID)
	resp, err := p.doRequest(ctx, http.MethodGet, u, token, nil)
	if err != nil {
		return nil, &ProvisioningError{Operation: "list_group_members", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "list_group_members", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var users []userResponse
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, &ProvisioningError{Operation: "list_group_members", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	return users, nil
}

// seedAdminFromTokenOwner adds the API-token owner to the target admin group and
// best-effort copies members of common named admin groups.
func (p *Provisioner) seedAdminFromTokenOwner(ctx context.Context, baseURL, token, targetGroupID string) error {
	owner, err := p.getTokenOwner(ctx, baseURL, token)
	if err != nil {
		return err
	}
	if err := p.addUserToGroup(ctx, baseURL, token, targetGroupID, owner.ID); err != nil {
		return err
	}
	p.seedNamedAdminGroup(ctx, baseURL, token, targetGroupID)
	return nil
}

func (p *Provisioner) seedNamedAdminGroup(ctx context.Context, baseURL, token, targetGroupID string) {
	for _, name := range adminGroupNames {
		found, err := p.findGroupByName(ctx, baseURL, token, name)
		if err != nil {
			p.logger.WarnContext(ctx, "error searching admin group, continuing", "group", name, "error", err)
			continue
		}
		if found == nil {
			continue
		}
		members, err := p.listGroupMembers(ctx, baseURL, token, found.ID)
		if err != nil {
			p.logger.WarnContext(ctx, "error listing admin group members, continuing", "group", name, "error", err)
			return
		}
		for _, u := range members {
			if err := p.addUserToGroup(ctx, baseURL, token, targetGroupID, u.ID); err != nil {
				p.logger.WarnContext(ctx, "error adding admin member, continuing", "user_id", u.ID, "error", err)
			}
		}
		return
	}
}

func (p *Provisioner) configureGroupsScope(ctx context.Context, baseURL, token string) error {
	scopesURL := baseURL + "/api/v1/authorizationServers/default/scopes"
	resp, err := p.doRequest(ctx, http.MethodGet, scopesURL, token, nil)
	if err != nil {
		return &ProvisioningError{Operation: "list_scopes", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ProvisioningError{Operation: "list_scopes", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var scopes []scopeResponse
	if err := json.NewDecoder(resp.Body).Decode(&scopes); err != nil {
		return &ProvisioningError{Operation: "list_scopes", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	for i := range scopes {
		if scopes[i].Name == "groups" {
			return nil
		}
	}
	body := map[string]interface{}{
		"name":            "groups",
		"description":     "Access to group membership information",
		"displayName":     "View your group memberships",
		"consent":         "IMPLICIT",
		"metadataPublish": "ALL_CLIENTS",
	}
	post, err := p.doRequest(ctx, http.MethodPost, scopesURL, token, body)
	if err != nil {
		return &ProvisioningError{Operation: "create_scope", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusOK && post.StatusCode != http.StatusCreated {
		return &ProvisioningError{Operation: "create_scope", OrgName: orgNameFromURL(baseURL), Err: parseError(post)}
	}
	return nil
}

func (p *Provisioner) configureGroupsClaim(ctx context.Context, baseURL, token string, filter GroupsClaimFilter) error {
	claimsURL := baseURL + "/api/v1/authorizationServers/default/claims"
	resp, err := p.doRequest(ctx, http.MethodGet, claimsURL, token, nil)
	if err != nil {
		return &ProvisioningError{Operation: "list_claims", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ProvisioningError{Operation: "list_claims", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var claims []claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return &ProvisioningError{Operation: "list_claims", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	var existing *claimResponse
	for i := range claims {
		if claims[i].Name == "groups" && claims[i].ValueType == "GROUPS" {
			existing = &claims[i]
			break
		}
	}
	body := map[string]interface{}{
		"name":                 "groups",
		"status":               "ACTIVE",
		"claimType":            "IDENTITY",
		"valueType":            "GROUPS",
		"group_filter_type":    filter.Type,
		"value":                filter.Value,
		"alwaysIncludeInToken": true,
		"conditions":           map[string]interface{}{"scopes": []string{}},
	}
	if existing != nil {
		put, err := p.doRequest(ctx, http.MethodPut, claimsURL+"/"+existing.ID, token, body)
		if err != nil {
			return &ProvisioningError{Operation: "update_claim", OrgName: orgNameFromURL(baseURL), Err: err}
		}
		defer put.Body.Close()
		if put.StatusCode != http.StatusOK {
			return &ProvisioningError{Operation: "update_claim", OrgName: orgNameFromURL(baseURL), Err: parseError(put)}
		}
		return nil
	}
	post, err := p.doRequest(ctx, http.MethodPost, claimsURL, token, body)
	if err != nil {
		return &ProvisioningError{Operation: "create_claim", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusOK && post.StatusCode != http.StatusCreated {
		return &ProvisioningError{Operation: "create_claim", OrgName: orgNameFromURL(baseURL), Err: parseError(post)}
	}
	return nil
}

func (p *Provisioner) configureAccessPolicy(ctx context.Context, baseURL, token, appLabel string) error {
	policiesURL := baseURL + "/api/v1/authorizationServers/default/policies"
	policyName := appLabel + " Access Policy"
	ruleName := appLabel + " Default Rule"

	resp, err := p.doRequest(ctx, http.MethodGet, policiesURL, token, nil)
	if err != nil {
		return &ProvisioningError{Operation: "list_policies", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ProvisioningError{Operation: "list_policies", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var policies []policyResponse
	if err := json.NewDecoder(resp.Body).Decode(&policies); err != nil {
		return &ProvisioningError{Operation: "list_policies", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	var policyID string
	for i := range policies {
		if policies[i].Name == policyName {
			policyID = policies[i].ID
			break
		}
	}
	if policyID == "" {
		body := map[string]interface{}{
			"name":        policyName,
			"description": "Access policy for " + appLabel,
			"priority":    1,
			"status":      "ACTIVE",
			"type":        "OAUTH_AUTHORIZATION_POLICY",
			"conditions":  map[string]interface{}{"clients": map[string]interface{}{"include": []string{"ALL_CLIENTS"}}},
		}
		post, err := p.doRequest(ctx, http.MethodPost, policiesURL, token, body)
		if err != nil {
			return &ProvisioningError{Operation: "create_policy", OrgName: orgNameFromURL(baseURL), Err: err}
		}
		defer post.Body.Close()
		if post.StatusCode != http.StatusOK && post.StatusCode != http.StatusCreated {
			return &ProvisioningError{Operation: "create_policy", OrgName: orgNameFromURL(baseURL), Err: parseError(post)}
		}
		var created policyResponse
		if err := json.NewDecoder(post.Body).Decode(&created); err != nil {
			return &ProvisioningError{Operation: "create_policy", OrgName: orgNameFromURL(baseURL), Err: err}
		}
		policyID = created.ID
	}

	rulesURL := fmt.Sprintf("%s/api/v1/authorizationServers/default/policies/%s/rules", baseURL, policyID)
	rulesResp, err := p.doRequest(ctx, http.MethodGet, rulesURL, token, nil)
	if err != nil {
		return &ProvisioningError{Operation: "list_policy_rules", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer rulesResp.Body.Close()
	if rulesResp.StatusCode != http.StatusOK {
		return &ProvisioningError{Operation: "list_policy_rules", OrgName: orgNameFromURL(baseURL), Err: parseError(rulesResp)}
	}
	var rules []policyRuleResponse
	if err := json.NewDecoder(rulesResp.Body).Decode(&rules); err != nil {
		return &ProvisioningError{Operation: "list_policy_rules", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	for i := range rules {
		if rules[i].Name == ruleName {
			return nil
		}
	}
	body := map[string]interface{}{
		"name":     ruleName,
		"priority": 1,
		"status":   "ACTIVE",
		"type":     "RESOURCE_ACCESS",
		"conditions": map[string]interface{}{
			"grantTypes": map[string]interface{}{"include": []string{"authorization_code"}},
			"scopes":     map[string]interface{}{"include": []string{"openid", "profile", "email", "groups"}},
			"people":     map[string]interface{}{"groups": map[string]interface{}{"include": []string{"EVERYONE"}}},
		},
		"actions": map[string]interface{}{
			"token": map[string]interface{}{
				"accessTokenLifetimeMinutes":  60,
				"refreshTokenLifetimeMinutes": 0,
				"refreshTokenWindowMinutes":   10080,
			},
		},
	}
	post, err := p.doRequest(ctx, http.MethodPost, rulesURL, token, body)
	if err != nil {
		return &ProvisioningError{Operation: "create_policy_rule", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusOK && post.StatusCode != http.StatusCreated {
		return &ProvisioningError{Operation: "create_policy_rule", OrgName: orgNameFromURL(baseURL), Err: parseError(post)}
	}
	return nil
}

type appSearchResult struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Credentials struct {
		OAuthClient struct {
			ClientID string `json:"client_id"`
		} `json:"oauthClient"`
	} `json:"credentials"`
}

func (p *Provisioner) findAppByLabel(ctx context.Context, baseURL, token, label string) (*appSearchResult, error) {
	searchTerm := label
	if idx := strings.Index(label, " "); idx > 0 {
		searchTerm = label[:idx]
	}
	u := baseURL + "/api/v1/apps?q=" + url.QueryEscape(searchTerm) + "&limit=200"
	resp, err := p.doRequest(ctx, http.MethodGet, u, token, nil)
	if err != nil {
		return nil, &ProvisioningError{Operation: "find_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "find_app", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var results []appSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, &ProvisioningError{Operation: "find_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	for i := range results {
		if results[i].Label == label {
			return &results[i], nil
		}
	}
	return nil, nil
}

func (p *Provisioner) findGroupByName(ctx context.Context, baseURL, token, name string) (*groupResponse, error) {
	searchTerm := name
	if idx := strings.Index(name, " "); idx > 0 {
		searchTerm = name[:idx]
	}
	u := baseURL + "/api/v1/groups?q=" + url.QueryEscape(searchTerm) + "&limit=200"
	resp, err := p.doRequest(ctx, http.MethodGet, u, token, nil)
	if err != nil {
		return nil, &ProvisioningError{Operation: "find_group", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "find_group", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var results []groupResponse
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, &ProvisioningError{Operation: "find_group", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	for i := range results {
		if results[i].Profile.Name == name {
			return &results[i], nil
		}
	}
	return nil, nil
}

func (p *Provisioner) getApp(ctx context.Context, baseURL, token, appID string) (map[string]interface{}, error) {
	resp, err := p.doRequest(ctx, http.MethodGet, baseURL+"/api/v1/apps/"+appID, token, nil)
	if err != nil {
		return nil, &ProvisioningError{Operation: "get_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "get_app", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var app map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, &ProvisioningError{Operation: "get_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	return app, nil
}

// mergeAppUpdate strips server-managed fields and applies the desired mutations
// so the app body can be PUT back (Okta requires a full representation on PUT).
func mergeAppUpdate(app map[string]interface{}, label, callbackURL, appURL string) {
	for _, k := range []string{"_links", "_embedded", "orn", "created", "lastUpdated"} {
		delete(app, k)
	}
	app["label"] = label

	creds, ok := app["credentials"].(map[string]interface{})
	if !ok || creds == nil {
		creds = map[string]interface{}{}
		app["credentials"] = creds
	}
	oauthCreds, ok := creds["oauthClient"].(map[string]interface{})
	if !ok || oauthCreds == nil {
		oauthCreds = map[string]interface{}{}
		creds["oauthClient"] = oauthCreds
	}
	oauthCreds["token_endpoint_auth_method"] = "client_secret_post"

	settings, ok := app["settings"].(map[string]interface{})
	if !ok || settings == nil {
		settings = map[string]interface{}{}
		app["settings"] = settings
	}
	oauthSet, ok := settings["oauthClient"].(map[string]interface{})
	if !ok || oauthSet == nil {
		oauthSet = map[string]interface{}{}
		settings["oauthClient"] = oauthSet
	}
	oauthSet["client_uri"] = appURL
	oauthSet["redirect_uris"] = []string{callbackURL}
	oauthSet["post_logout_redirect_uris"] = []string{appURL}
	if _, ok := oauthSet["response_types"]; !ok {
		oauthSet["response_types"] = []string{"code"}
	}
	if _, ok := oauthSet["grant_types"]; !ok {
		oauthSet["grant_types"] = []string{"authorization_code"}
	}
	if _, ok := oauthSet["consent_method"]; !ok {
		oauthSet["consent_method"] = "REQUIRED"
	}
	if _, ok := oauthSet["idp_initiated_login"]; !ok {
		oauthSet["idp_initiated_login"] = map[string]interface{}{"mode": "DISABLED"}
	}
}

func (p *Provisioner) updateApp(ctx context.Context, baseURL, token, appID string, appData map[string]interface{}) (*appResponse, error) {
	resp, err := p.doRequest(ctx, http.MethodPut, baseURL+"/api/v1/apps/"+appID, token, appData)
	if err != nil {
		return nil, &ProvisioningError{Operation: "update_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ProvisioningError{Operation: "update_app", OrgName: orgNameFromURL(baseURL), Err: parseError(resp)}
	}
	var app appResponse
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, &ProvisioningError{Operation: "update_app", OrgName: orgNameFromURL(baseURL), Err: err}
	}
	return &app, nil
}

func (p *Provisioner) doRequest(ctx context.Context, method, u, token string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, &RequestError{Op: "marshal request body", Cause: err}
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, &RequestError{Op: "create request", Cause: err}
	}
	req.Header.Set("Authorization", "SSWS "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return p.httpClient.Do(req)
}

func parseError(resp *http.Response) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return &HTTPError{StatusCode: resp.StatusCode, ReadErr: err}
	}
	var oktaErr errorResponse
	if err := json.Unmarshal(data, &oktaErr); err != nil {
		return &HTTPError{StatusCode: resp.StatusCode, Detail: string(data)}
	}
	if oktaErr.ErrorSummary != "" {
		return &HTTPError{StatusCode: resp.StatusCode, Detail: fmt.Sprintf("%s (%s)", oktaErr.ErrorSummary, oktaErr.ErrorCode)}
	}
	return &HTTPError{StatusCode: resp.StatusCode, Detail: string(data)}
}

func orgNameFromURL(baseURL string) string {
	const prefix = "https://"
	const suffix = ".okta.com"
	if len(baseURL) > len(prefix)+len(suffix) {
		return baseURL[len(prefix) : len(baseURL)-len(suffix)]
	}
	return baseURL
}
