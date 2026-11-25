// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package cli

import (
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestBarrierStart_ReadsTheTrustDomain pins that the barrier daemon's start
// reads iam.spiffe_trust_domain: on a non-durable engine start runs
// first-boot again, and first-boot refuses to issue the admin certificate
// without a trust domain.
func TestBarrierStart_ReadsTheTrustDomain(t *testing.T) {
	ports := barrierDaemonPorts{REST: 8443, GRPC: 9443, QUIC: 8444, MCP: 8445, Unix: "/run/quicbaas-barrier-test.sock"}
	for _, tc := range []struct {
		name         string
		enforceAuthz bool
	}{
		{name: "insecure-posture", enforceAuthz: false},
		{name: "enforce-authz", enforceAuthz: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := barrierStartArgs(t, t.TempDir(), ports, barrierDaemonOptions{
				Strategy:     "none",
				EnforceAuthz: tc.enforceAuthz,
			})
			i := slices.Index(args, "--config")
			if i < 0 || i+1 >= len(args) || args[i+1] == "" {
				t.Fatalf("start args %v do not pass --config <path>", args)
			}

			// #nosec G304 - the path is rooted at t.TempDir()
			raw, err := os.ReadFile(args[i+1])
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			var cfg struct {
				IAM struct {
					SPIFFETrustDomain string `yaml:"spiffe_trust_domain"`
				} `yaml:"iam"`
				Security map[string]any `yaml:"security"`
			}
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if cfg.IAM.SPIFFETrustDomain != barrierTrustDomain {
				t.Errorf("iam.spiffe_trust_domain = %q, want %q",
					cfg.IAM.SPIFFETrustDomain, barrierTrustDomain)
			}

			if tc.enforceAuthz {
				if len(cfg.Security) != 0 {
					t.Errorf("enforcing daemon's config sets security keys %v; authz must stay enforced", cfg.Security)
				}
				return
			}
			if v, ok := cfg.Security["require_authz"].(bool); !ok || v {
				t.Errorf("security.require_authz = %v, want false", cfg.Security["require_authz"])
			}
			if v, ok := cfg.Security["insecure_dev_mode"].(bool); !ok || !v {
				t.Errorf("security.insecure_dev_mode = %v, want true", cfg.Security["insecure_dev_mode"])
			}
		})
	}
}
