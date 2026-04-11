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

// Package encryption provides field-level encryption for QuicBaaS entities.
//
// # Overview
//
// This package enables transparent encryption and decryption of sensitive fields
// in struct types using Go struct tags. It supports AES-128-GCM and AES-256-GCM
// authenticated encryption, providing both confidentiality and integrity.
//
// # Features
//
//   - Field-level encryption via struct tags
//   - AES-128-GCM and AES-256-GCM support
//   - Authenticated encryption with integrity verification
//   - Reflection-based automatic encryption/decryption
//   - Base64 encoding for JSON safety
//   - Prometheus metrics for monitoring
//   - Comprehensive error handling
//
// # Security Guarantees
//
//   - Cryptographically secure random nonce generation
//   - Authentication tags prevent tampering
//   - Constant-time key validation
//   - No key material in error messages
//   - Memory-safe operations
//
// # Usage
//
// Define a struct with encryption tags:
//
//	type User struct {
//	    ID       uint64 `json:"id"`
//	    Username string `json:"username"`
//	    Email    string `json:"email" encrypt:"aes256"`
//	    Password string `json:"password" encrypt:"aes256"`
//	}
//
// Encrypt data before storage:
//
//	user := &User{
//	    ID:       1,
//	    Username: "alice",
//	    Email:    "alice@example.com",
//	    Password: "secret123",
//	}
//
//	key, err := GenerateKey(AlgorithmAES256)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	encrypted, err := MarshalWithEncryption(user, key)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Decrypt data after retrieval:
//
//	decrypted := &User{}
//	err = UnmarshalWithDecryption(encrypted, decrypted, key)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Supported Tag Values
//
//   - `encrypt:"aes128"` - Use AES-128-GCM (16-byte key)
//   - `encrypt:"aes256"` - Use AES-256-GCM (32-byte key)
//
// # Supported Field Types
//
// Only the following field types can be encrypted:
//   - string
//   - []byte
//
// # Integration with GenericStateMachine
//
// To integrate with the state machine, modify the encoding functions:
//
//	import "github.com/jeremyhahn/go-quicbaas/pkg/storage/encryption"
//
//	func EncodeEntityWithEncryption[E KeyValueEntity](entity E, key []byte) ([]byte, error) {
//	    // Check if entity has encrypted fields
//	    if encryption.HasEncryptedFields(reflect.TypeOf(entity)) {
//	        return encryption.MarshalWithEncryption(entity, key)
//	    }
//	    return json.Marshal(entity)
//	}
//
//	func DecodeEntityWithDecryption[E KeyValueEntity](data []byte, key []byte) (E, error) {
//	    var entity E
//	    if encryption.HasEncryptedFields(reflect.TypeOf(entity)) {
//	        if err := encryption.UnmarshalWithDecryption(data, &entity, key); err != nil {
//	            return entity, err
//	        }
//	        return entity, nil
//	    }
//	    if err := json.Unmarshal(data, &entity); err != nil {
//	        return entity, err
//	    }
//	    return entity, nil
//	}
//
// # Key Management
//
// IMPORTANT: Proper key management is critical for security.
//
// Best practices:
//   - Use a dedicated key management service (KMS)
//   - Rotate keys regularly
//   - Use different keys for different environments
//   - Never hardcode keys in source code
//   - Store keys in secure vaults (HashiCorp Vault, AWS KMS, etc.)
//   - Use key derivation functions for password-based keys
//
// Example key generation:
//
//	// Generate a new AES-256 key
//	key, err := encryption.GenerateKey(encryption.AlgorithmAES256)
//	if err != nil {
//	    return err
//	}
//	// Store key securely (e.g., in HashiCorp Vault)
//
// # Performance Considerations
//
//   - AES-GCM is highly optimized on modern CPUs with AES-NI
//   - Encryption adds ~1-5 microseconds per field
//   - Use AES-128 for better performance if 256-bit is not required
//   - Pre-allocate buffers for high-throughput scenarios
//   - Consider caching parsed encryption metadata
//
// # FIPS 140-3 Compliance
//
// When built with FIPS mode enabled, this package uses FIPS-approved algorithms:
//   - AES encryption (FIPS 197)
//   - GCM mode (NIST SP 800-38D)
//   - Random number generation (NIST SP 800-90A)
//
// Build with FIPS support:
//
//	go build -tags=fips
//
// # Metrics
//
// Initialize metrics at application startup:
//
//	encryption.InitMetrics()
//
// Available metrics:
//   - quicbaas_encryption_operations_total - Total encryption operations
//   - quicbaas_encryption_decryption_operations_total - Total decryption operations
//   - quicbaas_encryption_errors_total - Total encryption errors
//   - quicbaas_encryption_duration_seconds - Time spent encrypting
//   - quicbaas_encryption_encrypted_bytes_total - Total bytes encrypted
//
// # Error Handling
//
// All errors are wrapped in QBError with appropriate error codes:
//   - ErrEncryption - Encryption operation failed
//   - ErrDecryption - Decryption or authentication failed
//   - ErrInvalidInput - Invalid input parameters
//   - ErrUnimplemented - Feature not yet implemented
//
// Example error handling:
//
//	data, err := MarshalWithEncryption(entity, key)
//	if err != nil {
//	    var qbErr *errors.QBError
//	    if errors.As(err, &qbErr) {
//	        switch qbErr.Code {
//	        case errors.ErrEncryption:
//	            // Handle encryption error
//	        case errors.ErrInvalidInput:
//	            // Handle validation error
//	        }
//	    }
//	}
//
// # Security Warnings
//
// WARNING: This package provides encryption at rest and in crypto within
// the database. It does NOT protect against:
//   - Memory dumps or core dumps
//   - Side-channel attacks
//   - Compromised encryption keys
//   - Malicious database administrators with key access
//
// For additional security:
//   - Use memory encryption (Intel SGX, AMD SEV)
//   - Enable full disk encryption
//   - Implement proper access controls
//   - Monitor key access and usage
//   - Use hardware security modules (HSM) for key storage
//
// # Thread Safety
//
// All functions in this package are thread-safe and can be called concurrently
// from multiple goroutines.
//
// # Testing
//
// Run tests with coverage:
//
//	go test -v -race -coverprofile=coverage.out
//	go tool cover -html=coverage.out
//
// Run benchmarks:
//
//	go test -bench=. -benchmem
package encryption
