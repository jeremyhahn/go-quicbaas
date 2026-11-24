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

//go:build integration

// Package cli — dynamic-secrets CLI verb command factories for the
// credential generator, which mints secret material into go-qrdb's
// credential store. Commands whose stdout carries a secret value set
// RedactStdout so the value never reaches the test log.
package cli

import (
	"fmt"
	"time"
)

// dynamicSecretsTimeout bounds every dynamic-secrets command.
const dynamicSecretsTimeout = 15 * time.Second

// DynamicSecretsRoleCreateCredentialCommand creates a role backed by the
// credential generator. Each config entry is passed as --config key=value.
func DynamicSecretsRoleCreateCredentialCommand(role string, config ...string) CommandDef {
	args := []string{
		"dynamic-secrets", "role", "create", role,
		"--generator-type", "credential",
		"--default-ttl", "10m",
		"--max-ttl", "1h",
	}
	for _, kv := range config {
		args = append(args, "--config", kv)
	}
	return CommandDef{
		Name:          fmt.Sprintf("dynamic-secrets role create %s", role),
		Args:          args,
		ExpectSuccess: true,
		Timeout:       dynamicSecretsTimeout,
	}
}

// DynamicSecretsGenerateJSONCommand generates a credential from role and
// prints the whole response as JSON. The output carries the secret value.
func DynamicSecretsGenerateJSONCommand(role string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("dynamic-secrets generate %s", role),
		Args:          []string{"dynamic-secrets", "generate", role, "--output", "json"},
		ExpectSuccess: true,
		RedactStdout:  true,
		Timeout:       dynamicSecretsTimeout,
	}
}

// DynamicSecretsLeaseListCommand lists the caller's leases as JSON.
func DynamicSecretsLeaseListCommand() CommandDef {
	return CommandDef{
		Name:          "dynamic-secrets lease list",
		Args:          []string{"dynamic-secrets", "lease", "list", "--output", "json"},
		ExpectSuccess: true,
		Timeout:       dynamicSecretsTimeout,
	}
}

// DynamicSecretsLeaseRevokeCommand revokes a lease, which deletes its
// credential's material from the store.
func DynamicSecretsLeaseRevokeCommand(leaseID string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("dynamic-secrets lease revoke %s", leaseID),
		Args:          []string{"dynamic-secrets", "lease", "revoke", leaseID},
		ExpectSuccess: true,
		Timeout:       dynamicSecretsTimeout,
	}
}

// DynamicSecretsRoleDeleteCommand deletes a role.
func DynamicSecretsRoleDeleteCommand(role string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("dynamic-secrets role delete %s", role),
		Args:          []string{"dynamic-secrets", "role", "delete", role},
		ExpectSuccess: true,
		Timeout:       dynamicSecretsTimeout,
	}
}

// CredentialRetrieveRedactedCommand reads a credential from the system
// credential service without logging its value. ExpectSuccess is false: the
// caller checks the outcome itself.
func CredentialRetrieveRedactedCommand(name string) CommandDef {
	return CommandDef{
		Name:         fmt.Sprintf("credential retrieve %s", name),
		Args:         []string{"credential", "retrieve", name},
		RedactStdout: true,
		Timeout:      dynamicSecretsTimeout,
	}
}
