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
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
)

// Example demonstrates a complete processing workflow.
func Example() {
	// Create a default processor
	proc, err := New(DefaultConfig())
	if err != nil {
		return
	}

	// Example Go file content
	goContent := `package main

import "fmt"

// Greet prints a greeting
func Greet(name string) {
	fmt.Printf("Hello, %s! This is a test function with enough content.\n", name)
}

// Calculate performs a calculation
func Calculate(a, b int) int {
	return a + b
}
`

	// Process Go content
	ctx := context.Background()
	_, _ = proc.ProcessContent(ctx, []byte(goContent), "example.go")
}

// WalkAndProcess walks a directory tree and processes all supported files.
func WalkAndProcess(dir string) error {
	// Create processor
	proc, err := New(DefaultConfig())
	if err != nil {
		return &ProcessorCauseError{Message: "failed to create processor", Cause: err}
	}

	// Collect files to process
	var files []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Filter supported extensions
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go", ".md", ".txt", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".h", ".rs":
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return &ProcessorCauseError{Message: "failed to walk directory", Cause: err}
	}

	if len(files) == 0 {
		return nil
	}

	// Process files
	ctx := context.Background()
	_, _ = proc.ProcessBatch(ctx, files)

	return nil
}

// ProcessWithCustomStrategy processes a file with custom strategy configuration.
func ProcessWithCustomStrategy() error {
	// Create config with strategy overrides
	config := DefaultConfig()
	config.StrategyOverrides = map[language.Language]string{
		language.LanguagePython: "text", // Use text chunking for Python
	}
	config.EnableDeduplication = true

	proc, err := New(config)
	if err != nil {
		return &ProcessorCauseError{Message: "failed to create processor", Cause: err}
	}

	// Process the file
	ctx := context.Background()
	_, err = proc.Process(ctx, "example.py")
	if err != nil {
		return &ProcessorCauseError{Message: "failed to process file", Cause: err}
	}

	return nil
}
