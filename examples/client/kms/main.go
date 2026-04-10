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

// Example: Key Management Service (KMS)
//
// This example demonstrates:
// - Connecting to QuicBaaS using the SDK with multiprotocol support
// - Generating cryptographic keys with different algorithms
// - Listing and retrieving key information
// - Key rotation for security best practices
// - Using keys for encryption/decryption operations
// - Key lifecycle management (deletion)
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
//
// Usage:
//
//	# Run with default gRPC transport
//	go run main.go
//
//	# Run with REST transport
//	go run main.go -transport rest -address http://localhost:8080
//
//	# Run with QUIC transport
//	go run main.go -transport quic -address localhost:50052
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS KMS Example ===")
	fmt.Printf("Transport: %s\n", *transportType)
	fmt.Printf("Address:   %s\n", *address)
	fmt.Println()

	// Create client with the specified transport
	var opts []quicbaas.Option
	opts = append(opts, quicbaas.WithTimeout(30*time.Second))
	// Select transport based on flag
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		// default is gRPC, no option needed
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Step 1: Generate an encryption key
	fmt.Println("Step 1: Generating AES-256-GCM encryption key...")
	genResp, err := client.KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      "my-encryption-key",
		Algorithm: quicbaas.AlgorithmAES256GCM,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
	})
	if err != nil {
		log.Fatalf("Failed to generate key: %v", err)
	}
	fmt.Printf("  Key ID:     %s\n", genResp.KeyID)
	fmt.Printf("  Version:    %d\n", genResp.Version)
	fmt.Printf("  Algorithm:  %s\n", genResp.Algorithm)
	encryptionKeyID := genResp.KeyID

	// Step 2: Generate a signing key
	fmt.Println("\nStep 2: Generating Ed25519 signing key...")
	signResp, err := client.KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      "my-signing-key",
		Algorithm: quicbaas.AlgorithmED25519,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageSign, quicbaas.KeyUsageVerify},
	})
	if err != nil {
		log.Fatalf("Failed to generate signing key: %v", err)
	}
	fmt.Printf("  Key ID:     %s\n", signResp.KeyID)
	fmt.Printf("  Algorithm:  %s\n", signResp.Algorithm)
	signingKeyID := signResp.KeyID

	// Step 3: List all keys
	fmt.Println("\nStep 3: Listing all keys...")
	listResp, err := client.KMS().ListKeys(ctx)
	if err != nil {
		log.Fatalf("Failed to list keys: %v", err)
	}
	fmt.Printf("  Found %d keys:\n", len(listResp.Keys))
	for _, key := range listResp.Keys {
		fmt.Printf("    - %s (%s) v%d\n", key.Name, key.Algorithm, key.Version)
	}

	// Step 4: Get key details
	fmt.Println("\nStep 4: Getting encryption key details...")
	keyInfo, err := client.KMS().GetKey(ctx, encryptionKeyID)
	if err != nil {
		log.Fatalf("Failed to get key: %v", err)
	}
	fmt.Printf("  Name:       %s\n", keyInfo.Name)
	fmt.Printf("  Algorithm:  %s\n", keyInfo.Algorithm)
	fmt.Printf("  Version:    %d\n", keyInfo.Version)
	fmt.Printf("  Created:    %s\n", keyInfo.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Usage:      ")
	for i, usage := range keyInfo.Usage {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(usage)
	}
	fmt.Println()

	// Step 5: Use key for encryption/decryption
	fmt.Println("\nStep 5: Encrypting and decrypting data...")
	plaintext := []byte("Hello, QuicBaaS KMS!")
	fmt.Printf("  Plaintext:  %s\n", string(plaintext))

	encryptResp, err := client.Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     encryptionKeyID,
		Plaintext: plaintext,
	})
	if err != nil {
		log.Fatalf("Failed to encrypt: %v", err)
	}
	fmt.Printf("  Ciphertext: %x (truncated)\n", encryptResp.Ciphertext[:min(32, len(encryptResp.Ciphertext))])

	decryptResp, err := client.Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      encryptionKeyID,
		Ciphertext: encryptResp.Ciphertext,
	})
	if err != nil {
		log.Fatalf("Failed to decrypt: %v", err)
	}
	fmt.Printf("  Decrypted:  %s\n", string(decryptResp.Plaintext))

	// Step 6: Sign and verify data
	fmt.Println("\nStep 6: Signing and verifying data...")
	message := []byte("Important message to sign")

	signDataResp, err := client.Crypto().Sign(ctx, &quicbaas.SignRequest{
		KeyID:   signingKeyID,
		Data:    message,
		HashAlg: quicbaas.HashSHA256,
	})
	if err != nil {
		log.Fatalf("Failed to sign: %v", err)
	}
	fmt.Printf("  Message:    %s\n", string(message))
	fmt.Printf("  Signature:  %x (truncated)\n", signDataResp.Signature[:min(32, len(signDataResp.Signature))])

	verifyResp, err := client.Crypto().Verify(ctx, &quicbaas.VerifyRequest{
		KeyID:     signingKeyID,
		Data:      message,
		Signature: signDataResp.Signature,
		HashAlg:   quicbaas.HashSHA256,
	})
	if err != nil {
		log.Fatalf("Failed to verify: %v", err)
	}
	fmt.Printf("  Valid:      %v\n", verifyResp.Valid)

	// Step 7: Rotate a key
	fmt.Println("\nStep 7: Rotating encryption key...")
	rotateResp, err := client.KMS().RotateKey(ctx, encryptionKeyID)
	if err != nil {
		log.Fatalf("Failed to rotate key: %v", err)
	}
	fmt.Printf("  Key ID:      %s\n", rotateResp.KeyID)
	fmt.Printf("  New Version: %d\n", rotateResp.NewVersion)

	// Step 8: Verify rotation
	fmt.Println("\nStep 8: Verifying key rotation...")
	keyInfo, err = client.KMS().GetKey(ctx, encryptionKeyID)
	if err != nil {
		log.Fatalf("Failed to get rotated key: %v", err)
	}
	fmt.Printf("  Current Version: %d\n", keyInfo.Version)
	fmt.Printf("  Rotated At:      %s\n", keyInfo.RotatedAt.Format(time.RFC3339))

	// Step 9: Delete keys (cleanup)
	fmt.Println("\nStep 9: Cleaning up keys...")
	if err := client.KMS().DeleteKey(ctx, encryptionKeyID); err != nil {
		log.Printf("  Warning: Failed to delete encryption key: %v", err)
	} else {
		fmt.Printf("  Deleted: %s\n", encryptionKeyID)
	}

	if err := client.KMS().DeleteKey(ctx, signingKeyID); err != nil {
		log.Printf("  Warning: Failed to delete signing key: %v", err)
	} else {
		fmt.Printf("  Deleted: %s\n", signingKeyID)
	}

	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  1. KMS provides secure key generation with multiple algorithms")
	fmt.Println("  2. Keys can be used for encryption, decryption, signing, and verification")
	fmt.Println("  3. Key rotation maintains security without changing key IDs")
	fmt.Println("  4. The SDK supports multiple transports (gRPC, REST, QUIC)")
	fmt.Println("  5. All operations are audited for compliance")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
