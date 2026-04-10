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

// Package main provides Crypto Encryption CLI commands for QuicBaaS.
package main

import (
	stdcontext "context"
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
	cryptoData      string
	cryptoContext   string
	cryptoAlgorithm string
	cryptoSignature string
	cryptoBytes     int
	cryptoFormat    string
	cryptoBits      int
	cryptoOutputFmt string
	cryptoBase64    bool
)

// newCryptoCommand creates the crypto command group.
func newCryptoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crypto",
		Short: "Crypto encryption and signing commands",
		Long: `Crypto commands for encryption-as-a-service operations.

These commands provide cryptographic operations without exposing key material:
  - Encrypt/decrypt data
  - Sign/verify data
  - Generate HMACs
  - Generate random bytes
  - Hash data
  - Generate data encryption keys`,
	}

	cmd.PersistentFlags().StringVar(&cryptoOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newCryptoEncryptCommand())
	cmd.AddCommand(newCryptoDecryptCommand())
	cmd.AddCommand(newCryptoRewrapCommand())
	cmd.AddCommand(newCryptoSignCommand())
	cmd.AddCommand(newCryptoVerifyCommand())
	cmd.AddCommand(newCryptoHashCommand())
	cmd.AddCommand(newCryptoRandomCommand())
	cmd.AddCommand(newCryptoDataKeyCommand())

	return cmd
}

// newCryptoEncryptCommand creates the crypto encrypt command.
func newCryptoEncryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encrypt <key-name>",
		Short: "Encrypt data",
		Long: `Encrypt data using a named crypto key.

Examples:
  # Encrypt string data
  quicbaas crypto encrypt mykey --data "sensitive data"

  # Encrypt with context
  quicbaas crypto encrypt mykey --data "data" --context "user-123"

  # Encrypt base64 data
  quicbaas crypto encrypt mykey --data SGVsbG8= --base64`,
		Args: cobra.ExactArgs(1),
		RunE: runCryptoEncryptCommand,
	}

	cmd.Flags().StringVar(&cryptoData, "data", "", "Data to encrypt (required)")
	cmd.Flags().StringVar(&cryptoContext, "context", "", "Additional context data")
	cmd.Flags().BoolVar(&cryptoBase64, "base64", false, "Data is base64 encoded")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runCryptoEncryptCommand executes the crypto encrypt command.
func runCryptoEncryptCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	var plaintext []byte
	var err error

	if cryptoBase64 {
		plaintext, err = base64.StdEncoding.DecodeString(cryptoData)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "crypto.encrypt", "invalid base64 data")
		}
	} else {
		plaintext = []byte(cryptoData)
	}

	var context []byte
	if cryptoContext != "" {
		context = []byte(cryptoContext)
	}

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.EncryptRequest{
		KeyID:     keyName,
		Plaintext: plaintext,
		Context:   context,
	}

	resp, err := executeCryptoEncrypt(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoEncryptResponse(resp, cryptoOutputFmt, os.Stdout)
}

// newCryptoDecryptCommand creates the crypto decrypt command.
func newCryptoDecryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decrypt <key-name>",
		Short: "Decrypt data",
		Long: `Decrypt data using a named crypto key.

Examples:
  # Decrypt hex-encoded ciphertext
  quicbaas crypto decrypt mykey --data "abcd1234..."

  # Decrypt with context
  quicbaas crypto decrypt mykey --data "abcd..." --context "user-123"

  # Decrypt base64 ciphertext
  quicbaas crypto decrypt mykey --data SGVsbG8= --base64`,
		Args: cobra.ExactArgs(1),
		RunE: runCryptoDecryptCommand,
	}

	cmd.Flags().StringVar(&cryptoData, "data", "", "Data to decrypt (hex or base64) (required)")
	cmd.Flags().StringVar(&cryptoContext, "context", "", "Additional context data")
	cmd.Flags().BoolVar(&cryptoBase64, "base64", false, "Data is base64 encoded (default: hex)")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runCryptoDecryptCommand executes the crypto decrypt command.
func runCryptoDecryptCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	var ciphertext []byte
	var err error

	if cryptoBase64 {
		ciphertext, err = base64.StdEncoding.DecodeString(cryptoData)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "crypto.decrypt", "invalid base64 data")
		}
	} else {
		ciphertext, err = hex.DecodeString(cryptoData)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "crypto.decrypt", "invalid hex data")
		}
	}

	var context []byte
	if cryptoContext != "" {
		context = []byte(cryptoContext)
	}

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.DecryptRequest{
		KeyID:      keyName,
		Ciphertext: ciphertext,
		Context:    context,
	}

	resp, err := executeCryptoDecrypt(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoDecryptResponse(resp, cryptoOutputFmt, os.Stdout)
}

// newCryptoRewrapCommand creates the crypto rewrap command.
func newCryptoRewrapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rewrap <key-name>",
		Short: "Re-encrypt data with latest key version",
		Long: `Re-encrypt data with the latest version of a crypto key.

This is useful after key rotation to upgrade ciphertext to the newest key version.

Examples:
  # Rewrap ciphertext
  quicbaas crypto rewrap mykey --data "abcd1234..."`,
		Args: cobra.ExactArgs(1),
		RunE: runCryptoRewrapCommand,
	}

	cmd.Flags().StringVar(&cryptoData, "data", "", "Ciphertext to rewrap (hex) (required)")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runCryptoRewrapCommand executes the crypto rewrap command.
func runCryptoRewrapCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	ciphertext, err := hex.DecodeString(cryptoData)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "crypto.rewrap", "invalid hex data")
	}

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.RewrapRequest{
		KeyID:      keyName,
		Ciphertext: ciphertext,
	}

	resp, err := executeCryptoRewrap(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoRewrapResponse(resp, cryptoOutputFmt, os.Stdout)
}

// newCryptoSignCommand creates the crypto sign command.
func newCryptoSignCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign <key-name>",
		Short: "Sign data",
		Long: `Generate a cryptographic signature for data.

Examples:
  # Sign data with SHA256
  quicbaas crypto sign signing-key --data "message to sign"

  # Sign with specific algorithm
  quicbaas crypto sign signing-key --data "message" --algorithm SHA512`,
		Args: cobra.ExactArgs(1),
		RunE: runCryptoSignCommand,
	}

	cmd.Flags().StringVar(&cryptoData, "data", "", "Data to sign (required)")
	cmd.Flags().StringVar(&cryptoAlgorithm, "algorithm", "SHA256", "Hash algorithm (SHA256, SHA384, SHA512)")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runCryptoSignCommand executes the crypto sign command.
func runCryptoSignCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.SignRequest{
		KeyID:         keyName,
		Message:       []byte(cryptoData),
		HashAlgorithm: parseHashAlgorithm(cryptoAlgorithm),
	}

	resp, err := executeCryptoSign(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoSignResponse(resp, cryptoOutputFmt, os.Stdout)
}

// newCryptoVerifyCommand creates the crypto verify command.
func newCryptoVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <key-name>",
		Short: "Verify a signature",
		Long: `Verify a cryptographic signature.

Examples:
  # Verify signature
  quicbaas crypto verify signing-key --data "message" --signature base64-signature

  # Verify with specific algorithm
  quicbaas crypto verify signing-key --data "message" --signature sig --algorithm SHA512`,
		Args: cobra.ExactArgs(1),
		RunE: runCryptoVerifyCommand,
	}

	cmd.Flags().StringVar(&cryptoData, "data", "", "Signed data (required)")
	cmd.Flags().StringVar(&cryptoSignature, "signature", "", "Signature to verify (base64) (required)")
	cmd.Flags().StringVar(&cryptoAlgorithm, "algorithm", "SHA256", "Hash algorithm")

	cmd.MarkFlagRequired("data")
	cmd.MarkFlagRequired("signature")

	return cmd
}

// runCryptoVerifyCommand executes the crypto verify command.
func runCryptoVerifyCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	signature, err := base64.StdEncoding.DecodeString(cryptoSignature)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "crypto.verify", "invalid signature format (expected base64)")
	}

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.VerifyRequest{
		KeyID:         keyName,
		Message:       []byte(cryptoData),
		Signature:     signature,
		HashAlgorithm: parseHashAlgorithm(cryptoAlgorithm),
	}

	resp, err := executeCryptoVerify(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoVerifyResponse(resp, cryptoOutputFmt, os.Stdout)
}

// newCryptoHashCommand creates the crypto hash command.
func newCryptoHashCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hash",
		Short: "Hash data",
		Long: `Generate a cryptographic hash of data.

Examples:
  # Hash with SHA256 (default)
  quicbaas crypto hash --data "message to hash"

  # Hash with specific algorithm
  quicbaas crypto hash --data "message" --algorithm SHA512`,
		RunE: runCryptoHashCommand,
	}

	cmd.Flags().StringVar(&cryptoData, "data", "", "Data to hash (required)")
	cmd.Flags().StringVar(&cryptoAlgorithm, "algorithm", "SHA256", "Hash algorithm")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runCryptoHashCommand executes the crypto hash command.
func runCryptoHashCommand(cmd *cobra.Command, args []string) error {
	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.HashRequest{
		Data:      []byte(cryptoData),
		Algorithm: parseHashAlgorithm(cryptoAlgorithm),
	}

	resp, err := executeCryptoHash(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoHashResponse(resp, cryptoOutputFmt, os.Stdout)
}

// newCryptoRandomCommand creates the crypto random command.
func newCryptoRandomCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "random",
		Short: "Generate random bytes",
		Long: `Generate cryptographically secure random bytes from the server.

Examples:
  # Generate 32 random bytes (default)
  quicbaas crypto random

  # Generate specific number of bytes
  quicbaas crypto random --bytes 64

  # Generate as hex format
  quicbaas crypto random --bytes 32 --format hex`,
		RunE: runCryptoRandomCommand,
	}

	cmd.Flags().IntVar(&cryptoBytes, "bytes", 32, "Number of random bytes to generate")
	cmd.Flags().StringVar(&cryptoFormat, "format", "base64", "Output format (base64, hex)")

	return cmd
}

// runCryptoRandomCommand executes the crypto random command.
func runCryptoRandomCommand(cmd *cobra.Command, args []string) error {
	if cryptoBytes < 1 || cryptoBytes > 1024*1024 {
		return errors.Newf(errors.ErrInvalidInput, "crypto.random", "bytes must be between 1 and 1048576")
	}

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.GenerateRandomRequest{
		NumBytes: cryptoBytes,
	}

	resp, err := executeCryptoRandom(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoRandomResponse(resp, cryptoFormat, cryptoOutputFmt, os.Stdout)
}

// newCryptoDataKeyCommand creates the crypto datakey command.
func newCryptoDataKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "datakey <key-name>",
		Short: "Generate a data encryption key",
		Long: `Generate a data encryption key wrapped by a named crypto key.

Returns both the plaintext key (for immediate use) and wrapped key (for storage).

Examples:
  # Generate 256-bit AES key
  quicbaas crypto datakey mykey

  # Generate 128-bit AES key
  quicbaas crypto datakey mykey --bits 128`,
		Args: cobra.ExactArgs(1),
		RunE: runCryptoDataKeyCommand,
	}

	cmd.Flags().IntVar(&cryptoBits, "bits", 256, "Key size in bits (128, 192, 256)")

	return cmd
}

// runCryptoDataKeyCommand executes the crypto datakey command.
func runCryptoDataKeyCommand(cmd *cobra.Command, args []string) error {
	keyName := args[0]

	var algorithm service.KeyAlgorithm
	switch cryptoBits {
	case 128:
		algorithm = service.KeyAlgAES128GCM
	case 192:
		algorithm = service.KeyAlgAES192GCM
	case 256:
		algorithm = service.KeyAlgAES256GCM
	default:
		return errors.Newf(errors.ErrInvalidInput, "crypto.datakey", "bits must be 128, 192, or 256")
	}

	client, err := cryptoClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Build KeyAttributes for the data key
	attrs := dataKeyAlgorithmToKeyAttributes(algorithm)

	req := &service.GenerateDataKeyRequest{
		KeyID:         keyName,
		KeyAttributes: attrs,
	}

	resp, err := executeCryptoDataKey(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatCryptoDataKeyResponse(resp, cryptoOutputFmt, os.Stdout)
}

// Crypto service execution functions

func executeCryptoEncrypt(ctx stdcontext.Context, client CryptoClient, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.encrypt", "invalid client type")
	}
	return client.KMSEncrypt(ctx, req)
}

func executeCryptoDecrypt(ctx stdcontext.Context, client CryptoClient, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.decrypt", "invalid client type")
	}
	return client.KMSDecrypt(ctx, req)
}

func executeCryptoRewrap(ctx stdcontext.Context, client CryptoClient, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.rewrap", "invalid client type")
	}
	return client.KMSRewrap(ctx, req)
}

func executeCryptoSign(ctx stdcontext.Context, client CryptoClient, req *service.SignRequest) (*service.SignResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.sign", "invalid client type")
	}
	return client.KMSSign(ctx, req)
}

func executeCryptoVerify(ctx stdcontext.Context, client CryptoClient, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.verify", "invalid client type")
	}
	return client.KMSVerify(ctx, req)
}

func executeCryptoHash(ctx stdcontext.Context, client CryptoClient, req *service.HashRequest) (*service.HashResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.hash", "invalid client type")
	}
	return client.KMSHash(ctx, req)
}

func executeCryptoRandom(ctx stdcontext.Context, client CryptoClient, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.random", "invalid client type")
	}
	return client.KMSGenerateRandom(ctx, req)
}

func executeCryptoDataKey(ctx stdcontext.Context, client CryptoClient, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "crypto.datakey", "invalid client type")
	}
	return client.KMSGenerateDataKey(ctx, req)
}

// parseHashAlgorithm converts a CLI algorithm string to a service.HashAlgorithm.
func parseHashAlgorithm(alg string) service.HashAlgorithm {
	switch strings.ToUpper(alg) {
	case "SHA256":
		return service.HashAlgSHA256
	case "SHA384":
		return service.HashAlgSHA384
	case "SHA512":
		return service.HashAlgSHA512
	case "SHA3-256":
		return service.HashAlgSHA3_256
	case "SHA3-384":
		return service.HashAlgSHA3_384
	case "SHA3-512":
		return service.HashAlgSHA3_512
	case "BLAKE2B-256":
		return service.HashAlgBLAKE2b256
	case "BLAKE2B-512":
		return service.HashAlgBLAKE2b512
	default:
		return service.HashAlgSHA256
	}
}

// Formatting functions

func formatCryptoEncryptResponse(resp *service.EncryptResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(w, "%s\n", hex.EncodeToString(resp.Ciphertext))
		return nil
	}
}

func formatCryptoDecryptResponse(resp *service.DecryptResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(w, "%s\n", string(resp.Plaintext))
		return nil
	}
}

func formatCryptoRewrapResponse(resp *service.RewrapResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Old Version:\t%d\n", resp.OldKeyVersion)
		fmt.Fprintf(tw, "New Version:\t%d\n", resp.NewKeyVersion)
		fmt.Fprintf(tw, "Ciphertext (hex):\t%s\n", hex.EncodeToString(resp.Ciphertext))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s\n", hex.EncodeToString(resp.Ciphertext))
		return nil
	}
}

func formatCryptoSignResponse(resp *service.SignResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.HashAlgorithm)
		fmt.Fprintf(tw, "Signature (base64):\t%s\n", base64.StdEncoding.EncodeToString(resp.Signature))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s\n", base64.StdEncoding.EncodeToString(resp.Signature))
		return nil
	}
}

func formatCryptoVerifyResponse(resp *service.VerifyResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Valid:\t%v\n", resp.Valid)
		return tw.Flush()

	default:
		if resp.Valid {
			fmt.Fprintf(w, "Signature valid\n")
		} else {
			fmt.Fprintf(w, "Signature INVALID\n")
		}
		return nil
	}
}

func formatCryptoHashResponse(resp *service.HashResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.Algorithm)
		fmt.Fprintf(tw, "Hash (hex):\t%s\n", hex.EncodeToString(resp.Hash))
		fmt.Fprintf(tw, "Hash (base64):\t%s\n", base64.StdEncoding.EncodeToString(resp.Hash))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s\n", hex.EncodeToString(resp.Hash))
		return nil
	}
}

func formatCryptoRandomResponse(resp *service.GenerateRandomResponse, outputFormat, format string, w io.Writer) error {
	parsedFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch parsedFormat {
	case cli.OutputFormatJSON:
		var encoded string
		if outputFormat == "hex" {
			encoded = hex.EncodeToString(resp.RandomBytes)
		} else {
			encoded = base64.StdEncoding.EncodeToString(resp.RandomBytes)
		}
		output := map[string]interface{}{
			"random_bytes": encoded,
			"format":       outputFormat,
			"length":       len(resp.RandomBytes),
			"timestamp":    resp.Timestamp,
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Length:\t%d bytes\n", len(resp.RandomBytes))
		if outputFormat == "hex" {
			fmt.Fprintf(tw, "Random (hex):\t%s\n", hex.EncodeToString(resp.RandomBytes))
		} else {
			fmt.Fprintf(tw, "Random (base64):\t%s\n", base64.StdEncoding.EncodeToString(resp.RandomBytes))
		}
		return tw.Flush()

	default:
		if outputFormat == "hex" {
			fmt.Fprintf(w, "%s\n", hex.EncodeToString(resp.RandomBytes))
		} else {
			fmt.Fprintf(w, "%s\n", base64.StdEncoding.EncodeToString(resp.RandomBytes))
		}
		return nil
	}
}

func formatCryptoDataKeyResponse(resp *service.GenerateDataKeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"key_id":          resp.KeyID,
			"plaintext":       base64.StdEncoding.EncodeToString(resp.Plaintext),
			"ciphertext_blob": base64.StdEncoding.EncodeToString(resp.CiphertextBlob),
			"timestamp":       resp.Timestamp,
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nDATA ENCRYPTION KEY GENERATED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "\nKey ID:\t%s\n", resp.KeyID)
		fmt.Fprintf(tw, "\nPlaintext Key (use immediately, do not store):\n")
		fmt.Fprintf(tw, "  Base64:\t%s\n", base64.StdEncoding.EncodeToString(resp.Plaintext))
		fmt.Fprintf(tw, "  Hex:\t%s\n", hex.EncodeToString(resp.Plaintext))
		fmt.Fprintf(tw, "\nWrapped Key (store securely):\n")
		fmt.Fprintf(tw, "  Base64:\t%s\n", base64.StdEncoding.EncodeToString(resp.CiphertextBlob))
		fmt.Fprintf(tw, "\nTimestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Plaintext: %s\n", base64.StdEncoding.EncodeToString(resp.Plaintext))
		fmt.Fprintf(w, "Wrapped: %s\n", base64.StdEncoding.EncodeToString(resp.CiphertextBlob))
		return nil
	}
}

// dataKeyAlgorithmToKeyAttributes converts a CLI algorithm string to KeyAttributes for data keys.
func dataKeyAlgorithmToKeyAttributes(algorithm service.KeyAlgorithm) *kctypes.KeyAttributes {
	attrs := &kctypes.KeyAttributes{}

	// Map algorithm string to KeyAttributes fields
	switch algorithm {
	case service.KeyAlgAES128GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
	case service.KeyAlgAES192GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
	case service.KeyAlgAES256GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	case service.KeyAlgChaCha20Poly1305:
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
	case service.KeyAlgXChaCha20Poly1305:
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
	default:
		// Default to AES-256-GCM for data keys
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	}

	return attrs
}
