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

import "strings"

// RoleResolution is the outcome of mapping an IdP token's groups/roles claims to
// tenant-scoped quicbaas roles.
type RoleResolution struct {
	// RoleIDs are the resolved tenant role IDs (or names) to assign.
	RoleIDs []string

	// UsedDefaults is true when no claim mappings were configured and the
	// provider DefaultRoles were used as the fallback.
	UsedDefaults bool
}

// NoScopeMatchError is returned when claim->scope mappings are configured but the
// IdP token's groups/roles matched none of them. The federated login is denied
// (fail-closed), mirroring NoGroupMatchError for the role axis.
type NoScopeMatchError struct {
	ProviderID string
}

func (e *NoScopeMatchError) Error() string {
	return "iam: federated login denied: no IdP group or role matched any configured scope mapping"
}

// resolveRoles maps the IdP token's groups/roles claims to tenant roles using
// the provider's ClaimMappings, fail-closed:
//
//   - If Groups or Roles claim mappings are configured (i.e. the provider names
//     a groups/roles claim AND defines GroupRoleMappings entries), the IdP
//     values are matched against those entries. If none match, a
//     *NoGroupMatchError is returned and the login MUST be denied.
//   - If no claim mappings are configured, the provider DefaultRoles are used.
//
// The matching is case-insensitive on the IdP group/role value and supports an
// optional STARTS_WITH prefix match per mapping entry.
func resolveRoles(provider *ExternalProvider, claims map[string]interface{}) (*RoleResolution, error) {
	mappings := provider.ClaimMappings
	mapped := mappings != nil && len(mappings.GroupRoleMappings) > 0

	if !mapped {
		// No claim->role mappings configured: fall back to static DefaultRoles.
		return &RoleResolution{
			RoleIDs:      append([]string(nil), provider.DefaultRoles...),
			UsedDefaults: true,
		}, nil
	}

	// Collect the IdP-asserted group/role values from the configured claims.
	var idpValues []string
	if mappings.Groups != "" {
		idpValues = append(idpValues, claimStringSlice(claims[mappings.Groups])...)
	}
	if mappings.Roles != "" {
		idpValues = append(idpValues, claimStringSlice(claims[mappings.Roles])...)
	}

	resolved := matchRoleMappings(mappings.GroupRoleMappings, idpValues)
	if len(resolved) == 0 {
		// Mappings configured but nothing matched: fail closed.
		return nil, &NoGroupMatchError{ProviderID: provider.ID}
	}

	return &RoleResolution{RoleIDs: resolved}, nil
}

// ScopeResolution is the outcome of mapping an IdP token's groups/roles claims
// to platform token scopes (the `scp` catalog used by the issued token).
type ScopeResolution struct {
	// Scopes are the resolved platform token scopes to grant.
	Scopes []string

	// UsedDefaults is true when no GroupScopeMappings were configured and the
	// provider DefaultScopes were used as the fallback.
	UsedDefaults bool
}

// resolveScopes maps the IdP token's groups/roles claims to platform token
// scopes using the provider's ClaimMappings, fail-closed. It mirrors
// resolveRoles exactly, but targets the `scp` catalog (e.g. "<svc>.<verb>")
// rather than tenant roles:
//
//   - If GroupScopeMappings are configured (non-empty), the IdP group/role
//     values (read from the Groups/Roles claims) are matched against them. If
//     none match, a *NoScopeMatchError is returned and the login MUST be denied.
//   - If no GroupScopeMappings are configured, the provider DefaultScopes are
//     used as the back-compatible fallback.
//
// Scopes are mapped directly from IdP claims (not derived from resolved roles)
// because the platform `scp` catalog (service.verb capabilities) does not map
// one-to-one onto tenant roles; keeping the two resolutions independent lets a
// tenant grant capabilities and roles on different axes. The matching is
// case-insensitive and supports an optional STARTS_WITH prefix per entry,
// identical to role resolution.
func resolveScopes(provider *ExternalProvider, claims map[string]interface{}) (*ScopeResolution, error) {
	mappings := provider.ClaimMappings
	mapped := mappings != nil && len(mappings.GroupScopeMappings) > 0

	if !mapped {
		// No claim->scope mappings configured: fall back to static DefaultScopes.
		return &ScopeResolution{
			Scopes:       append([]string(nil), provider.DefaultScopes...),
			UsedDefaults: true,
		}, nil
	}

	var idpValues []string
	if mappings.Groups != "" {
		idpValues = append(idpValues, claimStringSlice(claims[mappings.Groups])...)
	}
	if mappings.Roles != "" {
		idpValues = append(idpValues, claimStringSlice(claims[mappings.Roles])...)
	}

	resolved := matchScopeMappings(mappings.GroupScopeMappings, idpValues)
	if len(resolved) == 0 {
		// Mappings configured but nothing matched: fail closed.
		return nil, &NoScopeMatchError{ProviderID: provider.ID}
	}

	return &ScopeResolution{Scopes: resolved}, nil
}

// matchScopeMappings returns the de-duplicated set of scopes whose mapping
// matched at least one IdP value.
func matchScopeMappings(mappings []GroupScopeMapping, idpValues []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range mappings {
		for _, v := range idpValues {
			if scopeGroupMatches(m, v) {
				if _, ok := seen[m.Scope]; !ok {
					seen[m.Scope] = struct{}{}
					out = append(out, m.Scope)
				}
				break
			}
		}
	}
	return out
}

// scopeGroupMatches reports whether an IdP group/role value satisfies a scope
// mapping entry, using the same case-insensitive exact/STARTS_WITH semantics as
// groupMatches.
func scopeGroupMatches(m GroupScopeMapping, idpValue string) bool {
	want := strings.ToLower(strings.TrimSpace(m.Group))
	got := strings.ToLower(strings.TrimSpace(idpValue))
	if want == "" || got == "" {
		return false
	}
	if m.Prefix {
		return strings.HasPrefix(got, want)
	}
	return got == want
}

// matchRoleMappings returns the de-duplicated set of role IDs whose mapping
// matched at least one IdP value.
func matchRoleMappings(mappings []GroupRoleMapping, idpValues []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range mappings {
		for _, v := range idpValues {
			if groupMatches(m, v) {
				if _, ok := seen[m.RoleID]; !ok {
					seen[m.RoleID] = struct{}{}
					out = append(out, m.RoleID)
				}
				break
			}
		}
	}
	return out
}

// groupMatches reports whether an IdP group/role value satisfies a mapping
// entry. Matching is case-insensitive. When Prefix is set the IdP value must
// start with the mapping value (STARTS_WITH); otherwise exact match is used.
func groupMatches(m GroupRoleMapping, idpValue string) bool {
	want := strings.ToLower(strings.TrimSpace(m.Group))
	got := strings.ToLower(strings.TrimSpace(idpValue))
	if want == "" || got == "" {
		return false
	}
	if m.Prefix {
		return strings.HasPrefix(got, want)
	}
	return got == want
}

// claimStringSlice coerces a JSON claim value into a string slice. It accepts a
// single string, a []string, or a []interface{} of strings (the shapes IdPs use
// for groups/roles claims).
func claimStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, e := range val {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// claimEmailVerified reports whether the IdP claims assert a verified email.
// The OIDC `email_verified` claim is a boolean per the spec, but some IdPs
// (and JSON decoders that round-trip through interface{}) surface it as the
// string "true"/"false". Both shapes are handled. A missing or unrecognised
// value is treated as NOT verified (fail-closed).
func claimEmailVerified(claims map[string]interface{}) bool {
	v, ok := claims["email_verified"]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(strings.TrimSpace(val), "true")
	default:
		return false
	}
}

// emailDomain returns the lower-cased domain part of an email address, or "" when
// the address has no '@' or an empty domain.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

// emailDomainAllowed reports whether email's domain is permitted given the
// provider's AllowedEmailDomains allowlist. An empty allowlist permits all
// domains (back-compatible). Matching is case-insensitive. An email with no
// resolvable domain is rejected whenever an allowlist is configured.
func emailDomainAllowed(allowed []string, email string) bool {
	if len(allowed) == 0 {
		return true
	}
	domain := emailDomain(email)
	if domain == "" {
		return false
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), domain) {
			return true
		}
	}
	return false
}
