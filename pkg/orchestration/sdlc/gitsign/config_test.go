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

package gitsign

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// sessionEnv is a complete helper environment over files in dir.
func sessionEnv(dir string) map[string]string {
	return map[string]string{
		EnvMCPURL:                   "https://quicbaas.internal:8443/mcp",
		orchestration.MCPCertEnvVar: filepath.Join(dir, "session.crt"),
		orchestration.MCPKeyEnvVar:  filepath.Join(dir, "session.key"),
		orchestration.MCPCAEnvVar:   filepath.Join(dir, "ca.crt"),
		EnvKeyName:                  "project-signing",
		EnvKeyScope:                 "tenant",
		EnvKeyOwner:                 "acme",
	}
}

func getenvFrom(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

// writeSessionCertificate writes a self-signed certificate, its key, and the
// same certificate as the CA bundle to the paths env names.
func TestConfigFromEnvReadsTheSession(t *testing.T) {
	env := sessionEnv("/run/session")
	cfg, err := ConfigFromEnv(getenvFrom(env))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.MCPURL != env[EnvMCPURL] || cfg.CertFile != env[orchestration.MCPCertEnvVar] ||
		cfg.KeyFile != env[orchestration.MCPKeyEnvVar] || cfg.CAFile != env[orchestration.MCPCAEnvVar] {
		t.Fatalf("connection settings not read: %+v", cfg)
	}
	if cfg.Key.Name != "project-signing" || cfg.Key.Scope != "tenant" || cfg.Key.Owner != "acme" {
		t.Fatalf("key reference not read: %+v", cfg.Key)
	}
}

func TestConfigFromEnvRefusesIncompleteSessions(t *testing.T) {
	for _, name := range requiredVariables {
		t.Run("missing "+name, func(t *testing.T) {
			env := sessionEnv("/run/session")
			delete(env, name)
			_, err := ConfigFromEnv(getenvFrom(env))
			var invalid *InvalidConfigError
			if !errors.As(err, &invalid) || invalid.Variable != name {
				t.Fatalf("error %v does not name %s", err, name)
			}
		})
	}
	for name, endpoint := range map[string]string{
		"plain http": "http://quicbaas.internal/mcp",
		"no host":    "https:///mcp",
		"unparsable": "https://quic baas/%zz",
	} {
		t.Run(name, func(t *testing.T) {
			env := sessionEnv("/run/session")
			env[EnvMCPURL] = endpoint
			_, err := ConfigFromEnv(getenvFrom(env))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error %v does not unwrap to ErrInvalidConfig", err)
			}
		})
	}
}
