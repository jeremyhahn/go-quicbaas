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

// Package crypto provides the encryption/decryption service client for the QuicBaaS SDK.
//
// The crypto client provides cryptographic operations using keys managed by the KMS.
// This enables applications to encrypt/decrypt data without direct access to keys.
//
// # Encryption
//
//	client := crypto.NewClient(transport)
//
//	// Encrypt data using a managed key
//	resp, err := client.Encrypt(ctx, &crypto.EncryptRequest{
//	    KeyID:     "my-encryption-key",
//	    Plaintext: []byte("sensitive data"),
//	    Context:   []byte("additional authenticated data"), // optional AAD
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	ciphertext := resp.Ciphertext
//
// # Decryption
//
//	resp, err := client.Decrypt(ctx, &crypto.DecryptRequest{
//	    KeyID:      "my-encryption-key",
//	    Ciphertext: ciphertext,
//	    Context:    []byte("additional authenticated data"), // must match encryption
//	})
//	plaintext := resp.Plaintext
//
// # Digital Signatures
//
//	// Sign data
//	signResp, err := client.Sign(ctx, &crypto.SignRequest{
//	    KeyID:   "my-signing-key",
//	    Data:    []byte("data to sign"),
//	    HashAlg: crypto.HashSHA256,
//	})
//
//	// Verify signature
//	verifyResp, err := client.Verify(ctx, &crypto.VerifyRequest{
//	    KeyID:     "my-signing-key",
//	    Data:      []byte("data to sign"),
//	    Signature: signResp.Signature,
//	    HashAlg:   crypto.HashSHA256,
//	})
//	if verifyResp.Valid {
//	    fmt.Println("Signature is valid")
//	}
package crypto
