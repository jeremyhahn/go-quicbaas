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

//go:build integration

// Package cli provides shared CLI command definitions for integration tests.
// These definitions ensure all protocols (gRPC, REST, QUIC, Unix, MCP) are tested
// consistently with the same command patterns and expected behaviors.
package cli

import (
	"fmt"
	"strings"
	"time"
)

// CommandDef defines a CLI command to test.
type CommandDef struct {
	// Name is a human-readable name for the test
	Name string

	// Args are the command-line arguments (excluding binary, --address, --transport)
	Args []string

	// Setup is an optional command to run before this command
	Setup *CommandDef

	// Cleanup is an optional command to run after this command
	Cleanup *CommandDef

	// ExpectSuccess indicates if the command should succeed (exit code 0)
	ExpectSuccess bool

	// ExpectOutput contains substrings that should appear in stdout
	ExpectOutput []string

	// ExpectError contains substrings that should appear in stderr (for expected failures)
	ExpectError []string

	// Timeout is the maximum execution time for this command
	Timeout time.Duration

	// SkipTransports lists transports to skip for this command
	SkipTransports []Transport

	// RequiresUnseal indicates the server must be unsealed for this command
	RequiresUnseal bool
}

// UniqueKey generates a unique key for test isolation.
func UniqueKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// =============================================================================
// KMS Key Commands
// =============================================================================

// kmsTransportsToSkip defines which transports to skip for KMS operations.
// KMS operations are supported via gRPC, Unix, and MCP transports.
// REST and QUIC do not have KMS service implementations.
var kmsTransportsToSkip = []Transport{TransportREST, TransportQUIC}

// KMSKeyCreateCommand returns a command to create a KMS key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyCreateCommand(keyName, algorithm string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key generate %s", keyName),
		Args:           []string{"kms", "key", "generate", keyName, "--algorithm", algorithm},
		ExpectSuccess:  true,
		ExpectOutput:   []string{keyName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyGetCommand returns a command to get a KMS key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyGetCommand(keyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key get %s", keyName),
		Args:           []string{"kms", "key", "get", keyName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{keyName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyListCommand returns a command to list KMS keys.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyListCommand() CommandDef {
	return CommandDef{
		Name:           "kms key list",
		Args:           []string{"kms", "key", "list"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyRotateCommand returns a command to rotate a KMS key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyRotateCommand(keyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key rotate %s", keyName),
		Args:           []string{"kms", "key", "rotate", keyName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{keyName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyDeleteCommand returns a command to delete a KMS key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyDeleteCommand(keyName string, force bool) CommandDef {
	args := []string{"kms", "key", "delete", keyName}
	if force {
		args = append(args, "--force")
	}
	return CommandDef{
		Name:           fmt.Sprintf("kms key delete %s", keyName),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{keyName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyEnableCommand returns a command to enable a KMS key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyEnableCommand(keyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key enable %s", keyName),
		Args:           []string{"kms", "key", "enable", keyName},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyDisableCommand returns a command to disable a KMS key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyDisableCommand(keyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key disable %s", keyName),
		Args:           []string{"kms", "key", "disable", keyName},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyVersionsCommand returns a command to list key versions.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyVersionsCommand(keyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key versions %s", keyName),
		Args:           []string{"kms", "key", "versions", keyName},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// =============================================================================
// KMS Encryption Commands
// =============================================================================

// KMSEncryptCommand returns a command to encrypt data.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSEncryptCommand(keyName, plaintext string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms encrypt with %s", keyName),
		Args:           []string{"kms", "encrypt", keyName, "--data", plaintext},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSDecryptCommand returns a command to decrypt data.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSDecryptCommand(keyName, ciphertext string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms decrypt with %s", keyName),
		Args:           []string{"kms", "decrypt", keyName, "--data", ciphertext},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// =============================================================================
// KMS DEK Commands
// =============================================================================

// dekTransportsToSkip defines which transports to skip for DEK operations.
// DEK operations are supported via gRPC, Unix, and MCP transports.
// REST and QUIC do not have DEK service implementations.
var dekTransportsToSkip = []Transport{TransportREST, TransportQUIC}

// KMSDEKCreateCommand returns a command to create a DEK.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKCreateCommand(path, algorithm string) CommandDef {
	// Server normalizes paths by removing trailing slashes
	normalizedPath := strings.TrimSuffix(path, "/")
	return CommandDef{
		Name:           fmt.Sprintf("kms dek generate %s", path),
		Args:           []string{"kms", "dek", "generate", path, "--algorithm", algorithm},
		ExpectSuccess:  true,
		ExpectOutput:   []string{normalizedPath},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKGetCommand returns a command to get a DEK.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKGetCommand(path string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms dek get %s", path),
		Args:           []string{"kms", "dek", "get", path},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKListCommand returns a command to list DEKs.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKListCommand(prefix string) CommandDef {
	args := []string{"kms", "dek", "list"}
	if prefix != "" {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           "kms dek list",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKRotateCommand returns a command to rotate a DEK.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKRotateCommand(path string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms dek rotate %s", path),
		Args:           []string{"kms", "dek", "rotate", path},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKDeleteCommand returns a command to delete a DEK.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKDeleteCommand(path string, force bool) CommandDef {
	args := []string{"kms", "dek", "delete", path}
	if force {
		args = append(args, "--force")
	}
	return CommandDef{
		Name:           fmt.Sprintf("kms dek delete %s", path),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKStatusCommand returns a command to get DEK status.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKStatusCommand(path string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms dek status %s", path),
		Args:           []string{"kms", "dek", "status", path},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKSetDefaultCommand returns a command to set a default DEK.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKSetDefaultCommand(path string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms dek set-default %s", path),
		Args:           []string{"kms", "dek", "set-default", path},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// KMSDEKSetSafetyCommand returns a command to set DEK safety limits.
// DEK operations are supported via gRPC, Unix, and MCP transports.
func KMSDEKSetSafetyCommand(path string, maxOps, maxBytes uint64) CommandDef {
	args := []string{"kms", "dek", "set-safety", path}
	if maxOps > 0 {
		args = append(args, "--max-operations", fmt.Sprintf("%d", maxOps))
	}
	if maxBytes > 0 {
		args = append(args, "--max-bytes", fmt.Sprintf("%d", maxBytes))
	}
	return CommandDef{
		Name:           fmt.Sprintf("kms dek set-safety %s", path),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: dekTransportsToSkip,
	}
}

// =============================================================================
// KMS Key Export/Import/Wrap Commands
// =============================================================================

// KMSKeyImportCommand returns a command to import a key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyImportCommand(keyName, algorithm, material string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key import %s", keyName),
		Args:           []string{"kms", "key", "import", keyName, "--algorithm", algorithm, "--material", material},
		ExpectSuccess:  true,
		ExpectOutput:   []string{keyName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyExportCommand returns a command to export a key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyExportCommand(keyName, wrappingKey, wrappingAlgorithm, format string) CommandDef {
	args := []string{"kms", "key", "export", keyName}
	if wrappingKey != "" {
		args = append(args, "--wrapping-key", wrappingKey)
	}
	if wrappingAlgorithm != "" {
		args = append(args, "--wrapping-algorithm", wrappingAlgorithm)
	}
	if format != "" {
		args = append(args, "--format", format)
	}
	return CommandDef{
		Name:           fmt.Sprintf("kms key export %s", keyName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeyUnwrapCommand returns a command to unwrap a key.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeyUnwrapCommand(keyName, wrappingKey, material, algorithm string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("kms key unwrap %s", keyName),
		Args:           []string{"kms", "key", "unwrap", keyName, "--wrapping-key", wrappingKey, "--material", material, "--algorithm", algorithm},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSKeySetSafetyCommand returns a command to set key safety limits.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSKeySetSafetyCommand(keyName string, maxOps, maxBytes uint64) CommandDef {
	args := []string{"kms", "key", "set-safety", keyName}
	if maxOps > 0 {
		args = append(args, "--max-operations", fmt.Sprintf("%d", maxOps))
	}
	if maxBytes > 0 {
		args = append(args, "--max-bytes", fmt.Sprintf("%d", maxBytes))
	}
	return CommandDef{
		Name:           fmt.Sprintf("kms key set-safety %s", keyName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// =============================================================================
// KMS Backend Commands
// =============================================================================

// KMSBackendStatusCommand returns a command to get backend status.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSBackendStatusCommand() CommandDef {
	return CommandDef{
		Name:           "kms backend status",
		Args:           []string{"kms", "backend", "status"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSBackendTestCommand returns a command to test backend connectivity.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSBackendTestCommand() CommandDef {
	return CommandDef{
		Name:           "kms backend test",
		Args:           []string{"kms", "backend", "test"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// =============================================================================
// KMS Safety Commands
// =============================================================================

// KMSSafetyCheckCommand returns a command to check safety.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSSafetyCheckCommand(threshold int) CommandDef {
	args := []string{"kms", "safety", "check"}
	if threshold > 0 {
		args = append(args, "--threshold", fmt.Sprintf("%d", threshold))
	}
	return CommandDef{
		Name:           "kms safety check",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// KMSSafetyReportCommand returns a command to generate safety report.
// KMS operations are supported via gRPC, Unix, and MCP transports.
func KMSSafetyReportCommand() CommandDef {
	return CommandDef{
		Name:           "kms safety report",
		Args:           []string{"kms", "safety", "report"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: kmsTransportsToSkip,
	}
}

// =============================================================================
// Secrets Commands
// =============================================================================

// secretsTransportsToSkip defines which transports to skip for secrets operations.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
// REST and QUIC do not have secrets service implementations.
var secretsTransportsToSkip = []Transport{TransportREST, TransportQUIC}

// SecretsStoreCommand returns a command to store a secret.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
func SecretsStoreCommand(path, value string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("secrets put %s", path),
		Args:           []string{"secrets", "put", path, "--data", "value=" + value},
		ExpectSuccess:  true,
		ExpectOutput:   []string{path},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: secretsTransportsToSkip,
	}
}

// SecretsGetCommand returns a command to get a secret.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
func SecretsGetCommand(path string, version int) CommandDef {
	args := []string{"secrets", "get", path}
	if version > 0 {
		args = append(args, "--version", fmt.Sprintf("%d", version))
	}
	return CommandDef{
		Name:           fmt.Sprintf("secrets get %s", path),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: secretsTransportsToSkip,
	}
}

// SecretsListCommand returns a command to list secrets.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
func SecretsListCommand(prefix string) CommandDef {
	args := []string{"secrets", "list"}
	if prefix != "" {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           "secrets list",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: secretsTransportsToSkip,
	}
}

// SecretsDeleteCommand returns a command to delete a secret.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
func SecretsDeleteCommand(path string, allVersions bool) CommandDef {
	args := []string{"secrets", "delete", path}
	if allVersions {
		args = append(args, "--all-versions")
	}
	return CommandDef{
		Name:           fmt.Sprintf("secrets delete %s", path),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: secretsTransportsToSkip,
	}
}

// SecretsVersionsCommand returns a command to list secret versions.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
func SecretsVersionsCommand(path string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("secrets versions %s", path),
		Args:           []string{"secrets", "versions", path},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: secretsTransportsToSkip,
	}
}

// SecretsRotateCommand returns a command to rotate a secret with new data.
// This uses the dedicated "secrets rotate" command which creates a new version
// of the secret with the provided data.
// Secrets operations are supported via gRPC, Unix, and MCP transports.
func SecretsRotateCommand(path, newValue string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("secrets rotate %s", path),
		Args:           []string{"secrets", "rotate", path, "--data", "value=" + newValue},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: secretsTransportsToSkip,
	}
}

// =============================================================================
// Data Commands
// =============================================================================

// DataPutCommand returns a command to put data.
// Data operations are supported via gRPC and REST transports.
// MCP transport is not yet implemented for CLI data operations.
func DataPutCommand(entityType, key, value string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("data put %s/%s", entityType, key),
		Args:           []string{"data", "put", entityType, key, value},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: []Transport{TransportMCP},
	}
}

// DataGetCommand returns a command to get data.
// Data operations are supported via gRPC and REST transports.
// MCP transport is not yet implemented for CLI data operations.
func DataGetCommand(entityType, key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("data get %s/%s", entityType, key),
		Args:           []string{"data", "get", entityType, key},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: []Transport{TransportMCP},
	}
}

// DataDeleteCommand returns a command to delete data.
// Includes --yes flag to skip confirmation prompt.
// Data operations are supported via gRPC and REST transports.
// MCP transport is not yet implemented for CLI data operations.
func DataDeleteCommand(entityType, key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("data delete %s/%s", entityType, key),
		Args:           []string{"data", "delete", entityType, key, "--yes"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: []Transport{TransportMCP},
	}
}

// DataListCommand returns a command to list data.
// Data operations are supported via gRPC and REST transports.
// MCP transport is not yet implemented for CLI data operations.
func DataListCommand(entityType string, limit int) CommandDef {
	args := []string{"data", "list", entityType}
	if limit > 0 {
		args = append(args, "--limit", fmt.Sprintf("%d", limit))
	}
	return CommandDef{
		Name:           fmt.Sprintf("data list %s", entityType),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: []Transport{TransportMCP},
	}
}

// =============================================================================
// Cluster Commands
// =============================================================================

// ClusterStatusCommand returns a command to get cluster status.
// Cluster service is fully implemented in all transports (gRPC, REST, Unix, MCP).
func ClusterStatusCommand() CommandDef {
	return CommandDef{
		Name:          "cluster status",
		Args:          []string{"cluster", "status"},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Time Series Commands
// =============================================================================

// tsTransportsToSkip defines which transports to skip for time series operations.
// Time series operations are currently supported via gRPC only.
// REST, MCP, QUIC, and Unix transports do not have time series CLI support implemented yet.
var tsTransportsToSkip = []Transport{TransportQUIC, TransportUnix, TransportREST, TransportMCP}

// TSWriteCommand returns a command to write a single time series data point.
// Time series service is implemented for gRPC and REST transports.
func TSWriteCommand(metric string, value float64, tags map[string]string, timestamp string) CommandDef {
	args := []string{"timeseries", "write", "--metric", metric, "--value", fmt.Sprintf("%f", value)}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	if timestamp != "" {
		args = append(args, "--timestamp", timestamp)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries write %s", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSWriteWithTagsCommand returns a command to write with tags as comma-separated pairs.
func TSWriteWithTagsCommand(metric string, value float64, tagsStr string, timestamp string) CommandDef {
	args := []string{"timeseries", "write", "--metric", metric, "--value", fmt.Sprintf("%f", value)}
	if tagsStr != "" {
		args = append(args, "--tags", tagsStr)
	}
	if timestamp != "" {
		args = append(args, "--timestamp", timestamp)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries write %s with tags", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSWriteBatchCommand returns a command to write multiple time series data points.
func TSWriteBatchCommand(jsonFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries write-batch %s", jsonFile),
		Args:           []string{"timeseries", "write-batch", jsonFile},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSQueryCommand returns a command to query time series data.
func TSQueryCommand(metric, startTime, endTime string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s", metric),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSQueryWithTagsCommand returns a command to query time series data with tag filter.
func TSQueryWithTagsCommand(metric, startTime, endTime, tagsStr string) CommandDef {
	args := []string{
		"timeseries", "query",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
	}
	if tagsStr != "" {
		args = append(args, "--tags", tagsStr)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries query %s with tags", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSQueryWithAggregationCommand returns a command to query with aggregation.
func TSQueryWithAggregationCommand(metric, startTime, endTime, aggregation string, intervalSec int) CommandDef {
	args := []string{
		"timeseries", "query",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
		"--aggregation", aggregation,
	}
	if intervalSec > 0 {
		args = append(args, "--interval", fmt.Sprintf("%d", intervalSec))
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries query %s with %s aggregation", metric, aggregation),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSQueryWithGroupByCommand returns a command to query with group-by.
func TSQueryWithGroupByCommand(metric, startTime, endTime string, groupBy []string) CommandDef {
	args := []string{
		"timeseries", "query",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
	}
	for _, g := range groupBy {
		args = append(args, "--group-by", g)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries query %s with group-by", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSQueryWithLimitCommand returns a command to query with limit.
func TSQueryWithLimitCommand(metric, startTime, endTime string, limit int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s with limit %d", metric, limit),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
			"--limit", fmt.Sprintf("%d", limit),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSDeleteCommand returns a command to delete time series data.
func TSDeleteCommand(metric string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries delete %s", metric),
		Args:           []string{"timeseries", "delete", "--metric", metric},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSDeleteWithTagsCommand returns a command to delete time series data with tags filter.
func TSDeleteWithTagsCommand(metric, tagsStr string) CommandDef {
	args := []string{"timeseries", "delete", "--metric", metric}
	if tagsStr != "" {
		args = append(args, "--tags", tagsStr)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries delete %s with tags", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSDeleteWithTimeRangeCommand returns a command to delete time series data within a time range.
func TSDeleteWithTimeRangeCommand(metric, startTime, endTime string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries delete %s with time range", metric),
		Args: []string{
			"timeseries", "delete",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSGetRetentionCommand returns a command to get retention policy for a metric.
func TSGetRetentionCommand(metric string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries get-retention %s", metric),
		Args:           []string{"timeseries", "get-retention", metric},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSSetRetentionCommand returns a command to set retention policy for a metric.
func TSSetRetentionCommand(metric string, durationSec int64) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries set-retention %s", metric),
		Args: []string{
			"timeseries", "set-retention", metric,
			"--duration", fmt.Sprintf("%d", durationSec),
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSSetRetentionWithResolutionCommand returns a command to set retention policy with resolution.
func TSSetRetentionWithResolutionCommand(metric string, durationSec, resolutionSec int64, aggregation string) CommandDef {
	args := []string{
		"timeseries", "set-retention", metric,
		"--duration", fmt.Sprintf("%d", durationSec),
	}
	if resolutionSec > 0 {
		args = append(args, "--resolution", fmt.Sprintf("%d", resolutionSec))
	}
	if aggregation != "" {
		args = append(args, "--aggregation", aggregation)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries set-retention %s with resolution", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSListMetricsCommand returns a command to list available metrics.
func TSListMetricsCommand() CommandDef {
	return CommandDef{
		Name:           "timeseries list-metrics",
		Args:           []string{"timeseries", "list-metrics"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSListMetricsWithPrefixCommand returns a command to list metrics with a prefix filter.
func TSListMetricsWithPrefixCommand(prefix string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries list-metrics prefix=%s", prefix),
		Args:           []string{"timeseries", "list-metrics", "--prefix", prefix},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TSListMetricsWithLimitCommand returns a command to list metrics with limit and cursor.
func TSListMetricsWithLimitCommand(limit int, cursor string) CommandDef {
	args := []string{"timeseries", "list-metrics", "--limit", fmt.Sprintf("%d", limit)}
	if cursor != "" {
		args = append(args, "--cursor", cursor)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries list-metrics limit=%d", limit),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Time Series Error Commands (for testing error handling)
// =============================================================================

// TSWriteErrorMissingMetricCommand returns a command that should fail due to missing metric.
func TSWriteErrorMissingMetricCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries write error missing metric",
		Args:          []string{"timeseries", "write", "--value", "100.0"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// TSQueryErrorMissingRequiredCommand returns a command that should fail due to missing required flags.
func TSQueryErrorMissingRequiredCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries query error missing required",
		Args:          []string{"timeseries", "query", "--metric", "test.metric"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// TSQueryErrorInvalidTimeFormatCommand returns a command that should fail due to invalid time format.
func TSQueryErrorInvalidTimeFormatCommand() CommandDef {
	return CommandDef{
		Name: "timeseries query error invalid time format",
		Args: []string{
			"timeseries", "query",
			"--metric", "test.metric",
			"--start", "invalid-time",
			"--end", "also-invalid",
		},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// TSSetRetentionErrorMissingDurationCommand returns a command that should fail due to missing duration.
func TSSetRetentionErrorMissingDurationCommand(metric string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("timeseries set-retention %s error missing duration", metric),
		Args:          []string{"timeseries", "set-retention", metric},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// RAG Commands
// =============================================================================

// RAGTransportsToSkip defines which transports to skip for RAG operations.
// RAG uses REST API since it orchestrates multiple services.
// gRPC, QUIC, Unix, and MCP transports do not have RAG CLI support.
var RAGTransportsToSkip = []Transport{TransportGRPC, TransportQUIC, TransportUnix, TransportMCP}

// RAGQueryCommand returns a command to perform a RAG query.
// RAG uses REST API since it orchestrates multiple services.
func RAGQueryCommand(query, namespace string) CommandDef {
	queryPreview := query
	if len(query) > 20 {
		queryPreview = query[:20]
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag query %s", queryPreview),
		Args:           []string{"rag", "query", query, "--namespace", namespace},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGQueryWithOptionsCommand returns a RAG query command with custom options.
func RAGQueryWithOptionsCommand(query, namespace string, topK, maxTokens int, minScore float32, template string, includeCitations bool) CommandDef {
	args := []string{"rag", "query", query, "--namespace", namespace}
	if topK > 0 {
		args = append(args, "--top-k", fmt.Sprintf("%d", topK))
	}
	if maxTokens > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%d", maxTokens))
	}
	if minScore > 0 {
		args = append(args, "--min-score", fmt.Sprintf("%.2f", minScore))
	}
	if template != "" {
		args = append(args, "--template", template)
	}
	if includeCitations {
		args = append(args, "--include-citations")
	}
	queryPreview := query
	if len(query) > 15 {
		queryPreview = query[:15]
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag query with options %s", queryPreview),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGQueryWithModelCommand returns a RAG query command with a specific model.
func RAGQueryWithModelCommand(query, namespace, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag query with model %s", model),
		Args:           []string{"rag", "query", query, "--namespace", namespace, "--model", model},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGQueryWithOutputCommand returns a RAG query command with specific output format.
func RAGQueryWithOutputCommand(query, namespace, output string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag query output=%s", output),
		Args:           []string{"rag", "query", query, "--namespace", namespace, "--output", output},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGContextCommand returns a command to build context from results file.
func RAGContextCommand(resultsFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag context %s", resultsFile),
		Args:           []string{"rag", "context", "--results", resultsFile},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGContextWithOptionsCommand returns a RAG context command with custom options.
func RAGContextWithOptionsCommand(resultsFile string, maxTokens int, template, truncation string, includeCitations bool) CommandDef {
	args := []string{"rag", "context", "--results", resultsFile}
	if maxTokens > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%d", maxTokens))
	}
	if template != "" {
		args = append(args, "--template", template)
	}
	if truncation != "" {
		args = append(args, "--truncation", truncation)
	}
	if includeCitations {
		args = append(args, "--include-citations")
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag context with options %s", resultsFile),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGContextWithOutputCommand returns a RAG context command with specific output format.
func RAGContextWithOutputCommand(resultsFile, output string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag context output=%s", output),
		Args:           []string{"rag", "context", "--results", resultsFile, "--output", output},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGHelpCommand returns a command to show RAG help.
func RAGHelpCommand() CommandDef {
	return CommandDef{
		Name:          "rag help",
		Args:          []string{"rag", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"query", "context"},
		Timeout:       10 * time.Second,
	}
}

// RAGQueryHelpCommand returns a command to show RAG query help.
func RAGQueryHelpCommand() CommandDef {
	return CommandDef{
		Name:          "rag query help",
		Args:          []string{"rag", "query", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"namespace", "top-k", "max-tokens"},
		Timeout:       10 * time.Second,
	}
}

// RAGContextHelpCommand returns a command to show RAG context help.
func RAGContextHelpCommand() CommandDef {
	return CommandDef{
		Name:          "rag context help",
		Args:          []string{"rag", "context", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"results", "max-tokens", "template"},
		Timeout:       10 * time.Second,
	}
}

// RAGQueryMissingNamespaceCommand returns a RAG query command without namespace (should fail).
func RAGQueryMissingNamespaceCommand(query string) CommandDef {
	return CommandDef{
		Name:           "rag query missing namespace",
		Args:           []string{"rag", "query", query},
		ExpectSuccess:  false,
		ExpectError:    []string{"namespace"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGQueryEmptyQueryCommand returns a RAG query command with empty query (should fail).
func RAGQueryEmptyQueryCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           "rag query empty",
		Args:           []string{"rag", "query", "", "--namespace", namespace},
		ExpectSuccess:  false,
		ExpectError:    []string{"empty"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGContextMissingResultsCommand returns a RAG context command without results file (should fail).
func RAGContextMissingResultsCommand() CommandDef {
	return CommandDef{
		Name:           "rag context missing results",
		Args:           []string{"rag", "context"},
		ExpectSuccess:  false,
		ExpectError:    []string{"results"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGContextInvalidFileCommand returns a RAG context command with non-existent file (should fail).
func RAGContextInvalidFileCommand() CommandDef {
	return CommandDef{
		Name:           "rag context invalid file",
		Args:           []string{"rag", "context", "--results", "/nonexistent/path/results.json"},
		ExpectSuccess:  false,
		ExpectError:    []string{"no such file"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// =============================================================================
// Messaging Commands
// =============================================================================

// msgTransportsToSkip defines which transports to skip for messaging operations.
// Messaging operations are currently only supported via gRPC transport.
// REST, MCP, and QUIC transports are not yet implemented.
var msgTransportsToSkip = []Transport{TransportREST, TransportMCP, TransportQUIC}

// MsgChannelCreateCommand returns a command to create a messaging channel.
// Messaging operations are currently only supported via gRPC transport.
func MsgChannelCreateCommand(channelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg create-channel %s", channelName),
		Args:           []string{"msg", "create-channel", channelName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{channelName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgChannelCreateWithOptionsCommand returns a command to create a channel with options.
// Messaging operations are currently only supported via gRPC transport.
func MsgChannelCreateWithOptionsCommand(channelName string, bufferSize, maxSubscribers int32, persistent bool, ttl string) CommandDef {
	args := []string{"msg", "create-channel", channelName}
	if bufferSize > 0 {
		args = append(args, "--buffer-size", fmt.Sprintf("%d", bufferSize))
	}
	if maxSubscribers > 0 {
		args = append(args, "--max-subscribers", fmt.Sprintf("%d", maxSubscribers))
	}
	if persistent {
		args = append(args, "--persistent")
	}
	if ttl != "" {
		args = append(args, "--ttl", ttl)
	}
	return CommandDef{
		Name:           fmt.Sprintf("msg create-channel %s with options", channelName),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{channelName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgChannelListCommand returns a command to list messaging channels.
// Messaging operations are currently only supported via gRPC transport.
func MsgChannelListCommand() CommandDef {
	return CommandDef{
		Name:           "msg list-channels",
		Args:           []string{"msg", "list-channels"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgChannelListWithFilterCommand returns a command to list channels with filter.
// Messaging operations are currently only supported via gRPC transport.
func MsgChannelListWithFilterCommand(filter string, limit, offset int32) CommandDef {
	args := []string{"msg", "list-channels"}
	if filter != "" {
		args = append(args, "--filter", filter)
	}
	if limit > 0 {
		args = append(args, "--limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		args = append(args, "--offset", fmt.Sprintf("%d", offset))
	}
	return CommandDef{
		Name:           "msg list-channels with filter",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgChannelGetCommand returns a command to get channel information.
// Messaging operations are currently only supported via gRPC transport.
func MsgChannelGetCommand(channelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg channel-info %s", channelName),
		Args:           []string{"msg", "channel-info", channelName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{channelName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgChannelDeleteCommand returns a command to delete a messaging channel.
// Messaging operations are currently only supported via gRPC transport.
func MsgChannelDeleteCommand(channelName string, force bool) CommandDef {
	args := []string{"msg", "delete-channel", channelName}
	if force {
		args = append(args, "--force")
	}
	return CommandDef{
		Name:           fmt.Sprintf("msg delete-channel %s", channelName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgPublishCommand returns a command to publish a message to a channel.
// Messaging operations are currently only supported via gRPC transport.
func MsgPublishCommand(channelName, payload string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg publish to %s", channelName),
		Args:           []string{"msg", "publish", channelName, "--payload", payload},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgPublishWithOptionsCommand returns a command to publish with options.
// Messaging operations are currently only supported via gRPC transport.
func MsgPublishWithOptionsCommand(channelName, payload, dedupID, priority, ttl string) CommandDef {
	args := []string{"msg", "publish", channelName, "--payload", payload}
	if dedupID != "" {
		args = append(args, "--dedup-id", dedupID)
	}
	if priority != "" {
		args = append(args, "--priority", priority)
	}
	if ttl != "" {
		args = append(args, "--ttl", ttl)
	}
	return CommandDef{
		Name:           fmt.Sprintf("msg publish to %s with options", channelName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgPublishDeltaCommand returns a command to publish a delta message.
// Messaging operations are currently only supported via gRPC transport.
func MsgPublishDeltaCommand(channelName, oldValue, newValue string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg publish-delta to %s", channelName),
		Args:           []string{"msg", "publish-delta", channelName, "--old", oldValue, "--new", newValue},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgPublishDeltaWithEntityCommand returns a command to publish a delta with entity info.
// Messaging operations are currently only supported via gRPC transport.
func MsgPublishDeltaWithEntityCommand(channelName, oldValue, newValue, entityID, entityType string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("msg publish-delta to %s with entity", channelName),
		Args: []string{
			"msg", "publish-delta", channelName,
			"--old", oldValue,
			"--new", newValue,
			"--entity-id", entityID,
			"--entity-type", entityType,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgListSubscriptionsCommand returns a command to list subscriptions.
// Messaging operations are currently only supported via gRPC transport.
func MsgListSubscriptionsCommand() CommandDef {
	return CommandDef{
		Name:           "msg list-subscriptions",
		Args:           []string{"msg", "list-subscriptions"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgListSubscriptionsByChannelCommand returns a command to list subscriptions for a channel.
// Messaging operations are currently only supported via gRPC transport.
func MsgListSubscriptionsByChannelCommand(channelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg list-subscriptions for %s", channelName),
		Args:           []string{"msg", "list-subscriptions", "--channel", channelName},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgDedupStatsCommand returns a command to get deduplication statistics.
// Messaging operations are currently only supported via gRPC transport.
func MsgDedupStatsCommand(channelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg dedup-stats %s", channelName),
		Args:           []string{"msg", "dedup-stats", channelName},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgDedupStatsWithOptionsCommand returns a command to get dedup stats with options.
// Messaging operations are currently only supported via gRPC transport.
func MsgDedupStatsWithOptionsCommand(channelName string, reset bool, window string) CommandDef {
	args := []string{"msg", "dedup-stats", channelName}
	if reset {
		args = append(args, "--reset")
	}
	if window != "" {
		args = append(args, "--window", window)
	}
	return CommandDef{
		Name:           fmt.Sprintf("msg dedup-stats %s with options", channelName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgHelpCommand returns a command to show messaging help.
func MsgHelpCommand() CommandDef {
	return CommandDef{
		Name:          "msg help",
		Args:          []string{"msg", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"create-channel", "publish", "subscribe"},
		Timeout:       10 * time.Second,
	}
}

// MsgChannelCreateMissingNameCommand returns a command that should fail due to missing channel name.
func MsgChannelCreateMissingNameCommand() CommandDef {
	return CommandDef{
		Name:           "msg create-channel missing name",
		Args:           []string{"msg", "create-channel"},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgPublishMissingPayloadCommand returns a command that should fail due to missing payload.
func MsgPublishMissingPayloadCommand(channelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg publish to %s missing payload", channelName),
		Args:           []string{"msg", "publish", channelName},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: msgTransportsToSkip,
	}
}

// MsgPublishDeltaMissingValuesCommand returns a command that should fail due to missing old/new values.
func MsgPublishDeltaMissingValuesCommand(channelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("msg publish-delta to %s missing values", channelName),
		Args:           []string{"msg", "publish-delta", channelName},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: msgTransportsToSkip,
	}
}

// =============================================================================
// Test Suites
// =============================================================================

// KMSKeySuite returns all KMS key commands for testing a complete key lifecycle.
func KMSKeySuite(keyName string) []CommandDef {
	return []CommandDef{
		KMSKeyCreateCommand(keyName, "AES-256-GCM"),
		KMSKeyGetCommand(keyName),
		KMSKeyListCommand(),
		KMSKeyVersionsCommand(keyName),
		KMSKeyRotateCommand(keyName),
		KMSKeyDisableCommand(keyName),
		KMSKeyEnableCommand(keyName),
		KMSKeyDeleteCommand(keyName, true),
	}
}

// KMSDEKSuite returns all DEK commands for testing a complete DEK lifecycle.
func KMSDEKSuite(dekPath string) []CommandDef {
	return []CommandDef{
		KMSDEKCreateCommand(dekPath, "AES-256-GCM"),
		KMSDEKGetCommand(dekPath),
		KMSDEKListCommand(""),
		KMSDEKStatusCommand(dekPath),
		KMSDEKRotateCommand(dekPath),
		KMSDEKSetDefaultCommand(dekPath),
		KMSDEKDeleteCommand(dekPath, true),
	}
}

// SecretsSuite returns all secrets commands for testing a complete secrets lifecycle.
func SecretsSuite(secretPath, secretValue string) []CommandDef {
	return []CommandDef{
		SecretsStoreCommand(secretPath, secretValue),
		SecretsGetCommand(secretPath, 0),
		SecretsListCommand(""),
		SecretsVersionsCommand(secretPath),
		SecretsRotateCommand(secretPath, secretValue+"-rotated"),
		SecretsGetCommand(secretPath, 0),
		SecretsDeleteCommand(secretPath, true),
	}
}

// DataSuite returns all data commands for testing basic CRUD operations.
func DataSuite(entityType, key, value string) []CommandDef {
	return []CommandDef{
		DataPutCommand(entityType, key, value),
		DataGetCommand(entityType, key),
		DataListCommand(entityType, 10),
		DataDeleteCommand(entityType, key),
	}
}

// TimeSeriesSuite returns commands for testing a complete time series lifecycle.
// This includes write, query, retention policy, list metrics, and delete operations.
func TimeSeriesSuite(metric string, startTime, endTime string) []CommandDef {
	return []CommandDef{
		// Write data points
		TSWriteWithTagsCommand(metric, 75.5, "host=server1,region=us-east-1", ""),
		TSWriteWithTagsCommand(metric, 82.3, "host=server2,region=us-east-1", ""),
		// List metrics to verify
		TSListMetricsCommand(),
		TSListMetricsWithPrefixCommand(strings.Split(metric, ".")[0]),
		// Query data
		TSQueryCommand(metric, startTime, endTime),
		TSQueryWithTagsCommand(metric, startTime, endTime, "host=server1"),
		TSQueryWithLimitCommand(metric, startTime, endTime, 10),
		// Set and get retention policy
		TSSetRetentionCommand(metric, 2592000), // 30 days
		TSGetRetentionCommand(metric),
		// Delete data
		TSDeleteCommand(metric),
	}
}

// RAGHelpSuite returns help commands for RAG.
func RAGHelpSuite() []CommandDef {
	return []CommandDef{
		RAGHelpCommand(),
		RAGQueryHelpCommand(),
		RAGContextHelpCommand(),
	}
}

// RAGErrorSuite returns error case commands for RAG.
func RAGErrorSuite() []CommandDef {
	return []CommandDef{
		RAGQueryMissingNamespaceCommand("test query"),
		RAGContextMissingResultsCommand(),
		RAGContextInvalidFileCommand(),
	}
}

// MsgChannelSuite returns commands for testing a complete messaging channel lifecycle.
func MsgChannelSuite(channelName string) []CommandDef {
	return []CommandDef{
		MsgChannelCreateCommand(channelName),
		MsgChannelGetCommand(channelName),
		MsgChannelListCommand(),
		MsgDedupStatsCommand(channelName),
		MsgChannelDeleteCommand(channelName, true),
	}
}

// MsgHelpSuite returns help commands for messaging.
func MsgHelpSuite() []CommandDef {
	return []CommandDef{
		MsgHelpCommand(),
	}
}

// MsgErrorSuite returns error case commands for messaging.
func MsgErrorSuite(channelName string) []CommandDef {
	return []CommandDef{
		MsgChannelCreateMissingNameCommand(),
		MsgPublishMissingPayloadCommand(channelName),
		MsgPublishDeltaMissingValuesCommand(channelName),
	}
}

// =============================================================================
// Vector Transport Configuration
// =============================================================================

// vectorTransportsToSkip defines which transports to skip for vector operations.
// Vector operations are supported via gRPC, Unix, and MCP transports.
// REST transport does not properly implement vector service interface methods -
// the REST handlers use type assertions that fail because QuicBaaS interface
// doesn't directly expose vector methods (they're accessed via Vector() accessor).
// QUIC transport does not have vector support implemented.
var vectorTransportsToSkip = []Transport{TransportREST, TransportQUIC}

// =============================================================================
// Embedding Transport Configuration
// =============================================================================

// embeddingTransportsToSkip defines which transports to skip for embedding operations.
// The embedding manager is now wired into the server lifecycle via startEmbedding()
// and connected to the service builder via WithEmbeddingManager().
var embeddingTransportsToSkip []Transport
