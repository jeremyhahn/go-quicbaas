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
	"net/url"
	"strings"
)

// ResourceIndicatorConfig contains configuration for resource indicator validation.
type ResourceIndicatorConfig struct {
	// Enabled indicates whether resource indicators are supported.
	Enabled bool

	// AllowedResources is a list of allowed resource URIs.
	// If empty, any valid URI is accepted.
	AllowedResources []string

	// RequireHTTPS indicates whether resource URIs must use HTTPS.
	// Defaults to true in production.
	RequireHTTPS bool
}

// DefaultResourceIndicatorConfig returns default resource indicator configuration.
func DefaultResourceIndicatorConfig() *ResourceIndicatorConfig {
	return &ResourceIndicatorConfig{
		Enabled:          true,
		AllowedResources: nil,
		RequireHTTPS:     true,
	}
}

// ValidateResourceIndicator validates a resource indicator URI per RFC 8707.
// Returns an error if the resource is invalid.
func ValidateResourceIndicator(resource string, config *ResourceIndicatorConfig) error {
	if resource == "" {
		// Empty resource is allowed if not required
		return nil
	}

	// Parse the resource URI
	u, err := url.Parse(resource)
	if err != nil {
		return &ResourceURIError{Detail: "invalid resource URI", Cause: err}
	}

	// Must be an absolute URI
	if !u.IsAbs() {
		return &ResourceURIError{Detail: "resource must be an absolute URI"}
	}

	// Check scheme
	if config != nil && config.RequireHTTPS {
		if u.Scheme != "https" {
			// Allow http for localhost (development)
			if u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") {
				// Allow for development
			} else {
				return &ResourceURIError{Detail: "resource URI must use HTTPS"}
			}
		}
	}

	// Must not have fragment
	if u.Fragment != "" {
		return &ResourceURIError{Detail: "resource URI must not have a fragment"}
	}

	// Check against allowed resources if configured
	if config != nil && len(config.AllowedResources) > 0 {
		found := false
		for _, allowed := range config.AllowedResources {
			if resource == allowed || strings.HasPrefix(resource, allowed) {
				found = true
				break
			}
		}
		if !found {
			return &ResourceURIError{Detail: "resource URI not in allowed list"}
		}
	}

	return nil
}

// ValidateDefaultAudience validates a per-app default service audience.
//
// A default audience is the value an OAuth client's token carries in its `aud`
// claim when the client does NOT send an RFC 8707 `resource` indicator. It
// lets downstream resource servers that are configured for a fixed service
// audience (e.g. "api://orchestrator") accept tokens from clients that never
// learned to send a resource indicator.
//
// Validation mirrors ValidateResourceIndicator (absolute URI, no fragment) but
// additionally accepts the "api://<service>" custom scheme used for service
// audiences — the same shape a `resource` indicator carries when it lands in
// the audience. Empty is valid and means "no default" (current behavior:
// audience is built from the client/issuer only). An https URI is also valid.
func ValidateDefaultAudience(audience string) error {
	if audience == "" {
		// Empty means no default; fully backward compatible.
		return nil
	}

	u, err := url.Parse(audience)
	if err != nil {
		return &ResourceURIError{Detail: "invalid default audience URI", Cause: err}
	}

	if !u.IsAbs() {
		return &ResourceURIError{Detail: "default audience must be an absolute URI"}
	}

	if u.Fragment != "" {
		return &ResourceURIError{Detail: "default audience URI must not have a fragment"}
	}

	switch u.Scheme {
	case "api", "https":
		// "api://<service>" service audiences and https resource URIs are both
		// acceptable default audiences.
	case "http":
		// Allow http only for localhost (development), matching the resource
		// indicator validator.
		if u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
			return &ResourceURIError{Detail: "default audience URI must use https or api scheme"}
		}
	default:
		return &ResourceURIError{Detail: "default audience URI must use https or api scheme"}
	}

	return nil
}

// BuildAudience constructs the audience claim for a token.
// If resource is provided, it's included in the audience.
// The clientID is always included in the audience.
func BuildAudience(clientID string, resource string) []string {
	return BuildAudienceWithDefault(clientID, resource, "")
}

// BuildAudienceWithDefault constructs the audience claim for a token, honoring
// a per-app default service audience.
//
// Precedence:
//   - When the client sent a `resource` indicator (resource != ""), that value
//     is appended to the audience and the default is NOT applied — the explicit
//     client resource always wins (RFC 8707 override).
//   - When the client sent NO resource and a defaultResource is configured,
//     the default is appended so downstream validators configured for that
//     service audience accept the token.
//   - When neither is set, the audience is just the clientID (today's
//     behavior; fully backward compatible).
//
// The clientID is always present in the audience. A value already equal to the
// clientID is not duplicated.
func BuildAudienceWithDefault(clientID string, resource string, defaultResource string) []string {
	audience := []string{clientID}
	extra := resource
	if extra == "" {
		extra = defaultResource
	}
	if extra != "" && extra != clientID {
		audience = append(audience, extra)
	}
	return audience
}

// ValidateAudience checks if the token audience contains the expected resource.
func ValidateAudience(audience []string, expectedResource string) bool {
	if expectedResource == "" {
		return true
	}
	for _, aud := range audience {
		if aud == expectedResource {
			return true
		}
	}
	return false
}
