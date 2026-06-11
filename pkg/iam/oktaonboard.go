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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/oktaprovision"
)

// OktaOnboardingError indicates a failure during one-step Okta tenant
// onboarding. It carries the step that failed so the operator knows whether the
// Okta org, the federation provider, or the home-realm routing was the problem.
type OktaOnboardingError struct {
	// Step is the onboarding phase that failed: "provision", "register_provider",
	// or "register_domain".
	Step string

	// Err is the underlying cause.
	Err error
}

func (e *OktaOnboardingError) Error() string {
	return "iam: okta onboarding failed at step " + e.Step + ": " + e.Err.Error()
}

func (e *OktaOnboardingError) Unwrap() error { return e.Err }

// OktaRoleGroup pairs an Okta role group to create/reuse with the tenant role it
// maps to. The Okta group NAME (not its ID) is what the IdP emits in the
// upstream token's groups claim, so it is what the federation GroupRoleMapping
// matches against.
type OktaRoleGroup struct {
	// Name is the Okta group name (e.g. "Acme Administrators"). It is created or
	// reused in the org and emitted in the groups claim.
	Name string

	// Description is the Okta group description.
	Description string

	// RoleID is the tenant-scoped quicbaas role assigned to a user whose IdP
	// token carries this group. Required.
	RoleID string

	// Prefix, when true, makes the resulting GroupRoleMapping a STARTS_WITH match
	// instead of an exact match.
	Prefix bool
}

// OktaOnboardingRequest is the single input to OnboardTenant. It carries the
// Okta org + SSWS admin token, the tenant, the email domains the tenant owns,
// and the role-group→role mappings. From this one struct the orchestrator
// drives ProvisionApp → RegisterProvider → RegisterDomain.
type OktaOnboardingRequest struct {
	// TenantID is the quicbaas tenant being onboarded. Required.
	TenantID string

	// ProviderName is the federation provider name to register under the tenant
	// (e.g. "okta"). Required.
	ProviderName string

	// APIToken is the Okta Management-API SSWS token. Used only for the
	// provisioning call; never stored on the provider.
	APIToken string

	// OrgName is the Okta org subdomain label (e.g. "acme" for acme.okta.com).
	OrgName string

	// AppLabel is the OAuth app label to create or reuse in the org. Required.
	AppLabel string

	// CallbackURL is the OAuth redirect URI registered on the app and used by
	// BeginLogin. Required.
	CallbackURL string

	// AppURL is the application's base URL (client_uri / post-logout redirect).
	AppURL string

	// EmailDomains are the email domains the tenant owns. Each is both (a) added
	// to the provider's AllowedEmailDomains allowlist and (b) registered in the
	// home-realm directory so a login from that domain routes to this tenant.
	// Required (at least one).
	EmailDomains []string

	// RoleGroups are the Okta groups to provision and their tenant-role mappings.
	// Required (at least one) so the resulting provider is never zero-role.
	RoleGroups []OktaRoleGroup

	// SeedAdminGroupName, when non-empty, is the RoleGroup name into which the
	// API-token owner is seeded so at least one administrator can log in
	// immediately.
	SeedAdminGroupName string

	// GroupsClaimFilter scopes which Okta group names appear in the token. When
	// zero-valued the orchestrator derives a STARTS_WITH filter from the common
	// prefix of the role-group names; set it explicitly to override.
	GroupsClaimFilter oktaprovision.GroupsClaimFilter

	// Scopes overrides the OAuth scopes requested. Defaults to
	// ["openid", "email", "profile", "groups"] so the groups claim is emitted.
	Scopes []string

	// AutoProvision enables JIT provisioning on the registered provider.
	AutoProvision bool

	// AllowLinking enables account-linking-by-email on the registered provider.
	AllowLinking bool

	// RequireExplicitRoleMapping fails logins closed when no role resolves.
	RequireExplicitRoleMapping bool
}

// OktaOnboardingResult reports what the one-step onboarding produced.
type OktaOnboardingResult struct {
	// Provider is the registered federation provider (with ClientSecret redacted
	// in logs by callers; it is populated here so the caller can persist it).
	Provider *ExternalProvider

	// OktaResult is the raw provisioning result (issuer/clientID/secret/groups).
	OktaResult *oktaprovision.Result

	// RegisteredDomains are the email domains bound to the tenant in the HRD.
	RegisteredDomains []string
}

// OktaOnboarder performs one-step Okta tenant onboarding by composing the Okta
// Management-API provisioner, the federation manager, and the home-realm
// directory. It is the single entry point that takes an operator from
// "Okta org + token + domains" to a working, HRD-routable broker provider.
type OktaOnboarder struct {
	provisioner *oktaprovision.Provisioner
	federation  FederationManager
	hrd         *HomeRealmDirectory
}

// NewOktaOnboarder builds an onboarder. All three collaborators are required;
// the constructor fails closed if any is nil.
func NewOktaOnboarder(provisioner *oktaprovision.Provisioner, federation FederationManager, hrd *HomeRealmDirectory) (*OktaOnboarder, error) {
	if provisioner == nil {
		return nil, &OktaOnboardingError{Step: "init", Err: &MissingCollaboratorError{Name: "provisioner"}}
	}
	if federation == nil {
		return nil, &OktaOnboardingError{Step: "init", Err: &MissingCollaboratorError{Name: "federation manager"}}
	}
	if hrd == nil {
		return nil, &OktaOnboardingError{Step: "init", Err: &MissingCollaboratorError{Name: "home-realm directory"}}
	}
	return &OktaOnboarder{
		provisioner: provisioner,
		federation:  federation,
		hrd:         hrd,
	}, nil
}

// MissingCollaboratorError indicates a required onboarder collaborator was nil.
type MissingCollaboratorError struct {
	Name string
}

func (e *MissingCollaboratorError) Error() string {
	return "iam: required onboarder collaborator is nil: " + e.Name
}

// OnboardTenant runs the full one-step onboarding, fail-closed, in order:
//
//  1. oktaprovision.ProvisionApp — create/reuse the Okta OAuth app + role groups
//     and configure the groups scope/claim/policy, yielding the issuer, client
//     credentials, and resolved group IDs.
//  2. FederationManager.RegisterProvider — build an ExternalProvider from the
//     provisioning Result (issuer/clientID/secret/scopes) with GroupRoleMapping
//     entries derived from the request's RoleGroups (matched on the Okta group
//     NAME the IdP emits) and AllowedEmailDomains set to the request domains.
//  3. HomeRealmDirectory.RegisterDomain — bind each email domain to the tenant
//     so a login from that domain is routable to this provider.
//
// A failure at any step returns an *OktaOnboardingError naming the step. Steps
// 1 and 3 are idempotent (existing apps/groups are reused; a domain already
// bound to THIS tenant is a no-op); step 2 is rejected if the provider name
// already exists for the tenant.
func (o *OktaOnboarder) OnboardTenant(ctx context.Context, req OktaOnboardingRequest) (*OktaOnboardingResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	// Step 1: provision the Okta org.
	provReq := oktaprovision.ProvisionRequest{
		APIToken:           req.APIToken,
		OrgName:            req.OrgName,
		AppLabel:           req.AppLabel,
		CallbackURL:        req.CallbackURL,
		AppURL:             req.AppURL,
		RoleGroups:         req.toProvisionRoleGroups(),
		GroupsClaimFilter:  req.resolveGroupsClaimFilter(),
		SeedAdminGroupName: req.SeedAdminGroupName,
	}
	result, err := o.provisioner.ProvisionApp(ctx, provReq)
	if err != nil {
		return nil, &OktaOnboardingError{Step: "provision", Err: err}
	}

	// Step 2: register the federation provider built from the provisioning
	// result + the request's role and domain policy.
	provider := req.buildProvider(result)
	if err := o.federation.RegisterProvider(ctx, provider); err != nil {
		return nil, &OktaOnboardingError{Step: "register_provider", Err: err}
	}

	// Step 3: bind each email domain to the tenant in the home-realm directory.
	registered := make([]string, 0, len(req.EmailDomains))
	for _, domain := range req.EmailDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if err := o.hrd.RegisterDomain(ctx, req.TenantID, domain); err != nil {
			return nil, &OktaOnboardingError{Step: "register_domain", Err: err}
		}
		registered = append(registered, domain)
	}

	return &OktaOnboardingResult{
		Provider:          provider,
		OktaResult:        result,
		RegisteredDomains: registered,
	}, nil
}

// validate enforces the required inputs fail-closed.
func (r *OktaOnboardingRequest) validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return &OktaOnboardingError{Step: "validate", Err: &OnboardingInputError{Reason: "tenant ID is required"}}
	}
	if strings.TrimSpace(r.ProviderName) == "" {
		return &OktaOnboardingError{Step: "validate", Err: &OnboardingInputError{Reason: "provider name is required"}}
	}
	if len(r.EmailDomains) == 0 {
		return &OktaOnboardingError{Step: "validate", Err: &OnboardingInputError{Reason: "at least one email domain is required"}}
	}
	if len(r.RoleGroups) == 0 {
		return &OktaOnboardingError{Step: "validate", Err: &OnboardingInputError{Reason: "at least one role group is required"}}
	}
	for _, rg := range r.RoleGroups {
		if strings.TrimSpace(rg.Name) == "" || strings.TrimSpace(rg.RoleID) == "" {
			return &OktaOnboardingError{Step: "validate", Err: &OnboardingInputError{Reason: "each role group requires a name and role ID"}}
		}
	}
	return nil
}

// OnboardingInputError indicates an invalid onboarding request.
type OnboardingInputError struct {
	Reason string
}

func (e *OnboardingInputError) Error() string {
	return "iam: invalid okta onboarding request: " + e.Reason
}

// toProvisionRoleGroups maps the onboarding role groups to provisioner role
// groups (name + description only; the role binding stays on the federation
// side).
func (r *OktaOnboardingRequest) toProvisionRoleGroups() []oktaprovision.RoleGroup {
	out := make([]oktaprovision.RoleGroup, 0, len(r.RoleGroups))
	for _, rg := range r.RoleGroups {
		out = append(out, oktaprovision.RoleGroup{Name: rg.Name, Description: rg.Description})
	}
	return out
}

// resolveGroupsClaimFilter returns the explicit filter when set, otherwise
// derives a STARTS_WITH filter from the longest common prefix of the role-group
// names so only this tenant's groups appear in the token. When no shared prefix
// exists, the zero filter is returned (provisioner skips the filter).
func (r *OktaOnboardingRequest) resolveGroupsClaimFilter() oktaprovision.GroupsClaimFilter {
	if r.GroupsClaimFilter.Type != "" {
		return r.GroupsClaimFilter
	}
	names := make([]string, 0, len(r.RoleGroups))
	for _, rg := range r.RoleGroups {
		names = append(names, rg.Name)
	}
	prefix := longestCommonPrefix(names)
	prefix = strings.TrimRight(prefix, " ")
	if prefix == "" {
		return oktaprovision.GroupsClaimFilter{}
	}
	return oktaprovision.GroupsClaimFilter{Type: "STARTS_WITH", Value: prefix}
}

// buildProvider constructs the ExternalProvider from the provisioning result and
// the request policy: issuer/clientID/secret/scopes from Okta, GroupRoleMapping
// entries keyed on the Okta group NAME (which the IdP emits in the groups
// claim), and AllowedEmailDomains from the request domains.
func (r *OktaOnboardingRequest) buildProvider(result *oktaprovision.Result) *ExternalProvider {
	scopes := r.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile", "groups"}
	}

	mappings := make([]GroupRoleMapping, 0, len(r.RoleGroups))
	for _, rg := range r.RoleGroups {
		// Only map groups that were actually provisioned/resolved in the org.
		if _, ok := result.Groups[rg.Name]; !ok {
			continue
		}
		mappings = append(mappings, GroupRoleMapping{
			Group:  rg.Name,
			RoleID: rg.RoleID,
			Prefix: rg.Prefix,
		})
	}

	domains := make([]string, 0, len(r.EmailDomains))
	for _, d := range r.EmailDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			domains = append(domains, d)
		}
	}

	return &ExternalProvider{
		TenantID:                   r.TenantID,
		Name:                       r.ProviderName,
		Type:                       ProviderTypeOIDC,
		Enabled:                    true,
		ClientID:                   result.ClientID,
		ClientSecret:               result.ClientSecret,
		IssuerURL:                  result.Issuer,
		Scopes:                     scopes,
		AutoProvision:              r.AutoProvision,
		AllowLinking:               r.AllowLinking,
		AllowedEmailDomains:        domains,
		RequireExplicitRoleMapping: r.RequireExplicitRoleMapping,
		ClaimMappings: &ClaimMappings{
			Subject:           DefaultSubjectClaim,
			Email:             DefaultEmailClaim,
			Name:              DefaultNameClaim,
			Groups:            "groups",
			GroupRoleMappings: mappings,
		},
	}
}

// longestCommonPrefix returns the longest common string prefix of the inputs, or
// "" when there is none. A single input yields itself.
func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, v := range values[1:] {
		max := len(prefix)
		if len(v) < max {
			max = len(v)
		}
		i := 0
		for i < max && prefix[i] == v[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			break
		}
	}
	return prefix
}
