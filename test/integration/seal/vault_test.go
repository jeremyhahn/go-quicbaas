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

//go:build integration && vault

// vault_test.go exercises the HashiCorp Vault Transit barrier sealing
// strategy end-to-end against a Vault dev server. It is compiled only when
// the daemon under test was built with the `vault` tag (so the seal factory
// is present) AND the suite is built with `integration`.
//
// Setup enables the Transit secrets engine (the dev server mounts it on
// demand) and creates the AES-256-GCM Transit key the seal strategy wraps the
// barrier root key under, then boots a daemon whose seal.strategy=vault points
// at the same dev server, and drives the init -> seal -> unseal round-trip.
package seal

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

// Vault dev-server env. The seal compose sets VAULT_ADDR + VAULT_TOKEN to the
// dev server's root token (a dev server runs unsealed with an in-memory
// store, so no unseal ceremony is needed).
const (
	vaultAddrEnv  = "VAULT_ADDR"
	vaultTokenEnv = "VAULT_TOKEN"
)

// transitMountPath / transitKeyName are the mount + key the seal strategy
// resolves. The key CN in the factory's KeyAttributes is the configured
// key_name, so the YAML key_name and the Transit key created here must match.
const (
	transitMountPath = "transit"
	transitKeyName   = "barrier-root"
)

// resolveVault returns (address, token) for the Vault dev server, falling
// back to the compose defaults.
func resolveVault() (address, token string) {
	address = os.Getenv(vaultAddrEnv)
	if address == "" {
		address = "http://vault:8200"
	}
	token = os.Getenv(vaultTokenEnv)
	if token == "" {
		token = "root"
	}
	return address, token
}

// provisionTransitKey enables the Transit engine (idempotently) and creates
// the AES-256-GCM Transit key the seal strategy wraps the root key under.
func provisionTransitKey(t *testing.T, address, token string) {
	t.Helper()

	cfg := vaultapi.DefaultConfig()
	cfg.Address = address
	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("new vault client: %v", err)
	}
	client.SetToken(token)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Enable the Transit secrets engine. A dev server may already have it
	// mounted from a previous test run; an "already in use" error is benign.
	if err := client.Sys().MountWithContext(ctx, transitMountPath, &vaultapi.MountInput{
		Type: "transit",
	}); err != nil && !isVaultAlreadyMounted(err) {
		t.Fatalf("enable transit engine: %v", err)
	}

	// Create the AES-256-GCM Transit key. The go-xkms vault backend's
	// SymmetricEncrypter expects exactly this type (aes256-gcm96).
	keyPath := fmt.Sprintf("%s/keys/%s", transitMountPath, transitKeyName)
	if _, err := client.Logical().WriteWithContext(ctx, keyPath, map[string]interface{}{
		"type": "aes256-gcm96",
	}); err != nil {
		t.Fatalf("create transit key %s: %v", transitKeyName, err)
	}
}

// isVaultAlreadyMounted reports whether err is Vault's "path is already in
// use" response to a re-mount of the Transit engine.
func isVaultAlreadyMounted(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already in use") || strings.Contains(msg, "existing mount")
}

// vaultConfigYAML renders the daemon seal config for the vault strategy.
func vaultConfigYAML(address, token string) string {
	return fmt.Sprintf(`seal:
  enabled: true
  strategy: vault
  vault:
    address: %q
    token: %q
    transit_path: %q
    key_name: %q
    tls_skip_verify: false
`, address, token, transitMountPath, transitKeyName)
}

// TestVaultSealRoundTrip boots a daemon configured with the vault strategy
// pointed at a Vault dev server and asserts the init -> seal -> unseal
// round-trip recovers the barrier root key.
func TestVaultSealRoundTrip(t *testing.T) {
	address, token := resolveVault()
	waitForVault(t, address, token)
	provisionTransitKey(t, address, token)

	cfgYAML := vaultConfigYAML(address, token)
	runCloudSealRoundTrip(t, "vault", cfgYAML)
}

// waitForVault polls the dev server's health endpoint until it reports
// initialized + unsealed, or the deadline fires. Channel-driven readiness
// (no time.Sleep) per CLAUDE.md.
func waitForVault(t *testing.T, address, token string) {
	t.Helper()

	cfg := vaultapi.DefaultConfig()
	cfg.Address = address
	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("new vault client: %v", err)
	}
	client.SetToken(token)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("vault dev server at %s never became ready: %v", address, ctx.Err())
		case <-ticker.C:
			health, herr := client.Sys().HealthWithContext(ctx)
			if herr != nil {
				continue
			}
			if health.Initialized && !health.Sealed {
				return
			}
		}
	}
}
