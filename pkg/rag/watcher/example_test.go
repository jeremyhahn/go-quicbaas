//go:build rag

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

package watcher_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/watcher"
)

// ExampleNew demonstrates creating a basic file watcher.
func ExampleNew() {
	config := watcher.Config{
		Paths:         []string{"./docs"},
		DebounceDelay: 100 * time.Millisecond,
		BatchInterval: 1 * time.Second,
	}

	w, err := watcher.New(config)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Stop()

	fmt.Println("Watcher created successfully")
	// Output: Watcher created successfully
}

// ExampleDefaultConfig demonstrates using the default configuration.
func ExampleDefaultConfig() {
	config := watcher.DefaultConfig()
	config.Paths = []string{"./docs", "./src"}

	fmt.Printf("Debounce Delay: %v\n", config.DebounceDelay)
	fmt.Printf("Batch Interval: %v\n", config.BatchInterval)
	fmt.Printf("Buffer Size: %d\n", config.BufferSize)

	// Output:
	// Debounce Delay: 100ms
	// Batch Interval: 1s
	// Buffer Size: 1000
}

// ExampleConfig_Validate demonstrates configuration validation.
func ExampleConfig_Validate() {
	config := watcher.Config{
		Paths:         []string{"/tmp"},
		DebounceDelay: 100 * time.Millisecond,
		BatchInterval: 1 * time.Second,
	}

	if err := config.Validate(); err != nil {
		fmt.Println("Invalid:", err)
	} else {
		fmt.Println("Valid configuration")
	}

	// Output: Valid configuration
}

// ExampleWatcher_AddPath demonstrates dynamically adding paths.
func ExampleWatcher_AddPath() {
	tmpDir1, _ := os.MkdirTemp("", "watcher-add1")
	tmpDir2, _ := os.MkdirTemp("", "watcher-add2")
	defer os.RemoveAll(tmpDir1)
	defer os.RemoveAll(tmpDir2)

	config := watcher.Config{
		Paths:         []string{tmpDir1},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
	}

	w, _ := watcher.New(config)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.Start(ctx)

	// Add second path
	if err := w.AddPath(tmpDir2); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Path added successfully")
	}

	// Output: Path added successfully
}

// ExampleEvent_IsModification demonstrates checking if an event is a modification.
func ExampleEvent_IsModification() {
	events := []watcher.Event{
		{Type: watcher.EventCreate, Path: "/file1.txt"},
		{Type: watcher.EventModify, Path: "/file2.txt"},
		{Type: watcher.EventDelete, Path: "/file3.txt"},
	}

	for _, event := range events {
		if event.IsModification() {
			fmt.Printf("%s is a modification\n", event.Type)
		} else {
			fmt.Printf("%s is not a modification\n", event.Type)
		}
	}

	// Output:
	// create is a modification
	// modify is a modification
	// delete is not a modification
}

// ExampleDefaultExclusions demonstrates using default exclusion patterns.
func ExampleDefaultExclusions() {
	exclusions := watcher.DefaultExclusions()

	fmt.Printf("Excludes .git: %v\n", contains(exclusions, ".git/"))
	fmt.Printf("Excludes node_modules: %v\n", contains(exclusions, "node_modules/"))
	fmt.Printf("Total count: %d\n", len(exclusions))

	// Output:
	// Excludes .git: true
	// Excludes node_modules: true
	// Total count: 15
}

// Helper function for example
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ExampleWatcher_integration demonstrates integration with a document processor.
func ExampleWatcher_integration() {
	tmpDir, _ := os.MkdirTemp("", "watcher-integration")
	defer os.RemoveAll(tmpDir)

	config := watcher.Config{
		Paths:         []string{tmpDir},
		Exclude:       []string{"*.tmp"},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		IgnoreHidden:  true,
	}

	w, _ := watcher.New(config)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.Start(ctx)

	// Simulate document processing - use atomic counter to avoid race
	var count atomic.Int32
	go func() {
		for event := range w.Events() {
			if event.IsModification() {
				count.Add(1)
			}
		}
	}()

	// Create files
	os.WriteFile(filepath.Join(tmpDir, "doc1.txt"), []byte("content"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "doc2.md"), []byte("markdown"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "skip.tmp"), []byte("temp"), 0644) // Excluded

	// Wait for events to be processed
	time.Sleep(300 * time.Millisecond)

	fmt.Printf("Processed %d files\n", count.Load())
	// Output: Processed 2 files
}
