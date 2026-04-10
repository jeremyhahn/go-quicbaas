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

package codec_test

import (
	"fmt"
	"log"

	"github.com/jeremyhahn/go-quicbaas/pkg/codec"
)

// User entity with JSON tags for API responses.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// Product entity with explicit YAML codec preference.
type Product struct {
	ID    int     `quicbaas:"codec=yaml" yaml:"id"`
	Name  string  `yaml:"name"`
	Price float64 `yaml:"price"`
}

// Example_basicUsage demonstrates basic dual-codec functionality.
func Example_basicUsage() {
	// Create codec manager with default config
	// Storage: msgpack (efficient binary format)
	// Wire: json (human-readable API format)
	mgr, err := codec.NewCodecManager(codec.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}

	user := &User{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
	}

	// Store to disk using msgpack (efficient)
	storageData, err := mgr.EncodeForStorage(user)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Storage size: %d bytes (msgpack)\n", len(storageData))

	// Retrieve from storage
	fromStorage := &User{}
	err = mgr.DecodeFromStorage(storageData, fromStorage)
	if err != nil {
		log.Fatal(err)
	}

	// Send over API using JSON (readable)
	wireData, codecName, err := mgr.EncodeForWire(fromStorage)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wire size: %d bytes (%s)\n", len(wireData), codecName)
	fmt.Printf("Wire data: %s\n", string(wireData))

	// Output:
	// Storage size: 44 bytes (msgpack)
	// Wire size: 55 bytes (json)
	// Wire data: {"id":1,"username":"alice","email":"alice@example.com"}
}

// Example_explicitCodec demonstrates explicit codec selection via tags.
func Example_explicitCodec() {
	mgr, err := codec.NewCodecManager(codec.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}

	product := &Product{
		ID:    100,
		Name:  "Laptop",
		Price: 999.99,
	}

	// Storage still uses msgpack (efficient)
	storageData, err := mgr.EncodeForStorage(product)
	if err != nil {
		log.Fatal(err)
	}

	// Wire uses YAML (as specified by quicbaas:"codec=yaml" tag)
	wireData, codecName, err := mgr.EncodeForWire(product)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wire codec: %s\n", codecName)
	fmt.Printf("Wire data:\n%s", string(wireData))

	// Verify storage and wire use different codecs
	fmt.Printf("\nStorage != Wire: %v\n", string(storageData) != string(wireData))

	// Output:
	// Wire codec: yaml
	// Wire data:
	// id: 100
	// name: Laptop
	// price: 999.99
	//
	// Storage != Wire: true
}

// Example_singleCodecMode demonstrates single-codec mode.
func Example_singleCodecMode() {
	// Single-codec mode: use same codec for storage and wire
	cfg := &codec.Config{
		StorageCodec:     "json",
		DefaultWireCodec: "json",
		AutoDetect:       false,
		DualCodecMode:    false, // Single codec for both
	}

	mgr, err := codec.NewCodecManager(cfg)
	if err != nil {
		log.Fatal(err)
	}

	user := &User{ID: 2, Username: "bob", Email: "bob@example.com"}

	// Storage uses JSON
	storageData, err := mgr.EncodeForStorage(user)
	if err != nil {
		log.Fatal(err)
	}

	// Wire also uses JSON (same as storage in single mode)
	wireData, codecName, err := mgr.EncodeForWire(user)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Storage codec: %s\n", mgr.GetStorageCodecName())
	fmt.Printf("Wire codec: %s\n", codecName)
	fmt.Printf("Same encoding: %v\n", string(storageData) == string(wireData))

	// Output:
	// Storage codec: json
	// Wire codec: json
	// Same encoding: true
}

// Example_customConfig demonstrates custom codec configuration.
func Example_customConfig() {
	// Custom config: JSON for storage, msgpack for wire
	cfg := &codec.Config{
		StorageCodec:     "json",    // JSON storage for debugging
		DefaultWireCodec: "msgpack", // Binary wire for efficiency
		AutoDetect:       true,      // Enable auto-detection
		DualCodecMode:    true,      // Dual-codec mode
	}

	mgr, err := codec.NewCodecManager(cfg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Storage codec: %s\n", mgr.GetStorageCodecName())
	fmt.Printf("Default wire codec: %s\n", mgr.GetDefaultWireCodecName())
	fmt.Printf("Auto-detect enabled: %v\n", mgr.IsAutoDetectEnabled())
	fmt.Printf("Dual-codec mode: %v\n", mgr.IsDualCodecMode())

	// List all available codecs
	codecs := mgr.ListCodecs()
	fmt.Printf("Available codecs: %d\n", len(codecs))

	// Output:
	// Storage codec: json
	// Default wire codec: msgpack
	// Auto-detect enabled: true
	// Dual-codec mode: true
	// Available codecs: 4
}

// Example_roundTrip demonstrates complete storage and wire round-trip.
func Example_roundTrip() {
	mgr, err := codec.NewCodecManager(codec.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}

	// Original entity
	original := &User{ID: 3, Username: "charlie", Email: "charlie@example.com"}
	fmt.Printf("Original: ID=%d, Username=%s\n", original.ID, original.Username)

	// Encode for storage (msgpack)
	storageData, err := mgr.EncodeForStorage(original)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Stored %d bytes\n", len(storageData))

	// Decode from storage
	fromStorage := &User{}
	err = mgr.DecodeFromStorage(storageData, fromStorage)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("From storage: ID=%d, Username=%s\n", fromStorage.ID, fromStorage.Username)

	// Encode for wire (json, auto-detected)
	wireData, codecName, err := mgr.EncodeForWire(fromStorage)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wire encoded with %s (%d bytes)\n", codecName, len(wireData))

	// Client decodes wire data
	clientEntity := &User{}
	err = mgr.DecodeFromWire(wireData, clientEntity, codecName)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Client received: ID=%d, Username=%s\n", clientEntity.ID, clientEntity.Username)

	// Verify data integrity
	fmt.Printf("Data intact: %v\n", original.ID == clientEntity.ID && original.Username == clientEntity.Username)

	// Output:
	// Original: ID=3, Username=charlie
	// Stored 48 bytes
	// From storage: ID=3, Username=charlie
	// Wire encoded with json (59 bytes)
	// Client received: ID=3, Username=charlie
	// Data intact: true
}
