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

// dek_tenant_commands.go provides CommandDef builders for the six
// `quicbaas dek tenant` verbs, which drive the caller's tenant DEK keyring.
// Every builder requests JSON output so tests can decode the result, and
// none skips a transport: the node serves the keyring on all of them.
// Negative cases reuse a builder and set ExpectSuccess = false.

package cli

import (
	"strconv"
	"time"
)

// DEKTenantStatusCommand builds `quicbaas dek tenant status --output json`.
func DEKTenantStatusCommand() CommandDef {
	return CommandDef{
		Name:          "dek tenant status",
		Args:          []string{"dek", "tenant", "status", "--output", "json"},
		ExpectSuccess: true,
		Timeout:       30 * time.Second,
	}
}

// DEKTenantRotateCommand builds `quicbaas dek tenant rotate --output json`.
func DEKTenantRotateCommand() CommandDef {
	return CommandDef{
		Name:          "dek tenant rotate",
		Args:          []string{"dek", "tenant", "rotate", "--output", "json"},
		ExpectSuccess: true,
		Timeout:       30 * time.Second,
	}
}

// DEKTenantRetireCommand builds `quicbaas dek tenant retire --version N
// --output json`.
func DEKTenantRetireCommand(version uint64) CommandDef {
	return CommandDef{
		Name:          "dek tenant retire " + strconv.FormatUint(version, 10),
		Args:          []string{"dek", "tenant", "retire", "--version", strconv.FormatUint(version, 10), "--output", "json"},
		ExpectSuccess: true,
		Timeout:       30 * time.Second,
	}
}

// DEKTenantEncryptCommand builds `quicbaas dek tenant encrypt --plaintext P
// --aad A --output json`.
func DEKTenantEncryptCommand(plaintext, aad string) CommandDef {
	return CommandDef{
		Name:          "dek tenant encrypt",
		Args:          []string{"dek", "tenant", "encrypt", "--plaintext", plaintext, "--aad", aad, "--output", "json"},
		ExpectSuccess: true,
		Timeout:       30 * time.Second,
	}
}

// DEKTenantDecryptCommand builds `quicbaas dek tenant decrypt --ciphertext C
// --aad A --output json`.
func DEKTenantDecryptCommand(ciphertext, aad string) CommandDef {
	return CommandDef{
		Name:          "dek tenant decrypt",
		Args:          []string{"dek", "tenant", "decrypt", "--ciphertext", ciphertext, "--aad", aad, "--output", "json"},
		ExpectSuccess: true,
		Timeout:       30 * time.Second,
	}
}

// DEKTenantRewrapCommand builds `quicbaas dek tenant rewrap --ciphertext C
// --aad A --output json`.
func DEKTenantRewrapCommand(ciphertext, aad string) CommandDef {
	return CommandDef{
		Name:          "dek tenant rewrap",
		Args:          []string{"dek", "tenant", "rewrap", "--ciphertext", ciphertext, "--aad", aad, "--output", "json"},
		ExpectSuccess: true,
		Timeout:       30 * time.Second,
	}
}
