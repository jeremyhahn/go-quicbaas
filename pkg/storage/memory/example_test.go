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

package memory_test

import (
	"fmt"
	"io"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

// silentLogger creates a logger that doesn't output anything for examples.
func silentLogger() *logger.Logger {
	log, _ := logger.New(logger.LogConfig{
		Level:  "fatal", // Only fatal messages (none expected in examples)
		Format: "json",
		Output: "stdout",
	})
	return log
}

func ExampleMemoryStorage() {
	// Create in-memory storage with a silent logger
	storage := memory.New(silentLogger())
	defer storage.Close()

	// Set some key-value pairs
	storage.Set([]byte("user:1"), []byte("Alice"))
	storage.Set([]byte("user:2"), []byte("Bob"))
	storage.Set([]byte("user:3"), []byte("Charlie"))

	// Get a value
	value, _ := storage.Get([]byte("user:2"))
	fmt.Printf("user:2 = %s\n", value)

	// Scan all users
	results, _ := storage.Scan([]byte("user:"), nil)
	fmt.Printf("Total users: %d\n", len(results))

	// Output:
	// user:2 = Bob
	// Total users: 3
}

func ExampleMemoryStorage_Snapshot() {
	// Create storage and add data
	storage := memory.New(silentLogger())
	defer storage.Close()

	storage.Set([]byte("config:1"), []byte("value1"))
	storage.Set([]byte("config:2"), []byte("value2"))

	// Create a snapshot
	snapshot := storage.Snapshot()
	fmt.Printf("Snapshot contains %d items\n", len(snapshot))

	// Modify original storage
	storage.Set([]byte("config:3"), []byte("value3"))

	// Create second storage and restore from snapshot
	storage2 := memory.New(silentLogger())
	defer storage2.Close()
	storage2.Restore(snapshot)

	fmt.Printf("Original has %d items\n", storage.Len())
	fmt.Printf("Restored has %d items\n", storage2.Len())

	// Output:
	// Snapshot contains 2 items
	// Original has 3 items
	// Restored has 2 items
}

func ExampleMemoryStorage_Scan() {
	storage := memory.New(silentLogger())
	defer storage.Close()

	// Insert ordered data
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("item:%02d", i))
		storage.Set(key, []byte(fmt.Sprintf("value-%02d", i)))
	}

	// Scan a range [item:03, item:07)
	results, _ := storage.Scan([]byte("item:03"), []byte("item:07"))

	fmt.Printf("Range scan returned %d items:\n", len(results))
	for _, pair := range results {
		fmt.Printf("  %s = %s\n", pair[0], pair[1])
	}

	// Output:
	// Range scan returned 4 items:
	//   item:03 = value-03
	//   item:04 = value-04
	//   item:05 = value-05
	//   item:06 = value-06
}

// Ensure io is imported.
var _ = io.Discard
