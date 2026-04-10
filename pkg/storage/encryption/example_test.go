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

package encryption_test

import (
	"fmt"
	"log"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/storage/encryption"
)

// User represents a user entity with encrypted sensitive fields.
type User struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email" encrypt:"aes256"`
	Password string `json:"password" encrypt:"aes256"`
	Profile  string `json:"profile"`
}

// GetID implements KeyValueEntity interface.
func (u *User) GetID() uint64 {
	return u.ID
}

// SetID implements KeyValueEntity interface.
func (u *User) SetID(id uint64) {
	u.ID = id
}

// Example_basicEncryption demonstrates basic field encryption.
func Example_basicEncryption() {
	// Generate an encryption key
	key, err := encryption.GenerateKey(encryption.AlgorithmAES256)
	if err != nil {
		log.Fatal(err)
	}

	// Create a user with sensitive data
	user := &User{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "super-secret-password",
		Profile:  "Software Engineer",
	}

	// Marshal with encryption
	encrypted, err := encryption.MarshalWithEncryption(user, key)
	if err != nil {
		log.Fatal(err)
	}

	// Check that data was encrypted
	fmt.Printf("Data encrypted: %v\n", len(encrypted) > 100)

	// Unmarshal with decryption
	decryptedUser := &User{}
	err = encryption.UnmarshalWithDecryption(encrypted, decryptedUser, key)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Username: %s\n", decryptedUser.Username)
	fmt.Printf("Email: %s\n", decryptedUser.Email)
	fmt.Printf("Profile: %s\n", decryptedUser.Profile)

	// Output:
	// Data encrypted: true
	// Username: alice
	// Email: alice@example.com
	// Profile: Software Engineer
}

// Example_checkEncryptedFields demonstrates checking if a struct has encrypted fields.
func Example_checkEncryptedFields() {
	type SafeData struct {
		ID   uint64 `json:"id"`
		Name string `json:"name"`
	}

	type SensitiveData struct {
		ID     uint64 `json:"id"`
		Secret string `json:"secret" encrypt:"aes256"`
	}

	// Check SafeData
	hasEncrypted := encryption.HasEncryptedFields(reflect.TypeOf(SafeData{}))
	fmt.Printf("SafeData has encrypted fields: %v\n", hasEncrypted)

	// Check SensitiveData
	hasEncrypted = encryption.HasEncryptedFields(reflect.TypeOf(SensitiveData{}))
	fmt.Printf("SensitiveData has encrypted fields: %v\n", hasEncrypted)

	// Output:
	// SafeData has encrypted fields: false
	// SensitiveData has encrypted fields: true
}

// Example_mixedAlgorithms demonstrates using both AES-128 and AES-256.
func Example_mixedAlgorithms() {
	type Document struct {
		ID      uint64 `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content" encrypt:"aes128"` // Less sensitive
		Notes   string `json:"notes" encrypt:"aes256"`   // More sensitive
	}

	// Use AES-256 key (can derive both AES-128 and AES-256)
	key, _ := encryption.GenerateKey(encryption.AlgorithmAES256)

	doc := &Document{
		ID:      1,
		Title:   "Project Plan",
		Content: "General project overview...",
		Notes:   "Confidential business strategy...",
	}

	// Marshal - automatically uses the correct algorithm for each field
	encrypted, _ := encryption.MarshalWithEncryption(doc, key)
	fmt.Printf("Encrypted successfully: %v\n", len(encrypted) > 0)

	// Unmarshal
	decrypted := &Document{}
	_ = encryption.UnmarshalWithDecryption(encrypted, decrypted, key)
	fmt.Printf("Title: %s\n", decrypted.Title)

	// Output:
	// Encrypted successfully: true
	// Title: Project Plan
}

// Example_keyGeneration demonstrates generating encryption keys.
func Example_keyGeneration() {
	// Generate AES-128 key
	key128, err := encryption.GenerateKey(encryption.AlgorithmAES128)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AES-128 key size: %d bytes\n", len(key128))

	// Generate AES-256 key
	key256, err := encryption.GenerateKey(encryption.AlgorithmAES256)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AES-256 key size: %d bytes\n", len(key256))

	// Output:
	// AES-128 key size: 16 bytes
	// AES-256 key size: 32 bytes
}

// Example_errorHandling demonstrates error handling.
func Example_errorHandling() {
	type InvalidData struct {
		ID       uint64 `json:"id"`
		BadField int    `json:"bad_field" encrypt:"aes256"` // Invalid: int can't be encrypted
	}

	// This will return an error because int fields can't be encrypted
	err := encryption.ValidateEncryptionTags(reflect.TypeOf(InvalidData{}))
	if err != nil {
		fmt.Println("Validation failed: field type not supported")
	}

	// Output:
	// Validation failed: field type not supported
}
