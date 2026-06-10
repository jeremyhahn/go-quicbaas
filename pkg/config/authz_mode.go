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

package config

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// DefaultAuthzMode is the deployment-level default authorization mode used when
// security.authz_mode is unset. It matches the platform validator's historical
// behavior (offline scope decision + cached online liveness).
const DefaultAuthzMode = string(platformtoken.AuthzModeHybrid)

// DefaultLivenessCacheTTL bounds how long a positive hybrid-mode liveness result
// is cached before the next request live-checks again. It is the deployment-level
// default used when security.liveness_cache_ttl is unset (zero).
const DefaultLivenessCacheTTL = 15 * time.Second

// InvalidAuthzModeError is returned when an authz_mode string does not name one
// of the supported platform authorization modes.
type InvalidAuthzModeError struct {
	// Value is the offending authz_mode string.
	Value string
}

// Error implements the error interface.
func (e *InvalidAuthzModeError) Error() string {
	return "config: security.authz_mode: must be one of offline, online, hybrid; got " + e.Value
}

// ParseAuthzMode validates an authz_mode string and maps it to the SDK
// platformtoken.AuthzMode. An empty string resolves to the deployment default
// (hybrid). Any other unrecognized value returns an *InvalidAuthzModeError.
func ParseAuthzMode(s string) (platformtoken.AuthzMode, error) {
	switch s {
	case "":
		return platformtoken.AuthzMode(DefaultAuthzMode), nil
	case string(platformtoken.AuthzModeOffline):
		return platformtoken.AuthzModeOffline, nil
	case string(platformtoken.AuthzModeOnline):
		return platformtoken.AuthzModeOnline, nil
	case string(platformtoken.AuthzModeHybrid):
		return platformtoken.AuthzModeHybrid, nil
	default:
		return "", &InvalidAuthzModeError{Value: s}
	}
}

// EffectiveAuthzMode returns the deployment-level authorization mode parsed from
// AuthzMode, defaulting to hybrid when unset. It returns an
// *InvalidAuthzModeError when AuthzMode names an unsupported mode.
func (c *SecurityConfig) EffectiveAuthzMode() (platformtoken.AuthzMode, error) {
	return ParseAuthzMode(c.AuthzMode)
}

// EffectiveLivenessCacheTTL returns the configured liveness cache TTL, defaulting
// to DefaultLivenessCacheTTL when unset (zero or negative).
func (c *SecurityConfig) EffectiveLivenessCacheTTL() time.Duration {
	if c.LivenessCacheTTL <= 0 {
		return DefaultLivenessCacheTTL
	}
	return c.LivenessCacheTTL
}

// ValidateAuthzMode returns an *InvalidAuthzModeError when AuthzMode is set to an
// unsupported value. An empty AuthzMode is valid and resolves to the default.
func (c *SecurityConfig) ValidateAuthzMode() error {
	_, err := ParseAuthzMode(c.AuthzMode)
	return err
}
