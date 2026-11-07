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

package config_test

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// An EXTERNAL test package for the reason the other sdlc drift tests are one:
// pkg/sdlc reaches pkg/config through pkg/interfaces, so an in-package test
// importing it would be a cycle.

// TestSDLCWebhookPublicURL_TheLoaderAndTheModuleAgree pins the two refusals
// together.
//
// The rule is stated TWICE and has to be: the loader refuses a bad origin at
// startup so one operator reads one message, and sdlc.NewOnboarder refuses it
// again because an embedder that never went through the loader still appends
// the webhook route to whatever it was handed. Two statements of one rule
// drift, and the drift is invisible — the looser side accepts an origin the
// stricter one would reject, the delivery is provisioned, read-back
// verification compares the same malformed string and agrees, and conformance
// calls the repository healthy while nothing arrives.
//
// The table is the ORIGIN SHAPES, and both sides must answer every one the
// same way. It is not a test of either message.
func TestSDLCWebhookPublicURL_TheLoaderAndTheModuleAgree(t *testing.T) {
	for name, tc := range map[string]struct {
		url     string
		wantErr bool
	}{
		"absent":          {"", false},
		"blank":           {"   ", false},
		"relative":        {"/api/v1", true},
		"no host":         {"https://", true},
		"wrong scheme":    {"ftp://factory.example.com", true},
		"bare hostname":   {"factory.example.com", true},
		"query":           {"https://factory.example.com?x=1", true},
		"bare query":      {"https://factory.example.com?", true},
		"fragment":        {"https://factory.example.com#frag", true},
		"userinfo":        {"https://user:pass@factory.example.com", true},
		"https":           {"https://factory.example.com", false},
		"http with port":  {"http://192.168.1.10:8443", false},
		"path is allowed": {"https://factory.example.com/quicbaas", false},
		"trailing slash":  {"https://factory.example.com/", false},
	} {
		t.Run(name, func(t *testing.T) {
			loaderErr := config.ValidateSDLCWebhookPublicURL(tc.url)

			_, moduleErr := sdlc.NormalizeWebhookPublicURL(tc.url)

			if (loaderErr != nil) != tc.wantErr {
				t.Errorf("the loader answered %v for %q, want refused=%v", loaderErr, tc.url, tc.wantErr)
			}
			if (moduleErr != nil) != tc.wantErr {
				t.Errorf("the module answered %v for %q, want refused=%v", moduleErr, tc.url, tc.wantErr)
			}
			if (loaderErr != nil) != (moduleErr != nil) {
				t.Fatalf("the loader and the module disagree about %q: loader=%v module=%v",
					tc.url, loaderErr, moduleErr)
			}
		})
	}
}
