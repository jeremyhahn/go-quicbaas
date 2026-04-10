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

// Package kms provides the key management client for the QuicBaaS SDK.
//
// The KMS client supports cryptographic key lifecycle management including
// generation, rotation, and deletion of encryption keys.
//
// # Key Generation
//
//	client := kms.NewClient(transport)
//
//	// Generate a new AES-256-GCM key for encryption
//	resp, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
//	    Name:      "my-encryption-key",
//	    Algorithm: kms.AlgorithmAES256GCM,
//	    Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	keyID := resp.KeyID
//
// # Key Rotation
//
// Keys should be rotated periodically for security:
//
//	resp, err := client.RotateKey(ctx, keyID)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Key rotated to version %d\n", resp.NewVersion)
//
// # Supported Algorithms
//
// Symmetric:
//   - AlgorithmAES256GCM: AES-256-GCM (recommended for encryption)
//   - AlgorithmChaCha20Poly1305: ChaCha20-Poly1305
//
// Asymmetric:
//   - AlgorithmRSA2048: RSA with 2048-bit key
//   - AlgorithmRSA4096: RSA with 4096-bit key
//   - AlgorithmECDSAP256: ECDSA with P-256 curve
//   - AlgorithmECDSAP384: ECDSA with P-384 curve
//   - AlgorithmED25519: Ed25519 (recommended for signing)
//
// # Key Usage
//
// Keys can be restricted to specific operations:
//   - KeyUsageEncrypt: Key can be used for encryption
//   - KeyUsageDecrypt: Key can be used for decryption
//   - KeyUsageSign: Key can be used for signing
//   - KeyUsageVerify: Key can be used for verification
//   - KeyUsageWrap: Key can be used to wrap other keys
//   - KeyUsageUnwrap: Key can be used to unwrap keys
package kms
