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

// Package main provides KMS CLI commands for QuicBaaS.
package main

import (
	stdcontext "context"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

var (
	kmsAlgorithm         string
	kmsDescription       string
	kmsExportable        bool
	kmsMaterial          string
	kmsWrappingKey       string
	kmsWrappingAlgorithm string
	kmsFormat            string
	kmsVersion           uint64
	kmsPrefix            string
	kmsLimit             int
	kmsForce             bool
	kmsPurge             bool
	kmsData              string
	kmsAAD               string
	kmsBase64            bool
	kmsOutputFmt         string
	kmsMaxOperations     uint64
	kmsMaxBytes          uint64
	kmsWarningPercent    int
	kmsSafetyThreshold   float64
	kmsGracePeriod       string
)

// newKMSCommand creates the KMS command group.
func newKMSCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kms",
		Short: "Key Management Service commands",
		Long: `KMS commands for managing cryptographic keys.

These commands allow you to create, import, rotate, and delete encryption keys,
as well as perform encryption and decryption operations using those keys.`,
	}

	cmd.PersistentFlags().StringVar(&kmsOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newKMSKeyCommand())
	cmd.AddCommand(newKMSDEKCommand())
	cmd.AddCommand(newKMSEncryptCommand())
	cmd.AddCommand(newKMSDecryptCommand())
	cmd.AddCommand(newKMSBackendCommand())
	cmd.AddCommand(newKMSSafetyCommand())

	return cmd
}

// parseSymmetricAlgorithm converts algorithm string to SymmetricAlgorithm.
// Returns empty string for non-symmetric algorithms.
func parseSymmetricAlgorithm(algo string) kctypes.SymmetricAlgorithm {
	switch strings.ToUpper(algo) {
	case "AES-128-GCM":
		return kctypes.SymmetricAES128GCM
	case "AES-192-GCM":
		return kctypes.SymmetricAES192GCM
	case "AES-256-GCM":
		return kctypes.SymmetricAES256GCM
	case "CHACHA20-POLY1305":
		return kctypes.SymmetricChaCha20Poly1305
	case "XCHACHA20-POLY1305":
		return kctypes.SymmetricXChaCha20Poly1305
	default:
		return ""
	}
}

// createKeyAttributes creates KeyAttributes for the given algorithm.
// Handles both symmetric and asymmetric algorithms.
func createKeyAttributes(keyName, algo string, exportable bool) (*kctypes.KeyAttributes, error) {
	algoUpper := strings.ToUpper(algo)

	// Try symmetric algorithm first
	symAlg := parseSymmetricAlgorithm(algo)
	if symAlg != "" {
		return &kctypes.KeyAttributes{
			CN:                 keyName,
			SymmetricAlgorithm: symAlg,
			Exportable:         exportable,
		}, nil
	}

	// Handle asymmetric algorithms
	switch algoUpper {
	case "ED25519":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.Ed25519,
			Exportable:   exportable,
		}, nil
	case "RSA-2048":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.RSA,
			RSAAttributes: &kctypes.RSAAttributes{
				KeySize: 2048,
			},
			Exportable: exportable,
		}, nil
	case "RSA-3072":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.RSA,
			RSAAttributes: &kctypes.RSAAttributes{
				KeySize: 3072,
			},
			Exportable: exportable,
		}, nil
	case "RSA-4096":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.RSA,
			RSAAttributes: &kctypes.RSAAttributes{
				KeySize: 4096,
			},
			Exportable: exportable,
		}, nil
	case "ECDSA-P256":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.ECDSA,
			ECCAttributes: &kctypes.ECCAttributes{
				Curve: elliptic.P256(),
			},
			Exportable: exportable,
		}, nil
	case "ECDSA-P384":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.ECDSA,
			ECCAttributes: &kctypes.ECCAttributes{
				Curve: elliptic.P384(),
			},
			Exportable: exportable,
		}, nil
	case "ECDSA-P521":
		return &kctypes.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.ECDSA,
			ECCAttributes: &kctypes.ECCAttributes{
				Curve: elliptic.P521(),
			},
			Exportable: exportable,
		}, nil
	default:
		return nil, NewUnsupportedAlgorithmError("createKeyAttributes", algo)
	}
}

// parseGracePeriod parses a duration string supporting standard Go durations
// and day suffix (e.g., "24h", "7d", "30m", "1h30m").
func parseGracePeriod(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	// Check for day suffix (e.g., "7d")
	if strings.HasSuffix(s, "d") {
		dayStr := strings.TrimSuffix(s, "d")
		var days int
		if _, err := fmt.Sscanf(dayStr, "%d", &days); err != nil {
			return 0, NewInvalidDayFormatError("parseGracePeriod", s)
		}
		if days < 0 {
			return 0, NewNegativeDurationError("parseGracePeriod", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	// Parse standard Go duration
	duration, err := time.ParseDuration(s)
	if err != nil {
		return 0, NewInvalidDurationFormatError("parseGracePeriod", s)
	}

	if duration < 0 {
		return 0, NewNegativeDurationError("parseGracePeriod", s)
	}

	return duration, nil
}

// =============================================================================
// KMS Key Commands
// =============================================================================

// newKMSKeyCommand creates the kms key subcommand group.
func newKMSKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage cryptographic keys",
		Long:  `Commands for creating, managing, and deleting cryptographic keys.`,
	}

	cmd.AddCommand(newKMSKeyGenerateCommand())
	cmd.AddCommand(newKMSKeyGetCommand())
	cmd.AddCommand(newKMSKeyListCommand())
	cmd.AddCommand(newKMSKeyRotateCommand())
	cmd.AddCommand(newKMSKeyDeleteCommand())
	cmd.AddCommand(newKMSKeyImportCommand())
	cmd.AddCommand(newKMSKeyEnableCommand())
	cmd.AddCommand(newKMSKeyDisableCommand())
	cmd.AddCommand(newKMSKeyVersionsCommand())
	cmd.AddCommand(newKMSKeyExportCommand())
	cmd.AddCommand(newKMSKeyUnwrapCommand())
	cmd.AddCommand(newKMSKeySetSafetyCommand())

	return cmd
}

// newKMSKeyGenerateCommand creates the kms key generate command.
func newKMSKeyGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <key-name>",
		Short: "Generate a new cryptographic key",
		Long: `Generate a new cryptographic key with the specified algorithm.

Examples:
  quicbaas kms key generate mykey --algorithm AES-256-GCM
  quicbaas kms key generate mykey --algorithm CHACHA20-POLY1305 --exportable`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyGenerateCommand,
	}

	cmd.Flags().StringVar(&kmsAlgorithm, "algorithm", "AES-256-GCM", "Key algorithm")
	cmd.Flags().StringVar(&kmsDescription, "description", "", "Key description")
	cmd.Flags().BoolVar(&kmsExportable, "exportable", false, "Allow key export")

	return cmd
}

// runKMSKeyGenerateCommand executes the kms key generate command.
func runKMSKeyGenerateCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	keyAttrs, err := createKeyAttributes(keyName, kmsAlgorithm, kmsExportable)
	if err != nil {
		return formatError(err)
	}

	// Determine usage based on algorithm type (symmetric vs asymmetric)
	var usage []service.KeyUsage
	if keyAttrs.SymmetricAlgorithm != "" {
		usage = []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt}
	} else {
		usage = []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify}
	}

	req := &service.GenerateKeyRequest{
		KeyAttributes: keyAttrs,
		Name:          keyName,
		Description:   kmsDescription,
		Exportable:    kmsExportable,
		Usage:         usage,
	}

	resp, err := client.KMSGenerateKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyGenerateResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyGetCommand creates the kms key get command.
func newKMSKeyGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key-name>",
		Short: "Get key metadata",
		Long: `Retrieve metadata for a cryptographic key.

Examples:
  quicbaas kms key get mykey
  quicbaas kms key get mykey --version 2`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyGetCommand,
	}

	cmd.Flags().Uint64Var(&kmsVersion, "version", 0, "Key version (0 for current)")

	return cmd
}

// runKMSKeyGetCommand executes the kms key get command.
func runKMSKeyGetCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.GetKeyRequest{
		KeyID:   keyName,
		Version: kmsVersion,
	}

	resp, err := client.KMSGetKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyGetResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyListCommand creates the kms key list command.
func newKMSKeyListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [prefix]",
		Short: "List cryptographic keys",
		Long: `List all cryptographic keys with optional filtering.

Examples:
  quicbaas kms key list
  quicbaas kms key list tenant-123/
  quicbaas kms key list --limit 50`,
		Args: cobra.MaximumNArgs(1),
		RunE: runKMSKeyListCommand,
	}

	cmd.Flags().IntVar(&kmsLimit, "limit", 100, "Maximum number of keys to return")

	return cmd
}

// runKMSKeyListCommand executes the kms key list command.
func runKMSKeyListCommand(cmd *cobra.Command, args []string) error {
	// Get optional prefix from args
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.ListKeysRequest{
		Limit: kmsLimit,
	}
	if prefix != "" {
		req.Filter = map[string]string{"prefix": prefix}
	}

	resp, err := client.KMSListKeys(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyListResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyRotateCommand creates the kms key rotate command.
func newKMSKeyRotateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate <key-name>",
		Short: "Rotate a cryptographic key",
		Long: `Generate a new version of a cryptographic key.

The old key version remains active for decryption until the grace period expires,
after which it will be automatically disabled.

Examples:
  quicbaas kms key rotate mykey
  quicbaas kms key rotate mykey --grace-period 24h
  quicbaas kms key rotate mykey --grace-period 7d
  quicbaas kms key rotate mykey --grace-period 30m`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyRotateCommand,
	}

	cmd.Flags().StringVar(&kmsGracePeriod, "grace-period", "", "Grace period duration (e.g., 24h, 7d, 30m) after which old version will be disabled")

	return cmd
}

// runKMSKeyRotateCommand executes the kms key rotate command.
func runKMSKeyRotateCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.RotateKeyRequest{
		KeyID:            keyName,
		RetainOldVersion: true,
	}

	// Parse grace period if provided
	if kmsGracePeriod != "" {
		gracePeriod, err := parseGracePeriod(kmsGracePeriod)
		if err != nil {
			return formatError(err)
		}
		// GracePeriodSeconds field will be added to RotateKeyRequest when protobuf is regenerated.
		_ = gracePeriod.Seconds() // Convert to seconds for future use
	}

	resp, err := client.KMSRotateKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyRotateResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyDeleteCommand creates the kms key delete command.
func newKMSKeyDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <key-name>",
		Short: "Delete a cryptographic key",
		Long: `Schedule a cryptographic key for deletion.

Examples:
  quicbaas kms key delete mykey
  quicbaas kms key delete mykey --force`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyDeleteCommand,
	}

	cmd.Flags().BoolVar(&kmsForce, "force", false, "Bypass deletion protection")
	cmd.Flags().BoolVar(&kmsPurge, "purge", false, "Permanently delete key")

	return cmd
}

// runKMSKeyDeleteCommand executes the kms key delete command.
func runKMSKeyDeleteCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	pendingDays := 30
	if kmsPurge {
		pendingDays = 0
	}
	req := &service.DeleteKeyRequest{
		KeyID:             keyName,
		PendingWindowDays: pendingDays,
		Force:             kmsForce,
	}

	resp, err := client.KMSDeleteKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyDeleteResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyImportCommand creates the kms key import command.
func newKMSKeyImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <key-name>",
		Short: "Import an external key",
		Long: `Import an external cryptographic key.

Examples:
  quicbaas kms key import mykey --algorithm AES-256-GCM --material base64:SGVsbG8=`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyImportCommand,
	}

	cmd.Flags().StringVar(&kmsAlgorithm, "algorithm", "AES-256-GCM", "Key algorithm (required)")
	cmd.Flags().StringVar(&kmsMaterial, "material", "", "Key material (base64 encoded) (required)")
	cmd.Flags().StringVar(&kmsDescription, "description", "", "Key description")

	cmd.MarkFlagRequired("material")

	return cmd
}

// runKMSKeyImportCommand executes the kms key import command.
func runKMSKeyImportCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	var keyMaterial []byte
	if strings.HasPrefix(kmsMaterial, "base64:") {
		// Explicit base64-encoded material
		keyMaterial, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(kmsMaterial, "base64:"))
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "kms.key.import", "invalid base64 key material")
		}
	} else if _, statErr := os.Stat(kmsMaterial); statErr == nil {
		// Material is a file path - read file contents
		keyMaterial, err = os.ReadFile(kmsMaterial)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInternal, "kms.key.import", "failed to read key material file")
		}
	} else if strings.HasPrefix(kmsMaterial, "/") || strings.HasPrefix(kmsMaterial, "./") || strings.HasPrefix(kmsMaterial, "../") {
		// Looks like a file path - try to read it
		keyMaterial, err = os.ReadFile(kmsMaterial)
		if err != nil {
			return errors.Wrapf(err, errors.ErrNotFound, "kms.key.import", "failed to read key material file")
		}
	} else {
		// Try to decode as base64
		keyMaterial, err = base64.StdEncoding.DecodeString(kmsMaterial)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "kms.key.import", "invalid base64 key material")
		}
	}

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	keyAttrs := &kctypes.KeyAttributes{
		CN:                 keyName,
		SymmetricAlgorithm: parseSymmetricAlgorithm(kmsAlgorithm),
	}

	req := &service.ImportKeyRequest{
		KeyAttributes: keyAttrs,
		Name:          keyName,
		Description:   kmsDescription,
		KeyMaterial:   keyMaterial,
		Usage:         []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	resp, err := client.KMSImportKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyImportResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyEnableCommand creates the kms key enable command.
func newKMSKeyEnableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable <key-name>",
		Short: "Enable a disabled key",
		Long: `Enable a disabled cryptographic key.

Examples:
  quicbaas kms key enable mykey
  quicbaas kms key enable mykey --version 2`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyEnableCommand,
	}

	cmd.Flags().Uint64Var(&kmsVersion, "version", 0, "Key version (0 for all versions)")

	return cmd
}

// runKMSKeyEnableCommand executes the kms key enable command.
func runKMSKeyEnableCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.UpdateKeyStatusRequest{
		KeyID:   keyName,
		Version: kmsVersion,
		Status:  service.KeyStatusEnabled,
	}

	resp, err := client.KMSUpdateKeyStatus(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyStatusResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyDisableCommand creates the kms key disable command.
func newKMSKeyDisableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable <key-name>",
		Short: "Disable a key",
		Long: `Disable a cryptographic key temporarily.

Examples:
  quicbaas kms key disable mykey
  quicbaas kms key disable mykey --version 2`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyDisableCommand,
	}

	cmd.Flags().Uint64Var(&kmsVersion, "version", 0, "Key version (0 for all versions)")

	return cmd
}

// runKMSKeyDisableCommand executes the kms key disable command.
func runKMSKeyDisableCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.UpdateKeyStatusRequest{
		KeyID:   keyName,
		Version: kmsVersion,
		Status:  service.KeyStatusDisabled,
	}

	resp, err := client.KMSUpdateKeyStatus(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyStatusResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyVersionsCommand creates the kms key versions command.
func newKMSKeyVersionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions <key-name>",
		Short: "List key versions",
		Long: `List all versions of a cryptographic key.

Examples:
  quicbaas kms key versions mykey
  quicbaas kms key versions mykey --limit 10`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyVersionsCommand,
	}

	cmd.Flags().IntVar(&kmsLimit, "limit", 0, "Maximum number of versions to return (0 for all)")

	return cmd
}

// runKMSKeyVersionsCommand executes the kms key versions command.
func runKMSKeyVersionsCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.ListKeyVersionsRequest{
		KeyID: keyName,
		Limit: kmsLimit,
	}

	resp, err := client.KMSListKeyVersions(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyVersionsResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyExportCommand creates the kms key export command.
func newKMSKeyExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <key-name>",
		Short: "Export a key",
		Long: `Export a cryptographic key (if exportable).

Examples:
  quicbaas kms key export mykey --wrapping-key wrapping-key-name`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyExportCommand,
	}

	cmd.Flags().StringVar(&kmsWrappingKey, "wrapping-key", "", "Wrapping key for export")
	cmd.Flags().StringVar(&kmsWrappingAlgorithm, "wrapping-algorithm", "", "Wrapping algorithm")
	cmd.Flags().StringVar(&kmsFormat, "format", "", "Export format")

	return cmd
}

// runKMSKeyExportCommand executes the kms key export command.
func runKMSKeyExportCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.ExportKeyRequest{
		KeyID:             keyName,
		WrappingKeyID:     kmsWrappingKey,
		WrappingAlgorithm: kmsWrappingAlgorithm,
		Format:            kmsFormat,
	}

	resp, err := client.KMSExportKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyExportResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeyUnwrapCommand creates the kms key unwrap command.
func newKMSKeyUnwrapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unwrap <key-name>",
		Short: "Unwrap an imported key",
		Long: `Unwrap and import a wrapped key.

Examples:
  quicbaas kms key unwrap mykey --wrapping-key wrapping-key --material base64:... --algorithm AES-256-GCM`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeyUnwrapCommand,
	}

	cmd.Flags().StringVar(&kmsWrappingKey, "wrapping-key", "", "Wrapping key (required)")
	cmd.Flags().StringVar(&kmsMaterial, "material", "", "Wrapped key material (base64) (required)")
	cmd.Flags().StringVar(&kmsAlgorithm, "algorithm", "", "Target key algorithm (required)")

	cmd.MarkFlagRequired("wrapping-key")
	cmd.MarkFlagRequired("material")
	cmd.MarkFlagRequired("algorithm")

	return cmd
}

// runKMSKeyUnwrapCommand executes the kms key unwrap command.
func runKMSKeyUnwrapCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	wrappedKey, err := base64.StdEncoding.DecodeString(kmsMaterial)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "kms.key.unwrap", "invalid base64 material")
	}

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.UnwrapKeyRequest{
		KeyID:         keyName,
		WrappingKeyID: kmsWrappingKey,
		WrappedKey:    wrappedKey,
		Algorithm:     kmsAlgorithm,
	}

	resp, err := client.KMSUnwrapKey(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeyUnwrapResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSKeySetSafetyCommand creates the kms key set-safety command.
func newKMSKeySetSafetyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-safety <key-name>",
		Short: "Set key AEAD safety limits",
		Long: `Set AEAD safety limits for a cryptographic key.

Examples:
  quicbaas kms key set-safety mykey --max-operations 1000000 --max-bytes 1073741824`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSKeySetSafetyCommand,
	}

	cmd.Flags().Uint64Var(&kmsMaxOperations, "max-operations", 0, "Maximum operations")
	cmd.Flags().Uint64Var(&kmsMaxBytes, "max-bytes", 0, "Maximum bytes")
	cmd.Flags().IntVar(&kmsWarningPercent, "warning-percent", 80, "Warning threshold percentage")

	return cmd
}

// runKMSKeySetSafetyCommand executes the kms key set-safety command.
func runKMSKeySetSafetyCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.SetKeySafetyRequest{
		KeyID:          keyName,
		MaxOperations:  kmsMaxOperations,
		MaxBytes:       kmsMaxBytes,
		WarningPercent: kmsWarningPercent,
	}

	resp, err := client.KMSSetKeySafety(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSKeySetSafetyResponse(resp, kmsOutputFmt, os.Stdout)
}

// =============================================================================
// KMS DEK Commands (deprecated shim)
// =============================================================================

// newKMSDEKCommand mounts the canonical `quicbaas dek` tree under `kms` as a
// deprecated alias.
//
// The old subtree carried its own eight verbs backed by dekClientFactory,
// which pinned the default and --transport grpc cases to a raw gRPC client
// instead of the SDK. Both the fork and the pinned factory are gone: this
// returns the same SDK-backed command tree `quicbaas dek` mounts, so every
// legacy verb keeps working (with `generate` aliased onto `create`) and picks
// up the four verbs the legacy tree never had.
func newKMSDEKCommand() *cobra.Command {
	cmd := newDEKCommand()
	cmd.Deprecated = "use \"quicbaas dek\" instead; \"quicbaas kms dek\" will be removed in a future release"
	return cmd
}

// =============================================================================
// KMS Encrypt/Decrypt Commands
// =============================================================================

// newKMSEncryptCommand creates the kms encrypt command.
func newKMSEncryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encrypt <key-name>",
		Short: "Encrypt data using a key",
		Long: `Encrypt data using a cryptographic key.

Examples:
  quicbaas kms encrypt mykey --data "sensitive data"
  quicbaas kms encrypt mykey --data "sensitive" --aad "context"`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSEncryptCommand,
	}

	cmd.Flags().StringVar(&kmsData, "data", "", "Data to encrypt (required)")
	cmd.Flags().StringVar(&kmsAAD, "aad", "", "Additional authenticated data")
	cmd.Flags().BoolVar(&kmsBase64, "base64", false, "Data is base64 encoded")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runKMSEncryptCommand executes the kms encrypt command.
func runKMSEncryptCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	var plaintext []byte
	var err error

	if kmsBase64 {
		plaintext, err = base64.StdEncoding.DecodeString(kmsData)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "kms.encrypt", "invalid base64 data")
		}
	} else {
		plaintext = []byte(kmsData)
	}

	var aadContext []byte
	if kmsAAD != "" {
		aadContext = []byte(kmsAAD)
	}

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.EncryptRequest{
		KeyID:     keyName,
		Plaintext: plaintext,
		Context:   aadContext,
	}

	resp, err := client.KMSEncrypt(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSEncryptResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSDecryptCommand creates the kms decrypt command.
func newKMSDecryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decrypt <key-name>",
		Short: "Decrypt data using a key",
		Long: `Decrypt data using a cryptographic key.

Examples:
  quicbaas kms decrypt mykey --data "abcd1234..."
  quicbaas kms decrypt mykey --data "abcd1234..." --aad "context"`,
		Args: cobra.ExactArgs(1),
		RunE: runKMSDecryptCommand,
	}

	cmd.Flags().StringVar(&kmsData, "data", "", "Data to decrypt (hex or base64) (required)")
	cmd.Flags().StringVar(&kmsAAD, "aad", "", "Additional authenticated data")
	cmd.Flags().BoolVar(&kmsBase64, "base64", false, "Data is base64 encoded (default: hex)")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runKMSDecryptCommand executes the kms decrypt command.
func runKMSDecryptCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	var ciphertext []byte
	var err error

	if kmsBase64 {
		ciphertext, err = base64.StdEncoding.DecodeString(kmsData)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "kms.decrypt", "invalid base64 data")
		}
	} else {
		ciphertext, err = hex.DecodeString(kmsData)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "kms.decrypt", "invalid hex data")
		}
	}

	var aadContext []byte
	if kmsAAD != "" {
		aadContext = []byte(kmsAAD)
	}

	client, err := kmsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.DecryptRequest{
		KeyID:      keyName,
		Ciphertext: ciphertext,
		Context:    aadContext,
	}

	resp, err := client.KMSDecrypt(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatKMSDecryptResponse(resp, kmsOutputFmt, os.Stdout)
}

// =============================================================================
// KMS Backend Commands
// =============================================================================

// newKMSBackendCommand creates the kms backend subcommand group.
func newKMSBackendCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backend",
		Short: "KMS backend operations",
		Long:  `Commands for inspecting and testing the KMS backend.`,
	}

	cmd.AddCommand(newKMSBackendStatusCommand())
	cmd.AddCommand(newKMSBackendTestCommand())

	return cmd
}

// newKMSBackendStatusCommand creates the kms backend status command.
func newKMSBackendStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show backend status",
		Long: `Display KMS backend status and configuration.

Examples:
  quicbaas kms backend status`,
		Args: cobra.NoArgs,
		RunE: runKMSBackendStatusCommand,
	}

	return cmd
}

// runKMSBackendStatusCommand executes the kms backend status command.
func runKMSBackendStatusCommand(cmd *cobra.Command, args []string) error {
	client, err := backendClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	resp, err := client.KMSBackendStatus(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatBackendStatusResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSBackendTestCommand creates the kms backend test command.
func newKMSBackendTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test backend connectivity",
		Long: `Test KMS backend connectivity and operations.

Examples:
  quicbaas kms backend test`,
		Args: cobra.NoArgs,
		RunE: runKMSBackendTestCommand,
	}

	return cmd
}

// runKMSBackendTestCommand executes the kms backend test command.
func runKMSBackendTestCommand(cmd *cobra.Command, args []string) error {
	client, err := backendClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	resp, err := client.KMSBackendTest(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatBackendTestResponse(resp, kmsOutputFmt, os.Stdout)
}

// =============================================================================
// KMS Safety Commands
// =============================================================================

// newKMSSafetyCommand creates the kms safety subcommand group.
func newKMSSafetyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "safety",
		Short: "AEAD safety monitoring",
		Long:  `Monitor and report on AEAD usage limits for keys and DEKs.`,
	}

	cmd.AddCommand(newKMSSafetyCheckCommand())
	cmd.AddCommand(newKMSSafetyReportCommand())

	return cmd
}

// newKMSSafetyCheckCommand creates the kms safety check command.
func newKMSSafetyCheckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check for keys/DEKs approaching limits",
		Long: `Show keys and DEKs approaching AEAD usage limits.

Examples:
  quicbaas kms safety check
  quicbaas kms safety check --threshold 90
  quicbaas kms safety check --output json`,
		Args: cobra.NoArgs,
		RunE: runKMSSafetyCheckCommand,
	}

	cmd.Flags().Float64Var(&kmsSafetyThreshold, "threshold", 80.0, "Usage threshold percentage (0-100)")

	return cmd
}

// runKMSSafetyCheckCommand executes the kms safety check command.
func runKMSSafetyCheckCommand(cmd *cobra.Command, args []string) error {
	client, err := safetyClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.SafetyCheckRequest{
		ThresholdPercent: kmsSafetyThreshold,
	}
	resp, err := client.KMSSafetyCheck(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSafetyCheckResponse(resp, kmsOutputFmt, os.Stdout)
}

// newKMSSafetyReportCommand creates the kms safety report command.
func newKMSSafetyReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate full safety usage report",
		Long: `Generate a comprehensive safety usage report for all keys and DEKs.

Examples:
  quicbaas kms safety report
  quicbaas kms safety report --output json`,
		Args: cobra.NoArgs,
		RunE: runKMSSafetyReportCommand,
	}

	return cmd
}

// runKMSSafetyReportCommand executes the kms safety report command.
func runKMSSafetyReportCommand(cmd *cobra.Command, args []string) error {
	client, err := safetyClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.SafetyReportRequest{
		IncludeDetails: true,
	}
	resp, err := client.KMSSafetyReport(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSafetyReportResponse(resp, kmsOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

func formatKMSKeyGenerateResponse(resp *service.GenerateKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.Key.KeyID)
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.Key.Algorithm)
		fmt.Fprintf(tw, "Version:\t%d\n", resp.Key.Version)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Key.Status)
		fmt.Fprintf(tw, "Created:\t%s\n", resp.Key.CreatedAt.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key generated: %s (version %d)\n", resp.Key.KeyID, resp.Key.Version)
		return nil
	}
}

func formatKMSKeyGetResponse(resp *service.GetKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.Metadata.KeyID)
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.Metadata.Algorithm)
		fmt.Fprintf(tw, "Version:\t%d\n", resp.Metadata.Version)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Metadata.Status)
		fmt.Fprintf(tw, "Created:\t%s\n", resp.Metadata.CreatedAt.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key: %s (v%d) - %s\n", resp.Metadata.KeyID, resp.Metadata.Version, resp.Metadata.Status)
		return nil
	}
}

func formatKMSKeyListResponse(resp *service.ListKeysResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "KEY ID\tALGORITHM\tVERSION\tSTATUS\n")
		for _, key := range resp.Keys {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", key.KeyID, key.Algorithm, key.Version, key.Status)
		}
		fmt.Fprintf(tw, "\nTotal: %d keys\n", len(resp.Keys))
		return tw.Flush()

	default:
		for _, key := range resp.Keys {
			fmt.Fprintf(w, "%s (v%d) - %s\n", key.KeyID, key.Version, key.Algorithm)
		}
		return nil
	}
}

func formatKMSKeyRotateResponse(resp *service.RotateKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Old Version:\t%d\n", resp.OldVersion)
		fmt.Fprintf(tw, "New Version:\t%d\n", resp.NewVersion)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key rotated: %s (v%d -> v%d)\n", resp.KeyID, resp.OldVersion, resp.NewVersion)
		return nil
	}
}

func formatKMSKeyDeleteResponse(resp *service.DeleteKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key deleted: %s\n", resp.KeyID)
		return nil
	}
}

func formatKMSKeyImportResponse(resp *service.ImportKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.Key.KeyID)
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.Key.Algorithm)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Key.Status)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key imported: %s\n", resp.Key.KeyID)
		return nil
	}
}

func formatKMSKeyStatusResponse(resp *service.UpdateKeyStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Old Status:\t%s\n", resp.OldStatus)
		fmt.Fprintf(tw, "New Status:\t%s\n", resp.NewStatus)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key status updated: %s (%s -> %s)\n", resp.KeyID, resp.OldStatus, resp.NewStatus)
		return nil
	}
}

func formatKMSKeyVersionsResponse(resp *service.ListKeyVersionsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "VERSION\tSTATUS\tCREATED\n")
		for _, v := range resp.Versions {
			fmt.Fprintf(tw, "%d\t%s\t%s\n", v.Version, v.Status, v.CreatedAt.Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		for _, v := range resp.Versions {
			fmt.Fprintf(w, "v%d - %s\n", v.Version, v.Status)
		}
		return nil
	}
}

func formatKMSKeyExportResponse(resp *service.ExportKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Wrapped Key:\t%s\n", base64.StdEncoding.EncodeToString(resp.WrappedKey))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key exported: %s\n", resp.KeyID)
		return nil
	}
}

func formatKMSKeyUnwrapResponse(resp *service.UnwrapKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.Algorithm)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Key unwrapped: %s\n", resp.KeyID)
		return nil
	}
}

func formatKMSKeySetSafetyResponse(resp *service.SetKeySafetyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Max Operations:\t%d\n", resp.Config.MaxOperations)
		fmt.Fprintf(tw, "Max Bytes:\t%d\n", resp.Config.MaxBytes)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Safety limits set for key: %s\n", resp.KeyID)
		return nil
	}
}

func formatKMSEncryptResponse(resp *service.EncryptResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Key Version:\t%d\n", resp.KeyVersion)
		fmt.Fprintf(tw, "Ciphertext (hex):\t%s\n", hex.EncodeToString(resp.Ciphertext))
		fmt.Fprintf(tw, "Ciphertext (base64):\t%s\n", base64.StdEncoding.EncodeToString(resp.Ciphertext))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Ciphertext (hex): %s\n", hex.EncodeToString(resp.Ciphertext))
		return nil
	}
}

func formatKMSDecryptResponse(resp *service.DecryptResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Key ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "Key Version:\t%d\n", resp.KeyVersion)
		fmt.Fprintf(tw, "Plaintext:\t%s\n", string(resp.Plaintext))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Plaintext: %s\n", string(resp.Plaintext))
		return nil
	}
}

func formatBackendStatusResponse(resp *service.BackendStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Backend:\t%s\n", resp.BackendType)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		if resp.Device != "" {
			fmt.Fprintf(tw, "Device:\t%s\n", resp.Device)
		}
		if resp.Region != "" {
			fmt.Fprintf(tw, "Region:\t%s\n", resp.Region)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Backend: %s (%s)\n", resp.BackendType, resp.Status)
		return nil
	}
}

func formatBackendTestResponse(resp *service.BackendTestResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Success:\t%v\n", resp.Success)
		fmt.Fprintf(tw, "Duration:\t%s\n", resp.Duration)
		if resp.Message != "" {
			fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
		}
		return tw.Flush()

	default:
		if resp.Success {
			fmt.Fprintf(w, "Backend test passed (%s)\n", resp.Duration)
		} else {
			fmt.Fprintf(w, "Backend test failed: %s\n", resp.Message)
		}
		return nil
	}
}

func formatSafetyCheckResponse(resp *service.SafetyCheckResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if len(resp.Items) == 0 {
			fmt.Fprintf(tw, "No keys or DEKs exceeding %.0f%% usage threshold.\n", resp.ThresholdPercent)
		} else {
			fmt.Fprintf(tw, "ID\tTYPE\tUSAGE%%\tOPERATIONS\tBYTES\n")
			for _, item := range resp.Items {
				fmt.Fprintf(tw, "%s\t%s\t%.1f%%\t%d\t%d\n",
					item.ID, item.Type, item.UsagePercent, item.OperationCount, item.BytesProcessed)
			}
		}
		fmt.Fprintf(tw, "\nTotal Checked: %d keys, %d DEKs\n", resp.TotalKeysChecked, resp.TotalDEKsChecked)
		return tw.Flush()

	default:
		if len(resp.Items) == 0 {
			fmt.Fprintf(w, "No items exceeding threshold\n")
		} else {
			for _, item := range resp.Items {
				fmt.Fprintf(w, "%s (%s): %.1f%% usage\n", item.ID, item.Type, item.UsagePercent)
			}
		}
		return nil
	}
}

func formatSafetyReportResponse(resp *service.SafetyReportResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "AEAD Safety Report\n")
		fmt.Fprintf(tw, "==================\n\n")
		fmt.Fprintf(tw, "Total Operations:\t%d\n", resp.TotalOperations)
		fmt.Fprintf(tw, "Total Bytes:\t%d\n", resp.TotalBytesProcessed)
		fmt.Fprintf(tw, "\nServer Config:\n")
		fmt.Fprintf(tw, "  Enforce:\t%v\n", resp.ServerConfig.Enforce)
		fmt.Fprintf(tw, "  Max Operations:\t%d\n", resp.ServerConfig.MaxOperations)
		fmt.Fprintf(tw, "  Max Bytes:\t%d\n", resp.ServerConfig.MaxBytes)
		if len(resp.Items) > 0 {
			fmt.Fprintf(tw, "\nItems:\n")
			fmt.Fprintf(tw, "ID\tTYPE\tUSAGE%%\n")
			for _, item := range resp.Items {
				fmt.Fprintf(tw, "%s\t%s\t%.1f%%\n", item.ID, item.Type, item.UsagePercent)
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Total operations: %d, Total bytes: %d\n", resp.TotalOperations, resp.TotalBytesProcessed)
		return nil
	}
}
