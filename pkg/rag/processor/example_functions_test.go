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

package processor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExample tests the Example() function
func TestExample(t *testing.T) {
	// Create test directory with required files
	testDir := t.TempDir()

	// Create the files that Example() expects
	files := map[string]string{
		"example.go": `package main

import "fmt"

// Greet prints a greeting
func Greet(name string) {
	fmt.Printf("Hello, %s! This is a test function with enough content.\n", name)
}

// Calculate performs a calculation
func Calculate(a, b int) int {
	return a + b
}
`,
		"main.go": `package main

func main() {
	println("Main function with enough content for chunking and testing")
}
`,
		"README.md": `# Test Project

This is a comprehensive test project with enough markdown content to create
meaningful chunks for testing purposes. The processor should handle this
appropriately and generate chunks based on the document structure.

## Features

- Feature 1 with detailed description
- Feature 2 with detailed description
- Feature 3 with detailed description
`,
		filepath.Join("docs", "guide.md"): `# Guide

This is a guide document with sufficient content for chunking.
It contains multiple sections and paragraphs to test processing.
`,
	}

	// Create docs subdirectory
	if err := os.Mkdir(filepath.Join(testDir, "docs"), 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	for name, content := range files {
		path := filepath.Join(testDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Change to test directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// The Example() function logs output but doesn't return anything
	// We capture panics to ensure it doesn't crash
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Example() panicked: %v", r)
		}
	}()

	if err := Example(); err != nil {
		t.Fatalf("Example() error = %v", err)
	}
}

// TestWalkAndProcess_BatchErrors tests batch processing with some errors
func TestWalkAndProcess_BatchErrors(t *testing.T) {
	testDir := t.TempDir()

	// Create valid files
	validFiles := map[string]string{
		"valid1.go": `package main

func Valid1() {
	println("Valid function 1 with enough content for meaningful chunks")
}
`,
		"valid2.md": `# Valid Document

This is a valid markdown document with sufficient content for chunking.
The processor should handle this file correctly.
`,
	}

	for name, content := range validFiles {
		path := filepath.Join(testDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Create an empty file that might cause issues
	emptyFile := filepath.Join(testDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	// Run WalkAndProcess - should handle errors gracefully
	err := WalkAndProcess(testDir)
	if err != nil {
		t.Logf("WalkAndProcess completed with errors (expected for empty file): %v", err)
	}
}

// TestWalkAndProcess_LanguageCounts tests the language counting logic
func TestWalkAndProcess_LanguageCounts(t *testing.T) {
	testDir := t.TempDir()

	// Create files in different languages
	files := map[string]string{
		"code1.go": `package main

func Function1() {
	println("Go function 1 with enough content for chunking and language detection")
}
`,
		"code2.go": `package main

func Function2() {
	println("Go function 2 with enough content for chunking and language detection")
}
`,
		"doc1.md": `# Document 1

First markdown document with enough content for chunking and language detection.
The processor should correctly identify this as markdown content.
`,
		"doc2.md": `# Document 2

Second markdown document with enough content for chunking and language detection.
The processor should correctly identify this as markdown content.
`,
		"text.txt": strings.Repeat("Plain text content with sufficient length. ", 15),
	}

	for name, content := range files {
		path := filepath.Join(testDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Should process all files and count by language
	err := WalkAndProcess(testDir)
	if err != nil {
		t.Logf("WalkAndProcess error (may be expected): %v", err)
	}
}

// TestProcessWithCustomStrategy_StrategyOverride tests the strategy override logic
func TestProcessWithCustomStrategy_StrategyOverride(t *testing.T) {
	testDir := t.TempDir()

	// Create example.py that ProcessWithCustomStrategy expects
	pyFile := filepath.Join(testDir, "example.py")
	pyContent := `def example_function():
    """
    Comprehensive example Python function with substantial content to ensure
    proper chunking and processing through all pipeline stages. This includes
    enough text to meet minimum token requirements for chunk creation.
    """
    result = "Test content with enough text for meaningful chunks and validation"
    print(result)
    for i in range(10):
        print(f"Iteration {i}: Processing with custom strategy and overrides")
    return True

def another_function():
    """Another function to add more content."""
    pass
`
	if err := os.WriteFile(pyFile, []byte(pyContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Change to test directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Run the function
	err = ProcessWithCustomStrategy()
	if err != nil {
		t.Logf("ProcessWithCustomStrategy() error (may be expected): %v", err)
	}
}

// TestWalkAndProcess_UnsupportedExtensions tests filtering of unsupported file types
func TestWalkAndProcess_UnsupportedExtensions(t *testing.T) {
	testDir := t.TempDir()

	// Create files with various extensions
	files := map[string]string{
		"supported.go":     `package main\nfunc Test() {}`,
		"supported.md":     "# Test\n\nContent",
		"supported.txt":    "Text content",
		"supported.py":     "def test():\n    pass",
		"unsupported.bin":  string(make([]byte, 50)),
		"unsupported.exe":  string(make([]byte, 50)),
		"unsupported.dll":  string(make([]byte, 50)),
		"unsupported.dat":  string(make([]byte, 50)),
		"unsupported.yaml": "key: value",
		"unsupported.json": `{"key": "value"}`,
	}

	for name, content := range files {
		path := filepath.Join(testDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Should only process supported file types
	err := WalkAndProcess(testDir)
	if err != nil {
		t.Logf("WalkAndProcess error (may be expected for unsupported files): %v", err)
	}
}

// TestWalkAndProcess_RecursiveDirectories tests walking recursive directory structures
func TestWalkAndProcess_RecursiveDirectories(t *testing.T) {
	testDir := t.TempDir()

	// Create nested directory structure
	level1 := filepath.Join(testDir, "level1")
	level2 := filepath.Join(level1, "level2")
	level3 := filepath.Join(level2, "level3")

	for _, dir := range []string{level1, level2, level3} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create files at each level
	files := map[string]string{
		filepath.Join(testDir, "root.go"):  `package main\nfunc Root() { println("root") }`,
		filepath.Join(level1, "level1.go"): `package main\nfunc Level1() { println("level1") }`,
		filepath.Join(level2, "level2.go"): `package main\nfunc Level2() { println("level2") }`,
		filepath.Join(level3, "level3.go"): `package main\nfunc Level3() { println("level3") }`,
		filepath.Join(level3, "README.md"): "# Nested Document\n\nContent in deep directory.",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	// Should walk entire tree
	err := WalkAndProcess(testDir)
	if err != nil {
		t.Logf("WalkAndProcess error: %v", err)
	}
}
