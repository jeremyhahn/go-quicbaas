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
	"errors"
	"strings"
	"testing"
)

// TestIAMDynamicClientRegistrationConfig_DefaultIsDisabled pins the safe
// default: the zero value must never permit registration.
func TestIAMDynamicClientRegistrationConfig_DefaultIsDisabled(t *testing.T) {
	var cfg IAMDynamicClientRegistrationConfig
	if cfg.Enabled {
		t.Fatal("dynamic client registration must default to disabled")
	}
	if cfg.AnonymousTenantID != "" {
		t.Fatal("anonymous registration must default to off")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the zero value must validate: %v", err)
	}
}

// TestIAMDynamicClientRegistrationConfig_Validate covers the accepted and
// rejected combinations.
func TestIAMDynamicClientRegistrationConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     IAMDynamicClientRegistrationConfig
		wantErr bool
	}{
		{"disabled ignores anonymous tenant", IAMDynamicClientRegistrationConfig{AnonymousTenantID: SystemTenantID}, false},
		{"enabled without anonymous tenant", IAMDynamicClientRegistrationConfig{Enabled: true}, false},
		{"enabled with non-system anonymous tenant", IAMDynamicClientRegistrationConfig{Enabled: true, AnonymousTenantID: "public"}, false},
		{"enabled with system anonymous tenant", IAMDynamicClientRegistrationConfig{Enabled: true, AnonymousTenantID: SystemTenantID}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected a validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr {
				return
			}
			var validationErr *ConfigValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error %T is not *ConfigValidationError", err)
			}
			if !strings.Contains(validationErr.Field, "dynamic_client_registration") {
				t.Fatalf("unexpected field %q", validationErr.Field)
			}
		})
	}
}
