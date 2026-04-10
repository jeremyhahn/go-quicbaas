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

// BuildAudience constructs the audience claim for a token.
// If resource is provided, it's included in the audience.
// The clientID is always included in the audience.
func BuildAudience(clientID string, resource string) []string {
	audience := []string{clientID}
	if resource != "" && resource != clientID {
		audience = append(audience, resource)
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
